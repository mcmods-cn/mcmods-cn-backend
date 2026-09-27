package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestOSSReadsWritesAndCleanupHaveObservableFailureContracts(t *testing.T) {
	handlersRaw, err := os.ReadFile("oss_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	adminRaw, err := os.ReadFile("oss_admin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	deletionRaw, err := os.ReadFile("oss_deletion_outbox.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, admin, deletion := string(handlersRaw), string(adminRaw), string(deletionRaw)
	for _, forbidden := range []string{
		"existing, exists = s.findExistingOSSFileByHashExact",
		"func (s *Server) findExistingOSSFileByHashExact(ctx context.Context, sha256 string, sizeBytes int64, lookup ossFileHashLookup) (map[string]any, bool)",
		"func (s *Server) findCompletedOSSUpload(ctx context.Context, uploaderID int64, req ossCompleteUploadRequest) (int64, map[string]any, bool)",
		"_ = ignoreNoRows(err)\n\t\treturn nil, false",
		"_, _ = s.db.Exec(\n\t\tr.Context(),\n\t\t`insert into oss_download_stats",
		"_, _ = s.db.Exec(\n\t\tr.Context(),\n\t\t`insert into oss_scan_logs",
		"_, _ = client.DeleteObject",
	} {
		if strings.Contains(handlers+admin, forbidden) {
			t.Fatalf("OSS path still hides a failure: %q", forbidden)
		}
	}
	for _, required := range []string{
		"if err = rows.Err(); err != nil",
		"existing, exists, err = s.findExistingOSSFileByHashExact",
		"func (s *Server) findExistingOSSFileByHashExact(ctx context.Context, sha256 string, sizeBytes int64, lookup ossFileHashLookup) (map[string]any, bool, error)",
		"func (s *Server) findCompletedOSSUpload(ctx context.Context, uploaderID int64, req ossCompleteUploadRequest) (int64, map[string]any, bool, error)",
		"recordOSSScanLog",
		"recordOSSDownloadStat",
		"enqueueUnregisteredOSSObjectDeletion",
		"context.WithoutCancel(ctx)",
	} {
		if !strings.Contains(handlers+admin+deletion, required) {
			t.Fatalf("OSS path is missing failure contract %q", required)
		}
	}
	if strings.Contains(handlers, "client.DeleteObject") || strings.Contains(admin, "client.DeleteObject") {
		t.Fatal("request-time OSS cleanup still deletes directly instead of using the durable outbox")
	}
}
