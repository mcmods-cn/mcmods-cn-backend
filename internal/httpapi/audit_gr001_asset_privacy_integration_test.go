package httpapi

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strings"
	"testing"
)

const auditGR001PrivateMarker = "AUDIT_UNRELATED_PRIVATE_GR001"

func TestAuditGR001UnknownAttachmentFailsBeforeCatalogWritesIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	file := f.upload(t, auditFixGRZip(t, map[string]string{"debug/private.json": `{"machine_path":"/home/synthetic/.minecraft","access_token":"` + auditGR001PrivateMarker + `","unknown_business_extension":{"value":3}}`}))
	job := f.job(t, file)
	err := f.server.importMCModsExportJob(f.ctx, job.ID)
	if err == nil || !strings.Contains(err.Error(), "unregistered_export_asset") {
		t.Errorf("expected precise unregistered asset rejection, got %v", err)
	}
	var status, detail string
	if err := f.db.QueryRow(f.ctx, `select status,error_detail::text from catalog_import_jobs where id=$1`, job.ID).Scan(&status, &detail); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(detail, "unregistered_export_asset") || strings.Contains(detail, auditGR001PrivateMarker) {
		t.Errorf("rejected job status=%s diagnostic=%s", status, detail)
	}
	if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job.ID); n != 0 {
		t.Errorf("unregistered attachment produced revisions=%d", n)
	}
	if n := f.number(t, `select count(*) from catalog_import_text_assets where json_content::text like $1`, "%"+auditGR001PrivateMarker+"%"); n != 0 {
		t.Errorf("private attachment persisted to public-capable asset table=%d", n)
	}
	if n := f.number(t, `select count(*) from mod_resource_version_details where version_id=$1`, f.versionID); n != 0 {
		t.Errorf("failed package produced public projection=%d", n)
	}
	f.require(t, "", "GET", "/api/v1/oss/files/"+file+"/content", nil, 404)
	t.Log("unregistered attachment: deterministic failed job, no catalog revision/public projection/private diagnostic; original owner source remains private")
}

func TestAuditGR001PartialFallbackAndRenderAssetsAreProjectedIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	paths := map[string]string{
		"manifest.json":                              `{"schema_version":"mcmods-export/v1","exporter_version":"audit-synthetic-legacy-v6","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","configuration":{"namespaces":["example"],"languages":["en-US"],"profile":"all"},"stages":[],"errors":[{"stage":"registries","message":"` + auditGR001PrivateMarker + `","access_token":"` + auditGR001PrivateMarker + `"}],"error_count":1}`,
		"compatibility/capabilities.json":            `{"schema_version":"mcmods-capabilities/v1","minecraft_version":"1.20.1","loader":"forge","format_contract":"mcmods-export/v1","capabilities":[{"id":"registry","status":"available","source":"runtime","access_token":"` + auditGR001PrivateMarker + `","nested":{"machine_path":"` + auditGR001PrivateMarker + `"}}]}`,
		"advancements/advancements.json":             `{"schema_version":"mcmods-advancements/v2","advancements":[{"id":"example:partial","namespace":"example","normalization_status":"partial","display":{"title_names":{"en-US":"Business advancement"},"icon":{"item":"example:audit_item"},"machine_path":"` + auditGR001PrivateMarker + `"},"criteria":["done"],"requirements":[["done"]],"raw_definition_path":"data/example/advancements/partial.json","access_token":"` + auditGR001PrivateMarker + `"}]}`,
		"data/example/advancements/partial.json":     `{"display":{"title":"Business title","icon":{"item":"example:audit_item"},"machine_path":"` + auditGR001PrivateMarker + `"},"criteria":{"done":{"trigger":"minecraft:impossible"}},"requirements":[["done"]],"rewards":{"experience":3},"access_token":"` + auditGR001PrivateMarker + `"}`,
		"assets/example/models/item/audit_item.json": `{"parent":"minecraft:item/generated","textures":{"layer0":"example:item/audit_item"},"display":{"gui":{"rotation":[0,90,0],"translation":[0,1,0],"scale":[1,1,1],"access_token":"` + auditGR001PrivateMarker + `"}},"machine_path":"` + auditGR001PrivateMarker + `"}`,
		"assets/example/models/item/audit_item.obj":  "# " + auditGR001PrivateMarker + "\nmtllib audit_item.mtl\no audit_item\nv 0 0 0\nv 1 0 0\nv 0 1 0\nvt 0 0\nvt 1 0\nvt 0 1\nvn 0 0 1\nf 1/1/1 2/2/1 3/3/1\n",
		"assets/example/models/item/audit_item.mtl":  "# " + auditGR001PrivateMarker + "\nnewmtl material0\nKd 1 0.5 0\nmap_Kd ../../textures/item/audit_item.png\n",
	}
	imagePath := "assets/example/textures/item/audit_item.png"
	pixels := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	pixels.SetNRGBA(0, 0, color.NRGBA{R: 39, G: 78, B: 91, A: 50})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, pixels); err != nil {
		t.Fatal(err)
	}
	plain := encoded.Bytes()
	payload := []byte("Comment\x00machine_path=/home/synthetic;access_token=" + auditGR001PrivateMarker)
	chunk := make([]byte, len(payload)+12)
	binary.BigEndian.PutUint32(chunk[:4], uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:], payload)
	binary.BigEndian.PutUint32(chunk[len(chunk)-4:], crc32.ChecksumIEEE(chunk[4:len(chunk)-4]))
	enriched := append(append(append([]byte(nil), plain[:len(plain)-12]...), chunk...), plain[len(plain)-12:]...)
	paths[imagePath] = string(enriched)
	_, revision := f.imported(t, auditFixGRZip(t, paths))
	for _, table := range []string{"catalog_import_packages", "catalog_import_text_assets", "resource_import_snapshots", "catalog_import_capabilities"} {
		if n := f.number(t, "select count(*) from "+table+" row where to_jsonb(row)::text like $1", "%"+auditGR001PrivateMarker+"%"); n != 0 {
			t.Errorf("private marker persisted in %s rows=%d", table, n)
		}
	}
	f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape("data/example/advancements/partial.json"), nil, 403)
	f.reviewGR(t, revision, "approved")
	for assetPath := range paths {
		if assetPath == "manifest.json" {
			continue
		}
		raw := f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape(assetPath), nil, 200)
		if bytes.Contains(raw, []byte(auditGR001PrivateMarker)) {
			t.Errorf("public raw bytes leaked metadata in %s", assetPath)
		}
		if strings.HasSuffix(assetPath, ".json") {
			var data any
			if json.Unmarshal(raw, &data) != nil {
				t.Errorf("projected asset is not JSON: %s", assetPath)
			}
		}
	}
	rawImage := f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape(imagePath)+"&proxy=renderer", nil, 200)
	decoded, err := png.Decode(bytes.NewReader(rawImage))
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			if decoded.At(x, y) != pixels.At(x, y) {
				t.Errorf("PNG pixel/alpha changed %d/%d", x, y)
			}
		}
	}
	if !bytes.Equal(rawImage, plain) {
		t.Error("derived PNG differs from original required pixel/color chunks")
	}
	var storedHash string
	if err := f.db.QueryRow(f.ctx, `select file.sha256 from catalog_import_media media join oss_files file on file.id=media.oss_file_id where revision_id=$1 and asset_path=$2`, revision, imagePath).Scan(&storedHash); err != nil {
		t.Fatal(err)
	}
	if storedHash != sha256Hex(plain) {
		t.Error("derived media hash does not match actual sanitized bytes")
	}
	raw := f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape("data/example/advancements/partial.json"), nil, 200)
	for _, business := range []string{"Business title", "example:audit_item", "minecraft:impossible", "experience"} {
		if !bytes.Contains(raw, []byte(business)) {
			t.Errorf("partial fallback lost business value %s", business)
		}
	}
	raw = f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape("assets/example/models/item/audit_item.obj"), nil, 200)
	for _, business := range []string{"mtllib audit_item.mtl", "v 1 0 0", "f 1/1/1 2/2/1 3/3/1"} {
		if !bytes.Contains(raw, []byte(business)) {
			t.Errorf("OBJ lost geometry/material reference %s", business)
		}
	}
	t.Log("HTTP upload -> real PostgreSQL/direct business Worker -> independent review -> anonymous actual asset bytes: projected partial JSON/model/OBJ/MTL retain business values; no private marker")
}

func TestAuditGR001NestedCatalogJSONPurposeProjectionIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{
		"advancements/advancements.json":   `{"schema_version":"mcmods-advancements/v2","advancements":[{"id":"example:reward","namespace":"example","display":{"title_names":{"en-US":"Reward title"},"icon":{"item":"example:audit_item"}},"criteria":["done"],"requirements":[["done"]],"rewards":{"experience":3,"access_token":"` + auditGR001PrivateMarker + `"}}]}`,
		"worldgen/loot_tables.json":        `{"schema_version":"mcmods-loot-tables/v2","loot_tables":[{"id":"example:chests/audit","namespace":"example","type":"minecraft:chest","pools":[{"rolls":2,"entries":[{"type":"minecraft:item","name":"example:audit_item","weight":3,"conditions":[{"condition":"minecraft:random_chance","chance":0.5,"access_token":"` + auditGR001PrivateMarker + `"}],"functions":[{"function":"minecraft:set_count","count":2}]}]}]}]}`,
		"worldgen/natural_generation.json": `{"schema_version":"mcmods-natural-generation/v2","export_mode":"normalized_catalog","entries":[{"id":"example:ore","generation_id":"example:ore","namespace":"example","entry_kind":"placed_feature","normalization_status":"complete","outputs":[{"block_id":"minecraft:diamond_ore","machine_path":"` + auditGR001PrivateMarker + `"}],"size":3,"placement":{"count":2,"height":{"type":"minecraft:uniform","min_inclusive":{"absolute":0},"max_inclusive":{"absolute":32}},"machine_path":"` + auditGR001PrivateMarker + `"}}]}`,
	}))
	if n := f.number(t, `select count(*) from resource_import_snapshots where revision_id=$1 and data::text like $2`, revision, "%"+auditGR001PrivateMarker+"%"); n != 0 {
		t.Errorf("nested unrelated metadata persisted in %d snapshots", n)
	}
	f.reviewGR(t, revision, "approved")
	rows, err := f.db.Query(f.ctx, `select entity.public_id,snapshot.data::text from resource_import_snapshots snapshot join catalog_entities entity on entity.id=snapshot.resource_id where snapshot.revision_id=$1 and snapshot.registry in ('advancements','loot_tables','natural_generation')`, revision)
	if err != nil {
		t.Fatal(err)
	}
	type resource struct{ id, data string }
	resources := []resource{}
	for rows.Next() {
		var r resource
		if err := rows.Scan(&r.id, &r.data); err != nil {
			t.Fatal(err)
		}
		resources = append(resources, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(resources) != 3 {
		t.Fatalf("registered nested catalogs=%d", len(resources))
	}
	var combined strings.Builder
	for _, r := range resources {
		raw := f.require(t, "", "GET", "/api/v1/mods/audit-gr-"+f.modCode+"/content-resources/"+r.id+"?version="+f.versionPublic, nil, 200)
		if bytes.Contains(raw, []byte(auditGR001PrivateMarker)) {
			t.Errorf("nested metadata publicly exposed for %s", r.id)
		}
		combined.Write(raw)
	}
	for _, business := range []string{"experience", "minecraft:random_chance", "0.5", "minecraft:set_count", "minecraft:diamond_ore"} {
		if !strings.Contains(combined.String(), business) {
			t.Errorf("purpose projection lost necessary business %s", business)
		}
	}
}

func TestAuditGR001BlockEntityModelJSONUsesTypedProjectionIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	meshPath := "block_entities/models/example/chest/default.mesh.json"
	objPath := "block_entities/models/example/chest/default.obj"
	vertex := map[string]any{"position": []int{0, 0, 0}, "normal": []int{0, 0, 1}, "uv": []int{0, 0}, "access_token": auditGR001PrivateMarker}
	mesh, _ := json.Marshal(map[string]any{"schema_version": blockEntityMeshSchema, "coordinate_space": "block_units", "uv_space": "source_texture_normalized", "uv_origin": "bottom_left", "vertex_count": 4, "quad_count": 1, "trailing_vertex_count": 0, "faces": []any{map[string]any{"index": 0, "direction": "south", "uv_bounds": map[string]int{"u_min": 0, "v_min": 0, "u_max": 0, "v_max": 0}, "vertices": []any{vertex, vertex, vertex, vertex}, "machine_path": auditGR001PrivateMarker}}, "access_token": auditGR001PrivateMarker})
	index, _ := json.Marshal(map[string]any{"schema_version": blockEntityIndexSchema, "coordinate_space": "block_units", "count": 1, "block_entities": []any{map[string]any{"block_id": "example:chest", "block_entity_type_id": "example:chest", "model_source": "runtime_renderer", "model_available": true, "variant_count": 1, "machine_path": auditGR001PrivateMarker, "variants": []any{map[string]any{"variant_id": "default", "obj": objPath, "mesh": meshPath, "vertex_count": 4, "quad_count": 1, "uv_space": "source_texture_normalized", "uv_complete": true, "texture_count": 0, "textures": []any{}, "access_token": auditGR001PrivateMarker}}}}})
	_, revision := f.imported(t, auditFixGRZip(t, map[string]string{
		"registries/blocks.json":      `{"registry":"blocks","entries":[{"id":"example:chest","namespace":"example","names":{"en-US":"Business chest"},"hardness":2}]}`,
		modExportBlockEntityIndexPath: string(index), meshPath: string(mesh), objPath: "# " + auditGR001PrivateMarker + "\no chest\nv 0 0 0\nv 1 0 0\nv 1 1 0\nv 0 1 0\nf 1 2 3 4\n",
	}))
	for _, table := range []string{"block_entity_model_snapshots", "block_entity_model_variants"} {
		if n := f.number(t, "select count(*) from "+table+" row where to_jsonb(row)::text like $1", "%"+auditGR001PrivateMarker+"%"); n != 0 {
			t.Errorf("model raw JSON retained private data in %s=%d", table, n)
		}
	}
	if n := f.number(t, `select count(*) from block_entity_model_variants variant join block_entity_model_snapshots model on model.id=variant.model_snapshot_id where model.revision_id=$1 and variant.vertex_count=4 and variant.quad_count=1 and jsonb_array_length(variant.mesh_data->'faces')=1`, revision); n != 1 {
		t.Errorf("mesh geometry lost: variants=%d", n)
	}
	f.reviewGR(t, revision, "approved")
	for _, p := range []string{objPath} {
		raw := f.require(t, "", "GET", "/api/v1/export-revisions/"+revision+"/assets/content?path="+url.QueryEscape(p), nil, 200)
		if bytes.Contains(raw, []byte(auditGR001PrivateMarker)) || !bytes.Contains(raw, []byte("f 1 2 3 4")) {
			t.Error("public model geometry/private metadata projection incorrect")
		}
	}
}

func TestAuditGR001UnknownBusinessExtensionIsExplicitlyRejectedIntegration(t *testing.T) {
	for _, test := range []struct{ name, path, data string }{
		{"model_codec", "assets/example/models/item/audit_item.json", `{"parent":"minecraft:item/generated","hologram_animation":{"frames":12}}`},
		{"raw_trigger_conditions", "data/example/advancements/custom.json", `{"criteria":{"done":{"trigger":"example:custom","conditions":{"quest_phase":3}}}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAuditFixGRFixture(t)
			overrides := map[string]string{test.path: test.data}
			if test.name == "raw_trigger_conditions" {
				overrides["advancements/advancements.json"] = `{"schema_version":"mcmods-advancements/v2","advancements":[{"id":"example:custom","namespace":"example","normalization_status":"partial","raw_definition_path":"` + test.path + `","display":{"title_names":{"en-US":"Custom business"}},"criteria":["done"]}]}`
			}
			job := f.job(t, f.upload(t, auditFixGRZip(t, overrides)))
			err := f.server.importMCModsExportJob(f.ctx, job.ID)
			if err == nil || !strings.Contains(err.Error(), "unregistered_export_asset") {
				t.Fatalf("unknown business extension silently accepted/dropped: %v", err)
			}
			if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job.ID); n != 0 {
				t.Errorf("unregistered extension wrote revisions=%d", n)
			}
		})
	}
}

func TestAuditGR001ReadOnlyDiagnosticScopesAndRedactsLegacyCandidatesIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	_, revision := f.imported(t, auditFixGRZip(t, nil))
	// A synthetic copy of the historical persisted row models an upgrade
	// candidate. It is not claimed as a reachable post-fix import operation.
	if _, err := f.db.Exec(f.ctx, `insert into catalog_import_text_assets(revision_id,asset_path,asset_kind,content_type,sha256,byte_length,json_content) values($1,'debug/legacy.json','json','application/json',$2,32,$3::jsonb)`, revision, strings.Repeat("a", 64), `{"access_token":"`+auditGR001PrivateMarker+`"}`); err != nil {
		t.Fatal(err)
	}
	report, err := DiagnoseImportPrivacy(f.ctx, f.db, revision, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ReadOnly || report.Truncated || len(report.Findings) != 1 || report.Findings[0].RevisionID != revision || report.Findings[0].Reason != "KNOWN_UNRELATED_PRIVATE_METADATA_KEY" {
		t.Fatalf("diagnostic scope=%+v", report)
	}
	raw, _ := json.Marshal(report)
	if bytes.Contains(raw, []byte(auditGR001PrivateMarker)) || bytes.Contains(raw, []byte("debug/legacy.json")) {
		t.Error("diagnostic report exposed private values/path")
	}
	if n := f.number(t, `select count(*) from catalog_import_text_assets where revision_id=$1 and asset_path='debug/legacy.json' and json_content->>'access_token'=$2`, revision, auditGR001PrivateMarker); n != 1 {
		t.Error("read-only diagnostic changed original row")
	}
	other, err := DiagnoseImportPrivacy(f.ctx, f.db, "absent-revision", 1000)
	if err != nil || other.Scanned != 0 || len(other.Findings) != 0 {
		t.Fatalf("cross-scope diagnostic=%+v/%v", other, err)
	}
	limited, err := DiagnoseImportPrivacy(f.ctx, f.db, revision, 1)
	if err != nil || !limited.Truncated || limited.Scanned != 1 {
		t.Fatalf("bounded diagnostic=%+v/%v", limited, err)
	}
}
