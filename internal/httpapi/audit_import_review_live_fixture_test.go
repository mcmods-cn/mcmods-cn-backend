package httpapi

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Pause the real configured-scope regression after successful current queue
// reads, before any approval. An external native browser supplies its own proof;
// fixture STOP and exit alone never certify browser success.
func auditGR004MaybeHoldBrowser(t *testing.T, f auditFixGRFixture, reviewer, revision, deniedRevision string) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_LIVE_IMPORT_REVIEW") != "1" {
		return
	}
	directory := os.Getenv("MCMODS_AUDIT_IMPORT_REVIEW_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("fresh absolute import review directory required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-import-review pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	frontend := os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if !filepath.IsAbs(frontend) {
		t.Fatal("clean absolute frontend checkout required")
	}
	checkouts := auditLiveSourceCheckouts(strings.TrimSpace(string(top)), frontend)
	revisions := checkouts.revisions(t)
	var schema, jobID, sourceSHA string
	if err := f.db.QueryRow(f.ctx, `select current_schema(),job.id,file.sha256 from catalog_import_revisions revision
		join catalog_import_jobs job on job.id=revision.job_id
		join catalog_import_packages package on package.id=job.package_id
		join oss_files file on file.id=package.archive_file_id where revision.id=$1`, revision).Scan(&schema, &jobID, &sourceSHA); err != nil {
		t.Fatal(err)
	}
	frontendOrigin := os.Getenv("MCMODS_AUDIT_FRONTEND_ORIGIN")
	if frontendOrigin != f.server.cfg.FrontendOrigin || frontendOrigin == "" {
		t.Fatal("live frontend origin does not match actual mounted server config")
	}
	authFile := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authFile, map[string]string{"reviewerToken": reviewer, "creatorToken": f.editor})
	runtime := map[string]any{
		"owner": "mcmods-live-import-review", "pid": os.Getpid(), "state": "READY", "createdAt": time.Now().UTC().Format(time.RFC3339Nano),
		"apiOrigin": f.origin.URL, "frontendOrigin": frontendOrigin, "repoRevisions": revisions, "ownedSchema": schema, "authFile": authFile,
		"queuePath": "/api/v1/admin/mod-content-reviews?category=catalog_import&limit=100", "queueExpectedTotal": 1,
		"revisionId": revision, "jobId": jobID, "sourceSHA256": sourceSHA,
		"projectId": f.modCode, "modSiteId": "audit-gr-" + f.modCode, "targetVersionPublicId": f.versionPublic,
		"expectedNamespace": "example", "expectedTargetLabel": "Audit controlled legacy format",
		"deniedRevisionId": deniedRevision, "deniedProjectId": f.otherModCode,
		"stopPath": filepath.Join(directory, "STOP"),
		"layer":    "ACTUAL_HTTP_POSTGRESQL_DIRECT_WORKER_REAL_PERMISSION_POST_AND_USER_GRANT_PUT_SYNTHETIC_LEGACY_V6; external native browser separate, no NATS or actual game claim",
	}
	auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	t.Logf("IMPORT_REVIEW_READY runtime=%s; actual scoped wildcard, denied project omitted, no browser PASS inferred", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			t.Fatal("owned import review fixture expired before STOP")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(directory, "STOP")); err == nil {
				runtime["state"], runtime["stoppedAt"] = "STOPPED", time.Now().UTC().Format(time.RFC3339Nano)
				auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
				return
			} else if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}
