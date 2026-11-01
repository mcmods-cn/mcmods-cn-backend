package main

import (
	"os"
	"path/filepath"
	"testing"
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
			if _, err := readPlan(path); (err == nil) != test.valid {
				t.Fatalf("strict private plan valid=%v error=%v", test.valid, err)
			}
		})
	}
}
