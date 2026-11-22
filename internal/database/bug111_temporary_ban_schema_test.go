package database

import (
	"strings"
	"testing"
)

func TestTemporaryBanSchemaEnforcesMinimumDuration(t *testing.T) {
	t.Parallel()
	schema := strings.Join(governanceAutomationSchemaStatements(), "\n")
	if !strings.Contains(schema, "ends_at is null or ends_at>=starts_at+interval '1 minute'") {
		t.Fatal("ban_records does not enforce its minimum temporary duration")
	}
}
