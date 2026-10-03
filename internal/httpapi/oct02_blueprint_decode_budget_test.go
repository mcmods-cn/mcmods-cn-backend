package httpapi

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/Tnze/go-mc/nbt"
)

func TestOCT02NBTPreflightBoundsAllCollectionTypes(t *testing.T) {
	for _, tag := range []byte{7, 11, 12} {
		for _, length := range []uint32{0x7fffffff, 0xffffffff, 17} {
			data := []byte{10, 0, 0, tag, 0, 1, 'x'}
			data = binary.BigEndian.AppendUint32(data, length)
			data = append(data, 0)
			if _, err := decodeNBTMap(data); err == nil {
				t.Fatalf("incomplete or excessive collection tag=%d length=%d accepted", tag, length)
			}
		}
	}
	for _, data := range [][]byte{
		{10, 0, 0, 9, 0, 1, 'x', 0, 0, 0, 0, 1, 0}, // Nonempty list of end tags.
		{10, 0, 0, 9, 0, 1, 'x', 1, 255, 255, 255, 255, 0},
		{10, 0, 0, 13, 0, 1, 'x', 0},
		{10, 0, 0, 0, 1},
	} {
		if _, err := decodeNBTMap(data); err == nil {
			t.Fatal("malformed or trailing NBT accepted")
		}
	}
}

func TestOCT02NBTPreflightBoundsNestedAndExpandedValues(t *testing.T) {
	data := []byte{10, 0, 0}
	for depth := 0; depth <= maxBlueprintNBTDepth; depth++ {
		data = append(data, 10, 0, 1, 'x')
	}
	data = append(data, bytes.Repeat([]byte{0}, maxBlueprintNBTDepth+2)...)
	if _, err := decodeNBTMap(data); err == nil {
		t.Fatal("overly nested NBT accepted")
	}
	budget := blueprintNBTBudget{allocation: maxBlueprintNBTAllocation - 1}
	if err := budget.reserve(2); err == nil {
		t.Fatal("aggregate allocation budget overflow accepted")
	}
	budget = blueprintNBTBudget{data: []byte{0}, values: maxBlueprintNBTValues}
	if err := budget.payload(10, 0); err == nil {
		t.Fatal("aggregate value budget overflow accepted")
	}
}

func TestOCT02NBTPreflightPreservesValidTagValues(t *testing.T) {
	root := map[string]any{"byte": int8(1), "short": int16(2), "int": int32(3), "long": int64(4),
		"float": float32(5), "double": float64(6), "bytes": []byte{1, 2}, "text": "中文",
		"ints": []int32{1, 2}, "longs": []int64{3, 4}, "list": []map[string]any{{"value": int32(7)}},
		"empty": []string{}, "compound": map[string]any{"nested": "value"}}
	var encoded bytes.Buffer
	if err := nbt.NewEncoder(&encoded).Encode(root, "fixture"); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeNBTMap(encoded.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if decoded["text"] != "中文" || decoded["compound"].(map[string]any)["nested"] != "value" || len(decoded) != len(root) {
		t.Fatalf("valid tag payload changed: %#v", decoded)
	}
}

func TestOCT02BlueprintRejectsNegativeNBTArrayWithoutPanic(t *testing.T) {
	// An incomplete, negative-length int array used to panic in the generic
	// decoder before its reader limit could protect the worker.
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("untrusted NBT reached a panicking allocation: %v", recovered)
		}
	}()
	if _, err := decodeNBTMap([]byte{10, 0, 0, 11, 0, 1, 'x', 255, 255, 255, 255, 0}); err == nil {
		t.Fatal("negative NBT array length was accepted")
	}
}

func TestOCT02LegacySchematicRejectsTruncatedArrays(t *testing.T) {
	for _, root := range []map[string]any{
		{"Width": int16(2), "Height": int16(1), "Length": int16(1), "Blocks": []byte{1}, "Data": []byte{0}},
		{"Width": int16(2), "Height": int16(1), "Length": int16(1), "Blocks": []byte{1, 1}, "Data": []byte{0}},
		{"Width": int16(2), "Height": int16(1), "Length": int16(1), "Blocks": []byte{1, 1}, "Data": []byte{0, 0}, "AddBlocks": []byte{}},
	} {
		if _, err := decodeLegacySchematic(root); err == nil {
			t.Fatal("truncated legacy schematic was silently normalized")
		}
	}
}

func TestOCT02LegacySchematicPreservesExtendedIDs(t *testing.T) {
	root := map[string]any{"Width": int16(2), "Height": int16(1), "Length": int16(1),
		"Blocks": []byte{0, 1}, "Data": []byte{7, 2}, "AddBlocks": []byte{0x21}}
	var encoded bytes.Buffer
	if err := nbt.NewEncoder(&encoded).Encode(root, "Schematic"); err != nil {
		t.Fatal(err)
	}
	document, err := decodeBlueprint(encoded.Bytes(), "schematic", "extended")
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Blocks) != 2 || document.Blocks[0].State.ID != "legacy:256" || document.Blocks[1].State.ID != "legacy:513" || document.Blocks[0].State.Properties["data"] != "7" {
		t.Fatalf("extended legacy IDs or metadata lost: %#v", document.Blocks)
	}
}
