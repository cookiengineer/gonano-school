package parquet

import (
	"encoding/binary"
	"errors"
)

var errBadEncoding = errors.New("parquet: corrupt encoding")

// decodePlainByteArray decodes count PLAIN-encoded BYTE_ARRAY values. Parquet
// PLAIN stores each value as a 4-byte little-endian length followed by that many
// bytes (this differs from the Thrift varint length used elsewhere in the
// format).
func decodePlainByteArray(data []byte, count int) ([][]byte, error) {
	out := make([][]byte, count)
	for index := 0; index < count; index++ {
		if len(data) < 4 {
			return nil, errBadEncoding
		}
		size := int(binary.LittleEndian.Uint32(data))
		data = data[4:]
		if size < 0 || size > len(data) {
			return nil, errBadEncoding
		}
		out[index] = data[:size]
		data = data[size:]
	}
	return out, nil
}

// decodePlainInt32 decodes count PLAIN-encoded INT32 values (4-byte LE each).
func decodePlainInt32(data []byte, count int) ([]int32, error) {
	if len(data) < count*4 {
		return nil, errBadEncoding
	}
	out := make([]int32, count)
	for index := 0; index < count; index++ {
		out[index] = int32(binary.LittleEndian.Uint32(data[index*4:]))
	}
	return out, nil
}

// decodePlainInt64 decodes count PLAIN-encoded INT64 values (8-byte LE each).
func decodePlainInt64(data []byte, count int) ([]int64, error) {
	if len(data) < count*8 {
		return nil, errBadEncoding
	}
	out := make([]int64, count)
	for index := 0; index < count; index++ {
		out[index] = int64(binary.LittleEndian.Uint64(data[index*8:]))
	}
	return out, nil
}

// decodeDictionary decodes a PLAIN dictionary page for a primitive type. The
// returned entries are raw bytes: BYTE_ARRAY values as-is, integers as their
// fixed-width little-endian encoding, so readers can reinterpret them.
func decodeDictionary(data []byte, count int, typ int32) ([][]byte, error) {
	switch typ {
	case typeByteArray:
		return decodePlainByteArray(data, count)
	case typeInt32:
		raw, err := decodePlainInt32(data, count)
		if err != nil {
			return nil, err
		}
		out := make([][]byte, len(raw))
		for index, value := range raw {
			entry := make([]byte, 4)
			binary.LittleEndian.PutUint32(entry, uint32(value))
			out[index] = entry
		}
		return out, nil
	case typeInt64:
		raw, err := decodePlainInt64(data, count)
		if err != nil {
			return nil, err
		}
		out := make([][]byte, len(raw))
		for index, value := range raw {
			entry := make([]byte, 8)
			binary.LittleEndian.PutUint64(entry, uint64(value))
			out[index] = entry
		}
		return out, nil
	default:
		return nil, errBadEncoding
	}
}

// decodeIntBytes decodes a fixed-width integer dictionary entry.
func decodeIntBytes(encoded []byte) int64 {
	switch len(encoded) {
	case 4:
		return int64(int32(binary.LittleEndian.Uint32(encoded)))
	case 8:
		return int64(binary.LittleEndian.Uint64(encoded))
	default:
		return 0
	}
}

// levelBitWidth returns the bit width required to encode levels up to maxLevel.
func levelBitWidth(maxLevel int) int {
	width := 0
	for (1 << width) <= maxLevel {
		width++
	}
	if width == 0 {
		width = 1
	}
	return width
}

// decodeHybrid decodes count values from the RLE/bit-packed hybrid encoding
// *without* a leading length prefix (the form used for dictionary indices).
// It returns exactly count values.
func decodeHybrid(data []byte, bitWidth, count int) ([]int32, error) {
	if bitWidth <= 0 {
		return nil, errBadEncoding
	}
	byteWidth := (bitWidth + 7) / 8
	out := make([]int32, 0, count)
	for len(out) < count {
		header, consumed, err := uvarint(data)
		if err != nil {
			return nil, err
		}
		data = data[consumed:]
		if header&1 == 1 {
			// Bit-packed run: header>>1 groups of 8 values.
			numGroups := int(header >> 1)
			if numGroups <= 0 || len(data) < numGroups*bitWidth {
				return nil, errBadEncoding
			}
			for group := 0; group < numGroups; group++ {
				groupBytes := data[group*bitWidth : (group+1)*bitWidth]
				for valueIndex := 0; valueIndex < 8 && len(out) < count; valueIndex++ {
					out = append(out, int32(readBitsLE(groupBytes, valueIndex*bitWidth, bitWidth)))
				}
			}
			data = data[numGroups*bitWidth:]
		} else {
			// RLE run: header>>1 repetitions of one value.
			numValues := int(header >> 1)
			if numValues <= 0 || len(data) < byteWidth {
				return nil, errBadEncoding
			}
			var value uint32
			for byteIndex := 0; byteIndex < byteWidth; byteIndex++ {
				value |= uint32(data[byteIndex]) << (8 * byteIndex)
			}
			data = data[byteWidth:]
			for index := 0; index < numValues && len(out) < count; index++ {
				out = append(out, int32(value))
			}
		}
	}
	return out, nil
}

// decodeLevels decodes a repetition or definition level run. Levels are always
// RLE encoded with a 4-byte little-endian length prefix, and the caller has
// already positioned data at that prefix. It returns the decoded levels and the
// number of bytes consumed.
func decodeLevels(data []byte, maxLevel, count int) ([]int32, int, error) {
	if maxLevel == 0 {
		return nil, 0, nil
	}
	if len(data) < 4 {
		return nil, 0, errBadParquet
	}
	length := int(binary.LittleEndian.Uint32(data))
	if length < 0 || 4+length > len(data) {
		return nil, 0, errBadParquet
	}
	levels, err := decodeHybrid(data[4:4+length], levelBitWidth(maxLevel), count)
	if err != nil {
		return nil, 0, err
	}
	return levels, 4 + length, nil
}

// decodeDefLevels decodes definition levels (maxRep == 0 columns).
func decodeDefLevels(data []byte, maxDef, count int) ([]int32, int, error) {
	return decodeLevels(data, maxDef, count)
}

// decodePlainBooleans decodes count PLAIN-encoded booleans. Parquet packs them
// one bit per value, least-significant bit first.
func decodePlainBooleans(data []byte, count int) ([]bool, error) {
	if len(data)*8 < count {
		return nil, errBadEncoding
	}
	out := make([]bool, count)
	for index := 0; index < count; index++ {
		out[index] = (data[index/8]>>(index%8))&1 == 1
	}
	return out, nil
}

// readBitsLE reads bitWidth bits starting at bitOffset in little-endian bit
// order from buffer.
func readBitsLE(buffer []byte, bitOffset, bitWidth int) uint64 {
	var value uint64
	for bitIndex := 0; bitIndex < bitWidth; bitIndex++ {
		bit := bitOffset + bitIndex
		if (buffer[bit/8]>>(bit%8))&1 == 1 {
			value |= 1 << bitIndex
		}
	}
	return value
}

var errCorrupt = errors.New("parquet: corrupt snappy block")

// snappyDecode decompresses a raw (unframed) Snappy block as used inside
// Parquet data pages, following Google's format_description.txt. The
// high offset bits of a 1-byte-offset copy are shifted in int space to avoid
// the uint8 truncation that silently corrupts offsets above 255.
func snappyDecode(src []byte) ([]byte, error) {
	length, consumed, err := uvarint(src)
	if err != nil {
		return nil, err
	}
	src = src[consumed:]
	dst := make([]byte, 0, length)
	pos := 0
	for len(src) > 0 {
		tag := src[0]
		src = src[1:]
		switch tag & 0x03 {
		case 0: // literal
			var litLen int
			switch tag >> 2 {
			case 60:
				if len(src) < 1 {
					return nil, errCorrupt
				}
				litLen = int(src[0]) + 1
				src = src[1:]
			case 61:
				if len(src) < 2 {
					return nil, errCorrupt
				}
				litLen = int(binary.LittleEndian.Uint16(src)) + 1
				src = src[2:]
			case 62:
				if len(src) < 3 {
					return nil, errCorrupt
				}
				litLen = int(loadUint24(src)) + 1
				src = src[3:]
			case 63:
				if len(src) < 4 {
					return nil, errCorrupt
				}
				litLen = int(binary.LittleEndian.Uint32(src)) + 1
				src = src[4:]
			default:
				litLen = int(tag>>2) + 1
			}
			if litLen < 0 || len(src) < litLen {
				return nil, errCorrupt
			}
			dst = append(dst, src[:litLen]...)
			src = src[litLen:]
			pos += litLen
		case 1: // copy with 1-byte offset
			if len(src) < 1 {
				return nil, errCorrupt
			}
			copyLength := 4 + int((tag>>2)&0x07)
			offset := int(tag&0xE0)<<3 | int(src[0])
			src = src[1:]
			if offset <= 0 || offset > pos {
				return nil, errCorrupt
			}
			for index := 0; index < copyLength; index++ {
				dst = append(dst, dst[pos-offset+index])
			}
			pos += copyLength
		case 2: // copy with 2-byte offset
			if len(src) < 2 {
				return nil, errCorrupt
			}
			copyLength := 1 + int(tag>>2)
			offset := int(binary.LittleEndian.Uint16(src))
			src = src[2:]
			if offset <= 0 || offset > pos {
				return nil, errCorrupt
			}
			for index := 0; index < copyLength; index++ {
				dst = append(dst, dst[pos-offset+index])
			}
			pos += copyLength
		case 3: // copy with 4-byte offset
			if len(src) < 4 {
				return nil, errCorrupt
			}
			copyLength := 1 + int(tag>>2)
			offset := int(binary.LittleEndian.Uint32(src))
			src = src[4:]
			if offset <= 0 || offset > pos {
				return nil, errCorrupt
			}
			for index := 0; index < copyLength; index++ {
				dst = append(dst, dst[pos-offset+index])
			}
			pos += copyLength
		}
	}
	if pos != int(length) {
		return nil, errCorrupt
	}
	return dst, nil
}

func uvarint(src []byte) (uint64, int, error) {
	var value uint64
	var shift uint
	for index := 0; index < len(src); index++ {
		currentByte := src[index]
		if currentByte < 0x80 {
			if index > 9 || (index == 9 && currentByte > 1) {
				return 0, 0, errCorrupt
			}
			return value | uint64(currentByte)<<shift, index + 1, nil
		}
		value |= uint64(currentByte&0x7f) << shift
		shift += 7
	}
	return 0, 0, errCorrupt
}

func loadUint24(buffer []byte) uint32 {
	return uint32(buffer[0]) | uint32(buffer[1])<<8 | uint32(buffer[2])<<16
}

func decompress(data []byte, codec int32) ([]byte, error) {
	switch codec {
	case codecUncompressed:
		return data, nil
	case codecSnappy:
		return snappyDecode(data)
	default:
		return nil, errBadCompression
	}
}

var errBadCompression = errors.New("parquet: unsupported compression codec")
