package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProjectUpdateNotificationProgressDoesNotRecountEveryBatch(t *testing.T) {
	workerRaw, err := os.ReadFile("project_update_notification_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	worker := strings.ToLower(string(workerRaw))
	if strings.Contains(worker, "notified_count=(select count(*) from notifications") {
		t.Fatal("project update worker still recounts all event notifications after every batch")
	}
	if !strings.Contains(worker, "notified_count=notified_count+$3") {
		t.Fatal("project update worker does not atomically accumulate successful inserts")
	}

	schemaRaw, err := os.ReadFile("../database/engagement_export_schema.go")
	if err != nil {
		t.Fatal(err)
	}
	schema := strings.ToLower(string(schemaRaw))
	if !strings.Contains(schema, "create index idx_notifications_project_update_event") ||
		!strings.Contains(schema, "on notifications(project_update_event_id) where project_update_event_id is not null") {
		t.Fatal("notifications are missing the event-first partial reconciliation index")
	}
}
