package parquet

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

func TestDecodePlainByteArrayUsesFourByteLength(t *testing.T) {
	var buffer bytes.Buffer
	for _, value := range []string{"hello", "", "world!"} {
		var length [4]byte
		binary.LittleEndian.PutUint32(length[:], uint32(len(value)))
		buffer.Write(length[:])
		buffer.WriteString(value)
	}
	values, err := decodePlainByteArray(buffer.Bytes(), 3)
	if err != nil {
		t.Fatalf("decodePlainByteArray: %v", err)
	}
	want := []string{"hello", "", "world!"}
	for index, value := range values {
		if string(value) != want[index] {
			t.Fatalf("value[%d] = %q, want %q", index, value, want[index])
		}
	}
}

func TestDecodeHybridRLEAndBitPacked(t *testing.T) {
	// RLE run: 5 repetitions of 7 (header = numValues<<1, value byte).
	rle, err := decodeHybrid([]byte{0x0a, 0x07}, 1, 5)
	if err != nil {
		t.Fatalf("rle: %v", err)
	}
	for index, value := range rle {
		if value != 7 {
			t.Fatalf("rle[%d] = %d, want 7", index, value)
		}
	}

	// One bit-packed group of 8 values with bit width 2: [1,2,3,0,0,0,0,0].
	packed := []byte{0x03, 0x39, 0x00}
	values, err := decodeHybrid(packed, 2, 8)
	if err != nil {
		t.Fatalf("bitpacked: %v", err)
	}
	want := []int32{1, 2, 3, 0, 0, 0, 0, 0}
	for index, value := range values {
		if value != want[index] {
			t.Fatalf("packed[%d] = %d, want %d", index, value, want[index])
		}
	}
}

func TestDecodeDefLevels(t *testing.T) {
	// 4-byte length 3, then an RLE run of 1000 one-bits.
	data := append([]byte{0x03, 0x00, 0x00, 0x00, 0xd0, 0x0f, 0x01}, 0xaa, 0xbb)
	levels, consumed, err := decodeDefLevels(data, 1, 1000)
	if err != nil {
		t.Fatalf("decodeDefLevels: %v", err)
	}
	if consumed != 7 {
		t.Fatalf("consumed = %d, want 7", consumed)
	}
	if len(levels) != 1000 {
		t.Fatalf("levels = %d, want 1000", len(levels))
	}
	for _, level := range levels {
		if level != 1 {
			t.Fatalf("level = %d, want 1", level)
		}
	}
}

// TestSnappyHighOffsetBits is a regression test for the uint8 shift that
// dropped the high bits of a 1-byte-offset copy, corrupting offsets above 255.
func TestSnappyHighOffsetBits(t *testing.T) {
	const literalLength = 256
	uncompressed := bytes.Repeat([]byte{'a'}, literalLength)
	uncompressed = append(uncompressed, 'a', 'a', 'a', 'a') // copy offset 256 length 4

	var block bytes.Buffer
	block.Write([]byte{0x84, 0x02}) // varint(260)
	block.WriteByte(0xF0)           // literal tag: 60 -> 1-byte length follows
	block.WriteByte(0xFF)           // length - 1
	block.Write(bytes.Repeat([]byte{'a'}, literalLength))
	block.WriteByte(0x21) // copy, 1-byte offset, high bit set (offset 256), length 4
	block.WriteByte(0x00) // offset low byte

	decoded, err := snappyDecode(block.Bytes())
	if err != nil {
		t.Fatalf("snappyDecode: %v", err)
	}
	if !bytes.Equal(decoded, uncompressed) {
		t.Fatalf("decoded %d bytes, want %d", len(decoded), len(uncompressed))
	}
}

func TestAssembleLists(t *testing.T) {
	reps := []int32{0, 1, 0}
	defs := []int32{2, 2, 1}
	values := []string{"a", "b"}
	rows, err := assembleLists(reps, defs, values, 2)
	if err != nil {
		t.Fatalf("assembleLists: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if len(rows[0]) != 2 || rows[0][0] != "a" || rows[0][1] != "b" {
		t.Fatalf("rows[0] = %v", rows[0])
	}
	if len(rows[1]) != 0 {
		t.Fatalf("rows[1] = %v, want empty", rows[1])
	}
}

func TestDecodePlainBooleans(t *testing.T) {
	values, err := decodePlainBooleans([]byte{0x0D, 0x01}, 9)
	if err != nil {
		t.Fatalf("decodePlainBooleans: %v", err)
	}
	want := []bool{true, false, true, true, false, false, false, false, true}
	for index, value := range values {
		if value != want[index] {
			t.Fatalf("value[%d] = %v, want %v", index, value, want[index])
		}
	}
}

// TestReadOpenR1Fixture reads a real OpenR1-Math-220k shard when
// GONANO_SCHOOL_PARQUET_OPENR1 is set, exercising nested LIST<STRING> and
// LIST<BOOL> decoding.
func TestReadOpenR1Fixture(t *testing.T) {
	path := os.Getenv("GONANO_SCHOOL_PARQUET_OPENR1")
	if path == "" {
		t.Skip("set GONANO_SCHOOL_PARQUET_OPENR1 to a OpenR1 parquet shard to run this test")
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	problem, err := reader.ReadColumnStrings(0, "problem")
	if err != nil {
		t.Fatalf("problem: %v", err)
	}
	generations, err := reader.ReadListStrings(0, []string{"generations", "list", "element"})
	if err != nil {
		t.Fatalf("generations: %v", err)
	}
	correctness, err := reader.ReadListBools(0, []string{"correctness_math_verify", "list", "element"})
	if err != nil {
		t.Fatalf("correctness: %v", err)
	}
	if len(problem) != len(generations) {
		t.Fatalf("rows mismatch: problem=%d generations=%d", len(problem), len(generations))
	}
	if len(generations) == 0 || len(generations[0]) == 0 {
		t.Fatal("no generations")
	}
	if !bytes.Contains([]byte(generations[0][0]), []byte("<think>")) {
		t.Fatalf("generation[0][0] does not start a trace: %q", generations[0][0][:min(40, len(generations[0][0]))])
	}
	if len(correctness) > 0 && len(correctness[0]) == 0 {
		t.Fatal("expected correctness values")
	}
}

// TestReadMixtureOfThoughtsFixture reads a real open-r1/Mixture-of-Thoughts
// shard when GONANO_SCHOOL_PARQUET_MOT is set, exercising nested
// LIST<STRUCT<string,string>> fields plus a flat INT64 column.
func TestReadMixtureOfThoughtsFixture(t *testing.T) {
	path := os.Getenv("GONANO_SCHOOL_PARQUET_MOT")
	if path == "" {
		t.Skip("set GONANO_SCHOOL_PARQUET_MOT to a Mixture-of-Thoughts shard to run this test")
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()

	contents, err := reader.ReadListStrings(0, []string{"messages", "list", "element", "content"})
	if err != nil {
		t.Fatalf("messages.content: %v", err)
	}
	roles, err := reader.ReadListStrings(0, []string{"messages", "list", "element", "role"})
	if err != nil {
		t.Fatalf("messages.role: %v", err)
	}
	tokens, err := reader.ReadColumnInt64(0, "num_tokens")
	if err != nil {
		t.Fatalf("num_tokens: %v", err)
	}
	source, err := reader.ReadColumnStrings(0, "source")
	if err != nil {
		t.Fatalf("source: %v", err)
	}
	if len(contents) == 0 || len(roles) == 0 {
		t.Fatal("empty messages")
	}
	if len(contents) != len(roles) || len(tokens) != len(contents) || len(source) != len(contents) {
		t.Fatalf("length mismatch: content=%d role=%d tokens=%d source=%d", len(contents), len(roles), len(tokens), len(source))
	}
	if len(contents[0]) == 0 || roles[0][0] != "user" {
		t.Fatalf("unexpected first message: role=%v content=%q", roles[0], contents[0])
	}
	if tokens[0] <= 0 {
		t.Fatalf("num_tokens[0] = %d, want > 0", tokens[0])
	}
}

// TestReadMagpieFixture reads a real Magpie parquet shard when
// GONANO_SCHOOL_PARQUET is set, exercising definition levels, a large snappy
// dictionary page, and RLE_DICTIONARY indices end to end.
func TestReadMagpieFixture(t *testing.T) {
	path := os.Getenv("GONANO_SCHOOL_PARQUET")
	if path == "" {
		t.Skip("set GONANO_SCHOOL_PARQUET to a Magpie parquet shard to run this test")
	}
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer reader.Close()
	if reader.NumRows() == 0 {
		t.Fatal("no rows")
	}
	instruction, err := reader.ReadColumnStrings(0, "instruction")
	if err != nil {
		t.Fatalf("instruction: %v", err)
	}
	response, err := reader.ReadColumnStrings(0, "response")
	if err != nil {
		t.Fatalf("response: %v", err)
	}
	if len(instruction) == 0 || len(response) == 0 {
		t.Fatal("empty columns")
	}
	if !bytes.Contains([]byte(response[0]), []byte("Okay")) && response[0] == "" {
		t.Fatalf("unexpected response[0] = %q", response[0])
	}
	if len(response[0]) <= len(instruction[0]) {
		t.Fatalf("response shorter than instruction: %d vs %d", len(response[0]), len(instruction[0]))
	}
}
