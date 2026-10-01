package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

type seedRecoveryFixture struct {
	actor, candidate, task, run, draft int64
	uid, draftUID, job                 string
}

func createSeedRecoveryFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, name string) seedRecoveryFixture {
	t.Helper()
	lease, candidate, job, raw, actor := seedAIFixture(t, ctx, pool, name)
	run := lease.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity).RunID
	if _, err := pool.Exec(ctx, `update seed_crawler_runs set started_at=now()-interval '1 minute' where id=$1`, run); err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	applySeedDraftTranslations(fields, nil, "mod")
	draftRaw, _ := json.Marshal(fields)
	f := seedRecoveryFixture{actor: actor, candidate: candidate, run: run, job: job}
	if err := pool.QueryRow(ctx, `insert into user_drafts(user_id,draft_key,project_key,kind,title,edit_url,payload,expires_at) values($1,$2,$3,'seed_crawler_import','Example','/mods/new',$4::jsonb,now()+interval '1 day') returning id,public_id`, actor, "seed-crawler:"+name, "mod:"+name, draftRaw).Scan(&f.draft, &f.draftUID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update seed_crawler_candidates set status='draft',payload=jsonb_build_object('draftPublicId',$2::text) where id=$1`, candidate, f.draftUID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update seed_crawler_runs set status='completed',lease_owner='',lease_expires_at=null,finished_at=now() where id=$1`, run); err != nil {
		t.Fatal(err)
	}
	payload := seedTranslationPayload{Scope: "seed_crawler", CandidateID: candidate, MetadataJobID: job, SourceHash: seedSourceHash(raw), RunID: run, RunToken: "old-finished", SourceLocale: "en-US", TargetLocale: "zh-CN", Items: []map[string]string{{"key": "primaryName", "text": "Example"}, {"key": "summary", "text": "Example"}, {"key": "bodyMarkdown", "text": "Example"}}}
	f.task, f.uid = insertAITestTask(t, ctx, pool, aiTaskContentTranslation, "failed", payload, 3000)
	if _, err := pool.Exec(ctx, `update ai_tasks set created_by=$2,started_at=now(),input_tokens=11,output_tokens=7,cost_micros=25 where id=$1`, f.task, actor); err != nil {
		t.Fatal(err)
	}
	var permission int64
	if err := pool.QueryRow(ctx, `insert into permissions(code,module,name) values('user.ai.daily_token_limit.1000000','user','Synthetic recovery quota') on conflict(code) do update set name=excluded.name returning id`).Scan(&permission); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into user_permissions(user_id,permission_id) values($1,$2)`, actor, permission); err != nil {
		t.Fatal(err)
	}
	return f
}

func retrySeedFixture(ctx context.Context, s *Server, f seedRecoveryFixture) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks/"+f.uid+"/retry", strings.NewReader(`{}`))
	r.SetPathValue("id", f.uid)
	r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: f.actor}))
	w := httptest.NewRecorder()
	s.retryAITask(w, r)
	return w
}

func recoveryMessage(t *testing.T, ctx context.Context, pool *pgxpool.Pool, response *httptest.ResponseRecorder) (aiTaskMessage, seedRecoveryPayload) {
	t.Helper()
	if response.Code != http.StatusCreated {
		t.Fatalf("retry status=%d body=%s", response.Code, response.Body.String())
	}
	var data struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	var msg aiTaskMessage
	var raw []byte
	if err := pool.QueryRow(ctx, `select id,task_uid,task_type,payload from ai_tasks where task_uid=$1`, data.Data.ID).Scan(&msg.TaskID, &msg.TaskUID, &msg.TaskType, &raw); err != nil {
		t.Fatal(err)
	}
	var p seedRecoveryPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return msg, p
}

func TestSeedCompletedRunAdminRecoveryDraftCASIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	cfg.NATS.OutboxEnabled = true
	var calls atomic.Int64
	var hold atomic.Bool
	var providerFail atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		if providerFail.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if hold.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":[{"key":"primaryName","text":"中文恢复"},{"key":"summary","text":"恢复简介"},{"key":"bodyMarkdown","text":"恢复正文"}]}`}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 7}})
	}))
	defer provider.Close()
	settings := aiTestConfig(provider.URL)
	setAITestConfig(t, ctx, pool, cfg, settings)
	if _, err := pool.Exec(ctx, `insert into seed_crawler_configs(enabled,ai_daily_token_budget) values(true,1000000)`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: cfg}
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	t.Run("completed original run explicitly restores its existing owner draft once", func(t *testing.T) {
		f := createSeedRecoveryFixture(t, ctx, pool, "recovernormal")
		if _, err := pool.Exec(ctx, `update user_drafts set payload=jsonb_set(payload,'{localizations}',(payload->'localizations')||'[{"locale":"fr-FR","name":"Human French","editorNote":"Keep"}]'::jsonb) where id=$1`, f.draft); err != nil {
			t.Fatal(err)
		}
		msg, payload := recoveryMessage(t, ctx, pool, retrySeedFixture(ctx, server, f))
		if payload.RunID == f.run || payload.DraftID != f.draft || payload.Scope != seedRecoveryScope {
			t.Fatal("recovery did not receive a separate identity and bound draft")
		}
		if duplicate := retrySeedFixture(ctx, server, f); duplicate.Code != http.StatusConflict || !strings.Contains(duplicate.Body.String(), "AI_RETRY_EXISTS") {
			t.Fatalf("duplicate retry=%d %s", duplicate.Code, duplicate.Body.String())
		}
		raw, _ := json.Marshal(msg)
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := worker.handleTask(ctx, raw); err != nil {
					t.Errorf("worker=%v", err)
				}
			}()
		}
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatalf("provider called=%d", calls.Load())
		}
		r := httptest.NewRequest(http.MethodGet, "/api/v1/user/drafts/"+f.draftUID, nil)
		r.SetPathValue("draftId", f.draftUID)
		r = r.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: f.actor}))
		w := httptest.NewRecorder()
		server.userDraftItem(w, r)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "中文恢复") || !strings.Contains(w.Body.String(), "恢复简介") || !strings.Contains(w.Body.String(), "恢复正文") || !strings.Contains(w.Body.String(), "Human French") || !strings.Contains(w.Body.String(), "editorNote") {
			t.Fatalf("existing draft not visible=%d %s", w.Code, w.Body.String())
		}
		var oldStatus, newStatus, runStatus string
		var oldUsage, oldCost, resources, outbox int64
		var latestStatus string
		if err := pool.QueryRow(ctx, `select status from seed_crawler_translation_tasks where candidate_id=$1 and locale='zh-CN'`, f.candidate).Scan(&latestStatus); err != nil || latestStatus != "completed" {
			t.Fatalf("latest seed projection=%s %v", latestStatus, err)
		}
		if err := pool.QueryRow(ctx, `select status,input_tokens+output_tokens,cost_micros from ai_tasks where id=$1`, f.task).Scan(&oldStatus, &oldUsage, &oldCost); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select t.status,r.status from ai_tasks t join seed_crawler_runs r on r.id=$2 where t.id=$1`, msg.TaskID, payload.RunID).Scan(&newStatus, &runStatus); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select count(*) from mods`).Scan(&resources); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_id=$1`, msg.TaskUID).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		if oldStatus != "failed" || oldUsage != 18 || oldCost != 25 || newStatus != "completed" || runStatus != "completed" || resources != 0 || outbox != 1 {
			t.Fatalf("old=%s/%d/%d new=%s run=%s public=%d outbox=%d", oldStatus, oldUsage, oldCost, newStatus, runStatus, resources, outbox)
		}
		if err := pool.QueryRow(ctx, `select status from seed_crawler_runs where id=$1`, f.run).Scan(&runStatus); err != nil || runStatus != "completed" {
			t.Fatalf("original run changed=%s %v", runStatus, err)
		}
	})
	for index, scenario := range []string{"draft edit", "human target", "metadata source edit", "draft delete", "draft deleted and recreated", "candidate delete", "draft expired", "task cancelled", "lease replaced", "lease expired", "actor disabled", "draft submitted"} {
		t.Run(scenario, func(t *testing.T) {
			f := createSeedRecoveryFixture(t, ctx, pool, fmt.Sprintf("recoverguard%d", index))
			msg, payload := recoveryMessage(t, ctx, pool, retrySeedFixture(ctx, server, f))
			hold.Store(true)
			release = make(chan struct{})
			raw, _ := json.Marshal(msg)
			done := make(chan error, 1)
			go func() { done <- worker.handleTask(ctx, raw) }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("provider not entered")
			}
			var err error
			switch scenario {
			case "draft edit":
				_, err = pool.Exec(ctx, `update user_drafts set title='Human title',updated_at=clock_timestamp() where id=$1`, f.draft)
			case "human target":
				_, err = pool.Exec(ctx, `update user_drafts set payload=jsonb_set(payload,'{localizations}',(payload->'localizations')||'[ {"locale":"zh-CN","name":"Manual","summary":"Human"} ]'::jsonb),updated_at=clock_timestamp() where id=$1`, f.draft)
			case "metadata source edit":
				_, err = pool.Exec(ctx, `update mod_metadata_import_jobs set result=result||'{"primaryName":"Changed"}'::jsonb where public_id=$1`, f.job)
			case "draft delete":
				_, err = pool.Exec(ctx, `delete from user_drafts where id=$1`, f.draft)
			case "draft deleted and recreated":
				_, err = pool.Exec(ctx, `with removed as(delete from user_drafts where id=$1 returning *) insert into user_drafts(user_id,draft_key,project_key,kind,title,edit_url,payload,expires_at) select user_id,draft_key,project_key,kind,title,edit_url,payload,expires_at from removed`, f.draft)
			case "actor disabled":
				_, err = pool.Exec(ctx, `update users set status='disabled' where id=$1`, f.actor)
			case "draft submitted":
				_, err = pool.Exec(ctx, `update user_drafts set submitted_at=now(),submitted_status='approved' where id=$1`, f.draft)
			case "candidate delete":
				_, err = pool.Exec(ctx, `delete from seed_crawler_candidates where id=$1`, f.candidate)
			case "draft expired":
				_, err = pool.Exec(ctx, `update user_drafts set expires_at=now()-interval '1 second' where id=$1`, f.draft)
			case "task cancelled":
				_, err = pool.Exec(ctx, `update ai_tasks set status='cancelled' where id=$1`, msg.TaskID)
			case "lease replaced":
				_, err = pool.Exec(ctx, `update seed_crawler_runs set lease_owner='replacement' where id=$1`, payload.RunID)
			case "lease expired":
				_, err = pool.Exec(ctx, `update seed_crawler_runs set lease_expires_at=now()-interval '1 second' where id=$1`, payload.RunID)
			}
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			hold.Store(false)
			if err = <-done; err == nil {
				t.Fatal("late recovery unexpectedly published")
			}
			var published, usage int64
			if err = pool.QueryRow(ctx, `select count(*) from user_drafts where user_id=$1 and payload::text like '%中文恢复%'`, f.actor).Scan(&published); err != nil || published != 0 {
				t.Fatalf("late draft publication=%d %v", published, err)
			}
			if err = pool.QueryRow(ctx, `select input_tokens+output_tokens from ai_tasks where id=$1`, msg.TaskID).Scan(&usage); err != nil || usage != 18 {
				t.Fatalf("returned billed usage lost=%d %v", usage, err)
			}
		})
	}

	t.Run("possibly billed old and new failures retain reservations without automatic recall", func(t *testing.T) {
		f := createSeedRecoveryFixture(t, ctx, pool, "recoverunknown")
		if _, err := pool.Exec(ctx, `update ai_tasks set input_tokens=0,output_tokens=0,cost_micros=0,quota_reserved_tokens=7000,payload=payload||'{"quotaReservedCostMicros":12345}'::jsonb where id=$1`, f.task); err != nil {
			t.Fatal(err)
		}
		msg, _ := recoveryMessage(t, ctx, pool, retrySeedFixture(ctx, server, f))
		providerFail.Store(true)
		before := calls.Load()
		raw, _ := json.Marshal(msg)
		if err := worker.handleTask(ctx, raw); err == nil {
			t.Fatal("503 should fail")
		}
		providerFail.Store(false)
		if err := worker.handleTask(ctx, raw); err != nil {
			t.Fatal(err)
		}
		worker.recoverTasks(ctx)
		if calls.Load() != before+1 {
			t.Fatalf("unknown request automatically repeated=%d/%d", calls.Load(), before)
		}
		var oldUsage, oldReserved, oldCost, newUsage, newReserved, newCost int64
		if err := pool.QueryRow(ctx, `select input_tokens+output_tokens,quota_reserved_tokens,(payload->>'quotaReservedCostMicros')::bigint from ai_tasks where id=$1`, f.task).Scan(&oldUsage, &oldReserved, &oldCost); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select input_tokens+output_tokens,quota_reserved_tokens,(payload->>'quotaReservedCostMicros')::bigint from ai_tasks where id=$1 and status='failed'`, msg.TaskID).Scan(&newUsage, &newReserved, &newCost); err != nil {
			t.Fatal(err)
		}
		if oldUsage != 0 || oldReserved != 7000 || oldCost != 12345 || newUsage != 0 || newReserved <= 0 || newCost <= 0 {
			t.Fatalf("unknown fees released: old %d/%d/%d new %d/%d/%d", oldUsage, oldReserved, oldCost, newUsage, newReserved, newCost)
		}
	})
	t.Run("legacy draft created within completed run receives a verified stable binding", func(t *testing.T) {
		f := createSeedRecoveryFixture(t, ctx, pool, "recoveroldvalid")
		if _, err := pool.Exec(ctx, `update seed_crawler_candidates set payload='{}'::jsonb where id=$1`, f.candidate); err != nil {
			t.Fatal(err)
		}
		msg, _ := recoveryMessage(t, ctx, pool, retrySeedFixture(ctx, server, f))
		raw, _ := json.Marshal(msg)
		if err := worker.handleTask(ctx, raw); err != nil {
			t.Fatal(err)
		}
		var bound string
		if err := pool.QueryRow(ctx, `select payload->>'draftPublicId' from seed_crawler_candidates where id=$1`, f.candidate).Scan(&bound); err != nil || bound != f.draftUID {
			t.Fatalf("legacy binding=%s %v", bound, err)
		}
	})
	t.Run("unbound legacy draft and already human target cannot be guessed or replaced", func(t *testing.T) {
		f := createSeedRecoveryFixture(t, ctx, pool, "recoverlegacy")
		if _, err := pool.Exec(ctx, `update seed_crawler_candidates set payload='{}'::jsonb where id=$1`, f.candidate); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `update user_drafts set updated_at=now()+interval '1 second' where id=$1`, f.draft); err != nil {
			t.Fatal(err)
		}
		if w := retrySeedFixture(ctx, server, f); w.Code != http.StatusConflict {
			t.Fatalf("unbound draft=%d %s", w.Code, w.Body.String())
		}
		if _, err := pool.Exec(ctx, `update seed_crawler_candidates set payload=jsonb_build_object('draftPublicId',$2::text) where id=$1`, f.candidate, f.draftUID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `update user_drafts set payload=jsonb_set(payload,'{localizations}',(payload->'localizations')||'[{"locale":"zh-CN","name":"Manual"}]'::jsonb) where id=$1`, f.draft); err != nil {
			t.Fatal(err)
		}
		if w := retrySeedFixture(ctx, server, f); w.Code != http.StatusConflict {
			t.Fatalf("human target=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("generic administrator creation cannot forge crawler recovery identity", func(t *testing.T) {
		body := fmt.Sprintf(`{"taskType":%q,"payload":{"scope":"seed_crawler_recovery"}}`, aiTaskContentTranslation)
		r := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks", strings.NewReader(body))
		r = r.WithContext(ctx)
		w := httptest.NewRecorder()
		server.createAITask(w, r)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "fenced crawler") {
			t.Fatalf("forged recovery=%d %s", w.Code, w.Body.String())
		}
	})
	t.Run("failed terminal cleanup is recovered without queue or another provider charge", func(t *testing.T) {
		f := createSeedRecoveryFixture(t, ctx, pool, "recovercleanup")
		msg, payload := recoveryMessage(t, ctx, pool, retrySeedFixture(ctx, server, f))
		if _, err := pool.Exec(ctx, `create function audit_reject_recovery_cleanup() returns trigger language plpgsql as $$ begin
 if new.stats->>'kind'='translation_recovery' and new.status='failed' then raise exception 'synthetic terminal cleanup failure'; end if; return new; end $$;
 create trigger audit_reject_recovery_cleanup before update on seed_crawler_runs for each row execute function audit_reject_recovery_cleanup()`); err != nil {
			t.Fatal(err)
		}
		before := calls.Load()
		providerFail.Store(true)
		raw, _ := json.Marshal(msg)
		if err := worker.handleTask(ctx, raw); err == nil {
			t.Fatal("503 must fail the task")
		}
		providerFail.Store(false)
		var runStatus, taskStatus, token string
		if err := pool.QueryRow(ctx, `select r.status,t.status,r.lease_owner from seed_crawler_runs r,ai_tasks t where r.id=$1 and t.id=$2`, payload.RunID, msg.TaskID).Scan(&runStatus, &taskStatus, &token); err != nil || runStatus != "running" || taskStatus != "failed" || token != payload.RunToken {
			t.Fatalf("failure fixture did not retain unfinished run: run=%s task=%s tokenMatches=%v error=%v", runStatus, taskStatus, token == payload.RunToken, err)
		}
		if _, err := pool.Exec(ctx, `drop trigger audit_reject_recovery_cleanup on seed_crawler_runs; drop function audit_reject_recovery_cleanup()`); err != nil {
			t.Fatal(err)
		}
		scanCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if err := worker.Start(scanCtx); !errors.Is(err, queue.ErrUnavailable) {
			t.Fatalf("missing queue start=%v", err)
		}
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			if err := pool.QueryRow(scanCtx, `select status,lease_owner from seed_crawler_runs where id=$1`, payload.RunID).Scan(&runStatus, &token); err != nil {
				t.Fatal(err)
			}
			if runStatus == "failed" && token == "" {
				break
			}
			select {
			case <-scanCtx.Done():
				t.Fatal("existing AI recovery scan did not finish the run")
			case <-ticker.C:
			}
		}
		if calls.Load() != before+1 {
			t.Fatal("terminal cleanup unexpectedly called the provider again")
		}
	})
	t.Run("site crawler and original actor budgets reject before any call", func(t *testing.T) {
		f := createSeedRecoveryFixture(t, ctx, pool, "recoverbudget")
		before := calls.Load()
		if _, err := pool.Exec(ctx, `update seed_crawler_configs set ai_daily_token_budget=1`); err != nil {
			t.Fatal(err)
		}
		if w := retrySeedFixture(ctx, server, f); w.Code != http.StatusTooManyRequests {
			t.Fatalf("crawler budget=%d %s", w.Code, w.Body.String())
		}
		if _, err := pool.Exec(ctx, `update seed_crawler_configs set ai_daily_token_budget=1000000`); err != nil {
			t.Fatal(err)
		}
		limited := settings
		limited.Quotas = []aiQuotaConfig{{Scope: "site", Period: "day", TokenLimit: 1}}
		setAITestConfig(t, ctx, pool, cfg, limited)
		if w := retrySeedFixture(ctx, server, f); w.Code != http.StatusTooManyRequests {
			t.Fatalf("site budget=%d %s", w.Code, w.Body.String())
		}
		setAITestConfig(t, ctx, pool, cfg, settings)
		if _, err := pool.Exec(ctx, `delete from user_permissions where user_id=$1`, f.actor); err != nil {
			t.Fatal(err)
		}
		if w := retrySeedFixture(ctx, server, f); w.Code != http.StatusTooManyRequests {
			t.Fatalf("actor budget=%d %s", w.Code, w.Body.String())
		}
		var retries int
		if err := pool.QueryRow(ctx, `select count(*) from ai_tasks where payload->>'retryOf'=$1`, f.uid).Scan(&retries); err != nil || retries != 0 || calls.Load() != before {
			t.Fatalf("rejected retry retained=%d calls=%d/%d %v", retries, calls.Load(), before, err)
		}
	})
}
