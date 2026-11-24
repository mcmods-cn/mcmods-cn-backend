package httpapi

// Exact original GR-G09/G48/G60 input groups, rebound to real current helpers.
// These tests use no database, HTTP, Worker, browser or provider fixture. They
// do not define policy for arbitrary NBT, unknown business extensions or IRR
// variant storage/review/publication.
import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAuditRemainingOriginalGRG09BoundedZIPMatrix(t *testing.T) {
	if maxExportFileCount != 200_000 || maxExportSingleFileSize != 512<<20 {
		t.Fatal("original fixed 200000-entry/512MiB boundaries changed; investigate contract before rebinding")
	}
	paths := []struct {
		name, path, want string
	}{
		{"parent", "../private.json", "unsafe ZIP path: \"../private.json\""},
		{"absolute", "/absolute.json", "unsafe ZIP path: \"/absolute.json\""},
		{"drive", "C:/audit.json", "unsafe ZIP path: \"C:/audit.json\""},
		{"backslash", "folder\\secret.json", "unsafe ZIP path: \"folder\\\\secret.json\""},
		{"nested_parent", "a/../../secret.json", "unsafe ZIP path: \"a/../../secret.json\""},
		{"nul", "bad\x00file.json", "unsafe ZIP path: \"bad\\x00file.json\""},
	}
	for _, sample := range paths {
		t.Run(sample.name, func(t *testing.T) {
			got, err := validateExportZIP([]*zip.File{{FileHeader: zip.FileHeader{
				Name: sample.path, UncompressedSize64: 16, CompressedSize64: 16,
			}}})
			if err == nil || err.Error() != sample.want || got != nil {
				t.Fatalf("input %q: got map=%v err=%v; want nil map and exact %q", sample.path, got, err, sample.want)
			}
		})
	}
	tests := []struct {
		name  string
		files []*zip.File
		want  string
	}{
		{"duplicate_case_fold", []*zip.File{
			{FileHeader: zip.FileHeader{Name: "Registry/a.json"}},
			{FileHeader: zip.FileHeader{Name: "registry/A.json"}},
		}, "duplicate ZIP path: registry/A.json"},
		{"entry_size_header", []*zip.File{{FileHeader: zip.FileHeader{
			Name: "a.json", UncompressedSize64: uint64(512<<20) + 1,
		}}}, "ZIP entry too large: a.json"},
		{"compression_header_budget", []*zip.File{{FileHeader: zip.FileHeader{
			Name: "a.json", CompressedSize64: 1, UncompressedSize64: 1001,
		}}}, "ZIP compression ratio is unsafe: a.json"},
		// Only 200001 pointers, not ZIP bodies. The count guard must run before
		// it can dereference a nil entry. No 8GiB payload is allocated.
		{"entry_count_header", make([]*zip.File, 200_001), "export ZIP file count is invalid"},
	}
	for _, sample := range tests {
		t.Run(sample.name, func(t *testing.T) {
			got, err := validateExportZIP(sample.files)
			if err == nil || err.Error() != sample.want || got != nil {
				t.Fatalf("got map=%v err=%v; want nil map and exact %q", got, err, sample.want)
			}
		})
	}
	t.Log("GR-G09: six original paths plus case-fold duplicate,512MiB+1 header,1001 ratio and200001-entry pointer slice; bounded unit only")
}

func TestAuditRemainingOriginalGRG60MalformedIRRMatrix(t *testing.T) {
	tests := []struct {
		name, source, raw, want string
	}{
		{"Empty", "irr", "", "catalog import is empty"},
		{"EmptyArray", "irr", "[]", "catalog import contains no entries"},
		{"UnsupportedSource", "unknown", "[]", "unsupported embedded icon catalog importer"},
		{"InvalidResourceID", "irr", "[{\"registerName\":\"bad/id\",\"type\":\"item\"}]", "entry 1 has invalid registerName \"bad/id\""},
		{"InvalidResourceType", "irr", "[{\"registerName\":\"example:audit\",\"type\":\"player\"}]", "entry 1 has unsupported type \"player\""},
		{"WrongNumericType", "irr", "[{\"registerName\":\"example:audit\",\"type\":\"item\",\"maxStackSize\":\"64\"}]", "decode catalog JSON array: json: cannot unmarshal string into Go struct field embeddedIconCatalogEntry.maxStackSize of type int"},
		{"NullEntry", "irr", "[null]", "entry 1 has invalid registerName \"\""},
		{"BadJSONLLine", "irr", "{\"registerName\":\"example:entity\",\"mod\":\"example\"}\n{", "decode catalog JSON line 2: unexpected end of JSON input"},
	}
	for _, sample := range tests {
		t.Run(sample.name, func(t *testing.T) {
			got, err := decodeEmbeddedIconCatalogForSource([]byte(sample.raw), sample.source)
			if err == nil || err.Error() != sample.want || got != nil {
				t.Fatalf("source=%q input=%q: got entries=%v err=%v; want nil entries and original exact %q", sample.source, sample.raw, got, err, sample.want)
			}
		})
	}
	t.Log("GR-G60: original8 literal source/input/full-error assertions; decoder unit only, no BusinessFieldProjection Worker fixture")
}

func TestAuditRemainingOriginalGRG48CandidateProjection(t *testing.T) {
	const private = "AUDIT_SYNTHETIC_UNRELATED_PRIVATE"
	base := exportJEIBinding{
		SlotID: "input_0", IngredientPresent: true, Clickable: true, SemanticRole: "input",
		Alternatives: []map[string]any{{
			"type": "minecraft:item_stack", "item": "example:audit_item", "count": float64(2),
			"nbt_snbt": "{Damage:0}", "machine_path": "/home/audit-player/.minecraft",
			"access_token": private, "player_uuid": private,
		}},
	}
	batch := newModExportWriteBatch()
	if err := queueExportRecipeBindings(batch, catalogResourceIdentityResolver{}, "audit-recipe", "audit-template", []exportJEIBinding{base}, false); err != nil {
		t.Fatal(err)
	}
	if len(batch.recipeBindings) != 1 || len(batch.recipeCandidates) != 1 {
		t.Fatalf("want exactly one slot and candidate, got %d/%d", len(batch.recipeBindings), len(batch.recipeCandidates))
	}
	binding, candidate := batch.recipeBindings[0], batch.recipeCandidates[0]
	if binding.ID == "" || binding.TemplateSlotID == "" || binding.RecipeSnapshotID != "audit-recipe" ||
		binding.SourceSlotID != "input_0" || binding.Ordinal != 0 || !binding.IngredientPresent || !binding.Clickable || binding.SemanticRole != "input" ||
		candidate.BindingID != binding.ID || candidate.AlternativeIndex != 0 {
		t.Fatalf("original candidate/slot association changed: binding=%+v candidate=%+v", binding, candidate)
	}
	if candidate.Amount == nil || *candidate.Amount != 2 || candidate.NBTSNBT != "{Damage:0}" || candidate.IngredientType != "minecraft:item_stack" || candidate.IngredientKind != "item" ||
		candidate.KindCode != "minecraft.item" || candidate.ResourceCanonicalID != "example:audit_item" || candidate.ResourceRawID != "example:audit_item" ||
		candidate.ResourceAliasID != "example:audit_item" || candidate.ResourceNamespace != "example" || candidate.ResourcePath != "audit_item" ||
		!strings.HasPrefix(candidate.ResourceIdentity, "res_") || len(candidate.ResourcePublicID) != 9 {
		t.Fatalf("necessary original business quantity/NBT/identity changed: %+v", candidate)
	}
	encoded, err := json.Marshal(batch.recipeCandidates)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{private, "/home/audit-player", "machine_path", "access_token", "player_uuid"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatalf("original unrelated candidate-root field or marker retained: %s", forbidden)
		}
	}
	t.Log("GR-G48: original3 root private fields absent; amount2/Damage0/item identity/slot retained in actual write projection only; no arbitrary NBT policy")
}
