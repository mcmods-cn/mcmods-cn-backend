package httpapi

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGovernanceReadsDoNotReturnUncheckedOrPartialData(t *testing.T) {
	handlers, err := os.ReadFile("governance_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	pagination, err := os.ReadFile("governance_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(handlers) + string(pagination)
	if strings.Contains(source, "_ = json.Unmarshal(raw, &result)") {
		t.Fatal("admin report detail still discards report snapshot decode errors")
	}
	for _, required := range []string{
		"collectOwnReportRows(rows, request.Limit+1)",
		"collectAdminReportRows(rows, request.Limit+1)",
		"collectPublicBlackroomRows(rows, request.Limit+1, now)",
		"decodeReportSnapshot",
		"logGovernanceReadFailure",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("governance reads are missing %q", required)
		}
	}
}

func TestGovernanceCollectorsRejectScanAndTerminalFailures(t *testing.T) {
	databaseFailure := errors.New("ARCH-030 database failure")
	for name, collect := range map[string]func(*arch027Rows) error{
		"own reports": func(rows *arch027Rows) error {
			_, err := collectOwnReportRows(rows, 1)
			return err
		},
		"admin reports": func(rows *arch027Rows) error {
			_, err := collectAdminReportRows(rows, 1)
			return err
		},
		"blackroom": func(rows *arch027Rows) error {
			_, err := collectPublicBlackroomRows(rows, 1, time.Date(2026, 8, 23, 0, 0, 0, 0, time.UTC))
			return err
		},
	} {
		t.Run(name+" scan", func(t *testing.T) {
			rows := &arch027Rows{remaining: 1, scan: func(...any) error { return databaseFailure }}
			if err := collect(rows); !errors.Is(err, databaseFailure) || !rows.closed {
				t.Fatalf("scan failure = %v, closed=%t", err, rows.closed)
			}
		})
		t.Run(name+" terminal", func(t *testing.T) {
			rows := &arch027Rows{err: databaseFailure}
			if err := collect(rows); !errors.Is(err, databaseFailure) || !rows.closed {
				t.Fatalf("terminal failure = %v, closed=%t", err, rows.closed)
			}
		})
	}
}

func TestReportSnapshotRejectsSilentNullOrNonObjectFallbacks(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `"text"`, "1", "{"} {
		if value, err := decodeReportSnapshot([]byte(raw)); err == nil {
			t.Fatalf("decodeReportSnapshot(%q) = %#v, nil", raw, value)
		}
	}
	value, err := decodeReportSnapshot([]byte(`{"targetType":"mod","id":"abc123def"}`))
	if err != nil || value["targetType"] != "mod" {
		t.Fatalf("valid report snapshot = %#v, %v", value, err)
	}
}
