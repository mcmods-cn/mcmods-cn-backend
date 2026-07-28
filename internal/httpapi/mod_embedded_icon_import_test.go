package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestDecodeEmbeddedIconCatalogJSONLines(t *testing.T) {
	icon := embeddedIconTestPNG(t, 32, 32)
	entries := []embeddedIconCatalogEntry{
		{Name: "方块", EnglishName: "Block", RegisterName: "example:block", Type: "Block", SmallIcon: icon, LargeIcon: icon},
		{Name: "物品", EnglishName: "Item", RegisterName: "example:item", Type: "Item", SmallIcon: icon, LargeIcon: icon},
	}
	var raw bytes.Buffer
	for _, entry := range entries {
		encoded, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		raw.Write(encoded)
		raw.WriteByte('\n')
	}
	decoded, err := decodeEmbeddedIconCatalog(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0].Type != "block" || decoded[1].Type != "item" {
		t.Fatalf("unexpected decoded entries: %#v", decoded)
	}
}

func TestRenderEmbeddedIconPNGUsesRequestedCanvas(t *testing.T) {
	source, err := decodeEmbeddedIconPNG(embeddedIconTestPNG(t, 32, 16))
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{32, 128, 256} {
		rendered, renderErr := renderEmbeddedIconPNG(source, size)
		if renderErr != nil {
			t.Fatal(renderErr)
		}
		config, _, decodeErr := image.DecodeConfig(bytes.NewReader(rendered))
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if config.Width != size || config.Height != size {
			t.Fatalf("rendered icon is %dx%d, want %dx%d", config.Width, config.Height, size, size)
		}
	}
}

func TestEmbeddedIconImportSourceAliases(t *testing.T) {
	if got := normalizeEmbeddedIconImportSource("IconRenderer"); got != iconRendererImportSource {
		t.Fatalf("IconRenderer normalized to %q", got)
	}
	if got := normalizeEmbeddedIconImportSource("LetMeSeeSee(YourCode)"); got != letMeSeeSeeImportSource {
		t.Fatalf("LetMeSeeSee normalized to %q", got)
	}
	if got := normalizeEmbeddedIconImportSource("IRR"); got != irrImportSource {
		t.Fatalf("IRR normalized to %q", got)
	}
}

func TestDecodeIRRCatalogSupportsItemsAndIconlessEntities(t *testing.T) {
	icon := embeddedIconTestPNG(t, 32, 32)
	raw := []byte(
		`{"name":"蜂蜜","englishName":"Honey","registerName":"geomastery:item_honey","type":"Item","maxStackSize":8,"smallIcon":"` + icon + `"}` + "\n" +
			`{"name":"entity.geomastery:spear_wood.name§r","Englishname":"entity.geomastery:spear_wood.name§r","mod":"geomastery","registerName":"geomastery:spear_wood","Icon":""}`,
	)
	entries, err := decodeEmbeddedIconCatalogForSource(raw, irrImportSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("decoded %d entries, want 2", len(entries))
	}
	if entries[0].Type != "item" || entries[0].MaxStackSize != 8 {
		t.Fatalf("unexpected IRR item: %#v", entries[0])
	}
	if entries[1].Type != "entity" || entries[1].Name != "entity.geomastery:spear_wood.name" {
		t.Fatalf("unexpected IRR entity: %#v", entries[1])
	}
}

func TestDecodeIconRendererCatalogSupportsEntityIcons(t *testing.T) {
	icon := embeddedIconTestPNG(t, 128, 128)
	raw := []byte(
		`{"name":"方块","englishName":"Block","registerName":"example:block","type":"Block","maxStacksSize":64,"smallIcon":"` + icon + `"}` + "\n" +
			`{"name":"实体","englishName":"Entity","registerName":"example:entities/test","mod":"example","type":"Entity","Icon":"` + icon + `"}`,
	)
	entries, err := decodeEmbeddedIconCatalogForSource(raw, iconRendererImportSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("decoded %d entries, want 2", len(entries))
	}
	if entries[0].Type != "block" || entries[0].MaxStackSize != 64 {
		t.Fatalf("unexpected IconRenderer block: %#v", entries[0])
	}
	if entries[1].Type != "entity" || entries[1].SmallIcon == "" || entries[1].LargeIcon == "" {
		t.Fatalf("unexpected IconRenderer entity: %#v", entries[1])
	}

	build, err := buildEmbeddedIconImport(
		ossConfigPayload{Prefix: "mcmods"},
		"mod000001", iconRendererImportSource, catalogResourceIdentityResolver{},
		map[string]string{"example": "revision-1"}, entries[1],
	)
	if err != nil {
		t.Fatal(err)
	}
	if build.resource.KindCode != "minecraft.entity_type" || build.resource.Registry != "entities" {
		t.Fatalf("unexpected entity resource: %#v", build.resource)
	}
	if build.resource.IconPath == "" || build.resource.PreviewPath == "" || len(build.media) != 3 || len(build.data) != 3 {
		t.Fatalf("IconRenderer entity did not produce all icon sizes: %#v", build)
	}
}

func TestDecodeIRRCatalogMergesDuplicateRegistryVariants(t *testing.T) {
	icon := embeddedIconTestPNG(t, 32, 32)
	raw := []byte(
		`{"name":"Banana","registerName":"geomastery:item_banana","type":"Item","metadata":0,"smallIcon":"` + icon + `"}` + "\n" +
			`{"name":"Rotten Banana","registerName":"geomastery:item_banana","type":"Item","metadata":0,"smallIcon":"` + icon + `"}`,
	)
	entries, err := decodeEmbeddedIconCatalogForSource(raw, irrImportSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || len(entries[0].IRRVariants) != 2 {
		t.Fatalf("unexpected merged IRR variants: %#v", entries)
	}
	if entries[0].IRRVariants[1].Name != "Rotten Banana" {
		t.Fatalf("unexpected alternate variant: %#v", entries[0].IRRVariants)
	}
}

func TestBuildIRRIconlessEntity(t *testing.T) {
	resolver := catalogResourceIdentityResolver{}
	entry := embeddedIconCatalogEntry{
		Name: "entity.geomastery:spear_wood.name", EnglishName: "entity.geomastery:spear_wood.name",
		RegisterName: "geomastery:spear_wood", Mod: "geomastery", Type: "entity",
	}
	build, err := buildEmbeddedIconImport(
		ossConfigPayload{Prefix: "mcmods"},
		"mod000001", irrImportSource, resolver,
		map[string]string{"geomastery": "revision-1"}, entry,
	)
	if err != nil {
		t.Fatal(err)
	}
	if build.resource.KindCode != "minecraft.entity_type" || build.resource.Registry != "entities" {
		t.Fatalf("unexpected entity resource: %#v", build.resource)
	}
	if build.resource.IconPath != "" || build.resource.PreviewPath != "" || len(build.media) != 0 || len(build.data) != 0 {
		t.Fatalf("iconless IRR entity unexpectedly produced media: %#v", build)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(build.resource.Data), &data); err != nil {
		t.Fatal(err)
	}
	if data["translationKey"] != "entity.geomastery:spear_wood.name" {
		t.Fatalf("unexpected translation key: %#v", data)
	}
}

func embeddedIconTestPNG(t *testing.T, width, height int) string {
	t.Helper()
	value := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			value.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(y), B: 180, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, value); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buffer.Bytes())
}
