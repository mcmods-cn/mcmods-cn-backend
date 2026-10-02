package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var originalGradeRowRE = regexp.MustCompile(`^\|\s*(` + findingIDPattern + `)\s*\|\s*(高|中|低)\s*\|`)
var originalCountRowRE = regexp.MustCompile(`^\|\s*(Critical|高|中|低|Info|合计)\s*\|\s*([0-9]+)\s*\|$`)

// The user confirmed on 2026-10-01 that immutable original audit statistics
// and evidence-based remediation grades are separate. This does not waive
// any inventory, closure, evidence, original-grade or quality requirement.
func scanOriginalSeverities(root string) (map[string]int, map[string]string, []string) {
	raw, err := os.ReadFile(filepath.Join(root, "11_FINDINGS_REGISTER.md"))
	if err != nil {
		return nil, nil, []string{fmt.Sprintf("read original severity evidence: %v", err)}
	}
	counts, grades, errs := parseOriginalSeverities(string(raw))
	want := map[string]int{"Critical": 0, "High": 176, "Medium": 214, "Low": 59, "Info": 0, "Total": 449}
	for severity, expected := range want {
		if actual, exists := counts[severity]; !exists || actual != expected {
			errs = append(errs, fmt.Sprintf("immutable original audit severity %s is %d (present=%t); expected %d", severity, actual, exists, expected))
		}
	}
	if len(grades) != 277 {
		errs = append(errs, fmt.Sprintf("original explicit severity inventory has %d IDs; expected 277", len(grades)))
	}
	return counts, grades, errs
}

func parseOriginalSeverities(raw string) (map[string]int, map[string]string, []string) {
	counts := map[string]int{}
	grades := map[string]string{}
	var errs []string
	labels := map[string]string{"高": "High", "中": "Medium", "低": "Low", "合计": "Total", "Critical": "Critical", "Info": "Info"}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if match := originalCountRowRE.FindStringSubmatch(line); match != nil {
			label := labels[match[1]]
			count, err := strconv.Atoi(match[2])
			if err != nil {
				errs = append(errs, fmt.Sprintf("invalid original %s count: %v", label, err))
				continue
			}
			if _, duplicate := counts[label]; duplicate {
				errs = append(errs, "duplicate original severity total "+label)
			}
			counts[label] = count
		}
		if match := originalGradeRowRE.FindStringSubmatch(line); match != nil {
			grade := labels[match[2]]
			if previous, duplicate := grades[match[1]]; duplicate {
				errs = append(errs, fmt.Sprintf("duplicate original grade %s: %s/%s", match[1], previous, grade))
			}
			grades[match[1]] = grade
		}
	}
	return counts, grades, errs
}

func severityRank(severity string) int {
	switch severity {
	case "Low":
		return 1
	case "Medium":
		return 2
	case "High":
		return 3
	default:
		return 0
	}
}

func validateReviewedSeverity(entry ledgerEntry, original string, errs *[]string) {
	if severityRank(entry.Severity) == 0 {
		*errs = append(*errs, entry.ID+" has no valid reviewed severity")
		return
	}
	if original != "" && severityRank(entry.Severity) < severityRank(original) {
		*errs = append(*errs, fmt.Sprintf("%s downgrades explicit original severity %s to %s", entry.ID, original, entry.Severity))
		return
	}
	if original == entry.Severity {
		return
	}
	if !hasReviewedSeverityReason(entry.Severity, entry.Notes) {
		*errs = append(*errs, entry.ID+" needs an explicit evidence-based reviewed severity reason")
	}
}

func hasReviewedSeverityReason(severity, notes string) bool {
	if placeholder(notes) {
		return false
	}
	index := strings.Index(notes, severity)
	var reason string
	if index >= 0 {
		reason = notes[index+len(severity):]
	} else {
		label := map[string]string{"High": "高", "Medium": "中", "Low": "低"}[severity]
		trimmed := strings.TrimSpace(notes)
		if label == "" || (!strings.HasPrefix(trimmed, label+"：") && !strings.HasPrefix(trimmed, label+":")) {
			return false
		}
		reason = trimmed[len(label):]
	}
	reason = strings.TrimSpace(reason)
	for _, priority := range []string{"/P0", "/P1", "/P2"} {
		reason = strings.TrimPrefix(reason, priority)
	}
	reason = strings.TrimSpace(strings.TrimLeft(reason, ":：；;，,。. "))
	return !placeholder(reason) && strings.ToUpper(reason) != "PASS" && reason != "通过"
}
