package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/systemactor"
)

func TestIndependentP1F001ActualClaimNotificationWorkerDeliversOnce(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.claim")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "creator.claim.review")
	s := f.origin.Config.Handler.(*Server)
	worker := NewNotificationWorker(f.db, nil, s.cache, s.cfg.SMTP, s.cfg.SettingsEncryptionKey)
	for _, action := range []string{"approved", "rejected"} {
		var creator string
		if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author',$1,$1,'approved') returning public_id`, "Independent notify "+action).Scan(&creator); err != nil {
			t.Fatal(err)
		}
		claim := decodeTEST022Data[struct {
			ID string `json:"id"`
		}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators/"+creator+"/claims", map[string]string{"proofMarkdown": "Independent synthetic claim proof"}, 201))
		path := "/api/v1/admin/creator-claims/" + claim.ID
		f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": action, "note": "Independent notification reason"}, 200)
		var eventID string
		var payload []byte
		if err := f.db.QueryRow(f.ctx, `select event_id,payload from nats_outbox where subject='notifications' and event_type=$1 and payload#>>'{data,claimId}'=$2`, "creator.claim."+action, claim.ID).Scan(&eventID, &payload); err != nil {
			t.Fatal(err)
		}
		var event notificationEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.RecipientID != f.userIDs[f.editor] || event.ActorID != f.userIDs[f.reviewer] || event.TemplateKey != "creator_claim_"+action {
			t.Fatalf("wrong committed notification intent: %+v", event)
		}
		for i := 0; i < 2; i++ {
			if err := worker.handleEvent(queue.WithEventID(f.ctx, eventID), payload); err != nil {
				t.Fatal(err)
			}
		}
		var count int
		if err := f.db.QueryRow(f.ctx, `select count(*) from notifications where recipient_id=$1 and data->>'claimId'=$2`, f.userIDs[f.editor], claim.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("actual notification rows=%d/%v", count, err)
		}
		got := f.require(t, f.editor, http.MethodGet, "/api/v1/notifications?limit=100", nil, 200)
		if !strings.Contains(string(got), claim.ID) || strings.Contains(string(got), "Independent synthetic claim proof") {
			t.Error("actual notification missing owner target or exposing private proof")
		}
		f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": action}, 409)
		if err := f.db.QueryRow(f.ctx, `select count(*) from nats_outbox where subject='notifications' and event_type=$1 and payload#>>'{data,claimId}'=$2`, "creator.claim."+action, claim.ID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("duplicate review notification intents=%d/%v", count, err)
		}
	}
	t.Log("Actual claim HTTP -> committed outbox payload -> production NotificationWorker twice -> one recipient notification+HTTPread; controlled direct delivery, not NATS transport")
}

func TestIndependentP1CMP006CrawlerRechecksAuthorityAndLeaseAfterFetch(t *testing.T) {
	for _, mode := range []string{"revoke-during-fetch", "replace-lease-during-fetch", "expire-lease-during-fetch", "replace-lease-before-publication", "revoke-before-publication"} {
		t.Run(mode, func(t *testing.T) {
			f := newTEST013Fixture(t)
			s := f.origin.Config.Handler.(*Server)
			var actor, runID int64
			if err := f.db.QueryRow(f.ctx, `insert into users(username,email,password_hash,email_verified,status) values($1,$2,'disabled-login',true,$3) returning id`, systemactor.AutobotUsername, systemactor.AutobotEmail, systemactor.AutobotStatus).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			grantTEST044Permissions(t, f.ctx, f.db, actor, "project.create", "project.edit", "project.no-review", "content.no-review")
			var changed atomic.Bool
			change := func() {
				if !changed.CompareAndSwap(false, true) {
					return
				}
				var err error
				if strings.HasPrefix(mode, "revoke-") {
					_, err = f.db.Exec(f.ctx, `update user_permissions set allow=false where user_id=$1`, actor)
				} else if mode == "expire-lease-during-fetch" {
					_, err = f.db.Exec(f.ctx, `update seed_crawler_runs set lease_expires_at=clock_timestamp()-interval '1 second' where id=$1 and status='running'`, runID)
				} else {
					_, err = f.db.Exec(f.ctx, `update seed_crawler_runs set lease_owner='independent-replacement',lease_expires_at=now()+interval '5 minutes' where id=$1 and status='running'`, runID)
				}
				if err != nil {
					t.Error(err)
				}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/search":
					fmt.Fprint(w, `{"hits":[{"project_id":"independent-crawl","slug":"independent-crawl","title":"Independent crawler","description":"Synthetic","downloads":1000}],"total_hits":1}`)
				case "/project/independent-crawl":
					if strings.HasSuffix(mode, "during-fetch") {
						change()
					}
					fmt.Fprint(w, `{"id":"independent-crawl","slug":"independent-crawl","title":"Independent crawler","description":"Synthetic","body":"Independent source body","project_type":"mod","status":"approved","client_side":"required","server_side":"required","categories":["utility"],"game_versions":["1.21.1"],"loaders":["neoforge"],"license":{"id":"MIT"}}`)
				case "/project/independent-crawl/version":
					fmt.Fprint(w, "[]")
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(provider.Close)
			cfg := defaultModImportConfig()
			cfg.Modrinth.BaseURL = provider.URL
			cfg.Modrinth.Token = ""
			cfg.RequestTimeoutSeconds = 2
			sealed, err := s.sealSystemSetting(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb)`, modImportConfigSettingKey, sealed); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(f.ctx, `insert into seed_crawler_configs(id,enabled,project_types,batch_size,daily_limit,minimum_downloads,interval_seconds,max_concurrency,ai_daily_token_budget,auto_submit_review) values(true,false,array['mod'],1,10,0,3600,1,0,true)`); err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRow(f.ctx, `insert into seed_crawler_runs(dry_run) values(false) returning id`).Scan(&runID); err != nil {
				t.Fatal(err)
			}
			worker := NewSeedCrawlerWorker(s.cfg, f.db)
			if strings.HasSuffix(mode, "before-publication") {
				// The first check protects the draft transaction. The second is
				// inside the actual create/publish transaction, after its tentative
				// domain writes, so this independently verifies final rollback.
				cfg := f.db.Config()
				var checks atomic.Int32
				cfg.ConnConfig.Tracer = independentQueryStartHook(func(data pgx.TraceQueryStartData) {
					if strings.Contains(data.SQL, "from seed_crawler_runs where id=$1 for update") && checks.Add(1) == 2 {
						change()
					}
				})
				pool, err := pgxpool.NewWithConfig(f.ctx, cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(pool.Close)
				worker.server.db = pool
			}
			processed, err := worker.processOne(f.ctx)
			if !processed {
				t.Fatalf("actual business Worker did not run: %v", err)
			}
			leaseLost := !strings.HasPrefix(mode, "revoke-")
			if leaseLost && !errors.Is(err, errSeedCrawlerLeaseOwnershipLost) {
				t.Errorf("expected exact stale lease error, got %v", err)
			} else if !leaseLost && err != nil {
				t.Fatal(err)
			}
			if !changed.Load() {
				t.Fatal("provider barrier did not execute")
			}
			var projects, approvedRevisions, publishedEvents int
			if e := f.db.QueryRow(f.ctx, `select count(*) from mods where modrinth_project_id='independent-crawl'`).Scan(&projects); e != nil {
				t.Fatal(e)
			}
			if e := f.db.QueryRow(f.ctx, `select count(*) from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id where request.submitted_by=$1 and request.status='approved' and revision.entity_type='mod'`, actor).Scan(&approvedRevisions); e != nil {
				t.Fatal(e)
			}
			if projects != 0 || approvedRevisions != 0 {
				t.Errorf("late/revoked worker published: projects=%d approvedRevisions=%d mode=%s", projects, approvedRevisions, mode)
			}
			if e := f.db.QueryRow(f.ctx, `select count(*) from nats_outbox where subject='project-updates'`).Scan(&publishedEvents); e != nil || publishedEvents != 0 {
				t.Errorf("late/revoked worker publication events=%d/%v", publishedEvents, e)
			}
			if !leaseLost {
				var diagnostic string
				if e := f.db.QueryRow(f.ctx, `select last_error from seed_crawler_candidates where external_project_id='independent-crawl'`).Scan(&diagnostic); e != nil {
					t.Fatal(e)
				}
				if !strings.Contains(diagnostic, errAutomationPermissionDenied.Error()) {
					t.Errorf("wrong authority diagnostic=%q", diagnostic)
				}
			}
		})
	}
}

type independentQueryStartHook func(pgx.TraceQueryStartData)

func (h independentQueryStartHook) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	h(data)
	return ctx
}
func (independentQueryStartHook) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}
