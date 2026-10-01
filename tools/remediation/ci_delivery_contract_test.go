package remediation

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackendCIDeliveryContract(t *testing.T) {
	root := remediationRepositoryRoot(t)
	read := func(relative string) string {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}

	ci := read(".github/workflows/ci.yml")
	for _, required := range []string{
		"go test ./... -count=1 -coverprofile=coverage.out",
		"minimum=27.0",
		"go vet ./...",
		"go build ./...",
		"MCMODS_RUN_DB_INTEGRATION: \"1\"",
		"go run ./tools/testing/db_suite",
		"MCMODS_RUN_ACTIVITY_LOAD: \"1\"",
		"POSTGRES_DB: mcmods_test",
		"uses: ./.github/actions/initialize-test-database",
		"CGO_ENABLED: \"1\"",
		"go test -race ./... -count=1",
		"docker build --pull",
	} {
		if !strings.Contains(ci, required) {
			t.Errorf("backend CI is missing %q", required)
		}
	}
	initAction := read(".github/actions/initialize-test-database/action.yml")
	for _, required := range []string{"APP_ENV: development", "DB_RESET_ON_START: \"true\"", "DB_RESET_CONFIRM: RESET mcmods_test", "go run ./cmd/db-reset"} {
		if !strings.Contains(initAction, required) {
			t.Errorf("CI test initialization is missing %q", required)
		}
	}
	if strings.Contains(ci, "uses: actions/checkout@v") || strings.Contains(ci, "uses: actions/setup-go@v") {
		t.Fatal("backend CI actions must be pinned to immutable commit SHAs")
	}

	release := read(".github/workflows/release-container.yml")
	for _, required := range []string{"tags:", "packages: write", "docker login ghcr.io", "sha-${GITHUB_SHA}"} {
		if !strings.Contains(release, required) {
			t.Errorf("backend release workflow is missing %q", required)
		}
	}

	dockerfile := read("Dockerfile")
	for _, required := range []string{"golang:1.26.6-bookworm", "CGO_ENABLED=0", "distroless/static-debian12:nonroot", "USER nonroot:nonroot"} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("backend Dockerfile is missing %q", required)
		}
	}
}
