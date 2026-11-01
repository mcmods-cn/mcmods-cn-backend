package httpapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/queue"
)

func auditReviewResultNotificationFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := auditFixProjectActivityFacts(t, f)
	rows, err := f.db.Query(f.ctx, `select tablename from pg_tables where schemaname=current_schema() and (tablename='creators' or tablename like 'creator_%' or tablename in ('content_localizations','content_creator_bindings','notification_actors','permission_audit_logs')) order by tablename`)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		var raw string
		if err = f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{name}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[name] = raw
	}
	return facts
}

func TestAuditCMP003ReviewResultNotificationSharesPublicationDecisionHTTPPostgresIntegration(t *testing.T) {
	for _, kind := range []string{"modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon", "creator"} {
		for _, decision := range []string{"approved", "rejected"} {
			t.Run(kind+"/"+decision, func(t *testing.T) {
				f := newAuditFixGRFixture(t)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create", "creator.create", "project.create."+kind)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
				var revision, public, table, dataKey string
				switch kind {
				case "modpack":
					snapshot := perf020ModpackSnapshot("notification-pack", 0)
					snapshot.PrimaryName = "Notification pack"
					snapshot.Compatibilities = []modLoaderCompatibilityPayload{{Loader: "forge", Versions: []string{"1.20.1"}}}
					result := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", snapshot, 201))
					revision, public, table, dataKey = result.SubmissionRevisionID, result.PublicID, "modpacks", "modpackId"
				case "creator":
					snapshot := creatorSnapshot{Kind: "author", Name: "Notification author", DescriptionMarkdown: "Owned notification atomicity fixture", DefaultLocale: "en-US", Localizations: []creatorLocalizationEdit{{Locale: "en-US", ContentMarkdown: "Owned notification atomicity fixture"}}, Links: []creatorLinkPayload{}}
					result := decodeTEST022Data[struct {
						PublicID   string `json:"publicId"`
						RevisionID string `json:"revisionId"`
						Status     string `json:"reviewStatus"`
					}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators", snapshot, 201))
					if result.Status != "pending" {
						t.Fatal("creator prerequisite unexpectedly bypassed independent review")
					}
					revision, public, table, dataKey = result.RevisionID, result.PublicID, "creators", "creatorId"
				default:
					snapshot := test014Snapshot(f.test013Fixture, kind, "notification-"+strings.ReplaceAll(kind, "_", "-"), "Notification "+kind, "1.20.1")
					result := test014Create(t, f.test013Fixture, snapshot)
					revision, public, table, dataKey = result.SubmissionRevisionID, result.PublicID, "simple_projects", "projectId"
				}
				if revision == "" || public == "" {
					t.Fatal("actual ordinary submission did not provide public identities")
				}
				if _, err := f.db.Exec(f.ctx, `create function audit_cmp003_review_notify_fault() returns trigger language plpgsql as $$ begin
     if new.payload->>'templateKey' in ('review_approved','review_rejected') then
      raise exception 'AUDIT_CMP003_REVIEW_NOTIFICATION_INTENT_UNAVAILABLE' using errcode='XX001',constraint='audit_cmp003_review_notification_intent';
     end if;
     return new;
    end $$;
    create trigger audit_cmp003_review_notify_fault before insert on nats_outbox for each row execute function audit_cmp003_review_notify_fault()`); err != nil {
					t.Fatal(err)
				}
				body := map[string]string{"status": decision, "note": "Independent notification decision fixture"}
				path := "/api/v1/content-revisions/" + revision
				before := auditReviewResultNotificationFacts(t, f.test013Fixture)
				failed := <-auditCompletionAsyncJSON(f.test013Fixture, f.reviewer, http.MethodPatch, path, body)
				if failed.err != nil {
					t.Fatal(failed.err)
				}
				after := auditReviewResultNotificationFacts(t, f.test013Fixture)
				if failed.status != 500 {
					changed := []string{}
					for key, prior := range before {
						if after[key] != prior {
							changed = append(changed, key)
						}
					}
					t.Fatalf("precise result-notification fault returnedHTTP%d instead of500; committed business tables=%v", failed.status, changed)
				}
				var problem struct {
					Error string `json:"error"`
					Code  string `json:"code"`
				}
				if json.Unmarshal(failed.raw, &problem) != nil || problem.Code != "HTTP_500" || !strings.Contains(problem.Error, "review notification") {
					t.Fatalf("unrelated failure did not prove result-notification boundary: code=%s error=%s", problem.Code, problem.Error)
				}
				if !reflect.DeepEqual(before, after) {
					auditFixRequireProjectFactsUnchanged(t, before, after)
				}
				if _, err := f.db.Exec(f.ctx, `drop trigger audit_cmp003_review_notify_fault on nats_outbox;drop function audit_cmp003_review_notify_fault()`); err != nil {
					t.Fatal(err)
				}
				f.require(t, f.reviewer, http.MethodPatch, path, body, 200)
				stable := auditReviewResultNotificationFacts(t, f.test013Fixture)
				f.require(t, f.reviewer, http.MethodPatch, path, body, 409)
				auditFixRequireProjectFactsUnchanged(t, stable, auditReviewResultNotificationFacts(t, f.test013Fixture))
				template := "review_" + decision
				var intents, events int
				if err := f.db.QueryRow(f.ctx, `select
     (select count(*) from nats_outbox where payload->>'templateKey'=$1 and payload->>'recipientId'=$2::text and payload->'data'->>$3::text=$4 and event_type='notification.direct'),
     (select count(*) from review_events event join change_requests request on request.id=event.change_request_id join content_revisions revision on revision.id=request.proposed_revision_id where revision.public_id=$5 and event.event_type=$6)`, template, strconv.FormatInt(f.userIDs[f.editor], 10), dataKey, public, revision, decision).Scan(&intents, &events); err != nil {
					t.Fatal(err)
				}
				if intents != 1 || events != 1 {
					t.Fatalf("recovery/duplicate result intents=%d decisionEvents=%d want1/1", intents, events)
				}
				var reviewStatus string
				var published *int64
				if err := f.db.QueryRow(f.ctx, `select review_status,published_revision_id from `+pgx.Identifier{table}.Sanitize()+` where public_id=$1`, public).Scan(&reviewStatus, &published); err != nil {
					t.Fatal(err)
				}
				if reviewStatus != decision || (decision == "approved") != (published != nil) {
					t.Fatalf("recovery state=%s pointerPresent=%t", reviewStatus, published != nil)
				}
				var eventID string
				var notificationRaw []byte
				if err := f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where payload->>'templateKey'=$1 and payload->>'recipientId'=$2::text and payload->'data'->>$3::text=$4 and event_type='notification.direct'`, template, strconv.FormatInt(f.userIDs[f.editor], 10), dataKey, public).Scan(&eventID, &notificationRaw); err != nil {
					t.Fatal(err)
				}
				notifications := &NotificationWorker{db: f.db, cache: f.server.cache}
				for range 2 {
					if err := notifications.handleEvent(queue.WithEventID(f.ctx, eventID), notificationRaw); err != nil {
						t.Fatal(err)
					}
				}
				if n := f.number(t, `select count(*) from notifications where source_event_id=$1 and recipient_id=$2 and template_key=$3 and title<>'' and body<>''`, eventID, f.userIDs[f.editor], template); n != 1 {
					t.Fatalf("actual duplicate notification consumer persisted %d result notifications want1", n)
				}
				t.Logf("CMP003_REVIEW_NOTIFY kind=%s decision=%s exactfailure500/zero fullfacts; recovery200/oneoriginaldirectintent/onedecision; duplicate409/zero sideeffects; actual duplicate notification handler1rendered result; noNATSTransportClaim", kind, decision)
			})
		}
	}
}
