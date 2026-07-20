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
