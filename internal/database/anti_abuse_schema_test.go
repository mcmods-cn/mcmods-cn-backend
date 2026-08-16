package database

import (
	"strings"
	"testing"
)

func TestAntiAbuseSchemaSeparatesSecurityEventsAndContentFingerprints(t *testing.T) {
	t.Parallel()
	schema := strings.Join(antiAbuseSchemaStatements(), "\n")
	for _, required := range []string{
		"create table anti_abuse_events", "create table anti_abuse_content_fingerprints",
		"create table anti_abuse_restrictions", "create table anti_abuse_challenges",
		"create table anti_abuse_bot_rules", "create table anti_abuse_daily_stats",
		"idx_anti_abuse_fingerprint_exact", "idx_anti_abuse_events_user_time",
	} {
		if !strings.Contains(schema, required) {
			t.Fatalf("anti-abuse schema is missing %q", required)
		}
	}
	if strings.Contains(schema, "request_body") || strings.Contains(schema, "cookie") || strings.Contains(schema, "authorization") {
		t.Fatal("anti-abuse schema must not persist request credentials or full bodies")
	}
}
