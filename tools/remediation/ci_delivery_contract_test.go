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
		"go test -p=1 -parallel=1 ./... -count=1",
		"CGO_ENABLED: \"1\"",
		"go test -race ./... -count=1",
		"docker build --pull",
	} {
		if !strings.Contains(ci, required) {
			t.Errorf("backend CI is missing %q", required)
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
