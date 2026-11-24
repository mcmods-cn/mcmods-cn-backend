package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type auditLiveCheckouts struct {
	backend, frontend, exporter string
}

// Discover standard sibling clones relative to the backend being tested.
// A declared worktree (including an existing fixture-specific frontend) wins.
func auditLiveSourceCheckouts(backend, frontend string) auditLiveCheckouts {
	if frontend == "" {
		frontend = os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	}
	if frontend == "" {
		frontend = filepath.Join(filepath.Dir(backend), "mcmods-cn-frontend")
	}
	exporter := os.Getenv("MCMODS_AUDIT_EXPORTER_REPO")
	if exporter == "" {
		exporter = filepath.Join(filepath.Dir(backend), "mcmods_exporter")
	}
	return auditLiveCheckouts{backend: backend, frontend: frontend, exporter: exporter}
}

func (c auditLiveCheckouts) revisions(t *testing.T) map[string]any {
	t.Helper()
	revisions := map[string]any{}
	for name, directory := range map[string]string{"mcmods-cn-backend": c.backend, "mcmods-cn-frontend": c.frontend, "mcmods_exporter": c.exporter} {
		if !filepath.IsAbs(directory) {
			t.Fatalf("an absolute checkout path is required for %s", name)
		}
		revision := auditJourneyRevision(directory)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("clean identified checkout required: %s at %s", name, directory)
		}
		revisions[name] = revision
	}
	return revisions
}

func (c auditLiveCheckouts) pinnedRevisions(t *testing.T) map[string]any {
	t.Helper()
	revisions := c.revisions(t)
	for repository, key := range map[string]string{
		"mcmods-cn-backend":  "MCMODS_AUDIT_EXPECT_BACKEND_COMMIT",
		"mcmods-cn-frontend": "MCMODS_AUDIT_EXPECT_FRONTEND_COMMIT",
		"mcmods_exporter":    "MCMODS_AUDIT_EXPECT_EXPORTER_COMMIT",
	} {
		expected := os.Getenv(key)
		if len(expected) != 40 || revisions[repository].(map[string]any)["commit"] != expected {
			t.Fatalf("%s must match the full commit declared in %s", repository, key)
		}
	}
	return revisions
}

func auditJourneyRevision(directory string) map[string]any {
	head, err := exec.Command("git", "-C", directory, "rev-parse", "HEAD").Output()
	if err != nil {
		return map[string]any{"error": "git revision unavailable"}
	}
	diff, err := exec.Command("git", "-C", directory, "diff", "--binary", "HEAD").Output()
	if err != nil {
		return map[string]any{"error": "git source diff unavailable"}
	}
	state, err := exec.Command("git", "-C", directory, "status", "--porcelain").Output()
	if err != nil {
		return map[string]any{"error": "git source status unavailable"}
	}
	digest := sha256.Sum256(diff)
	return map[string]any{"checkoutPath": directory, "commit": strings.TrimSpace(string(head)), "dirty": len(state) > 0, "trackedDiffSha256": hex.EncodeToString(digest[:])}
}

func TestAuditLiveReceiptsUseTheSelectedCheckouts(t *testing.T) {
	parent := t.TempDir()
	initRepo := func(name string) string {
		t.Helper()
		directory := filepath.Join(parent, name)
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
		for _, args := range [][]string{{"init", "--quiet"}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgSign=false", "commit", "--quiet", "--allow-empty", "-m", name}} {
			if out, err := exec.Command("git", append([]string{"-C", directory}, args...)...).CombinedOutput(); err != nil {
				t.Fatalf("create fixture checkout: %v, %s", err, out)
			}
		}
		return directory
	}
	backend := initRepo("backend")
	frontend := initRepo("mcmods-cn-frontend")
	exporter := initRepo("mcmods_exporter")
	alternative := initRepo("worktree")
	check := func(wantFrontend, wantExporter, fixtureFrontend string) {
		t.Helper()
		records := auditLiveSourceCheckouts(backend, fixtureFrontend).revisions(t)
		for repo, dir := range map[string]string{"mcmods-cn-backend": backend, "mcmods-cn-frontend": wantFrontend, "mcmods_exporter": wantExporter} {
			head, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
			if err != nil {
				t.Fatal(err)
			}
			got := records[repo].(map[string]any)
			if got["checkoutPath"] != dir || got["commit"] != strings.TrimSpace(string(head)) || got["dirty"] != false {
				t.Fatalf("%s receipt did not describe the selected tree: %v", repo, got)
			}
		}
	}
	t.Setenv("MCMODS_AUDIT_FRONTEND_REPO", "")
	t.Setenv("MCMODS_AUDIT_EXPORTER_REPO", "")
	check(frontend, exporter, "")
	t.Setenv("MCMODS_AUDIT_FRONTEND_REPO", alternative)
	t.Setenv("MCMODS_AUDIT_EXPORTER_REPO", alternative)
	check(alternative, alternative, "")
	check(frontend, alternative, frontend)
}
