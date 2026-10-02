package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestLevelRecalculationBatchIsBoundedAndSetBased(t *testing.T) {
	t.Parallel()
	if levelRecalculationBatchSize < 100 || levelRecalculationBatchSize > 1000 {
		t.Fatalf("unsafe level recalculation batch size %d", levelRecalculationBatchSize)
	}
	query := strings.ToLower(levelRecalculationBatchSQL)
	for _, required := range []string{
		"order by experience.user_id",
		"limit $3",
		"update user_experience",
		"delete from user_role_bindings",
		"insert into user_role_bindings",
		"source='level_track'",
		"cursor_user_id",
		"processed_count",
		"lease_expires_at",
	} {
		if !strings.Contains(query, required) {
			t.Fatalf("level recalculation batch SQL is missing %q", required)
		}
	}
}

func TestLevelConfigRequestOnlyPersistsVersionAndDurableJob(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("progression_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(source))
	for _, forbidden := range []string{
		"select user_id,experience from user_experience for update",
		"progression.synctrackrole(r.context(), tx",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("level configuration request still contains synchronous fan-out %q", forbidden)
		}
	}
	for _, required := range []string{
		"version=version+1",
		"insert into level_recalculation_jobs",
		"status='superseded'",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("level configuration request is missing durable handoff %q", required)
		}
	}
}
