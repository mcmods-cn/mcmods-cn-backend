package config

import (
	"os"
	"strings"
	"testing"
)

func TestSEC002GoDependencyAuditIsAnArchivedReleaseGate(t *testing.T) {
	workflowBytes, err := os.ReadFile("../../.github/workflows/dependency-audit.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(workflowBytes)
	for _, required := range []string{
		"pull_request:",
		"schedule:",
		"govulncheck@v1.7.0",
		"govulncheck -format json ./...",
		"continue-on-error: true",
		"if: always()",
		"steps.govulncheck.outcome != 'success'",
		"actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a",
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("dependency audit workflow is missing %q", required)
		}
	}
	if !strings.Contains(workflow, "actions/checkout@9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0") ||
		!strings.Contains(workflow, "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e") {
		t.Error("security workflow actions must be pinned to reviewed immutable revisions")
	}
	scanIndex := strings.Index(workflow, "id: govulncheck")
	archiveIndex := strings.Index(workflow, "name: Archive govulncheck report")
	enforceIndex := strings.Index(workflow, "name: Enforce vulnerability gate")
	if scanIndex < 0 || archiveIndex <= scanIndex || enforceIndex <= archiveIndex {
		t.Error("govulncheck report must be archived after scanning and before the release gate fails")
	}

	moduleBytes, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	module := string(moduleBytes)
	if !strings.Contains(module, "toolchain go1.26.6") ||
		!strings.Contains(module, "golang.org/x/image v0.45.0") ||
		!strings.Contains(module, "github.com/klauspost/compress v1.18.7") {
		t.Error("go.mod must retain the toolchain, image decoder, and compression security floors established by SEC-002")
	}
}
