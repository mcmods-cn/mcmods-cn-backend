package httpapi

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// The 1.20.1 beacon, bookshelf and fence models carry editor annotations.
// Only geometry and registered local texture references belong to the viewer.
func TestExportVanillaModelProjectionOmitsAuthoringFields(t *testing.T) {
	const name = "assets/minecraft/models/block/custom_fence_side_east.json"
	const model = `{"textures":{"side":"minecraft:block/oak_planks"},"elements":[{"name":"editor label","__comment":"editor note","from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"#side","uv":[0,0,16,16]}}}],"groups":[{"name":"editor group","origin":[8,8,8],"color":0,"children":[0]}]}`
	out, err := projectExportAsset(name, []byte(model), exportAssetModel)
	if err != nil || bytes.Contains(out, []byte("editor")) || bytes.Contains(out, []byte("groups")) || !bytes.Contains(out, []byte("#side")) || !bytes.Contains(out, []byte("oak_planks")) {
		t.Fatalf("model lost geometry/texture or published editor text: %v", err)
	}
	for _, invalid := range []string{
		strings.Replace(model, `"elements":`, `"unsupported_material":true,"elements":`, 1),
		strings.Replace(model, `"color":0`, `"color":0,"extra":true`, 1),
		strings.Replace(model, `"#side"`, `"https://example.invalid/texture.png"`, 1),
	} {
		if _, err := projectExportAsset(name, []byte(invalid), exportAssetModel); err == nil {
			t.Fatal("unsupported model data accepted")
		}
	}
}

func TestExportBlockEntityMeshV1Projection(t *testing.T) {
	const name = "block_entities/models/minecraft/sign/default.mesh.json"
	const vertex = `{"position":[0,0,0],"uv":[0.25,0.75],"normal":[0,1,0]}`
	mesh := `{"schema_version":"mcmods-uv-quad-mesh/v1","coordinate_space":"block_units","uv_space":"source_texture_normalized","uv_origin":"bottom_left","vertex_count":5,"quad_count":1,"trailing_vertex_count":1,"faces":[{"index":0,"direction":"up","uv_bounds":{"u_min":0.25,"u_max":0.25,"v_min":0.75,"v_max":0.75},"vertices":[` + strings.Join([]string{vertex, vertex, vertex, vertex}, ",") + `]}]}`
	out, err := projectExportAsset(name, []byte(mesh), exportAssetNormalized)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		Trailing int `json:"trailing_vertex_count"`
		Faces    []struct {
			Direction string
			UVBounds  map[string]float64 `json:"uv_bounds"`
			Vertices  []struct{ UV []float64 }
		}
	}
	if err = json.Unmarshal(out, &projected); err != nil || projected.Trailing != 1 || len(projected.Faces) != 1 || projected.Faces[0].Direction != "up" || projected.Faces[0].UVBounds["u_min"] != 0.25 || projected.Faces[0].Vertices[0].UV[1] != 0.75 {
		t.Fatal("mesh geometry or UV contract changed", err)
	}
	// The mesh is valid JSON even after whitespace padding; the read budget
	// must reject it before allocating or projecting the decoded geometry.
	oversized := append(bytes.Repeat([]byte(" "), int(maxExportBlockEntityMeshBytes)), []byte(mesh)...)
	if _, err := projectExportAsset(name, oversized, exportAssetNormalized); err == nil {
		t.Fatal("oversized mesh accepted")
	}
	for _, change := range [][2]string{
		{`"mcmods-uv-quad-mesh/v1"`, `"mcmods-uv-quad-mesh/v2"`}, {`"block_units"`, `"voxels"`}, {`"bottom_left"`, `"top_left"`},
		{`"direction":"up"`, `"direction":"unknown"`}, {`"index":0`, `"index":1`}, {`"vertex_count":5`, `"vertex_count":4`},
		{`"uv":[0.25,0.75]`, `"uv":[0.25]`}, {`"uv":[0.25,0.75]`, `"uv":[null,0.75]`}, {`"u_max":0.25`, `"u_max":0.1`},
		{`"normal":[0,1,0]`, `"normal":[0,1,0],"extra":true`},
	} {
		if _, err := projectExportAsset(name, []byte(strings.Replace(mesh, change[0], change[1], 1)), exportAssetNormalized); err == nil {
			t.Fatalf("invalid mesh accepted for field %s", change[0])
		}
	}
}
