package main

import (
	"flag"
	"io"
	"testing"
)

func TestScopeDoesNotTurnAnInvalidUserSelectionIntoAnAllUserRun(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		ok    bool
		user  string
		batch int
	}{
		{"all", nil, true, "", 200},
		{"batch", []string{"-batch-size", "40"}, true, "", 40},
		{"single", []string{"-user", "testuser1", "-batch-size", "20"}, true, "testuser1", 20},
		{"positional", []string{"testuser1"}, false, "", 0},
		{"positional after batch", []string{"-batch-size", "50", "testuser1"}, false, "", 0},
		{"empty expanded variable", []string{"-user", ""}, false, "", 0},
		{"whitespace", []string{"-user", "   "}, false, "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			flags := flag.NewFlagSet("user-statistics", flag.ContinueOnError)
			flags.SetOutput(io.Discard)
			got, err := parseScope(flags, tc.args)
			if (err == nil) != tc.ok || tc.ok && (got.UserPublicID != tc.user || got.BatchSize != tc.batch) {
				t.Fatalf("selection=%+v err=%v", got, err)
			}
		})
	}
}
