package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"mcmods-cn-backend/internal/activity"
)

func TestAuditCMP003CatalogTranslationReadOnlyDiagnosticCountsAndRejectsWrongDatabaseIntegration(t *testing.T) {
	x := newAuditCatalogTranslationFixture(t)
	f := x.f
	result := x.request(t, map[string]string{"targetLocale": "fr-FR", "sourceLocale": "en-US"})
	if result.status != http.StatusAccepted {
		t.Fatalf("actual prerequisiteHTTP%d", result.status)
	}
	auditFixWaitActivity(t, func() bool {
		return f.number(t, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor]) == 0 && f.number(t, `select count(*) from user_activity_events where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectMod) == 1
	})
	var schema, actor string
	if err := f.db.QueryRow(f.ctx, `select current_schema(),(select public_id from users where id=$1)`, f.userIDs[f.editor]).Scan(&schema, &actor); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("../../tools/audit/catalog-translation-activity/diagnose.sql")
	if err != nil {
		t.Fatal(err)
	}
	before := auditFixAIActivityFacts(t, f.test013Fixture)
	for _, wrong := range []bool{false, true} {
		cfg := f.db.Config().ConnConfig.Copy()
		name := cfg.Database
		if wrong {
			name += "_wrong"
		}
		for key, value := range map[string]string{"database": name, "schema": schema, "actor": actor, "target": f.modCode, "object_code": "mod", "since": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339), "until": time.Now().Add(time.Minute).UTC().Format(time.RFC3339), "limit": "100"} {
			cfg.RuntimeParams["mcmods.audit_"+key] = value
		}
		conn, err := pgx.ConnectConfig(f.ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		rows, runErr := conn.PgConn().Exec(f.ctx, string(script)).ReadAll()
		if wrong {
			var failure *pgconn.PgError
			if !errors.As(runErr, &failure) || failure.Code != "22023" || failure.Message != "AUDIT_CATALOG_ACTIVITY_DATABASE_MISMATCH" {
				conn.Close(f.ctx)
				t.Fatalf("wrong target did not refuse precisely: %v", runErr)
			}
			_, _ = conn.Exec(f.ctx, "rollback")
		} else {
			if runErr != nil {
				conn.Close(f.ctx)
				t.Fatal(runErr)
			}
			found := false
			for _, set := range rows {
				if len(set.FieldDescriptions) != 1 || string(set.FieldDescriptions[0].Name) != "diagnosis" {
					continue
				}
				var got struct {
					Dry        bool   `json:"dry_run"`
					Repair     bool   `json:"repair_supported"`
					ReadOnly   string `json:"transaction_read_only"`
					Known      bool   `json:"scope_known"`
					Tasks      int    `json:"tasks"`
					Activities int    `json:"retained_activity_rows"`
					Queued     int    `json:"queued_activity_intents"`
					Candidate  bool   `json:"count_difference_candidate"`
					Attributed bool   `json:"historical_attribution_proven"`
				}
				if len(set.Rows) != 1 || json.Unmarshal(set.Rows[0][0], &got) != nil || !got.Dry || got.Repair || got.ReadOnly != "on" || !got.Known || got.Tasks != 1 || got.Activities != 1 || got.Queued != 0 || got.Candidate || got.Attributed {
					conn.Close(f.ctx)
					t.Fatalf("read-only counts overstated the actual current task: %+v", got)
				}
				found = true
			}
			if !found {
				conn.Close(f.ctx)
				t.Fatal("diagnostic omitted typed result")
			}
		}
		if err := conn.Close(f.ctx); err != nil {
			t.Fatal(err)
		}
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAIActivityFacts(t, f.test013Fixture))
	t.Log("actual HTTP-created task+actual consumer→readonlyRR SQL1task/1activity/queue0, attributionfalse; exact wrongDB22023, no business/financial/log changes")
}
