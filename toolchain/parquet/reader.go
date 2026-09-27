package parquet

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// Reader reads the top-level flat columns of a Parquet file.
type Reader struct {
	file *os.File
	meta fileMetaData
}

// Open opens a Parquet file and parses its footer metadata.
func Open(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	size := info.Size()
	if size < 12 {
		file.Close()
		return nil, errBadParquet
	}
	tail := make([]byte, 8)
	if _, err := file.ReadAt(tail, size-8); err != nil {
		file.Close()
		return nil, err
	}
	if string(tail[4:]) != "PAR1" {
		file.Close()
		return nil, errBadParquet
	}
	metaLen := int64(binary.LittleEndian.Uint32(tail[:4]))
	if metaLen <= 0 || metaLen > size-8 {
		file.Close()
		return nil, errBadParquet
	}
	metaBytes := make([]byte, metaLen)
	if _, err := file.ReadAt(metaBytes, size-8-metaLen); err != nil {
		file.Close()
		return nil, err
	}
	meta, err := parseFileMetaData(metaBytes)
	if err != nil {
		file.Close()
		return nil, err
	}
	return &Reader{file: file, meta: meta}, nil
}

// Close closes the underlying file.
func (reader *Reader) Close() error { return reader.file.Close() }

// NumRowGroups returns the number of row groups.
func (reader *Reader) NumRowGroups() int { return len(reader.meta.rowGroups) }

// NumRows returns the total number of rows in the file.
func (reader *Reader) NumRows() int64 { return reader.meta.numRows }

// RowGroupNumRows returns the number of rows in a row group.
func (reader *Reader) RowGroupNumRows(index int) int64 {
	return reader.meta.rowGroups[index].numRows
}

// ColumnNames returns the top-level leaf column names, in schema order.
func (reader *Reader) ColumnNames() []string {
	var names []string
	for _, leaf := range reader.meta.leaves() {
		if leaf.maxRep == 0 && len(leaf.path) == 2 {
			names = append(names, leaf.name)
		}
	}
	return names
}

// HasColumn reports whether a top-level leaf column exists.
func (reader *Reader) HasColumn(name string) bool {
	_, ok := reader.meta.findLeaf(name)
	return ok
}

// HasColumnPath reports whether a leaf column exists at the given schema path
// (for example {"generations", "list", "element"}).
func (reader *Reader) HasColumnPath(path []string) bool {
	_, ok := reader.findLeafPath(path)
	return ok
}

func (reader *Reader) findChunk(rowGroupIndex int, name string) (columnChunk, bool) {
	for _, chunk := range reader.meta.rowGroups[rowGroupIndex].columns {
		if chunk.meta != nil && len(chunk.meta.pathInSchema) > 0 &&
			chunk.meta.pathInSchema[len(chunk.meta.pathInSchema)-1] == name {
			return chunk, true
		}
	}
	return columnChunk{}, false
}

// ReadColumnStrings reads a top-level BYTE_ARRAY column of one row group as
// strings. Null entries become empty strings.
func (reader *Reader) ReadColumnStrings(rowGroupIndex int, name string) ([]string, error) {
	if rowGroupIndex < 0 || rowGroupIndex >= len(reader.meta.rowGroups) {
		return nil, fmt.Errorf("parquet: row group %d out of range", rowGroupIndex)
	}
	leaf, ok := reader.meta.findLeaf(name)
	if !ok {
		return nil, fmt.Errorf("parquet: column %q not found", name)
	}
	if leaf.maxRep > 0 {
		return nil, fmt.Errorf("parquet: column %q is nested, which is not supported", name)
	}
	if leaf.typ != typeByteArray {
		return nil, fmt.Errorf("parquet: column %q is not a BYTE_ARRAY (type %d)", name, leaf.typ)
	}
	chunk, ok := reader.findChunk(rowGroupIndex, name)
	if !ok {
		return nil, fmt.Errorf("parquet: column %q not found in row group %d", name, rowGroupIndex)
	}
	dictionary, pages, err := reader.readPages(chunk)
	if err != nil {
		return nil, err
	}

	var out []string
	for _, page := range pages {
		values, levels, err := splitLevels(page, leaf.maxDef)
		if err != nil {
			return nil, err
		}
		defined := int(page.numValues)
		if levels != nil {
			defined = 0
			for _, level := range levels {
				if int(level) == leaf.maxDef {
					defined++
				}
			}
		}

		var decoded []string
		switch page.encoding {
		case encPlain:
			raw, err := decodePlainByteArray(values, defined)
			if err != nil {
				return nil, err
			}
			decoded = make([]string, len(raw))
			for index, value := range raw {
				decoded[index] = string(value)
			}
		case encRLEDictionary, encPlainDict:
			if len(values) < 1 {
				return nil, errBadEncoding
			}
			bitWidth := int(values[0])
			indices, err := decodeHybrid(values[1:], bitWidth, defined)
			if err != nil {
				return nil, err
			}
			decoded = make([]string, len(indices))
			for index, dictIndex := range indices {
				if dictIndex < 0 || int(dictIndex) >= len(dictionary) {
					return nil, errBadEncoding
				}
				decoded[index] = string(dictionary[dictIndex])
			}
		default:
			return nil, fmt.Errorf("parquet: unsupported encoding %d", page.encoding)
		}

		if levels == nil {
			out = append(out, decoded...)
			continue
		}
		// Interleave nulls (level != maxDef) as empty strings.
		next := 0
		for _, level := range levels {
			if int(level) == leaf.maxDef {
				if next >= len(decoded) {
					return nil, errBadEncoding
				}
				out = append(out, decoded[next])
				next++
			} else {
				out = append(out, "")
			}
		}
	}
	return out, nil
}

// ReadColumnInt64 reads a top-level INT32 or INT64 column of one row group as
// int64. Null entries become zero.
func (reader *Reader) ReadColumnInt64(rowGroupIndex int, name string) ([]int64, error) {
	if rowGroupIndex < 0 || rowGroupIndex >= len(reader.meta.rowGroups) {
		return nil, fmt.Errorf("parquet: row group %d out of range", rowGroupIndex)
	}
	leaf, ok := reader.meta.findLeaf(name)
	if !ok {
		return nil, fmt.Errorf("parquet: column %q not found", name)
	}
	if leaf.maxRep > 0 {
		return nil, fmt.Errorf("parquet: column %q is nested, which is not supported", name)
	}
	if leaf.typ != typeInt32 && leaf.typ != typeInt64 {
		return nil, fmt.Errorf("parquet: column %q is not an integer (type %d)", name, leaf.typ)
	}
	chunk, ok := reader.findChunk(rowGroupIndex, name)
	if !ok {
		return nil, fmt.Errorf("parquet: column %q not found in row group %d", name, rowGroupIndex)
	}
	dictionary, pages, err := reader.readPages(chunk)
	if err != nil {
		return nil, err
	}

	var out []int64
	for _, page := range pages {
		values, levels, err := splitLevels(page, leaf.maxDef)
		if err != nil {
			return nil, err
		}
		defined := countDefined(levels, leaf.maxDef, int(page.numValues))
		var decoded []int64
		switch page.encoding {
		case encPlain:
			if leaf.typ == typeInt32 {
				raw, err := decodePlainInt32(values, defined)
				if err != nil {
					return nil, err
				}
				decoded = make([]int64, len(raw))
				for index, value := range raw {
					decoded[index] = int64(value)
				}
			} else {
				decoded, err = decodePlainInt64(values, defined)
				if err != nil {
					return nil, err
				}
			}
		case encRLEDictionary, encPlainDict:
			if len(values) < 1 {
				return nil, errBadEncoding
			}
			bitWidth := int(values[0])
			indices, err := decodeHybrid(values[1:], bitWidth, defined)
			if err != nil {
				return nil, err
			}
			decoded = make([]int64, len(indices))
			for index, dictIndex := range indices {
				if dictIndex < 0 || int(dictIndex) >= len(dictionary) {
					return nil, errBadEncoding
				}
				decoded[index] = decodeIntBytes(dictionary[dictIndex])
			}
		default:
			return nil, fmt.Errorf("parquet: unsupported encoding %d", page.encoding)
		}

		if levels == nil {
			out = append(out, decoded...)
			continue
		}
		next := 0
		for _, level := range levels {
			if int(level) == leaf.maxDef {
				if next >= len(decoded) {
					return nil, errBadEncoding
				}
				out = append(out, decoded[next])
				next++
			} else {
				out = append(out, 0)
			}
		}
	}
	return out, nil
}

// splitLevels strips the definition levels from a data page, returning the
// remaining value bytes and the decoded levels (nil when the column is
// REQUIRED).
func splitLevels(page rawPage, maxDef int) ([]byte, []int32, error) {
	if maxDef == 0 {
		return page.data, nil, nil
	}
	levels, consumed, err := decodeDefLevels(page.data, maxDef, int(page.numValues))
	if err != nil {
		return nil, nil, err
	}
	return page.data[consumed:], levels, nil
}

type rawPage struct {
	numValues int32
	encoding  int32
	data      []byte
}

func (reader *Reader) readPages(chunk columnChunk) ([][]byte, []rawPage, error) {
	if chunk.meta == nil {
		return nil, nil, errBadParquet
	}
	var dictionary [][]byte
	var pages []rawPage

	if chunk.meta.dictionaryPageOffset > 0 {
		header, data, err := reader.readPageAt(chunk.meta.dictionaryPageOffset, chunk.meta.codec)
		if err != nil {
			return nil, nil, err
		}
		if header.dictionary == nil {
			return nil, nil, errBadParquet
		}
		dictionary, err = decodeDictionary(data, int(header.dictionary.numValues), chunk.meta.typ)
		if err != nil {
			return nil, nil, err
		}
	}

	offset := chunk.meta.dataPageOffset
	if offset == 0 {
		offset = chunk.fileOffset
	}
	var total int64
	for total < chunk.meta.numValues {
		header, data, err := reader.readPageAt(offset, chunk.meta.codec)
		if err != nil {
			return nil, nil, err
		}
		if header.typ == pageDictionary {
			offset += header.headerLen + int64(header.compressedSize)
			continue
		}
		if header.data == nil {
			return nil, nil, errBadParquet
		}
		pages = append(pages, rawPage{
			numValues: header.data.numValues,
			encoding:  header.data.encoding,
			data:      data,
		})
		total += int64(header.data.numValues)
		offset += header.headerLen + int64(header.compressedSize)
	}
	return dictionary, pages, nil
}

type rawPageHeader struct {
	typ              int32
	uncompressedSize int32
	compressedSize   int32
	headerLen        int64
	data             *dataPageHeader
	dictionary       *dictionaryPageHeader
}

func (reader *Reader) readPageAt(offset int64, codec int32) (rawPageHeader, []byte, error) {
	const headerWindow = 1 << 16
	window := make([]byte, headerWindow)
	bytesRead, err := reader.file.ReadAt(window, offset)
	if err != nil && err != io.EOF {
		return rawPageHeader{}, nil, err
	}
	window = window[:bytesRead]
	header, consumed, err := parsePageHeader(window)
	if err != nil {
		return rawPageHeader{}, nil, err
	}
	if header.compressedSize < 0 {
		return rawPageHeader{}, nil, errBadParquet
	}
	header.headerLen = int64(consumed)
	compressed := make([]byte, header.compressedSize)
	if _, err := reader.file.ReadAt(compressed, offset+int64(consumed)); err != nil {
		return rawPageHeader{}, nil, err
	}
	data, err := decompress(compressed, codec)
	if err != nil {
		return rawPageHeader{}, nil, err
	}
	return header, data, nil
}

func parsePageHeader(data []byte) (rawPageHeader, int, error) {
	reader := newCompactReader(data)
	var header rawPageHeader
	for {
		id, typ, ok, err := reader.readFieldHeader()
		if err != nil {
			return header, 0, err
		}
		if !ok {
			return header, reader.pos, nil
		}
		switch id {
		case 1:
			value, err := reader.zigzag()
			if err != nil {
				return header, 0, err
			}
			header.typ = int32(value)
		case 2:
			value, err := reader.zigzag()
			if err != nil {
				return header, 0, err
			}
			header.uncompressedSize = int32(value)
		case 3:
			value, err := reader.zigzag()
			if err != nil {
				return header, 0, err
			}
			header.compressedSize = int32(value)
		case 5:
			header.data, err = parseDataPageHeader(reader)
		case 7:
			header.dictionary, err = parseDictionaryPageHeader(reader)
		default:
			err = reader.skipValue(typ)
		}
		if err != nil {
			return header, 0, err
		}
	}
}

func parseDataPageHeader(reader *compactReader) (*dataPageHeader, error) {
	previousFieldID := reader.enterStruct()
	defer reader.exitStruct(previousFieldID)
	header := &dataPageHeader{}
	for {
		id, typ, ok, err := reader.readFieldHeader()
		if err != nil {
			return header, err
		}
		if !ok {
			return header, nil
		}
		switch id {
		case 1:
			value, err := reader.zigzag()
			if err != nil {
				return header, err
			}
			header.numValues = int32(value)
		case 2:
			value, err := reader.zigzag()
			if err != nil {
				return header, err
			}
			header.encoding = int32(value)
		case 3:
			value, err := reader.zigzag()
			if err != nil {
				return header, err
			}
			header.defLevelEncoding = int32(value)
		case 4:
			value, err := reader.zigzag()
			if err != nil {
				return header, err
			}
			header.repLevelEncoding = int32(value)
		default:
			err = reader.skipValue(typ)
		}
		if err != nil {
			return header, err
		}
	}
}

func parseDictionaryPageHeader(reader *compactReader) (*dictionaryPageHeader, error) {
	previousFieldID := reader.enterStruct()
	defer reader.exitStruct(previousFieldID)
	header := &dictionaryPageHeader{}
	for {
		id, typ, ok, err := reader.readFieldHeader()
		if err != nil {
			return header, err
		}
		if !ok {
			return header, nil
		}
		switch id {
		case 1:
			value, err := reader.zigzag()
			if err != nil {
				return header, err
			}
			header.numValues = int32(value)
		case 2:
			value, err := reader.zigzag()
			if err != nil {
				return header, err
			}
			header.encoding = int32(value)
		default:
			err = reader.skipValue(typ)
		}
		if err != nil {
			return header, err
		}
	}
}
