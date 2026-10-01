package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestResumedModExportCannotOverwriteImmutableObjectIntegration(t *testing.T) {
	s, actor := ossUploadTestServer(t, 1024)
	code := "r" + randomHex(4)
	actor.PermissionRules = append(actor.PermissionRules, security.PermissionRule{Code: "project.edit." + code, Allow: true})
	var modID int64
	if err := s.db.QueryRow(context.Background(), `insert into mods(project_code,slug,primary_name) values($1,$1,'synthetic resume fixture') returning id`, code).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := s.db.Exec(context.Background(), `delete from mods where id=$1`, modID); err != nil {
			t.Error(err)
		}
	})
	cfg := s.ossConfigFromSettings(context.Background())
	for _, size := range []int64{1024, maxOSSUploadBytes + 1} {
		request := ossUploadTestRequest(t, resumeModExportUploadRequest{
			ObjectKey: ossObjectPrefix(cfg.Prefix, ossModImportCategory(code, "mcmods-exporter", "packages")) + "/synthetic.zip", OriginalName: "synthetic.zip", ContentType: "application/zip", SizeBytes: size, SHA256: strings.Repeat("a", 64),
		}, actor)
		request.SetPathValue("siteId", code)
		response := httptest.NewRecorder()
		s.resumeModExportUpload(response, request)
		if size > maxOSSUploadBytes {
			if response.Code != http.StatusBadRequest {
				t.Fatalf("oversized resume was signed: status=%d", response.Code)
			}
			continue
		}
		if response.Code != http.StatusOK {
			t.Fatalf("valid immutable resume rejected: %d %s", response.Code, response.Body.String())
		}
		var value struct {
			Data struct{ Headers map[string]string } `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		found := false
		for key, value := range value.Data.Headers {
			if strings.EqualFold(key, "x-oss-forbid-overwrite") && value == "true" {
				found = true
			}
		}
		if !found {
			t.Fatal("resume dropped the immutable upload condition")
		}
	}
}
