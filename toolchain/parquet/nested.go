package parquet

import "fmt"

// Nested LIST<primitive> support. Only a single level of repetition is handled
// (maxRep == 1), which is what the reasoning datasets use for `generations`
// (LIST<STRING>) and the `correctness_*` columns (LIST<BOOL>). Struct elements
// are not assembled.

func (reader *Reader) findLeafPath(path []string) (leafColumn, bool) {
	for _, leaf := range reader.meta.leaves() {
		if len(leaf.path) != len(path)+1 {
			continue
		}
		match := true
		for index, name := range path {
			if leaf.path[index+1] != name {
				match = false
				break
			}
		}
		if match {
			return leaf, true
		}
	}
	return leafColumn{}, false
}

func (reader *Reader) findChunkPath(rowGroupIndex int, path []string) (columnChunk, bool) {
	for _, chunk := range reader.meta.rowGroups[rowGroupIndex].columns {
		if chunk.meta == nil || len(chunk.meta.pathInSchema) != len(path) {
			continue
		}
		match := true
		for index, name := range path {
			if chunk.meta.pathInSchema[index] != name {
				match = false
				break
			}
		}
		if match {
			return chunk, true
		}
	}
	return columnChunk{}, false
}

// splitNestedLevels strips the repetition and definition levels from a nested
// data page, returning the remaining value bytes and the decoded levels.
func splitNestedLevels(page rawPage, maxRep, maxDef int) ([]byte, []int32, []int32, error) {
	data := page.data
	var reps, defs []int32
	if maxRep > 0 {
		decoded, consumed, err := decodeLevels(data, maxRep, int(page.numValues))
		if err != nil {
			return nil, nil, nil, err
		}
		reps = decoded
		data = data[consumed:]
	}
	if maxDef > 0 {
		decoded, consumed, err := decodeLevels(data, maxDef, int(page.numValues))
		if err != nil {
			return nil, nil, nil, err
		}
		defs = decoded
		data = data[consumed:]
	}
	return data, reps, defs, nil
}

// countDefined returns how many values of a page are present (definition level
// equals maxDef). When maxDef is zero every value is defined.
func countDefined(defs []int32, maxDef, numValues int) int {
	if maxDef == 0 {
		return numValues
	}
	count := 0
	for _, level := range defs {
		if int(level) == maxDef {
			count++
		}
	}
	return count
}

// assembleLists groups values into one list per row using the repetition
// levels: a repetition level of zero starts a new row.
func assembleLists[T any](reps, defs []int32, values []T, maxDef int) ([][]T, error) {
	rows := make([][]T, 0)
	var current []T
	started := false
	next := 0
	for index := range reps {
		if reps[index] == 0 {
			if started {
				rows = append(rows, current)
			}
			current = nil
			started = true
		}
		defined := maxDef == 0 || (index < len(defs) && int(defs[index]) == maxDef)
		if defined {
			if next >= len(values) {
				return nil, errBadEncoding
			}
			current = append(current, values[next])
			next++
		}
	}
	if started {
		rows = append(rows, current)
	}
	return rows, nil
}

// ReadListStrings reads a single-level LIST<STRING> column of one row group.
func (reader *Reader) ReadListStrings(rowGroupIndex int, path []string) ([][]string, error) {
	leaf, ok := reader.findLeafPath(path)
	if !ok {
		return nil, fmt.Errorf("parquet: column %v not found", path)
	}
	if leaf.typ != typeByteArray {
		return nil, fmt.Errorf("parquet: column %v is not a BYTE_ARRAY", path)
	}
	chunk, ok := reader.findChunkPath(rowGroupIndex, path)
	if !ok {
		return nil, fmt.Errorf("parquet: column %v not found in row group %d", path, rowGroupIndex)
	}
	dictionary, pages, err := reader.readPages(chunk)
	if err != nil {
		return nil, err
	}

	var reps, defs []int32
	var values []string
	for _, page := range pages {
		data, pageReps, pageDefs, err := splitNestedLevels(page, leaf.maxRep, leaf.maxDef)
		if err != nil {
			return nil, err
		}
		defined := countDefined(pageDefs, leaf.maxDef, int(page.numValues))
		decoded, err := decodeStringValues(page.encoding, data, dictionary, defined)
		if err != nil {
			return nil, err
		}
		reps = append(reps, pageReps...)
		defs = append(defs, pageDefs...)
		values = append(values, decoded...)
	}
	return assembleLists(reps, defs, values, leaf.maxDef)
}

// ReadListBools reads a single-level LIST<BOOL> column of one row group.
func (reader *Reader) ReadListBools(rowGroupIndex int, path []string) ([][]bool, error) {
	leaf, ok := reader.findLeafPath(path)
	if !ok {
		return nil, fmt.Errorf("parquet: column %v not found", path)
	}
	if leaf.typ != typeBoolean {
		return nil, fmt.Errorf("parquet: column %v is not a BOOLEAN", path)
	}
	chunk, ok := reader.findChunkPath(rowGroupIndex, path)
	if !ok {
		return nil, fmt.Errorf("parquet: column %v not found in row group %d", path, rowGroupIndex)
	}
	_, pages, err := reader.readPages(chunk)
	if err != nil {
		return nil, err
	}

	var reps, defs []int32
	var values []bool
	for _, page := range pages {
		data, pageReps, pageDefs, err := splitNestedLevels(page, leaf.maxRep, leaf.maxDef)
		if err != nil {
			return nil, err
		}
		defined := countDefined(pageDefs, leaf.maxDef, int(page.numValues))
		var decoded []bool
		switch page.encoding {
		case encPlain:
			decoded, err = decodePlainBooleans(data, defined)
		case encRLE:
			var ints []int32
			ints, err = decodeHybrid(data, 1, defined)
			if err == nil {
				decoded = make([]bool, len(ints))
				for index, value := range ints {
					decoded[index] = value != 0
				}
			}
		default:
			err = fmt.Errorf("parquet: unsupported boolean encoding %d", page.encoding)
		}
		if err != nil {
			return nil, err
		}
		reps = append(reps, pageReps...)
		defs = append(defs, pageDefs...)
		values = append(values, decoded...)
	}
	return assembleLists(reps, defs, values, leaf.maxDef)
}

func decodeStringValues(encoding int32, data []byte, dictionary [][]byte, count int) ([]string, error) {
	switch encoding {
	case encPlain:
		raw, err := decodePlainByteArray(data, count)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(raw))
		for index, value := range raw {
			out[index] = string(value)
		}
		return out, nil
	case encRLEDictionary, encPlainDict:
		if len(data) < 1 {
			return nil, errBadEncoding
		}
		bitWidth := int(data[0])
		indices, err := decodeHybrid(data[1:], bitWidth, count)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(indices))
		for index, dictIndex := range indices {
			if dictIndex < 0 || int(dictIndex) >= len(dictionary) {
				return nil, errBadEncoding
			}
			out[index] = string(dictionary[dictIndex])
		}
		return out, nil
	default:
		return nil, fmt.Errorf("parquet: unsupported string encoding %d", encoding)
	}
}
