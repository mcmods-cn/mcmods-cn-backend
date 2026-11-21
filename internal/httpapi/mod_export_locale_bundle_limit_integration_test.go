package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A catalog-sized dictionary of ordinary text tooltips. The last note brings
// the standard JSON encoder's dictionary, including its newline, to size.
func catalogLocaleOfSize(t *testing.T, size int) map[string]string {
	t.Helper()
	const description = "Put a map into the first slot. Its preview shows the scale, the nearest landmark and the direction of north. "
	const tailKey = "guide.example.note"
	values := map[string]string{}
	used := 2 + 6 + len(tailKey) // delimiters/newline, then the final entry
	for i := 0; ; i++ {
		key := fmt.Sprintf("guide.example.map_%06d", i)
		value := description + fmt.Sprintf("Map number %d.", i)
		entrySize := 6 + len(key) + len(value)
		if used+entrySize > size {
			break
		}
		values[key] = value
		used += entrySize
	}
	values[tailKey] = strings.Repeat("Map ", (size-used+3)/4)[:size-used]
	raw, err := json.Marshal(values)
	if err != nil || len(raw)+1 != size {
		t.Fatalf("locale fixture size=%d, want=%d, err=%v", len(raw)+1, size, err)
	}
	return values
}

func TestExportLocaleLimitPreservesPublishedCatalogIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	// Exactly 16 MiB is readable. One more character in the next revision
	// remains within the source JSON and ZIP admission limits.
	values := catalogLocaleOfSize(t, int(maxExportLocaleBundleSize))
	raw, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	_, active := f.imported(t, auditFixGRZip(t, map[string]string{"translations/ko_kr.json": string(raw)}))
	f.reviewGR(t, active, "approved")
	note := values["guide.example.note"]
	values["guide.example.note"] = note + "."
	raw, err = json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	job := f.job(t, f.upload(t, auditFixGRZip(t, map[string]string{"translations/ko_kr.json": string(raw)})))
	err = f.server.importMCModsExportJob(f.ctx, job.ID)
	if err == nil {
		t.Fatal("worker accepted an unreadable dictionary")
	}
	if !strings.Contains(err.Error(), "ko-KR") || !strings.Contains(err.Error(), "16777216") {
		t.Fatalf("expected locale and uncompressed limit in import error, got %v", err)
	}
	status := f.require(t, f.editor, "GET", "/api/v1/mods/audit-gr-"+f.modCode+"/export-imports/"+job.ID, nil, 200)
	failed := decodeTEST022Data[modExportJobResponse](t, status)
	if failed.Status != "failed" || failed.ErrorCode != "import_failed" || failed.ErrorDetail["message"] != err.Error() {
		t.Fatalf("creator did not receive the import error: status=%s code=%s", failed.Status, failed.ErrorCode)
	}
	if n := f.number(t, `select count(*) from catalog_import_revisions where job_id=$1`, job.ID); n != 0 {
		t.Fatalf("new revisions after rejection = %d", n)
	}
	if n := f.number(t, `select count(*) from catalog_import_revisions where id=$1 and is_active`, active); n != 1 {
		t.Fatalf("previous active revisions = %d", n)
	}
	original, err := f.server.loadExportLocaleBundle(f.ctx, active, "ko-KR")
	if err != nil || len(original) != len(values) || original["guide.example.note"] != note {
		t.Fatalf("previous dictionary size=%d, note preserved=%t, cold read error=%v", len(original), original["guide.example.note"] == note, err)
	}
}
