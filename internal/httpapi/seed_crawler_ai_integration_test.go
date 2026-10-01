package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func seedAIFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, external string) (context.Context, int64, string, []byte, int64) {
	t.Helper()
	var user, run, candidate int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id`, external, external+"@example.invalid").Scan(&user); err != nil {
		t.Fatal(err)
	}
	token := randomHex(24)
	if err := pool.QueryRow(ctx, `insert into seed_crawler_runs(status,actor_id,lease_owner,lease_expires_at,attempts) values('running',$1,$2,now()+interval '5 minutes',1) returning id`, user, token).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into seed_crawler_candidates(run_id,external_project_id,project_type) values($1,$2,'mod') returning id`, run, external).Scan(&candidate); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"primaryName": "Example", "summary": "Example", "bodyMarkdown": "Example", "modrinthProjectId": external})
	var job string
	if err := pool.QueryRow(ctx, `insert into mod_metadata_import_jobs(user_id,project_type,provider,source_url,status,result) values($1,'mod','modrinth','https://modrinth.com/mod/example','completed',$2::jsonb) returning public_id`, user, string(raw)).Scan(&job); err != nil {
		t.Fatal(err)
	}
	return withSeedCrawlerLease(ctx, run, token), candidate, job, raw, user
}

func TestSeedCrawlerAITaskLedgerAndFencing(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var calls atomic.Int64
	var fail atomic.Bool
	var hold atomic.Bool
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if hold.Load() {
			entered <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"items":[{"key":"primaryName","text":"Example"}]}`}, "finish_reason": "stop"}}, "usage": map[string]int{"prompt_tokens": 11, "completion_tokens": 7}})
	}))
	defer provider.Close()
	cfg.NATS.OutboxEnabled = true
	setAITestConfig(t, ctx, pool, cfg, aiTestConfig(provider.URL))
	if _, err := pool.Exec(ctx, `insert into seed_crawler_configs(enabled,ai_daily_token_budget) values(true,1000000) on conflict(id) do update set enabled=true,ai_daily_token_budget=1000000`); err != nil {
		t.Fatal(err)
	}
	crawler := NewSeedCrawlerWorker(cfg, pool)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	items := []map[string]string{{"key": "primaryName", "text": "Example"}}
	leaseCtx, candidate, job, raw, user := seedAIFixture(t, ctx, pool, "seednormal")
	t.Run("concurrent enqueue and duplicate workers", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan aiTaskMessage, 8)
		errs := make(chan error, 8)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				msg, err := crawler.enqueueSeedTranslation(leaseCtx, candidate, job, raw, "zh-CN", items)
				results <- msg
				errs <- err
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		var first aiTaskMessage
		for msg := range results {
			if first.TaskID == 0 {
				first = msg
			}
			if msg.TaskID != first.TaskID {
				t.Fatal("duplicate task")
			}
		}
		message, _ := json.Marshal(first)
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := worker.handleTask(leaseCtx, message); err != nil {
					t.Errorf("worker: %v", err)
				}
			}()
		}
		wg.Wait()
		if calls.Load() != 1 {
			t.Fatalf("calls=%d", calls.Load())
		}
		var status string
		var input, output, outbox int64
		if err := pool.QueryRow(ctx, `select status,input_tokens,output_tokens from ai_tasks where id=$1`, first.TaskID).Scan(&status, &input, &output); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_id=$1`, first.TaskUID).Scan(&outbox); err != nil {
			t.Fatal(err)
		}
		if status != "completed" || input != 11 || output != 7 || outbox != 1 {
			t.Fatalf("status=%s usage=%d/%d outbox=%d", status, input, output, outbox)
		}
	})
	t.Run("completed translation reaches normal publication with task lineage", func(t *testing.T) {
		var payload map[string]any
		_ = json.Unmarshal(raw, &payload)
		payload["environment"] = "bothRequired"
		payload["primaryCategory"] = "utility"
		payload["officialStatus"] = "active"
		payload["sourceStatus"] = "open"
		payload["license"] = "MIT"
		payload["submissionMethod"] = "manual"
		applySeedDraftTranslations(payload, map[string]any{"zh-CN": map[string]any{"primaryName": "Example"}}, "mod")
		actor := security.Claims{Subject: user, PermissionRules: []security.PermissionRule{{Code: "project.no-review", Allow: true}}}
		if status, err := crawler.submitSeedDraft(leaseCtx, actor, "mod", "seednormal", "https://modrinth.com/mod/example", seedSubmissionPayload(payload)); err != nil || status != "approved" {
			t.Fatalf("autoSubmit status=%s err=%v", status, err)
		}
		var provenance, locale string
		var taskID int64
		if err := pool.QueryRow(ctx, `select localization.provenance,localization.source_locale,localization.ai_task_id from content_localizations localization join mods mod on mod.id=localization.subject_id where localization.subject_type='mod' and mod.modrinth_project_id='seednormal' and localization.locale='zh-CN'`).Scan(&provenance, &locale, &taskID); err != nil || provenance != "ai" || locale != "en-US" || taskID <= 0 {
			t.Fatalf("lineage=%s/%s task=%d err=%v", provenance, locale, taskID, err)
		}
	})
	t.Run("unknown billed failure retains reservation and never retries", func(t *testing.T) {
		fail.Store(true)
		msg, err := crawler.enqueueSeedTranslation(leaseCtx, candidate, job, raw, "de-DE", items)
		if err != nil {
			t.Fatal(err)
		}
		message, _ := json.Marshal(msg)
		if worker.handleTask(leaseCtx, message) == nil {
			t.Fatal("503 accepted")
		}
		repeated, err := crawler.enqueueSeedTranslation(leaseCtx, candidate, job, raw, "de-DE", items)
		if err != nil || repeated.TaskID != msg.TaskID {
			t.Fatalf("dedup %v", err)
		}
		if err = worker.handleTask(leaseCtx, message); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 {
			t.Fatalf("failed task repeated provider: %d", calls.Load())
		}
		if _, err = pool.Exec(ctx, `update seed_crawler_configs set ai_daily_token_budget=1 where id`); err != nil {
			t.Fatal(err)
		}
		if _, err = crawler.enqueueSeedTranslation(leaseCtx, candidate, job, raw, "fr-FR", items); !errors.Is(err, errAIQuotaExceeded) {
			t.Fatalf("budget err=%v", err)
		}
		var reserved int64
		if err = pool.QueryRow(ctx, `select quota_reserved_tokens from ai_tasks where id=$1 and status='failed' and input_tokens+output_tokens=0`, msg.TaskID).Scan(&reserved); err != nil || reserved <= 0 {
			t.Fatalf("unknown reservation=%d err=%v", reserved, err)
		}
	})
	t.Run("late provider result cannot publish after lease replacement", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `update seed_crawler_configs set ai_daily_token_budget=1000000 where id`); err != nil {
			t.Fatal(err)
		}
		newCtx, newCandidate, newJob, newRaw, _ := seedAIFixture(t, ctx, pool, "seedlate")
		msg, err := crawler.enqueueSeedTranslation(newCtx, newCandidate, newJob, newRaw, "zh-CN", items)
		if err != nil {
			t.Fatal(err)
		}
		fail.Store(false)
		hold.Store(true)
		message, _ := json.Marshal(msg)
		finished := make(chan error, 1)
		go func() { finished <- worker.handleTask(ctx, message) }()
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("provider never started")
		}
		lease := newCtx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
		if _, err = pool.Exec(ctx, `update seed_crawler_runs set lease_owner='replacement' where id=$1`, lease.RunID); err != nil {
			t.Fatal(err)
		}
		close(release)
		hold.Store(false)
		if err = <-finished; !errors.Is(err, errSeedCrawlerLeaseLost) {
			t.Fatalf("late result error=%v", err)
		}
		var status string
		var tokens int64
		if err = pool.QueryRow(ctx, `select status,input_tokens+output_tokens from ai_tasks where id=$1`, msg.TaskID).Scan(&status, &tokens); err != nil || status != "failed" || tokens != 18 {
			t.Fatalf("late accounting/status=%s %d err=%v", status, tokens, err)
		}
		var published int
		if err = pool.QueryRow(ctx, `select count(*) from seed_crawler_translation_tasks where candidate_id=$1 and status='completed'`, newCandidate).Scan(&published); err != nil || published != 0 {
			t.Fatalf("late translation published=%d err=%v", published, err)
		}
	})
	t.Run("expired or replaced worker cannot enqueue or submit", func(t *testing.T) {
		lease := leaseCtx.Value(seedCrawlerLeaseContextKey{}).(seedCrawlerLeaseIdentity)
		if _, err := pool.Exec(ctx, `update seed_crawler_configs set ai_daily_token_budget=1000000 where id`); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `update seed_crawler_runs set lease_owner='replacement' where id=$1`, lease.RunID); err != nil {
			t.Fatal(err)
		}
		if _, err := crawler.enqueueSeedTranslation(leaseCtx, candidate, job, raw, "ja-JP", items); !errors.Is(err, errSeedCrawlerLeaseLost) {
			t.Fatalf("old worker enqueue=%v", err)
		}
		if _, err := crawler.execSeedWrite(leaseCtx, `update seed_crawler_candidates set status='submitted' where id=$1`, candidate); !errors.Is(err, errSeedCrawlerLeaseLost) {
			t.Fatalf("old write=%v", err)
		}
		actor := security.Claims{Subject: user}
		draft := []byte(`{"primaryName":"Old seed","summary":"Old seed","environment":"bothRequired","primaryCategory":"utility","officialStatus":"active","sourceStatus":"open","license":"MIT","submissionMethod":"manual"}`)
		if _, err := crawler.submitSeedDraft(leaseCtx, actor, "mod", "not-created", "https://modrinth.com/mod/example", draft); err == nil || !strings.Contains(err.Error(), "409") {
			t.Fatalf("old autoSubmit=%v", err)
		}
		var count int
		if err := pool.QueryRow(ctx, `select count(*) from mods where primary_name='Old seed'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("old published=%d err=%v", count, err)
		}
	})
}

func TestSeedDraftTranslationsUseExistingEditorAndSubmissionContracts(t *testing.T) {
	for _, kind := range []string{"mod", "plugin"} {
		t.Run(kind, func(t *testing.T) {
			payload := map[string]any{"primaryName": "English", "summary": "English summary", "bodyMarkdown": "English body", "seedTranslations": "metadata", "importOrigin": "seed_crawler_import", "externalProjectId": "external"}
			if kind == "plugin" {
				delete(payload, "primaryName")
				delete(payload, "summary")
				delete(payload, "bodyMarkdown")
				payload["defaultLocale"] = "en-US"
				payload["localizations"] = []map[string]string{{"locale": "en-US", "name": "English", "summary": "English summary", "bodyMarkdown": "English body"}}
			}
			translations := map[string]any{"zh-CN": map[string]any{"primaryName": "中文", "summary": "中文简介", "bodyMarkdown": "中文正文"}}
			applySeedDraftTranslations(payload, translations, kind)
			raw := seedSubmissionPayload(payload)
			req := httptest.NewRequest(http.MethodPost, "/create", strings.NewReader(string(raw)))
			if kind == "mod" {
				var target createModRequest
				if err := decodeJSON(req, &target); err != nil || len(target.Localizations) != 2 || target.Localizations[1].Locale != "zh-CN" || target.Localizations[1].Name != "中文" {
					t.Fatalf("mod standard contract=%+v err=%v", target.Localizations, err)
				}
			} else {
				var target simpleProjectSnapshot
				if err := decodeJSON(req, &target); err != nil || len(target.Localizations) != 2 || target.Localizations[1].Locale != "zh-CN" || target.Localizations[1].BodyMarkdown != "中文正文" {
					t.Fatalf("simple standard contract=%+v err=%v", target.Localizations, err)
				}
			}
			if strings.Contains(string(raw), "seedTranslations") || strings.Contains(string(raw), "importOrigin") || strings.Contains(string(raw), "externalProjectId") {
				t.Fatal("task metadata leaked into create request")
			}
		})
	}
}

func TestSeedCrawlerLegacyUsageIsPreservedBeforeProjectionReplacement(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	setAITestConfig(t, ctx, pool, cfg, aiTestConfig("http://127.0.0.1:1"))
	if _, err := pool.Exec(ctx, `insert into seed_crawler_configs(enabled,ai_daily_token_budget) values(true,1000000)`); err != nil {
		t.Fatal(err)
	}
	leaseCtx, candidate, job, raw, _ := seedAIFixture(t, ctx, pool, "seedlegacy")
	if _, err := pool.Exec(ctx, `insert into seed_crawler_translation_tasks(candidate_id,locale,status,input_tokens,output_tokens) values($1,'fr-FR','completed',50,28)`, candidate); err != nil {
		t.Fatal(err)
	}
	model := aiTestConfig("").Models[0]
	for i := 0; i < 2; i++ {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = preserveLegacySeedAIAccountingTx(ctx, tx, model); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var usage int64
	if err := pool.QueryRow(ctx, `select count(*),sum(input_tokens+output_tokens) from ai_tasks where payload->>'scope'='seed_crawler_legacy'`).Scan(&count, &usage); err != nil || count != 1 || usage != 78 {
		t.Fatalf("known legacy count=%d usage=%d err=%v", count, usage, err)
	}
	if _, err := pool.Exec(ctx, `update seed_crawler_translation_tasks set input_tokens=0,output_tokens=0,status='failed' where candidate_id=$1`, candidate); err != nil {
		t.Fatal(err)
	}
	// Existing captured facts are immutable accounting; changing the old mutable
	// projection does not reduce usage or create another provider request.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = preserveLegacySeedAIAccountingTx(ctx, tx, model); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select sum(input_tokens+output_tokens) from ai_tasks where payload->>'scope'='seed_crawler_legacy'`).Scan(&usage); err != nil || usage != 78 {
		t.Fatalf("legacy fact overwritten=%d %v", usage, err)
	}
	if _, err = pool.Exec(ctx, `insert into seed_crawler_translation_tasks(candidate_id,locale,status) values($1,'ja-JP','failed')`, candidate); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = preserveLegacySeedAIAccountingTx(ctx, tx, model); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = NewSeedCrawlerWorker(cfg, pool).enqueueSeedTranslation(leaseCtx, candidate, job, raw, "zh-CN", []map[string]string{{"key": "primaryName", "text": "Example"}}); !errors.Is(err, errAIQuotaExceeded) {
		t.Fatalf("unknown legacy billing released budget=%v", err)
	}
	var outbox int
	if err = pool.QueryRow(ctx, `select count(*) from nats_outbox where subject='ai'`).Scan(&outbox); err != nil || outbox != 0 {
		t.Fatalf("legacy accounting queued provider calls=%d err=%v", outbox, err)
	}
}

func TestSeedCrawlerCandidateClaimSerializesDailySlots(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	if _, err := pool.Exec(ctx, `insert into seed_crawler_configs(enabled,daily_limit) values(true,1) on conflict(id) do update set daily_limit=1`); err != nil {
		t.Fatal(err)
	}
	crawler := NewSeedCrawlerWorker(cfg, pool)
	contexts := make([]context.Context, 8)
	runs := make([]int64, 8)
	for i := range contexts {
		token := randomHex(24)
		if err := pool.QueryRow(ctx, `insert into seed_crawler_runs(status,lease_owner,lease_expires_at) values('running',$1,now()+interval '5 minutes') returning id`, token).Scan(&runs[i]); err != nil {
			t.Fatal(err)
		}
		contexts[i] = withSeedCrawlerLease(ctx, runs[i], token)
	}
	claim := func(same bool) int64 {
		var wg sync.WaitGroup
		var winners atomic.Int64
		errs := make(chan error, 8)
		start := make(chan struct{})
		for i := range contexts {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				external := fmt.Sprintf("claim-%d", i)
				if same {
					external = "claim-same"
				}
				_, status, err := crawler.claimSeedCandidate(contexts[i], runs[i], "mod", seedModrinthHit{ProjectID: external}, false)
				if err != nil {
					errs <- err
				}
				if status == "claimed" {
					winners.Add(1)
				}
			}(i)
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		return winners.Load()
	}
	t.Run("different candidates cannot exceed one daily slot", func(t *testing.T) {
		if got := claim(false); got != 1 {
			t.Fatalf("claimed=%d want 1", got)
		}
	})
	if _, err := pool.Exec(ctx, `update seed_crawler_runs set lease_expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update seed_crawler_configs set daily_limit=20`); err != nil {
		t.Fatal(err)
	}
	for i := range contexts {
		if _, err := pool.Exec(ctx, `update seed_crawler_runs set lease_expires_at=now()+interval '5 minutes' where id=$1`, runs[i]); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("same candidate has only one live owner", func(t *testing.T) {
		if got := claim(true); got != 1 {
			t.Fatalf("same candidate claimed=%d want 1", got)
		}
	})
	var candidate, owner int64
	if err := pool.QueryRow(ctx, `select id,run_id from seed_crawler_candidates where external_project_id='claim-same'`).Scan(&candidate, &owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update seed_crawler_candidates set status='draft',updated_at=now()-interval '2 days' where id=$1`, candidate); err != nil {
		t.Fatal(err)
	}
	if _, status, err := crawler.claimSeedCandidate(contexts[0], runs[0], "mod", seedModrinthHit{ProjectID: "claim-same"}, false); err != nil || status != "duplicate" {
		t.Fatalf("finished claim=%s err=%v", status, err)
	}
	var changed bool
	if err := pool.QueryRow(ctx, `select updated_at>now()-interval '1 day' or run_id<>$2 from seed_crawler_candidates where id=$1`, candidate, owner).Scan(&changed); err != nil || changed {
		t.Fatalf("finished provenance/day changed=%v err=%v", changed, err)
	}
}
