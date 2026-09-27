package database

import (
	"strings"
	"testing"
)

func TestOPS010PopularityQueuesPersistTerminalStatusAndReadyIndexes(t *testing.T) {
	content := strings.ToLower(strings.Join(contentMetricsSchemaStatements(), "\n"))
	comments := strings.ToLower(strings.Join(commentSchemaStatements(), "\n"))
	for name, source := range map[string]string{"content": content, "comment": comments} {
		for _, required := range []string{
			"status text not null default 'pending'",
			"check(status in ('pending','processing','failed'))",
			"where status='pending'",
			"status='pending'",
			"status='failed'",
		} {
			if !strings.Contains(source, required) {
				t.Errorf("%s popularity queue schema is missing %q", name, required)
			}
		}
	}
}
