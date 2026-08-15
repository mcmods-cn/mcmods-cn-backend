package httpapi

import (
	"strings"
	"testing"
	"time"
)

func TestNormalizeActivityRetentionConfigRejectsUnknownAction(t *testing.T) {
	config := defaultActivityRetentionConfig()
	config.Actions["drop table users"] = activityRetentionPolicy{RetentionDays: 30, BatchSize: 1000}
	if _, err := normalizeActivityRetentionConfig(config); err == nil {
		t.Fatal("expected an unknown activity action to be rejected")
	}
}

func TestActivityCleanupWhereUsesOnlyPlaceholders(t *testing.T) {
	now := time.Now().UTC()
	filter := normalizedActivityCleanupFilter{ActionIDs: []int16{3}, UserIDs: []int64{42}, SnapshotBefore: now, From: timePointer(now.Add(-time.Hour))}
	where, args := activityCleanupWhere(filter, 1)
	if len(args) != 4 || !strings.Contains(where, "event.action_id=any($4::smallint[])") {
		t.Fatalf("unexpected cleanup predicate %q args=%#v", where, args)
	}
	if strings.Contains(where, "42") {
		t.Fatal("cleanup values must not be interpolated into SQL")
	}
}

func TestCleanupTimeBoundary(t *testing.T) {
	if _, err := parseCleanupBoundary("2026-08-15"); err == nil {
		t.Fatal("date without an explicit timezone must be rejected")
	}
	from, _ := parseCleanupBoundary("2026-08-15T00:00:00+08:00")
	if from == nil || from.Location() != time.UTC {
		t.Fatalf("expected UTC boundary, got %#v", from)
	}
}

func TestCleanupTokenComparison(t *testing.T) {
	token, digest, err := newCleanupConfirmationToken()
	if err != nil || !cleanupTokenMatches(token, digest) || cleanupTokenMatches(token+"x", digest) {
		t.Fatal("cleanup confirmation token validation failed")
	}
}

func TestActivityRetentionIdentifiersMatchAuthoritativeSchema(t *testing.T) {
	t.Parallel()
	if activityActionIDs["checkin"] != 10 {
		t.Fatal("activity cleanup action codes must match activity_actions")
	}
	if _, exists := activityActionIDs["check_in"]; exists {
		t.Fatal("legacy action spelling must not be exposed")
	}
	if activityObjectRouteTypes["server"] != "minecraft_server" {
		t.Fatal("server cleanup objects must resolve through their public route entity type")
	}
}
