package main

import (
	"os"
	"path/filepath"
	"testing"

	"mcmods-cn-backend/internal/httpapi"
)

func TestMaintenancePlanFileIsPrivateSingleStrictJSON(t *testing.T) {
	for _, test := range []struct {
		name, body string
		mode       os.FileMode
		valid      bool
	}{
		{"private", `{"version":1}`, 0600, true},
		{"public", `{"version":1}`, 0644, false},
		{"unknown-property", `{"version":1,"unreviewedAction":true}`, 0600, false},
		{"second-plan", `{"version":1} {"version":2}`, 0600, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plan.json")
			if err := os.WriteFile(path, []byte(test.body), test.mode); err != nil {
				t.Fatal(err)
			}
			// Creation mode is filtered by the runner's private umask. Set and
			// verify the intended fixture mode before asserting the CLI contract.
			if err := os.Chmod(path, test.mode); err != nil {
				t.Fatal(err)
			}
			stat, err := os.Stat(path)
			if err != nil || stat.Mode().Perm() != test.mode {
				t.Fatalf("fixture mode not established: %v", err)
			}
			if _, err := readPlan(path); (err == nil) != test.valid {
				t.Fatalf("strict private plan valid=%v error=%v", test.valid, err)
			}
		})
	}
}

func TestMaintenanceApplyHoldsAreNotSuccessfulProcessResults(t *testing.T) {
	if !reportHasHolds(httpapi.OSSByteMaintenanceReport{Counts: map[string]int{"NEEDS_SCOPED_REFERENCE_PLAN": 1}}, true) {
		t.Fatal("apply hold would exit as a successful operation")
	}
	if !reportHasHolds(httpapi.OSSByteMaintenanceReport{Counts: map[string]int{"PROVIDER_ERROR": 1}}, false) {
		t.Fatal("failed dry-run provider execution would exit as a successful diagnosis")
	}
	if reportHasHolds(httpapi.OSSByteMaintenanceReport{Counts: map[string]int{"MISSING_ORIGINAL_SOURCE": 1}}, false) {
		t.Fatal("completed read-only diagnosis confused missing proof with a command failure")
	}
	if reportHasHolds(httpapi.OSSByteMaintenanceReport{Counts: map[string]int{"PROMOTED": 1, "ALREADY_COMPLETED": 1}}, true) {
		t.Fatal("verified/idempotent apply result was treated as held")
	}
}
