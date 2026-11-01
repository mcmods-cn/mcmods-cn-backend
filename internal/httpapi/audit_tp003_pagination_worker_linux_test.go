package httpapi

import "testing"

func TestAuditTP003ActualHTTPPostgresWorkerPaginationTerminalMatrixIntegration(t *testing.T) {
	for _, test := range []auditCFWorkerCase{
		{name: "legal-release-only-page2", mode: "page2", pages: 2, archive: true, success: true},
		{name: "page2-429-exact-failure", mode: "429", pages: 2, errorText: "CurseForge file pagination failed: HTTP 429: provider request failed"},
		{name: "page2-real-deadline", mode: "timeout", pages: 2},
		{name: "repeated-page", mode: "repeat", pages: 2, errorText: "CurseForge file pagination repeated a file"},
		{name: "all-files-nonrelease", mode: "none", pages: 2, errorText: "CurseForge 整合包没有已批准的正式主文件"},
		{name: "legal-record1000", mode: "boundary-1000", pages: 20, archive: true, success: true},
		{name: "record1001-budget-exhausted", mode: "budget-1001", pages: 20, errorText: "CurseForge file pagination budget exhausted (1000 files)"},
	} {
		t.Run(test.name, func(t *testing.T) { auditRunCFWorkerCase(t, test) })
	}
}
