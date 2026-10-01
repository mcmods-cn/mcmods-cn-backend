package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProgressionListsRejectPostgreSQLStreamFailureCoreIntegration(t *testing.T) {
	for _, name := range []string{"tasks", "activity events"} {
		t.Run(name, func(t *testing.T) {
			ctx, pool, cfg := isolatedAITestDatabase(t)
			server := &Server{db: pool, cfg: cfg}
			handler := server.adminTasks
			seed := `insert into task_definitions(code,name,condition,rewards)
				values('synthetic-stream-task','Synthetic task','{}','{"experience":1}')`
			failure := `alter table task_definitions rename to audit_preserved_tasks;
				create view task_definitions as
				select id,audit_progression_text_failure() public_id,code,name,description,icon,
				translations,refresh_period,condition,rewards,status,created_at
				from audit_preserved_tasks`
			if name == "activity events" {
				handler = server.adminActivityEvents
				seed = `insert into user_activity_events(action_id,object_type_id,occurred_at)
					values(2,14,now())`
				failure = `alter table user_activity_events rename to audit_preserved_events;
					create view user_activity_events as
					select id,user_id,action_id,object_type_id,object_route_id,
					audit_progression_integer_failure() markdown_added_bytes,occurred_at
					from audit_preserved_events`
			}
			if _, err := pool.Exec(ctx, seed); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
			healthy := httptest.NewRecorder()
			handler(healthy, request)
			if healthy.Code != http.StatusOK || !strings.Contains(healthy.Body.String(), `"items":[{`) {
				t.Fatalf("healthy real table status=%d body=%s", healthy.Code, healthy.Body.String())
			}
			// Only this marker-owned disposable database is changed. Retain the
			// original tables, constraints and triggers; the view emits a real
			// PostgreSQL execution error while pgx advances the result stream.
			if _, err := pool.Exec(ctx, `create function audit_progression_text_failure() returns text
				language plpgsql volatile as $$ begin raise exception 'synthetic stream failure'; end $$;
				create function audit_progression_integer_failure() returns integer
				language plpgsql volatile as $$ begin raise exception 'synthetic stream failure'; end $$`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, failure); err != nil {
				t.Fatal(err)
			}
			failed := httptest.NewRecorder()
			handler(failed, request)
			if failed.Code != http.StatusInternalServerError {
				t.Fatalf("failed real PostgreSQL stream status=%d, want500: %s", failed.Code, failed.Body.String())
			}
			if strings.Contains(failed.Body.String(), `"items"`) || strings.Contains(failed.Body.String(), "synthetic stream failure") {
				t.Fatalf("failed stream returned a success list or database details: %s", failed.Body.String())
			}
		})
	}
}
