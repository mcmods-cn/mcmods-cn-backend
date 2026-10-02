package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestOriginalSeverityEvidenceKeepsImmutableTotalsAndExplicitGrades(t *testing.T) {
	counts, grades, errs := scanOriginalSeverities(filepath.FromSlash("../../../docs/audit/full-project-audit"))
	want := map[string]int{"Critical": 0, "High": 176, "Medium": 214, "Low": 59, "Info": 0, "Total": 449}
	if len(errs) != 0 || !reflect.DeepEqual(counts, want) || len(grades) != 277 || grades["TEST-042"] != "Medium" || grades["STYLE-003"] != "" {
		t.Fatalf("original evidence changed: counts=%v grades=%d errors=%v", counts, len(grades), errs)
	}
}

func TestOriginalSeverityParserRejectsDuplicateFacts(t *testing.T) {
	for _, raw := range []string{"| 高 | 176 |\n| 高 | 175 |", "| BUG-001 | 高 | detail |\n| BUG-001 | 中 | detail |"} {
		_, _, errs := parseOriginalSeverities(raw)
		if len(errs) != 1 {
			t.Fatalf("duplicate source accepted: %v", errs)
		}
	}
}

func TestOriginalSeveritySourceCannotDisappearOrHaveTotalsRewritten(t *testing.T) {
	if _, _, errs := scanOriginalSeverities(t.TempDir()); len(errs) == 0 {
		t.Fatal("missing original source accepted")
	}
	raw, err := os.ReadFile(filepath.FromSlash("../../../docs/audit/full-project-audit/11_FINDINGS_REGISTER.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, replacement := range []string{"| 高 | 175 |", "| 高 | 177 |", "| 高 | unknown |"} {
		root := t.TempDir()
		changed := strings.Replace(string(raw), "| 高 | 176 |", replacement, 1)
		if changed == string(raw) {
			t.Fatal("fixture did not change source")
		}
		if err := os.WriteFile(filepath.Join(root, "11_FINDINGS_REGISTER.md"), []byte(changed), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, errs := scanOriginalSeverities(root); len(errs) == 0 {
			t.Fatalf("altered immutable totals accepted: %s", replacement)
		}
	}
}

func TestReviewedSeverityNeverDowngradesAnExplicitOriginalGrade(t *testing.T) {
	for _, original := range []string{"Low", "Medium", "High"} {
		for _, reviewed := range []string{"Low", "Medium", "High"} {
			var errs []string
			validateReviewedSeverity(ledgerEntry{ID: "BUG-001", Severity: reviewed, Notes: reviewed + "/P1：实际权限或数据影响证据"}, original, &errs)
			wantFailure := severityRank(reviewed) < severityRank(original)
			if (len(errs) != 0) != wantFailure {
				t.Fatalf("%s -> %s: errors=%v", original, reviewed, errs)
			}
		}
	}
}

func TestUnlistedAndRaisedSeveritiesNeedAnExplicitRiskReason(t *testing.T) {
	for _, notes := range []string{"Medium/P1：状态分裂且恢复被延迟", "中：原始Outbox仍在但恢复状态分裂"} {
		var errs []string
		validateReviewedSeverity(ledgerEntry{ID: "OPS-009", Severity: "Medium", Notes: notes}, "", &errs)
		if len(errs) != 0 {
			t.Fatal(errs)
		}
	}
	for _, notes := range []string{"", "TBD", "全部已修复", "Medium", "Medium/P1: PASS", "Medium/P1: 通过", "Low/P2：布局影响"} {
		var errs []string
		validateReviewedSeverity(ledgerEntry{ID: "OPS-009", Severity: "Medium", Notes: notes}, "", &errs)
		if len(errs) == 0 {
			t.Fatalf("missing/wrong severity evidence accepted: %q", notes)
		}
	}
	var errs []string
	validateReviewedSeverity(ledgerEntry{ID: "TEST-042", Severity: "High", Notes: "沿用旧分级"}, "Medium", &errs)
	if len(errs) == 0 {
		t.Fatal("unexplained raised grade accepted")
	}
}

func TestFinalEntryStillRequiresCodeTestCommandAndPassingResult(t *testing.T) {
	base := ledgerEntry{ID: "BUG-001", Status: "CLOSED", CurrentCode: "handler", FixSummary: "atomic transaction", AutomatedTest: "TestActualHTTP", ValidationCommand: "go test", Result: "PASS"}
	for _, field := range []string{"code", "fix", "test", "command", "result"} {
		entry := base
		switch field {
		case "code":
			entry.CurrentCode = ""
		case "fix":
			entry.FixSummary = "TBD"
		case "test":
			entry.AutomatedTest = ""
		case "command":
			entry.ValidationCommand = "待执行"
		case "result":
			entry.Result = "FAIL"
		}
		var errs []string
		validateFinalEntry(entry, &errs)
		if len(errs) == 0 {
			t.Fatalf("missing final %s evidence accepted", field)
		}
	}
}

func TestFinalValidatorCLIRetainsAllInventoryClosureAndEvidenceGuards(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	goBinary := os.Getenv("MCMODS_GO_BINARY")
	if goBinary == "" {
		goBinary = "go"
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	executable := filepath.Join(directory, "verify-findings")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	if output, err := exec.CommandContext(ctx, goBinary, "build", "-o", executable, ".").CombinedOutput(); err != nil {
		t.Fatalf("build real CLI: %v\n%s", err, output)
	}
	ledger, err := os.ReadFile(filepath.Join(root, "docs/remediation/full-audit-remediation/01_FINDING_STATUS.md"))
	if err != nil {
		t.Fatal(err)
	}
	auditRoot := filepath.Join(root, "docs/audit/full-project-audit")
	run := func(contents string, wantFailure bool) {
		t.Helper()
		path := filepath.Join(directory, "ledger.md")
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		output, err := exec.CommandContext(ctx, executable, "-audit", auditRoot, "-ledger", path).CombinedOutput()
		if (err != nil) != wantFailure {
			t.Fatalf("CLI failure=%t wanted=%t: %v\n%s", err != nil, wantFailure, err, output)
		}
		if wantFailure && !strings.Contains(string(output), "verification=FAIL") {
			t.Fatalf("failure was not actual validation: %s", output)
		}
		if !wantFailure && (!strings.Contains(string(output), "statuses CLOSED=449") || !strings.Contains(string(output), "original_audit_severities Critical=0 High=176 Info=0 Low=59 Medium=214 Total=449") || !strings.Contains(string(output), "verification=PASS")) {
			t.Fatalf("baseline incomplete: %s", output)
		}
	}
	mutate := func(id string, column int, value string) string {
		t.Helper()
		lines := strings.Split(string(ledger), "\n")
		for index, line := range lines {
			if strings.HasPrefix(line, "| "+id+" |") {
				parts := strings.Split(line, "|")
				parts[column] = " " + value + " "
				lines[index] = strings.Join(parts, "|")
				return strings.Join(lines, "\n")
			}
		}
		t.Fatalf("missing baseline %s", id)
		return ""
	}
	run(string(ledger), false)
	for _, testCase := range []struct {
		name, id string
		column   int
		value    string
	}{
		{"open", "BUG-001", 6, "OPEN"}, {"in-progress", "BUG-001", 6, "IN_PROGRESS"}, {"blocked", "BUG-001", 6, "BLOCKED_EXTERNAL"},
		{"missing-code-evidence", "BUG-001", 7, ""}, {"missing-test-evidence", "BUG-001", 11, "TBD"}, {"missing-command", "BUG-001", 12, "TBD"}, {"failed-result", "BUG-001", 13, "FAIL"},
		{"downgraded-original", "BUG-001", 2, "Medium"}, {"unresolved", "BUG-001", 2, "UNRESOLVED"}, {"wrong-category", "BUG-001", 3, "SEC"},
		{"missing-independent-risk-evidence", "STYLE-003", 15, "TBD"}, {"unknown-id", "BUG-001", 1, "BUG-999"},
	} {
		t.Run(testCase.name, func(t *testing.T) { run(mutate(testCase.id, testCase.column, testCase.value), true) })
	}
	var first string
	for _, line := range strings.Split(string(ledger), "\n") {
		if strings.HasPrefix(line, "| BUG-001 |") {
			first = line
			break
		}
	}
	if first == "" {
		t.Fatal("missing fixture row")
	}
	t.Run("duplicate", func(t *testing.T) { run(string(ledger)+"\n"+first+"\n", true) })
	t.Run("missing-id", func(t *testing.T) { run(strings.Replace(string(ledger), first+"\n", "", 1), true) })
	if err := ctx.Err(); err != nil {
		t.Fatal(fmt.Errorf("validator fixture deadline: %w", err))
	}
}
