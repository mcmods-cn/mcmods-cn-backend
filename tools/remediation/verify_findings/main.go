package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const expectedFindingCount = 449

var (
	findingIDPattern = `(?:BUG|SEC|PERF|DB|ARCH|REUSE|TEST|OPS|STYLE|MAP|LEGACY|DEAD)-[0-9]{3}`
	findingIDRE      = regexp.MustCompile(`\b` + findingIDPattern + `\b`)
	ledgerRowRE      = regexp.MustCompile(`^\|\s*(` + findingIDPattern + `)\s*\|`)
	allowedStatuses  = map[string]bool{
		"OPEN": true, "IN_PROGRESS": true, "FIXED_PENDING_VERIFICATION": true,
		"CLOSED": true, "NOT_APPLICABLE": true, "BLOCKED_EXTERNAL": true,
	}
	expectedCategories = map[string]int{
		"BUG": 151, "SEC": 47, "PERF": 72, "DB": 8, "ARCH": 33,
		"REUSE": 7, "TEST": 50, "OPS": 22, "STYLE": 9, "MAP": 12,
		"LEGACY": 22, "DEAD": 16,
	}
)

type ledgerEntry struct {
	ID                string
	Severity          string
	Type              string
	Module            string
	RootCause         string
	Status            string
	CurrentCode       string
	FixSummary        string
	DatabaseChange    string
	APIChange         string
	AutomatedTest     string
	ValidationCommand string
	Result            string
	RelatedCommit     string
	Notes             string
	Line              int
}

func main() {
	auditDir := flag.String("audit", filepath.FromSlash("docs/audit/full-project-audit"), "audit report directory")
	ledgerPath := flag.String("ledger", filepath.FromSlash("docs/remediation/full-audit-remediation/01_FINDING_STATUS.md"), "remediation ledger")
	allowOpen := flag.Bool("allow-open", false, "validate inventory while allowing non-final statuses and unresolved severities")
	flag.Parse()

	auditIDs, scanErrs := scanAudit(*auditDir)
	entries, duplicates, ledgerErrs := scanLedger(*ledgerPath)
	errs := append(scanErrs, ledgerErrs...)

	if len(auditIDs) != expectedFindingCount {
		errs = append(errs, fmt.Sprintf("audit contains %d unique Finding IDs; expected %d", len(auditIDs), expectedFindingCount))
	}
	categoryCounts := countCategories(auditIDs)
	for category, expected := range expectedCategories {
		if categoryCounts[category] != expected {
			errs = append(errs, fmt.Sprintf("audit category %s has %d IDs; expected %d", category, categoryCounts[category], expected))
		}
	}
	for id, lines := range duplicates {
		errs = append(errs, fmt.Sprintf("ledger contains duplicate %s at lines %v", id, lines))
	}

	statusCounts := map[string]int{}
	severityCounts := map[string]int{}
	for id := range auditIDs {
		entry, ok := entries[id]
		if !ok {
			errs = append(errs, "ledger is missing "+id)
			continue
		}
		statusCounts[entry.Status]++
		severityCounts[entry.Severity]++
		prefix := strings.SplitN(id, "-", 2)[0]
		if entry.Type != prefix {
			errs = append(errs, fmt.Sprintf("%s line %d has type %q; expected %q", id, entry.Line, entry.Type, prefix))
		}
		if !allowedStatuses[entry.Status] {
			errs = append(errs, fmt.Sprintf("%s line %d has invalid status %q", id, entry.Line, entry.Status))
		}
		if entry.Severity != "High" && entry.Severity != "Medium" && entry.Severity != "Low" {
			if !(*allowOpen && entry.Severity == "UNRESOLVED") {
				errs = append(errs, fmt.Sprintf("%s line %d has unresolved/invalid severity %q", id, entry.Line, entry.Severity))
			}
		}
		if !*allowOpen && entry.Status != "CLOSED" && entry.Status != "NOT_APPLICABLE" {
			errs = append(errs, fmt.Sprintf("%s remains %s", id, entry.Status))
		}
		if entry.Status == "CLOSED" || entry.Status == "NOT_APPLICABLE" {
			validateFinalEntry(entry, &errs)
		}
	}
	for id, entry := range entries {
		if _, ok := auditIDs[id]; !ok {
			errs = append(errs, fmt.Sprintf("ledger contains unknown Finding ID %s at line %d", id, entry.Line))
		}
	}

	if !*allowOpen {
		if severityCounts["High"] != 176 || severityCounts["Medium"] != 214 || severityCounts["Low"] != 59 {
			errs = append(errs, fmt.Sprintf("final severity totals are High=%d Medium=%d Low=%d; expected 176/214/59", severityCounts["High"], severityCounts["Medium"], severityCounts["Low"]))
		}
	}

	fmt.Printf("audit_unique=%d ledger_unique=%d\n", len(auditIDs), len(entries))
	printCounts("categories", categoryCounts)
	printCounts("severities", severityCounts)
	printCounts("statuses", statusCounts)
	if len(errs) == 0 {
		fmt.Println("verification=PASS")
		return
	}
	sort.Strings(errs)
	fmt.Printf("verification=FAIL issues=%d\n", len(errs))
	for _, err := range errs {
		fmt.Println("- " + err)
	}
	os.Exit(1)
}

func scanAudit(root string) (map[string]struct{}, []string) {
	ids := map[string]struct{}{}
	var errs []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, id := range findingIDRE.FindAllString(string(data), -1) {
			ids[id] = struct{}{}
		}
		return nil
	})
	if err != nil {
		errs = append(errs, fmt.Sprintf("scan audit: %v", err))
	}
	return ids, errs
}

func scanLedger(path string) (map[string]ledgerEntry, map[string][]int, []string) {
	file, err := os.Open(path)
	if err != nil {
		return map[string]ledgerEntry{}, nil, []string{fmt.Sprintf("open ledger: %v", err)}
	}
	defer file.Close()
	entries := map[string]ledgerEntry{}
	linesByID := map[string][]int{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		match := ledgerRowRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 17 {
			return entries, nil, []string{fmt.Sprintf("ledger line %d has %d columns; expected 15", lineNo, len(parts)-2)}
		}
		field := func(index int) string { return strings.TrimSpace(parts[index]) }
		entry := ledgerEntry{
			ID: field(1), Severity: field(2), Type: field(3), Module: field(4),
			RootCause: field(5), Status: field(6), CurrentCode: field(7),
			FixSummary: field(8), DatabaseChange: field(9), APIChange: field(10),
			AutomatedTest: field(11), ValidationCommand: field(12), Result: field(13),
			RelatedCommit: field(14), Notes: field(15), Line: lineNo,
		}
		linesByID[entry.ID] = append(linesByID[entry.ID], lineNo)
		entries[entry.ID] = entry
	}
	var errs []string
	if err := scanner.Err(); err != nil {
		errs = append(errs, fmt.Sprintf("scan ledger: %v", err))
	}
	duplicates := map[string][]int{}
	for id, lines := range linesByID {
		if len(lines) > 1 {
			duplicates[id] = lines
		}
	}
	return entries, duplicates, errs
}

func validateFinalEntry(entry ledgerEntry, errs *[]string) {
	required := map[string]string{
		"current code evidence": entry.CurrentCode,
		"automated test":        entry.AutomatedTest,
		"validation command":    entry.ValidationCommand,
		"result":                entry.Result,
	}
	if entry.Status == "CLOSED" {
		required["fix summary"] = entry.FixSummary
	}
	if entry.Status == "NOT_APPLICABLE" {
		required["notes/reason"] = entry.Notes
	}
	for name, value := range required {
		if placeholder(value) {
			*errs = append(*errs, fmt.Sprintf("%s is %s but %s is missing", entry.ID, entry.Status, name))
		}
	}
	if !strings.Contains(strings.ToUpper(entry.Result), "PASS") && !strings.Contains(entry.Result, "通过") {
		*errs = append(*errs, fmt.Sprintf("%s is %s but result does not state PASS/通过", entry.ID, entry.Status))
	}
}

func placeholder(value string) bool {
	value = strings.TrimSpace(value)
	return value == "" || value == "—" || value == "TBD" || value == "UNRESOLVED" || strings.HasPrefix(value, "待")
}

func countCategories(ids map[string]struct{}) map[string]int {
	counts := map[string]int{}
	for id := range ids {
		counts[strings.SplitN(id, "-", 2)[0]]++
	}
	return counts
}

func printCounts(label string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	fmt.Printf("%s %s\n", label, strings.Join(parts, " "))
}
