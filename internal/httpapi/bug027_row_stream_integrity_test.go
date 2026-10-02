package httpapi

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestFinishRowsPropagatesTerminalFailureAndCloses(t *testing.T) {
	wantErr := errors.New("terminal row failure")
	rows := &arch027Rows{err: wantErr}
	if err := finishRows(rows); !errors.Is(err, wantErr) || !rows.closed {
		t.Fatalf("finishRows error=%v closed=%t, want %v/true", err, rows.closed, wantErr)
	}
}

func TestBug027LiveRowStreamsCheckEveryTerminalError(t *testing.T) {
	tests := []struct {
		file      string
		functions []string
	}{
		{"mod_content_handlers.go", []string{"modContentVersions", "modContentTemplates", "modContentSections"}},
		{"mod_content_resource_handlers.go", []string{"modContentResources"}},
		{"simple_project_handlers.go", []string{"loadSimpleProjectAssociations"}},
		{"modpack_handlers.go", []string{"loadModpackAssociations"}},
		{"project_follow_handlers.go", []string{"myProjectFollows"}},
		{"server_probe_scheduler.go", []string{"probeDueMinecraftServersWithProbe"}},
		{"project_update_events.go", []string{"appendReviewedProjectUpdateEventTx"}},
		{"project_update_notification_worker.go", []string{"processPending", "process"}},
		{"notification_worker.go", []string{"sendBroadcastEmail"}},
	}
	for _, file := range tests {
		raw, err := os.ReadFile(file.file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, function := range file.functions {
			body := goFunctionBody(t, source, function)
			loops := strings.Count(body, "for rows.Next()")
			checks := strings.Count(body, "finishRows(rows)") + strings.Count(body, "rows.Err()")
			if loops == 0 || checks < loops {
				t.Errorf("%s.%s has %d row loops but only %d terminal checks", file.file, function, loops, checks)
			}
			for _, forbidden := range []string{"rows.Scan(&id) == nil", "rows.Scan(&userID); err == nil", "if rows.Scan("} {
				if strings.Contains(body, forbidden) {
					t.Errorf("%s.%s still discards a Scan failure through %q", file.file, function, forbidden)
				}
			}
		}
	}
}

func TestBug027DefaultSchedulerUsesTheStrictClaimImplementation(t *testing.T) {
	raw, err := os.ReadFile("server_probe_scheduler.go")
	if err != nil {
		t.Fatal(err)
	}
	body := goFunctionBody(t, string(raw), "probeDueMinecraftServers")
	if !strings.Contains(body, "probeDueMinecraftServersWithProbe(ctx, db, serverprobe.Probe)") {
		t.Fatal("production scheduler no longer delegates to the terminal-error-checked claim implementation with the default passive probe")
	}
}

func TestBug027PreviouslyRemediatedStreamsRemainStrict(t *testing.T) {
	tests := []struct {
		file     string
		function string
	}{
		{"notification_pagination.go", "loadNotificationPage"},
		{"seed_crawler_handlers.go", "adminSeedCrawlerRuns"},
		{"seed_crawler_handlers.go", "adminSeedCrawlerCandidates"},
		{"project_automation_worker.go", "scheduleDue"},
		{"mod_content_handlers.go", "modContentSectionResources"},
	}
	for _, test := range tests {
		raw, err := os.ReadFile(test.file)
		if err != nil {
			t.Fatal(err)
		}
		body := goFunctionBody(t, string(raw), test.function)
		if strings.Count(body, "rows.Err()")+strings.Count(body, "finishRows(rows)") < strings.Count(body, "for rows.Next()") {
			t.Errorf("previously remediated stream regressed: %s.%s", test.file, test.function)
		}
	}
}
