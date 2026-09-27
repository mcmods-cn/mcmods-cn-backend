package httpapi

import (
	"strings"
	"testing"
)

func TestBlueprintConversionRefusesSilentEntityLoss(t *testing.T) {
	base := blueprintDocument{
		SchemaVersion: blueprintSchemaVersion,
		Name:          "entity fidelity",
		SourceFormat:  "nbt",
		DataVersion:   3955,
		Size:          [3]int{1, 1, 1},
		Blocks: []blueprintBlock{{
			Position: [3]int{0, 0, 0},
			State:    blueprintBlockState{ID: "minecraft:chest"},
		}},
	}
	cases := []struct {
		name     string
		document blueprintDocument
	}{
		{name: "block entity", document: func() blueprintDocument {
			document := base
			document.BlockEntities = []map[string]any{{"id": "minecraft:chest", "Items": []any{map[string]any{"id": "minecraft:diamond"}}}}
			return document
		}()},
		{name: "entity", document: func() blueprintDocument {
			document := base
			document.Entities = []map[string]any{{"nbt": map[string]any{"id": "minecraft:armor_stand"}, "pos": []float64{0.5, 0.0, 0.5}}}
			return document
		}()},
	}
	for _, testCase := range cases {
		for _, format := range []string{"nbt", "schem", "litematic"} {
			t.Run(testCase.name+"/"+format, func(t *testing.T) {
				if _, _, err := encodeBlueprint(testCase.document, format); err == nil || !strings.Contains(strings.ToLower(err.Error()), "entity") {
					t.Fatalf("encode %s returned %v; want an explicit entity-fidelity rejection", format, err)
				}
			})
		}
	}
	if _, _, err := encodeBlueprint(cases[0].document, "json"); err != nil {
		t.Fatalf("normalized JSON should preserve entity data: %v", err)
	}
}

func TestBlueprintNBTDecodersRefuseEntityPayloadsTheyDoNotMap(t *testing.T) {
	base := blueprintDocument{
		SchemaVersion: blueprintSchemaVersion,
		Name:          "source entity fidelity",
		DataVersion:   3955,
		Size:          [3]int{1, 1, 1},
		Blocks: []blueprintBlock{{
			Position: [3]int{0, 0, 0},
			State:    blueprintBlockState{ID: "minecraft:stone"},
		}},
	}

	litematic, _, err := encodeLitematic(base)
	if err != nil {
		t.Fatal(err)
	}
	litematicRoot, err := decodeNBTMap(litematic)
	if err != nil {
		t.Fatal(err)
	}
	mainRegion := anyMap(anyMap(litematicRoot["Regions"])["Main"])
	mainRegion["Entities"] = []map[string]any{{"id": "minecraft:item"}}
	litematicWithEntity, _, err := encodeGzipNBT(litematicRoot, "Litematic")
	if err != nil {
		t.Fatal(err)
	}

	sponge, _, err := encodeSpongeSchematic(base)
	if err != nil {
		t.Fatal(err)
	}
	spongeRoot, err := decodeNBTMap(sponge)
	if err != nil {
		t.Fatal(err)
	}
	spongeRoot["BlockEntities"] = []map[string]any{{"Id": "minecraft:chest"}}
	spongeWithBlockEntity, _, err := encodeGzipNBT(spongeRoot, "Schematic")
	if err != nil {
		t.Fatal(err)
	}

	legacyWithEntity, _, err := encodeGzipNBT(map[string]any{
		"Width":        int16(1),
		"Height":       int16(1),
		"Length":       int16(1),
		"Blocks":       []byte{1},
		"Data":         []byte{0},
		"TileEntities": []map[string]any{{"id": "Chest"}},
	}, "Schematic")
	if err != nil {
		t.Fatal(err)
	}

	for _, testCase := range []struct {
		name   string
		format string
		data   []byte
	}{
		{name: "litematic entity", format: "litematic", data: litematicWithEntity},
		{name: "sponge block entity", format: "schem", data: spongeWithBlockEntity},
		{name: "legacy tile entity", format: "schematic", data: legacyWithEntity},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := decodeBlueprint(testCase.data, testCase.format, "lossy source"); err == nil || !strings.Contains(strings.ToLower(err.Error()), "entity") {
				t.Fatalf("decode %s returned %v; want an explicit entity-fidelity rejection", testCase.format, err)
			}
		})
	}
}
