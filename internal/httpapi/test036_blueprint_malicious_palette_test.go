package httpapi

import "testing"

func TestTEST036MalformedLitematicPaletteAndPackedDataCannotSilentlyLoseBlocks(t *testing.T) {
	for _, name := range []string{"unknown-index", "empty-name", "missing-packed-word"} {
		t.Run(name, func(t *testing.T) {
			raw, _, err := encodeBlueprint(test036Document(), "litematic")
			if err != nil {
				t.Fatal(err)
			}
			root, err := decodeNBTMap(raw)
			if err != nil {
				t.Fatal(err)
			}
			region := anyMap(anyMap(root["Regions"])["Main"])
			switch name {
			case "unknown-index":
				region["BlockStates"] = []int64{3}
			case "empty-name":
				anyMap(anySlice(region["BlockStatePalette"])[1])["Name"] = ""
			case "missing-packed-word":
				region["BlockStates"] = []int64{}
			}
			raw, _, err = encodeGzipNBT(root, "TEST036 malformed Litematic")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = decodeBlueprint(raw, "litematic", name); err == nil {
				t.Fatal("malformed source silently discarded its original blocks")
			}
		})
	}
}

func TestTEST036SpongePaletteRejectsEmptyStateAndUnknownIndex(t *testing.T) {
	for _, name := range []string{"empty-state", "unknown-index"} {
		t.Run(name, func(t *testing.T) {
			raw, _, err := encodeBlueprint(test036Document(), "schem")
			if err != nil {
				t.Fatal(err)
			}
			root, err := decodeNBTMap(raw)
			if err != nil {
				t.Fatal(err)
			}
			blocks := anyMap(root["Blocks"])
			palette := anyMap(blocks["Palette"])
			if _, exists := palette["minecraft:stone"]; !exists {
				t.Fatal("invalid fixture: the canonical v3 block palette was not selected")
			}
			if name == "empty-state" {
				palette[""] = palette["minecraft:stone"]
				delete(palette, "minecraft:stone")
			} else {
				data := byteSlice(blocks["Data"])
				if len(data) == 0 {
					t.Fatal("invalid fixture: no canonical block data")
				}
				data[0] = 127
				blocks["Data"] = data
			}
			raw, _, err = encodeGzipNBT(root, "TEST036 malformed Sponge")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = decodeBlueprint(raw, "schem", name); err == nil {
				t.Fatal("malformed Sponge palette was accepted")
			}
		})
	}
}
