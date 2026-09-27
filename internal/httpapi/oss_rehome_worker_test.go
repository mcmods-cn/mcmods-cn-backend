package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestOSSRehomeUsesOnlyDurableTransactionalScheduling(t *testing.T) {
	t.Parallel()
	rehomeSource, err := os.ReadFile("oss_rehome.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"make(chan", "sync.Map", "sync.Once", "ossRehomeQueue", "scheduleModGalleryOSSRehome"} {
		if strings.Contains(string(rehomeSource), forbidden) {
			t.Fatalf("legacy process-local rehome scheduler remains: %q", forbidden)
		}
	}
	expectedEnqueues := map[string]int{"mod_handlers.go": 1, "mod_revision_handlers.go": 2}
	for file, expected := range expectedEnqueues {
		source, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if strings.Contains(string(source), "scheduleModGalleryOSSRehome") {
			t.Fatalf("%s still schedules process-local rehome work", file)
		}
		if count := strings.Count(string(source), "enqueueOSSRehomeJobTx("); count != expected {
			t.Fatalf("%s has %d transactional rehome enqueues, want %d", file, count, expected)
		}
	}
	runtimeSource, err := os.ReadFile("../app/runtime.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(runtimeSource), "NewOSSRehomeWorker(cfg, db).Start(ctx)") {
		t.Fatal("application runtime does not start the durable OSS rehome worker")
	}
	routes, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`HandleFunc("GET /api/v1/admin/infrastructure/oss-rehomes", s.requirePermission("admin.config.read", s.adminOSSRehomeJobs))`,
		`HandleFunc("POST /api/v1/admin/infrastructure/oss-rehomes/{id}/replay", s.requirePermission("admin.config.write", s.adminReplayOSSRehome))`,
	} {
		if !strings.Contains(string(routes), required) {
			t.Fatalf("missing permission-guarded OSS rehome route %q", required)
		}
	}
}
