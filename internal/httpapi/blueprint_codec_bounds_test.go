package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"testing"

	"github.com/Tnze/go-mc/nbt"
)

func TestSpongeSchematicRejectsNegativePaletteIndex(t *testing.T) {
	root := map[string]any{"Width": int16(1), "Height": int16(1), "Length": int16(1),
		"Palette": map[string]any{"minecraft:stone": int32(-1)}, "BlockData": []byte{0}}
	if _, err := decodeSpongeSchematic(root); err == nil {
		t.Fatal("negative palette index was accepted")
	}
}

func TestSpongeSchematicSparsePaletteDoesNotAllocateByIndex(t *testing.T) {
	const index = 1 << 30
	root := map[string]any{"Width": int16(1), "Height": int16(1), "Length": int16(1),
		"Palette": map[string]any{"minecraft:stone": int32(index)}, "BlockData": appendVarInt(nil, index)}
	document, err := decodeSpongeSchematic(root)
	if err != nil || len(document.Blocks) != 1 || document.Blocks[0].State.ID != "minecraft:stone" {
		t.Fatalf("valid sparse palette was not decoded: blocks=%d err=%v", len(document.Blocks), err)
	}
	root["Palette"] = map[string]any{"minecraft:stone": int32(index), "minecraft:dirt": int32(index)}
	if _, err := decodeSpongeSchematic(root); err == nil {
		t.Fatal("duplicate palette index was accepted")
	}
}

func TestBlueprintNBTRejectsAllocationDeclarationsBeforeDecoder(t *testing.T) {
	for _, tag := range []byte{7, 9, 11, 12} {
		// Compound root, one named array/list, no array contents. A decoder that
		// allocates before reading would try to allocate from this 11-byte input.
		data := []byte{10, 0, 0, tag, 0, 1, 'x'}
		if tag == 9 {
			data = append(data, 10)
		}
		data = binary.BigEndian.AppendUint32(data, 0x7fffffff)
		if _, err := decodeNBTMap(data); err == nil {
			t.Fatalf("tag %d accepted an absent huge payload", tag)
		}
	}
	data := []byte{10, 0, 0}
	for range maxBlueprintNBTDepth + 1 {
		data = append(data, 10, 0, 0)
	}
	data = append(data, make([]byte, maxBlueprintNBTDepth+2)...)
	if _, err := decodeNBTMap(data); err == nil {
		t.Fatal("deeply nested NBT was accepted")
	}
}

func TestBlueprintNBTPreflightPreservesNormalAndCompressedData(t *testing.T) {
	root := map[string]any{"name": "synthetic fixture", "ints": []int32{1, 2},
		"longs": []int64{3}, "bytes": []byte{4, 5}, "empty": []any{}}
	var encoded bytes.Buffer
	if err := nbt.NewEncoder(&encoded).Encode(root, ""); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write(encoded.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{encoded.Bytes(), compressed.Bytes()} {
		decoded, err := decodeNBTMap(data)
		if err != nil || decoded["name"] != root["name"] {
			t.Fatalf("valid NBT was rejected: err=%v", err)
		}
	}
	if _, err := decodeNBTMap(append(encoded.Bytes(), 0)); err == nil {
		t.Fatal("trailing NBT data was accepted")
	}
}
