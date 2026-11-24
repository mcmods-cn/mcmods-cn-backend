package httpapi

import "testing"

func TestBlueprintCodecRoundTrips(t *testing.T) {
	document := blueprintDocument{
		SchemaVersion: blueprintSchemaVersion,
		Name:          "roundtrip",
		SourceFormat:  "json",
		DataVersion:   3465,
		Size:          [3]int{3, 2, 2},
		Blocks: []blueprintBlock{
			{Position: [3]int{0, 0, 0}, State: blueprintBlockState{ID: "minecraft:stone"}},
			{Position: [3]int{2, 1, 1}, State: blueprintBlockState{ID: "minecraft:oak_log", Properties: map[string]string{"axis": "y"}}},
		},
	}

	for _, format := range []string{"nbt", "schem", "litematic"} {
		t.Run(format, func(t *testing.T) {
			encoded, _, err := encodeBlueprint(document, format)
			if err != nil {
				t.Fatalf("encode %s: %v", format, err)
			}
			decoded, err := decodeBlueprint(encoded, format, "roundtrip")
			if err != nil {
				t.Fatalf("decode %s: %v", format, err)
			}
			if decoded.Size != document.Size {
				t.Fatalf("size = %v, want %v", decoded.Size, document.Size)
			}
			if len(decoded.Blocks) != len(document.Blocks) {
				t.Fatalf("block count = %d, want %d", len(decoded.Blocks), len(document.Blocks))
			}
			for index := range document.Blocks {
				if formatBlockState(decoded.Blocks[index].State) != formatBlockState(document.Blocks[index].State) || decoded.Blocks[index].Position != document.Blocks[index].Position {
					t.Fatalf("block %d = %#v, want %#v", index, decoded.Blocks[index], document.Blocks[index])
				}
			}
		})
	}
}

func TestFinalizeBlueprintDropsAir(t *testing.T) {
	document := finalizeBlueprint(blueprintDocument{Blocks: []blueprintBlock{
		{State: blueprintBlockState{ID: "minecraft:air"}},
		{State: blueprintBlockState{ID: "minecraft:stone"}},
	}}, "test", "json")
	if len(document.Blocks) != 1 || document.Blocks[0].State.ID != "minecraft:stone" {
		t.Fatalf("unexpected blocks: %#v", document.Blocks)
	}
}

// Sponge stores one palette reference per position, including air.
func TestSpongeBlockDataMatchesDeclaredDimensions(t *testing.T) {
	root := map[string]any{
		"Width": int16(1), "Height": int16(1), "Length": int16(1),
		"Blocks": map[string]any{
			"Palette": map[string]any{"minecraft:stone": int32(0), "minecraft:dirt": int32(1)},
			"Data":    []byte{0, 1},
		},
	}
	raw, _, err := encodeGzipNBT(root, "Schematic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeBlueprint(raw, "schem", "Two blocks"); err == nil {
		t.Fatal("two palette entries must not be silently shortened to one block")
	}
	root["Width"] = int16(2)
	raw, _, err = encodeGzipNBT(root, "Schematic")
	if err != nil {
		t.Fatal(err)
	}
	document, err := decodeBlueprint(raw, "schem", "Two blocks")
	if err != nil || len(document.Blocks) != 2 || document.Blocks[1].State.ID != "minecraft:dirt" {
		t.Fatalf("valid two-block schematic: %+v, %v", document.Blocks, err)
	}
}

func BenchmarkFinalizeBlueprintDense(b *testing.B) {
	blocks := make([]blueprintBlock, 100_000)
	for i := range blocks {
		blocks[i] = blueprintBlock{Position: [3]int{i % 100, i / 100 % 100, i / 10000}, State: blueprintBlockState{ID: "minecraft:stone"}}
	}
	document := blueprintDocument{Size: [3]int{100, 100, 10}, Blocks: blocks}
	b.ReportAllocs()
	for b.Loop() {
		if len(finalizeBlueprint(document, "Dense", "nbt").Blocks) != len(blocks) {
			b.Fatal("lost a non-air block")
		}
	}
}
