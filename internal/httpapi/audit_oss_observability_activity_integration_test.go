package httpapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditOSSObservabilityReadCannotEarnUploadOrDownloadContributionRewardIntegration(t *testing.T) {
	for _, entry := range []struct {
		route, action string
		actionID      int16
	}{
		{"/api/v1/admin/oss/uploads", "upload", activity.ActionUpload},
		{"/api/v1/admin/oss/downloads", "download", activity.ActionDownload},
	} {
		t.Run(entry.action, func(t *testing.T) {
			f := newTEST037Fixture(t)
			owner := f.userIDs[f.editor]
			grantTEST044Permissions(t, f.ctx, f.db, owner, "oss.read")
			if _, err := f.db.Exec(f.ctx, `update task_definitions set status='disabled'`); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('owned_observability','Owned observability read',jsonb_build_object('action',$1::text,'objectType','file','metric','count','target',1),'{}'::jsonb||'{"experience":25,"currencies":{"diamond":3}}'::jsonb,'active')`, entry.action); err != nil {
				t.Fatal(err)
			}
			monitor := auditFixActivityMonitor(t, f.test013Fixture)
			original := f.origin.Config.Handler
			completed := make(chan struct{}, 2)
			f.origin.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				original.ServeHTTP(w, r)
				if r.URL.Path == entry.route {
					completed <- struct{}{}
				}
			})
			for range 2 {
				auditOSSRequire(t, f, f.editor, http.MethodGet, entry.route, nil, 200)
				select {
				case <-completed:
				case <-time.After(10 * time.Second):
					t.Fatal("response middleware did not finish its activity boundary")
				}
			}
			// Request completion is an explicit barrier; Close then flushes both durable
			// and best-effort queues. No sleep or early zero can mask the old reward.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := monitor.Close(ctx); err != nil {
				t.Fatal(err)
			}
			var contributions, rewarded, xp, currency, ledgers, views int64
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from user_activity_events where user_id=$1 and action_id=$2 and object_type_id=$3),(select count(*) from user_task_progress where user_id=$1 and rewarded_at is not null),coalesce((select experience from user_experience where user_id=$1),0),coalesce((select b.balance from user_currency_balances b join currencies c on c.id=b.currency_id where b.user_id=$1 and c.code='diamond'),0),(select count(*) from currency_transactions where user_id=$1),(select count(*) from user_activity_events where user_id=$1 and action_id=$4 and object_type_id=$3)`, owner, entry.actionID, activity.ObjectFile, activity.ActionView).Scan(&contributions, &rewarded, &xp, &currency, &ledgers, &views); err != nil {
				t.Fatal(err)
			}
			if contributions+rewarded+xp+currency+ledgers != 0 {
				t.Fatalf("observability GET produced %s contribution/reward/xp/currency/ledger=%d/%d/%d/%d/%d", entry.action, contributions, rewarded, xp, currency, ledgers)
			}
			if views != 1 {
				t.Fatalf("explicit best-effort view history/throttle changed: views=%d want1", views)
			}
		})
	}
}

func TestAuditOSSDownloadTelemetryFailureRemainsBestEffortWithDurableCapabilityActivityIntegration(t *testing.T) {
	for _, scope := range []string{"user", "admin"} {
		t.Run(scope, func(t *testing.T) {
			f := newTEST037Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "oss.read")
			source := []byte("owned download telemetry bytes")
			ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "telemetry.zip", "application/octet-stream", "test037-telemetry", source)
			f.put(t, ticket, source)
			auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201)
			var prior int
			if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectFile).Scan(&prior); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(f.ctx, `create function audit_oss_telemetry_fault() returns trigger language plpgsql as $$ begin raise exception 'AUDIT_OSS_TELEMETRY_ONLY_FAILURE' using errcode='XX001'; end $$; create trigger audit_oss_telemetry_fault before insert or update on oss_download_stats for each row execute function audit_oss_telemetry_fault()`); err != nil {
				t.Fatal(err)
			}
			beforeMetric := f.server.ossWrites.snapshot().DownloadStatFailures
			route := "/api/v1/users/me/files/presign"
			if scope == "admin" {
				route = "/api/v1/admin/oss/files/presign"
			}
			payload := decodeTEST022Data[struct {
				URL string `json:"url"`
			}](t, auditOSSRequire(t, f, f.editor, http.MethodPost, route, map[string]any{"objectKey": ticket.ObjectKey}, 200))
			var downloads, intents int
			if err := f.db.QueryRow(f.ctx, `select (select count(*) from oss_download_stats where object_key=$1),(select count(*) from activity_event_outbox where user_id=$2 and action_id=$3 and object_type_id=$4)`, ticket.ObjectKey, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectFile).Scan(&downloads, &intents); err != nil || downloads != 0 || intents != prior+1 {
				t.Fatalf("telemetry failure changed authoritative outcome: statrows/intents=%d/%d prior=%d err=%v", downloads, intents, prior, err)
			}
			if got := f.server.ossWrites.snapshot().DownloadStatFailures; got != beforeMetric+1 {
				t.Fatalf("telemetry-only fault not observable: failures=%d prior=%d", got, beforeMetric)
			}
			request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, payload.URL, nil)
			if err != nil {
				t.Fatal("prepare owned signed download failed")
			}
			response, err := f.provider.Client().Do(request)
			if err != nil {
				t.Fatalf("owned signed download failed type=%T", err)
			}
			body, readErr := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if readErr != nil || response.StatusCode != 200 || !bytes.Equal(body, source) {
				t.Fatalf("telemetry outage broke authorized actual download: status=%d byteCount=%d readError=%v", response.StatusCode, len(body), readErr != nil)
			}
		})
	}
}
