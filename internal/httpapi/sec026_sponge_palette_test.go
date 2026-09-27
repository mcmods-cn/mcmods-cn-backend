package httpapi

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSEC026SpongePaletteRejectsUntrustedIndexShapesBeforeAllocation(t *testing.T) {
	tests := []struct {
		name    string
		palette map[string]any
		data    []byte
	}{
		{name: "negative", palette: map[string]any{"minecraft:stone": int32(-1)}, data: appendVarInt(nil, 0)},
		{name: "duplicate", palette: map[string]any{"minecraft:air": int32(0), "minecraft:stone": int32(0)}, data: appendVarInt(nil, 0)},
		{name: "sparse", palette: map[string]any{"minecraft:air": int32(0), "minecraft:stone": int32(2)}, data: appendVarInt(nil, 2)},
		{name: "non integer", palette: map[string]any{"minecraft:stone": float64(0)}, data: appendVarInt(nil, 0)},
		{name: "over budget index", palette: map[string]any{"minecraft:stone": int32(maxBlueprintMaterialCount + 1)}, data: appendVarInt(nil, maxBlueprintMaterialCount+1)},
		{name: "block data outside palette", palette: map[string]any{"minecraft:stone": int32(0)}, data: appendVarInt(nil, 1)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("malicious palette reached a panic instead of returning an error: %v", recovered)
				}
			}()
			_, err := decodeSpongeSchematic(spongeSEC026Root(testCase.palette, testCase.data))
			if err == nil {
				t.Fatal("malicious palette was accepted")
			}
		})
	}

	oversized := make(map[string]any, maxBlueprintMaterialCount+1)
	for index := 0; index <= maxBlueprintMaterialCount; index++ {
		oversized[fmt.Sprintf("example:block_%d", index)] = int32(index)
	}
	if _, err := decodeSpongeSchematic(spongeSEC026Root(oversized, appendVarInt(nil, 0))); err == nil {
		t.Fatal("palette entry count above the material budget was accepted")
	}
}

func TestSEC026TinyCompressedSchematicCannotSelectAllocationSize(t *testing.T) {
	index := maxBlueprintMaterialCount + 1
	raw, _, err := encodeGzipNBT(spongeSEC026Root(
		map[string]any{"minecraft:stone": int32(index)}, appendVarInt(nil, index)), "Schematic")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) >= 1024 {
		t.Fatalf("malicious fixture is no longer a tiny compressed input: %d bytes", len(raw))
	}
	if _, err = decodeBlueprint(raw, "schem", "sec026.schem"); err == nil {
		t.Fatal("tiny compressed sparse palette was accepted")
	}
}

func TestSEC026SpongePaletteUsesOnlyEntryBoundedAllocation(t *testing.T) {
	raw, err := os.ReadFile("blueprint_codec.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{"maxPalette :=", "maxPalette+1", "palette[intValue(value)]"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("untrusted palette allocation/index path remains: %q", forbidden)
		}
	}
	for _, required := range []string{"decodeSpongePalette", "maxBlueprintMaterialCount", "schematic block data references an unknown palette index"} {
		if !strings.Contains(source, required) {
			t.Fatalf("missing bounded Sponge palette contract %q", required)
		}
	}
}

func TestSEC026ValidDenseSpongePalettesRemainSupported(t *testing.T) {
	for name, root := range map[string]map[string]any{
		"version two": spongeSEC026Root(map[string]any{"minecraft:stone": int32(0)}, appendVarInt(nil, 0)),
		"version three": {
			"Width": int16(1), "Height": int16(1), "Length": int16(1),
			"Blocks": map[string]any{
				"Palette": map[string]any{"minecraft:air": int32(0), "minecraft:oak_log[axis=y]": int32(1)},
				"Data":    appendVarInt(nil, 1),
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			document, err := decodeSpongeSchematic(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(document.Blocks) != 1 {
				t.Fatalf("decoded blocks=%d, want 1", len(document.Blocks))
			}
		})
	}
}

func spongeSEC026Root(palette map[string]any, data []byte) map[string]any {
	return map[string]any{
		"Width": int16(1), "Height": int16(1), "Length": int16(1),
		"Palette": palette, "BlockData": data,
	}
}
