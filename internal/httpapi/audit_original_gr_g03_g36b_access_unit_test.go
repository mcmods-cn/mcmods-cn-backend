package httpapi

// Faithful current ports of original GR-G03 and GR-G36B. These are unit
// projections only: no upload, HTTP, database, Worker, vendor or game fixture.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"testing"
)

func TestAuditOriginalGRG03ManifestUnknownFieldsPreserveKnownGolden(t *testing.T) {
	// The expected document is a hand-authored contract fixture, not output
	// from another production normalization or serialization helper.
	const known = `{"schema_version":"mcmods-export/v1","exporter_version":"0.7.0","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","configuration":{"namespaces":["example"],"all_namespaces":false,"languages":["en-US"],"image_sizes":[32,128,256],"profile":"default","automatic":false,"modules":["registries","jei_recipes"]},"stages":[{"id":"registry","status":"completed"}],"errors":[],"error_count":0}`
	const enriched = `{"schema_version":"mcmods-export/v1","exporter_version":"0.7.0","status":"complete","minecraft_version":"1.20.1","loader":"forge","package_format":"zip","machine_path":"AUDIT_SYNTHETIC_UNRELATED_PRIVATE_TOP","configuration":{"namespaces":["example"],"all_namespaces":false,"languages":["en-US"],"image_sizes":[32,128,256],"profile":"default","automatic":false,"modules":["registries","jei_recipes"],"access_token":"AUDIT_SYNTHETIC_UNRELATED_PRIVATE_CONFIG"},"stages":[{"id":"registry","status":"completed","private_debug_log":"AUDIT_SYNTHETIC_UNRELATED_PRIVATE_STAGE"}],"errors":[],"error_count":0}`
	var expected map[string]any
	if err := json.Unmarshal([]byte(known), &expected); err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		name string
		raw  string
	}{
		{name: "clean_known_golden", raw: known},
		{name: "unknown_top_configuration_and_stage", raw: enriched},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var manifest modExportManifest
			if err := json.Unmarshal([]byte(sample.raw), &manifest); err != nil {
				t.Fatal(err)
			}
			if err := validateModExportManifest(manifest); err != nil {
				t.Fatalf("valid manifest rejected: %v", err)
			}
			encoded, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{
				"AUDIT_SYNTHETIC_UNRELATED_PRIVATE_TOP",
				"AUDIT_SYNTHETIC_UNRELATED_PRIVATE_CONFIG",
				"AUDIT_SYNTHETIC_UNRELATED_PRIVATE_STAGE",
				"machine_path", "access_token", "private_debug_log",
			} {
				if bytes.Contains(encoded, []byte(private)) {
					t.Fatalf("unknown manifest field or marker retained: %s", private)
				}
			}
			var actual map[string]any
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actual, expected) {
				t.Fatalf("known manifest projection changed: got %s, want %s", encoded, known)
			}
			t.Log("GR-G03: valid identity/configuration/stage golden retained; three unknown private locations absent; unit only")
		})
	}
}

func TestAuditOriginalGRG36BEmbeddedPNGMetadataPreservesThreeBusinessImages(t *testing.T) {
	raw := auditOriginalAccessG36BPNGWithPrivateText(t)
	encoded := base64.StdEncoding.EncodeToString(raw)
	entry := embeddedIconCatalogEntry{
		Name: "合成图标", EnglishName: "Audit image", RegisterName: "example:audit_item",
		Type: "item", SmallIcon: encoded, LargeIcon: encoded, MaxStackSize: 64,
	}
	build, err := buildEmbeddedIconImport(context.Background(), defaultOSSConfig(), "audit-project", iconRendererImportSource,
		catalogResourceIdentityResolver{}, map[string]string{"example": "audit-revision"}, entry)
	if err != nil {
		t.Fatal(err)
	}
	if len(build.media) != 3 || len(build.data) != 3 {
		t.Fatalf("builder produced media=%d/data=%d, want exactly three of each", len(build.media), len(build.data))
	}

	// Fixed samples from the 2x2 input below form the image oracle. No
	// production renderer, selector, minimizer or scale formula computes it.
	type pixel struct {
		x, y  int
		value color.NRGBA
	}
	expected := map[string]struct {
		size    int
		samples []pixel
	}{
		"icons/items/32/example/audit_item.png": {
			size: 32,
			samples: []pixel{{0, 0, color.NRGBA{B: 180, A: 255}}, {31, 0, color.NRGBA{R: 1, B: 180, A: 255}},
				{0, 31, color.NRGBA{G: 1, B: 180, A: 255}}, {16, 16, color.NRGBA{R: 1, G: 1, B: 180, A: 255}}},
		},
		"icons/items/128/example/audit_item.png": {
			size: 128,
			samples: []pixel{{0, 0, color.NRGBA{B: 180, A: 255}}, {127, 0, color.NRGBA{R: 1, B: 180, A: 255}},
				{0, 127, color.NRGBA{G: 1, B: 180, A: 255}}, {64, 64, color.NRGBA{R: 1, G: 1, B: 180, A: 255}}},
		},
		"icons/items/256/example/audit_item.png": {
			size: 256,
			samples: []pixel{{0, 0, color.NRGBA{B: 180, A: 255}}, {255, 0, color.NRGBA{R: 1, B: 180, A: 255}},
				{0, 255, color.NRGBA{G: 1, B: 180, A: 255}}, {128, 128, color.NRGBA{R: 1, G: 1, B: 180, A: 255}}},
		},
	}
	seen := map[string]bool{}
	for _, media := range build.media {
		want, exists := expected[media.AssetPath]
		if !exists || seen[media.AssetPath] {
			t.Fatalf("unexpected or repeated asset path: %s", media.AssetPath)
		}
		seen[media.AssetPath] = true
		data, exists := build.data[media.AssetPath]
		if !exists || len(data) == 0 {
			t.Fatalf("no actual bytes for %s", media.AssetPath)
		}
		for _, private := range []string{"AUDIT_SYNTHETIC_UNRELATED_PRIVATE", "/home/audit-player", "machine_path", "access_token"} {
			if bytes.Contains(data, []byte(private)) {
				t.Fatalf("private PNG text retained in %s: %s", media.AssetPath, private)
			}
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("invalid PNG %s: %v", media.AssetPath, err)
		}
		if decoded.Bounds() != image.Rect(0, 0, want.size, want.size) || media.Width != want.size || media.Height != want.size {
			t.Fatalf("wrong actual/stored dimensions for %s: %v/%dx%d", media.AssetPath, decoded.Bounds(), media.Width, media.Height)
		}
		for _, sample := range want.samples {
			actual := color.NRGBAModel.Convert(decoded.At(sample.x, sample.y)).(color.NRGBA)
			if actual != sample.value {
				t.Fatalf("business pixel changed in %s at %d,%d: got %#v, want %#v", media.AssetPath, sample.x, sample.y, actual, sample.value)
			}
		}
		digest := sha256.Sum256(data)
		if media.RevisionID != "audit-revision" || media.ByteLength != int64(len(data)) || media.Digest != hex.EncodeToString(digest[:]) {
			t.Fatalf("media references/hash/size do not describe actual asset %s", media.AssetPath)
		}
	}
	for asset := range expected {
		if !seen[asset] {
			t.Fatalf("required image absent: %s", asset)
		}
	}
	var names map[string]string
	if err := json.Unmarshal([]byte(build.resource.Names), &names); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, map[string]string{"zh-CN": "合成图标", "en-US": "Audit image"}) {
		t.Fatalf("business names lost: %s", build.resource.Names)
	}
	var business struct {
		ID           string            `json:"id"`
		Source       string            `json:"source"`
		MaxStackSize int               `json:"maxStackSize"`
		IconVariants map[string]string `json:"iconVariants"`
	}
	if err := json.Unmarshal([]byte(build.resource.Data), &business); err != nil {
		t.Fatal(err)
	}
	wantVariants := map[string]string{"32": "icons/items/32/example/audit_item.png", "128": "icons/items/128/example/audit_item.png", "256": "icons/items/256/example/audit_item.png"}
	if business.ID != "example:audit_item" || business.Source != "iconrenderer" || business.MaxStackSize != 64 || !reflect.DeepEqual(business.IconVariants, wantVariants) {
		t.Fatalf("business identity/stack/image references lost: %s", build.resource.Data)
	}
	if build.resource.IconPath != wantVariants["32"] || build.resource.PreviewPath != wantVariants["256"] {
		t.Fatal("resource icon/preview does not reference the built images")
	}
	t.Log("GR-G36B: same private-text 2x2 PNG built into 32/128/256 images; actual bytes/pixels, names, maxStack64 and references checked; builder unit only")
}

func auditOriginalAccessG36BPNGWithPrivateText(t *testing.T) []byte {
	t.Helper()
	// Exact 198-byte original continuation-private-text.png from the audit
	// attachment, including its valid tEXt chunk. Do not regenerate it using
	// a production renderer or metadata minimizer.
	const original = "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAGklEQVR4nGJhYNjCyMDAwsDIwMDAAAgAAP//CC0Av8OlcuEAAABndEVYdEF1ZGl0TWV0YWRhdGEAbWFjaGluZV9wYXRoPS9ob21lL2F1ZGl0LXBsYXllci8ubWluZWNyYWZ0O2FjY2Vzc190b2tlbj1BVURJVF9TWU5USEVUSUNfVU5SRUxBVEVEX1BSSVZBVEVTVCuYAAAAAElFTkSuQmCC"
	raw, err := base64.StdEncoding.DecodeString(original)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if len(raw) != 198 || hex.EncodeToString(digest[:]) != "d2a28ddfbdad3ceb283acdaafdd551d59c03731416048ab48b67c5838fd81345" {
		t.Fatal("original PNG fixture bytes changed")
	}
	payload := []byte("AuditMetadata\x00machine_path=/home/audit-player/.minecraft;access_token=AUDIT_SYNTHETIC_UNRELATED_PRIVATE")
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil || decoded.Bounds() != image.Rect(0, 0, 2, 2) || !bytes.Contains(raw, payload) {
		t.Fatalf("invalid private-text PNG fixture: %v", err)
	}
	t.Logf("GR-G36B original source PNG: bytes=%d sha256=%s", len(raw), hex.EncodeToString(digest[:]))
	return raw
}
