package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"testing"
)

func auditGeometryPNG(t *testing.T, width, height int, pixel color.NRGBA) []byte {
	t.Helper()
	pixels := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			pixels.SetNRGBA(x, y, pixel)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, pixels); err != nil {
		t.Fatal(err)
	}
	// Exercise the existing privacy projection on newly registered ingredient
	// images as well as preserving their real pixels and alpha.
	payload := []byte("Comment\x00" + auditGR001PrivateMarker)
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	plain := output.Bytes()
	return append(append(append([]byte{}, plain[:33]...), chunk...), plain[33:]...)
}

// Follow producer JeiIngredientStage.exportEntry/writeIndex/ingredientIconPath:
// icons[32], ingredient_type/kind and the namespaced PNG paths are real fields.
// This remains synthetic contract data, never a modified real game archive.
func auditRecipeGeometryAssetZIP(t *testing.T) []byte {
	t.Helper()
	base := auditRecipeGeometryZIP(t)
	archive, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = string(raw)
	}
	path := "recipes/jei/recipes/example/pressing.json"
	var document map[string]any
	if err := json.Unmarshal([]byte(files[path]), &document); err != nil {
		t.Fatal(err)
	}
	for _, value := range document["recipes"].([]any) {
		recipe := value.(map[string]any)
		alternative := recipe["bindings"].([]any)[1].(map[string]any)["alternatives"].([]any)[0].(map[string]any)
		switch recipe["recipe_id"] {
		case "example:geometry_fluid":
			alternative["resource_location"] = "example:geometry_fluid"
		case "example:geometry_gas":
			alternative["resource_location"] = "example:geometry_gas"
		}
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	files[path] = string(raw)
	files["registries/items.json"] = `{"registry":"items","entries":[{"id":"example:geometry_item","namespace":"example","names":{"en-US":"Synthetic geometry item"},"max_stack_size":64}]}`
	files["ingredients/ingredients.json"] = `{"schema_version":"mcmods-ingredients/v1","type_count":2,"ingredient_count":2,"types":[{"ingredient_type":"net.minecraftforge.fluids.FluidStack","ingredient_kind":"fluid","path_key":"net.minecraftforge.fluids.fluidstack","count":1,"entries":[{"ingredient_kind":"fluid","resource_location":"example:geometry_fluid","unique_id":"example:geometry_fluid","amount":750,"names":{"en-US":"Synthetic geometry fluid"},"tags":[],"icons":{"32":"ingredients/net.minecraftforge.fluids.fluidstack/32/example/geometry_fluid--0123456789abcdef.png"}}]},{"ingredient_type":"mekanism.api.chemical.gas.GasStack","ingredient_kind":"mekanism_gas","chemical_family":"mekanism","chemical_type":"gas","path_key":"mekanism.api.chemical.gas.gasstack","count":1,"entries":[{"ingredient_kind":"mekanism_gas","chemical_family":"mekanism","chemical_type":"gas","resource_location":"example:geometry_gas","unique_id":"example:geometry_gas","amount":1000,"names":{"en-US":"Synthetic geometry gas"},"tags":[],"icons":{"32":"ingredients/mekanism.api.chemical.gas.gasstack/32/example/geometry_gas--0123456789abcdef.png"}}]}]}`
	files["recipes/jei/backgrounds/example/pressing/template_0123456789abcdef.png"] = string(auditGeometryPNG(t, 528, 328, color.NRGBA{R: 210, G: 210, B: 210, A: 255}))
	files["icons/items/32/example/geometry_item.png"] = string(auditGeometryPNG(t, 32, 32, color.NRGBA{R: 240, G: 140, B: 30, A: 255}))
	files["ingredients/net.minecraftforge.fluids.fluidstack/32/example/geometry_fluid--0123456789abcdef.png"] = string(auditGeometryPNG(t, 32, 32, color.NRGBA{R: 30, G: 100, B: 240, A: 128}))
	files["ingredients/mekanism.api.chemical.gas.gasstack/32/example/geometry_gas--0123456789abcdef.png"] = string(auditGeometryPNG(t, 32, 32, color.NRGBA{R: 100, G: 210, B: 80, A: 192}))
	return auditFixGRZip(t, files)
}

func TestAuditKnownJEIIngredientAssetsImportServeSanitizedPixelsIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditRecipeGeometryAssetZIP(t))
	f.reviewGR(t, revision, "approved")
	for _, expected := range []struct {
		path   string
		width  int
		height int
		pixel  color.NRGBA
	}{
		{"recipes/jei/backgrounds/example/pressing/template_0123456789abcdef.png", 528, 328, color.NRGBA{R: 210, G: 210, B: 210, A: 255}},
		{"icons/items/32/example/geometry_item.png", 32, 32, color.NRGBA{R: 240, G: 140, B: 30, A: 255}},
		{"ingredients/net.minecraftforge.fluids.fluidstack/32/example/geometry_fluid--0123456789abcdef.png", 32, 32, color.NRGBA{R: 30, G: 100, B: 240, A: 128}},
		{"ingredients/mekanism.api.chemical.gas.gasstack/32/example/geometry_gas--0123456789abcdef.png", 32, 32, color.NRGBA{R: 100, G: 210, B: 80, A: 192}},
	} {
		path := "/api/v1/export-revisions/" + revision + "/assets/content?path=" + url.QueryEscape(expected.path)
		request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := f.origin.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("registered image %s HTTP%d/%v", expected.path, response.StatusCode, err)
		}
		if bytes.Contains(body, []byte(auditGR001PrivateMarker)) || bytes.Contains(body, []byte("tEXt")) {
			t.Fatal("newly registered image leaked unrelated PNG text")
		}
		decoded, err := png.Decode(bytes.NewReader(body))
		if err != nil || decoded.Bounds().Dx() != expected.width || decoded.Bounds().Dy() != expected.height || color.NRGBAModel.Convert(decoded.At(0, 0)).(color.NRGBA) != expected.pixel {
			t.Fatalf("registered business pixels/alpha changed %s: %v", expected.path, err)
		}
		if f.number(t, `select count(*) from catalog_import_media media join oss_files file on file.id=media.oss_file_id where media.revision_id=$1 and media.asset_path=$2 and file.sha256=$3`, revision, expected.path, sha256Hex(body)) != 1 {
			t.Fatal("served bytes disagree with actual asset row hash")
		}
	}
	t.Log("producer-supported ingredient icon paths, namespaced resources, exact pixels/alpha/hash and PNG privacy projection reached actual HTTP/Worker/independent approval/anonymous bytes")
}
