package httpapi

import (
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/auditdiag"
	"mcmods-cn-backend/internal/queue"
)

func TestAuditCMP003ReviewResultDiagnosticIsReadOnlyAndNeverTreatsAbsenceAsProofIntegration(t *testing.T) {
	for _, kind := range []string{"modpack", "plugin", "creator"} {
		for _, decision := range []string{"approved", "rejected"} {
			t.Run(kind+"/"+decision, func(t *testing.T) {
				f := newAuditFixGRFixture(t)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create", "creator.create", "project.create.plugin")
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "project.review")
				var revision, public string
				switch kind {
				case "modpack":
					snapshot := perf020ModpackSnapshot("diagnose-pack", 0)
					snapshot.PrimaryName = "Diagnostic pack"
					snapshot.Compatibilities = []modLoaderCompatibilityPayload{{Loader: "forge", Versions: []string{"1.20.1"}}}
					result := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", snapshot, 201))
					revision, public = result.SubmissionRevisionID, result.PublicID
				case "creator":
					snapshot := creatorSnapshot{Kind: "author", Name: "Diagnostic author", DescriptionMarkdown: "Owned notification diagnostic fixture", DefaultLocale: "en-US", Localizations: []creatorLocalizationEdit{{Locale: "en-US", ContentMarkdown: "Owned notification diagnostic fixture"}}, Links: []creatorLinkPayload{}}
					result := decodeTEST022Data[struct {
						PublicID   string `json:"publicId"`
						RevisionID string `json:"revisionId"`
					}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators", snapshot, 201))
					revision, public = result.RevisionID, result.PublicID
				default:
					result := test014Create(t, f.test013Fixture, test014Snapshot(f.test013Fixture, kind, "diagnose-plugin", "Diagnostic plugin", "1.20.1"))
					revision, public = result.SubmissionRevisionID, result.PublicID
				}
				scope := auditdiag.ReviewResultScope{Kind: kind, PublicID: public, Since: time.Now().Add(-time.Minute), Until: time.Now().Add(time.Minute), Limit: 10}
				read := func(wantDecisions int) []auditdiag.ReviewResult {
					t.Helper()
					before := auditReviewResultNotificationFacts(t, f.test013Fixture)
					tx, err := f.db.BeginTx(f.ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback(f.ctx)
					var mode string
					if err = tx.QueryRow(f.ctx, `show transaction_read_only`).Scan(&mode); err != nil || mode != "on" {
						t.Fatal("diagnostic snapshot must be actually read-only")
					}
					items, err := auditdiag.ReadReviewResults(f.ctx, tx, scope)
					if err != nil || len(items) != wantDecisions {
						t.Fatalf("read decisions=%d want%d err=%v", len(items), wantDecisions, err)
					}
					if err = tx.Rollback(f.ctx); err != nil {
						t.Fatal(err)
					}
					auditFixRequireProjectFactsUnchanged(t, before, auditReviewResultNotificationFacts(t, f.test013Fixture))
					return items
				}
				read(0)
				f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+revision, map[string]string{"status": decision}, 200)
				item := read(1)[0]
				if item.RevisionID != revision || item.Decision != decision || !item.RecipientExists || item.ObservableIntents != 1 || item.ObservableNotifications != 0 || item.MissingCandidate {
					t.Fatalf("ordinary committed intent misclassified: %+v", item)
				}
				var eventID string
				var raw []byte
				if err := f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where event_type='notification.direct' and payload->>'templateKey'=$1`, "review_"+decision).Scan(&eventID, &raw); err != nil {
					t.Fatal(err)
				}
				worker := &NotificationWorker{db: f.db, cache: f.server.cache}
				if err := worker.handleEvent(queue.WithEventID(f.ctx, eventID), raw); err != nil {
					t.Fatal(err)
				}
				// Simulate retention only in the owned fixture. An existing rendered
				// notification still prevents treating its absent outbox as missing.
				if _, err := f.db.Exec(f.ctx, `delete from nats_outbox where event_id=$1`, eventID); err != nil {
					t.Fatal(err)
				}
				item = read(1)[0]
				if item.ObservableIntents != 0 || item.ObservableNotifications != 1 || item.MissingCandidate {
					t.Fatalf("retained actual notification misclassified: %+v", item)
				}
				if _, err := f.db.Exec(f.ctx, `delete from notifications where source_event_id=$1`, eventID); err != nil {
					t.Fatal(err)
				}
				item = read(1)[0]
				if !item.MissingCandidate {
					t.Fatal("absence must be reported only as a review candidate")
				}
				other := scope
				other.PublicID = randomCatalogPublicID()
				items, err := auditdiag.ReadReviewResults(f.ctx, f.db, other)
				if err != nil || len(items) != 0 {
					t.Fatalf("exact aggregate scope leaked another target: %v", err)
				}
				t.Log("DIAGNOSTIC exact ordinary HTTP decision, readonly snapshots zero writes, retained notification prevents missing candidate, absence only candidate; synthetic retention does not authorize replay")
			})
		}
	}
}
