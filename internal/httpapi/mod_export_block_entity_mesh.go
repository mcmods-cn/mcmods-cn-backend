package httpapi

import (
	"encoding/json"
	"math"
)

// Mesh documents expand into several geometry arrays in the viewer. Bound the
// raw decode and the number of complete quads independently.
const (
	maxExportBlockEntityMeshBytes = int64(16 << 20)
	maxExportBlockEntityMeshQuads = 65_536
)

func exportBlockEntityMeshJSONRule() *exportJSONRule {
	number := exportNumberRule()
	vector := exportArrayRule(number)
	vertex := exportObjectRule(map[string]*exportJSONRule{"position": vector, "uv": vector, "normal": vector})
	bounds := exportObjectRule(map[string]*exportJSONRule{})
	exportFields(bounds, "u_min v_min u_max v_max", number)
	face := exportObjectRule(map[string]*exportJSONRule{"index": number, "direction": exportStringRule(), "vertices": exportArrayRule(vertex), "uv_bounds": bounds})
	mesh := exportObjectRule(map[string]*exportJSONRule{"faces": exportArrayRule(face)})
	mesh.nonNull = true
	exportFields(mesh, "schema_version coordinate_space uv_space uv_origin", exportStringRule())
	exportFields(mesh, "quad_count vertex_count trailing_vertex_count", number)
	return mesh
}

func projectExportBlockEntityMesh(name string, data []byte) ([]byte, exportUVQuadMesh, error) {
	var mesh exportUVQuadMesh
	var payload map[string]any
	if int64(len(data)) > maxExportBlockEntityMeshBytes || json.Unmarshal(data, &payload) != nil {
		return nil, mesh, exportAssetRegistrationError(exportAssetNormalized, name)
	}
	space, _ := payload["coordinate_space"].(string)
	uvSpace, _ := payload["uv_space"].(string)
	origin, _ := payload["uv_origin"].(string)
	faces, hasFaces := payload["faces"].([]any)
	trailing, ok := payload["trailing_vertex_count"].(float64)
	if payload["schema_version"] != blockEntityMeshSchema || space != "block_units" || origin != "bottom_left" ||
		(uvSpace != "source_texture_normalized" && uvSpace != "render_target_normalized") || !hasFaces ||
		len(faces) > maxExportBlockEntityMeshQuads || !ok || trailing < 0 || trailing > 3 || math.Trunc(trailing) != trailing ||
		payload["quad_count"] != float64(len(faces)) || payload["vertex_count"] != float64(4*len(faces))+trailing {
		return nil, mesh, exportAssetRegistrationError(exportAssetNormalized, name)
	}
	for i, face := range faces {
		if !validExportMeshFace(face, i) {
			return nil, mesh, exportAssetRegistrationError(exportAssetNormalized, name)
		}
	}
	projected, err := exportBlockEntityMeshJSONRule().project(payload)
	if err != nil {
		return nil, mesh, exportAssetRegistrationError(exportAssetNormalized, name)
	}
	mesh = exportUVQuadMesh{CoordinateSpace: space, UVSpace: uvSpace, UVOrigin: origin, VertexCount: 4*len(faces) + int(trailing), QuadCount: len(faces)}
	encoded, err := json.Marshal(projected)
	return encoded, mesh, err
}

func validExportMeshFace(value any, index int) bool {
	face, ok := value.(map[string]any)
	if !ok || face["index"] != float64(index) {
		return false
	}
	switch face["direction"] {
	case "up", "down", "north", "south", "east", "west":
	default:
		return false
	}
	vertices, ok := face["vertices"].([]any)
	if !ok || len(vertices) != 4 {
		return false
	}
	var uMin, vMin float32 = math.MaxFloat32, math.MaxFloat32
	var uMax, vMax float32 = -math.MaxFloat32, -math.MaxFloat32
	for _, raw := range vertices {
		vertex, ok := raw.(map[string]any)
		if !ok {
			return false
		}
		_, positionOK := exportMeshVector(vertex["position"], 3)
		_, normalOK := exportMeshVector(vertex["normal"], 3)
		uv, uvOK := exportMeshVector(vertex["uv"], 2)
		if !positionOK || !normalOK || !uvOK {
			return false
		}
		u, v := uv[0], uv[1]
		uMin, vMin, uMax, vMax = min(uMin, u), min(vMin, v), max(uMax, u), max(vMax, v)
	}
	bounds, ok := face["uv_bounds"].(map[string]any)
	if !ok {
		return false
	}
	// ObjVertexConsumer writes bounds at float precision; the vertex UVs in
	// JSON are the same floats widened to doubles by its vector writer.
	return exportMeshBound(bounds, "u_min", uMin) && exportMeshBound(bounds, "u_max", uMax) &&
		exportMeshBound(bounds, "v_min", vMin) && exportMeshBound(bounds, "v_max", vMax)
}

func exportMeshBound(bounds map[string]any, key string, expected float32) bool {
	n, ok := bounds[key].(float64)
	return ok && float32(n) == expected
}

func exportMeshVector(value any, size int) ([3]float32, bool) {
	var result [3]float32
	vector, ok := value.([]any)
	if !ok || len(vector) != size {
		return result, false
	}
	for index, component := range vector {
		n, ok := component.(float64)
		if !ok || math.Abs(n) > math.MaxFloat32 {
			return result, false
		}
		result[index] = float32(n)
	}
	return result, true
}
