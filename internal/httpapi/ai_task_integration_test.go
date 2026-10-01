package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

// Each test creates its own database, never changing the shared API test data
// or AI configuration. Cleanup proves ownership with the creation-time marker.
func isolatedAITestDatabase(t *testing.T) (context.Context, *pgxpool.Pool, config.Config) {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1")
	}
	cfg := config.Load()
	if cfg.Env != "test" {
		t.Fatal("AI integration tests require APP_ENV=test")
	}
	parsed, err := pgxpool.ParseConfig(cfg.DB.ConnString())
	if err != nil {
		t.Fatal("invalid test PostgreSQL configuration")
	}
	if parsed.ConnConfig.Host != "127.0.0.1" && parsed.ConnConfig.Host != "localhost" && parsed.ConnConfig.Host != "::1" {
		t.Fatal("AI test database creation requires loopback PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	admin, err := pgxpool.NewWithConfig(ctx, parsed.Copy())
	if err != nil {
		t.Fatal(err)
	}
	name := "mcmods_ai_" + randomHex(12)
	marker := "mcmods-audit-owned:" + randomHex(16)
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err = admin.Exec(ctx, "create database "+identifier); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "comment on database "+identifier+" is '"+marker+"'"); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	parsed.ConnConfig.Database = name
	pool, err := pgxpool.NewWithConfig(ctx, parsed)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		defer admin.Close()
		var actual string
		if err := admin.QueryRow(cleanupCtx, `select shobj_description(oid,'pg_database') from pg_database where datname=$1`, name).Scan(&actual); err != nil || actual != marker {
			t.Error("refusing cleanup: test database ownership could not be confirmed")
			return
		}
		if _, err := admin.Exec(cleanupCtx, "drop database "+identifier); err != nil {
			t.Errorf("owned database cleanup: %v", err)
		}
	})
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatalf("repeated migration: %v", err)
	}
	var patches int
	if err = pool.QueryRow(ctx, `select count(*) from schema_repair_history`).Scan(&patches); err != nil || patches != 2 {
		t.Fatalf("repair ledger count=%d err=%v", patches, err)
	}
	return ctx, pool, cfg
}

func setAITestConfig(t *testing.T, ctx context.Context, pool *pgxpool.Pool, cfg config.Config, value aiConfigPayload) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := security.EncryptSetting(cfg.SettingsEncryptionKey, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values('ai.config',$1::jsonb) on conflict(key) do update set value=excluded.value`, string(encrypted)); err != nil {
		t.Fatal(err)
	}
}

func aiTestConfig(endpoint string) aiConfigPayload {
	cfg := defaultAIConfig()
	cfg.Providers = []aiProviderConfig{{Code: "local", Enabled: true, Protocol: "openai-compatible", BaseURL: endpoint, APIKey: "synthetic-test-key"}}
	cfg.Models = []aiModelConfig{{Provider: "local", Model: "synthetic", Enabled: true, MaxOutputTokens: 32, InputPricePerMillion: 1, OutputPricePerMillion: 2}}
	cfg.Quotas = []aiQuotaConfig{{Scope: "site", Period: "day", RequestLimit: 100, TokenLimit: 1000000, CostLimitCNY: 100}}
	for i := range cfg.TaskModels {
		cfg.TaskModels[i].ModelKey = "local/synthetic"
		cfg.TaskModels[i].ConcurrencyLimit = 1
	}
	return cfg
}

func insertAITestTask(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskType, status string, payload any, reserved int64) (int64, string) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	uid := "ai_" + randomHex(16)
	var id int64
	if err = pool.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,payload,quota_reserved_tokens) values($1,$2,'local','synthetic',$3,$4::jsonb,$5) returning id`, uid, taskType, status, string(raw), reserved).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id, uid
}

func TestAITaskConcurrentClaimQuotaAndCrashRecoveryIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	settings := aiTestConfig("http://127.0.0.1")
	setAITestConfig(t, ctx, pool, cfg, settings)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	payload := map[string]any{"sourceLocale": "zh-CN", "targetLocale": "en-US", "items": []map[string]string{{"key": "greeting", "text": "你好"}}}
	ids := make([]int64, 8)
	for i := range ids {
		ids[i], _ = insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", payload, 100)
	}
	start := make(chan struct{})
	results := make(chan bool, len(ids))
	errs := make(chan error, len(ids))
	var ready sync.WaitGroup
	ready.Add(len(ids))
	for _, id := range ids {
		go func(id int64) {
			ready.Done()
			<-start
			claimed, err := worker.claimTask(ctx, id)
			results <- claimed
			errs <- err
		}(id)
	}
	ready.Wait()
	close(start)
	claimedCount := 0
	for range ids {
		if <-results {
			claimedCount++
		}
		err := <-errs
		if err != nil && !strings.Contains(err.Error(), "concurrency limit") {
			t.Fatal(err)
		}
	}
	if claimedCount != 1 {
		t.Fatalf("claimed=%d want1", claimedCount)
	}
	if _, err := pool.Exec(ctx, `update ai_tasks set started_at=now()-interval '10 minutes' where status='running'`); err != nil {
		t.Fatal(err)
	}
	// Recovery can execute without publishing pending work: an unavailable queue
	// stops wakeups, while the database transition remains authoritative.
	worker.recoverTasks(ctx)
	var failed int
	if err := pool.QueryRow(ctx, `select count(*) from ai_tasks where status='failed' and error like '%retry may incur%'`).Scan(&failed); err != nil || failed != 1 {
		t.Fatalf("crash recovery failed=%d err=%v", failed, err)
	}
	if _, err := pool.Exec(ctx, `update ai_tasks set status='cancelled' where status='failed'`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	quota := aiConfigPayload{Quotas: []aiQuotaConfig{{Scope: "site", Period: "day", TokenLimit: 800}}}
	if err = reserveAISiteQuotaTx(ctx, tx, quota, 1); !errors.Is(err, errAIQuotaExceeded) {
		t.Fatalf("cancelled billed call released reservation: %v", err)
	}
	// An unstarted queued cancellation releases its token reservation.
	if _, err = tx.Exec(ctx, `update ai_tasks set status='cancelled' where started_at is null`); err != nil {
		t.Fatal(err)
	}
	if err = reserveAISiteQuotaTx(ctx, tx, quota, 700); err != nil {
		t.Fatalf("unstarted cancellation retained quota: %v", err)
	}
}

func TestAITaskProviderFailureUsageCancelAndBudgetIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	started := make(chan struct{})
	resume := make(chan struct{})
	var once sync.Once
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		<-resume
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"length","message":{"content":"{}"}}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`))
	}))
	defer provider.Close()
	settings := aiTestConfig(provider.URL)
	setAITestConfig(t, ctx, pool, cfg, settings)
	raw := map[string]any{"sourceLocale": "zh-CN", "targetLocale": "en-US", "items": []map[string]string{{"key": "greeting", "text": "你好"}}}
	id, uid := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", raw, 100)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	message, _ := json.Marshal(aiTaskMessage{TaskID: id, TaskUID: uid, TaskType: aiTaskI18nTranslation})
	done := make(chan error, 1)
	go func() { done <- worker.handleTask(ctx, message) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := pool.Exec(ctx, `update ai_tasks set status='cancelled',finished_at=now() where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	close(resume)
	if err := <-done; err == nil {
		t.Fatal("truncated provider response was accepted")
	}
	var status string
	var input, output, cost int64
	if err := pool.QueryRow(ctx, `select status,input_tokens,output_tokens,cost_micros from ai_tasks where id=$1`, id).Scan(&status, &input, &output, &cost); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" || input != 11 || output != 7 || cost != 25 {
		t.Fatalf("cancelled billed result status=%s input=%d output=%d cost=%d", status, input, output, cost)
	}
	server := &Server{db: pool, cfg: cfg}
	cfg.NATS.OutboxEnabled = true
	server.cfg = cfg
	newID, newUID := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", raw, 100)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = server.enqueueAIOutboxTx(ctx, tx, newID, newUID, aiTaskI18nTranslation); err != nil {
		t.Fatal(err)
	}
	var reservedCost int64
	var outbox int
	if err = tx.QueryRow(ctx, `select (payload->>'quotaReservedCostMicros')::bigint from ai_tasks where id=$1`, newID).Scan(&reservedCost); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from nats_outbox where aggregate_id=$1`, newUID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if reservedCost != 200 || outbox != 1 {
		t.Fatalf("reservation=%d outbox=%d", reservedCost, outbox)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	settings.Models[0].InputPricePerMillion = 1000000
	settings.Models[0].OutputPricePerMillion = 1000000
	settings.Quotas[0].CostLimitCNY = 1
	setAITestConfig(t, ctx, pool, cfg, settings)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = server.enqueueAIOutboxTx(ctx, tx, newID, newUID, aiTaskI18nTranslation); !errors.Is(err, errAIQuotaExceeded) {
		t.Fatalf("cost budget overflow allowed: %v", err)
	}
}

func TestAICatalogReviewPublicationRechecksSourceAndHumanTargetIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	scenarios := []string{"normal", "source_changed", "human_target", "target_changed", "cancelled", "hidden_subject"}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			var modID int64
			var publicID string
			if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'ai-review-'||gen_random_uuid()::text,'Synthetic review','approved') returning id,project_code`).Scan(&modID, &publicID); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,revision_no) values('mod',$1,'zh-CN','源名称','human',true,'approved',1)`, modID); err != nil {
				t.Fatal(err)
			}
			target := int64(0)
			payload := contentTranslationTaskPayload{EntityID: modID, EntityType: "mod", PublicID: publicID, SourceLocale: "zh-CN", SourceRevisionNo: 1, TargetLocale: "en-US", TargetRevisionNo: &target}
			rawPayload, _ := json.Marshal(payload)
			uid := "ai_" + randomHex(16)
			var taskID int64
			if err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,status,payload) values($1,$2,'completed',$3::jsonb) returning id`, uid, aiTaskContentTranslation, string(rawPayload)).Scan(&taskID); err != nil {
				t.Fatal(err)
			}
			snapshot := catalogLocalizationSnapshot{SubjectPublicID: publicID, SubjectType: "mod", EntityID: modID, Locale: "en-US", Name: "Translated name", Provenance: "ai", SourceLocale: "zh-CN", SourceRevisionNo: 1, AITaskID: &taskID, AITaskPublicID: &uid, Editable: true}
			rawSnapshot, _ := json.Marshal(snapshot)
			revision, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: "mod", EntityID: modID, AggregateType: catalogAggregateLocalization, AggregateKey: contentLocalizationAggregateKey(publicID, "mod", "en-US"), Snapshot: rawSnapshot, Source: "ai", Status: "pending"})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "source_changed":
				_, err = tx.Exec(ctx, `update content_localizations set revision_no=2 where subject_type='mod' and subject_id=$1`, modID)
			case "human_target":
				_, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,revision_no) values('mod',$1,'en-US','Human protected','human_corrected',true,'approved',1)`, modID)
			case "target_changed":
				_, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,revision_no) values('mod',$1,'en-US','New AI','ai',true,'approved',1)`, modID)
			case "cancelled":
				_, err = tx.Exec(ctx, `update ai_tasks set status='cancelled' where id=$1`, taskID)
			case "hidden_subject":
				_, err = tx.Exec(ctx, `update mods set review_status='rejected' where id=$1`, modID)
			}
			if err != nil {
				t.Fatal(err)
			}
			err = publishCatalogLocalizationSnapshotTx(ctx, tx, revision.RevisionID, rawSnapshot, 0)
			if scenario == "normal" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, errCatalogEditorConflict) {
				t.Fatalf("stale publication result=%v", err)
			}
			var targetName string
			err = tx.QueryRow(ctx, `select name from content_localizations where subject_type='mod' and subject_id=$1 and locale='en-US'`, modID).Scan(&targetName)
			switch scenario {
			case "normal":
				if err != nil || targetName != "Translated name" {
					t.Fatalf("published=%q err=%v", targetName, err)
				}
			case "human_target":
				if targetName != "Human protected" {
					t.Fatalf("human translation overwritten=%q", targetName)
				}
			case "target_changed":
				if targetName != "New AI" {
					t.Fatalf("new AI overwritten=%q", targetName)
				}
			default:
				if !errors.Is(err, pgx.ErrNoRows) {
					t.Fatalf("stale task created target=%q err=%v", targetName, err)
				}
			}
		})
	}
}

func TestAIQuotaConcurrentReservationsIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	quota := aiConfigPayload{Quotas: []aiQuotaConfig{{Scope: "site", Period: "day", RequestLimit: 2, TokenLimit: 200}}}
	start := make(chan struct{})
	results := make(chan error, 8)
	var ready sync.WaitGroup
	ready.Add(8)
	for range 8 {
		go func() {
			ready.Done()
			<-start
			tx, err := pool.Begin(ctx)
			if err != nil {
				results <- err
				return
			}
			defer tx.Rollback(ctx)
			if err = reserveAISiteQuotaTx(ctx, tx, quota, 100); err != nil {
				results <- err
				return
			}
			_, err = tx.Exec(ctx, `insert into ai_tasks(task_uid,task_type,status,quota_reserved_tokens) values($1,$2,'queued',100)`, "ai_"+randomHex(16), aiTaskI18nTranslation)
			if err == nil {
				err = tx.Commit(ctx)
			}
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	successes := 0
	for range 8 {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, errAIQuotaExceeded) {
			t.Fatal(err)
		}
	}
	if successes != 2 {
		t.Fatalf("quota reservations succeeded=%d want2", successes)
	}
	var count, tokens int64
	if err := pool.QueryRow(ctx, `select count(*),sum(quota_reserved_tokens) from ai_tasks`).Scan(&count, &tokens); err != nil || count != 2 || tokens != 200 {
		t.Fatalf("ledger count=%d tokens=%d err=%v", count, tokens, err)
	}
}

func TestSchemaRepairConcurrentRootsCyclesAndRepeatedMigrateIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	var mod, version, template int64
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'tree-race-'||gen_random_uuid()::text,'Synthetic tree race','approved') returning id`).Scan(&mod); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_versions(mod_id) values($1) returning id`, mod).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select id from mod_content_templates where builtin and code='item_block'`).Scan(&template); err != nil {
		t.Fatal(err)
	}
	runRace := func(sql string, args [][]any) int {
		start := make(chan struct{})
		results := make(chan error, len(args))
		for _, arguments := range args {
			go func(arguments []any) { <-start; _, err := pool.Exec(ctx, sql, arguments...); results <- err }(arguments)
		}
		close(start)
		successes := 0
		for range args {
			if <-results == nil {
				successes++
			}
		}
		return successes
	}
	if count := runRace(`insert into mod_content_sections(mod_id,version_id,template_id,ordinal,display_mode) values($1,$2,$3,0,'compact')`, [][]any{{mod, version, template}, {mod, version, template}}); count != 1 {
		t.Fatalf("concurrent duplicate root writes=%d want1", count)
	}
	var first, second int64
	if err := pool.QueryRow(ctx, `select id from mod_content_sections where version_id=$1`, version).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mod_content_sections(mod_id,version_id,template_id,ordinal,display_mode) values($1,$2,$3,1,'compact') returning id`, mod, version, template).Scan(&second); err != nil {
		t.Fatal(err)
	}
	if count := runRace(`update mod_content_sections set parent_id=$2 where id=$1`, [][]any{{first, second}, {second, first}}); count != 1 {
		t.Fatalf("concurrent cyclic parent writes=%d want1", count)
	}
	results := make(chan error, 4)
	for range 4 {
		go func() { results <- database.Migrate(ctx, pool) }()
	}
	for range 4 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from schema_repair_history`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("concurrent migration duplicated patch count=%d err=%v", count, err)
	}
}

func TestCatalogLocalizationBatchKeepsHumanRevisionAndInvalidatesOnlyChangedSourceIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var mod int64
	var publicID string
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'locale-batch-'||gen_random_uuid()::text,'Synthetic batch','approved') returning id,project_code`).Scan(&mod, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,source_locale,revision_no,editable,review_status) values('mod',$1,'zh-CN','源','human','',2,true,'approved'),('mod',$1,'en-US','Original AI','ai','zh-CN',4,true,'approved'),('mod',$1,'ja-JP','Untouched AI','ai','zh-CN',1,true,'approved')`, mod); err != nil {
		t.Fatal(err)
	}
	revision, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: "mod", EntityID: mod, AggregateType: "mod", AggregateKey: publicID, Snapshot: json.RawMessage(`{}`), Status: "approved", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	// Sending unchanged source content must leave its revision and AI projections
	// untouched. The admin metadata save is not a translation source change.
	if err = publishCatalogLocalizationsTx(ctx, tx, mod, publicID, "mod", []catalogLocalizationEdit{{Locale: "zh-CN", Name: "源"}}, revision.RevisionID, 0); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(ctx, `select count(*) from content_localizations where subject_type='mod' and subject_id=$1 and provenance='ai'`, mod).Scan(&count); err != nil || count != 2 {
		t.Fatalf("unchanged source invalidated projections count=%d err=%v", count, err)
	}
	if err = publishCatalogLocalizationsTx(ctx, tx, mod, publicID, "mod", []catalogLocalizationEdit{{Locale: "zh-CN", Name: "新源"}, {Locale: "en-US", Name: "Human corrected"}}, revision.RevisionID, 0); err != nil {
		t.Fatal(err)
	}
	var provenance, name string
	var revisionNo int64
	if err = tx.QueryRow(ctx, `select name,provenance,revision_no from content_localizations where subject_type='mod' and subject_id=$1 and locale='en-US'`, mod).Scan(&name, &provenance, &revisionNo); err != nil || name != "Human corrected" || provenance != "human_corrected" || revisionNo != 5 {
		t.Fatalf("same-batch target lost history name=%q provenance=%s revision=%d err=%v", name, provenance, revisionNo, err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from content_localizations where subject_type='mod' and subject_id=$1 and locale='ja-JP'`, mod).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old untouched AI projection survived count=%d err=%v", count, err)
	}
}

func TestAIWorkerDuplicateDeliveryMakesOneProviderCallIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"items\":[{\"key\":\"greeting\",\"text\":\"Hello\"}]}"}}],"usage":{"prompt_tokens":12,"completion_tokens":8}}`))
	}))
	defer provider.Close()
	settings := aiTestConfig(provider.URL)
	setAITestConfig(t, ctx, pool, cfg, settings)
	taskID, uid := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "queued", map[string]any{"sourceLocale": "zh-CN", "targetLocale": "en-US", "items": []map[string]string{{"key": "greeting", "text": "你好"}}}, 100)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	raw, _ := json.Marshal(aiTaskMessage{TaskID: taskID, TaskUID: uid, TaskType: aiTaskI18nTranslation})
	start := make(chan struct{})
	results := make(chan error, 8)
	for range 8 {
		go func() { <-start; results <- worker.handleTask(ctx, raw) }()
	}
	close(start)
	for range 8 {
		if err := <-results; err != nil && !strings.Contains(err.Error(), "concurrency limit") {
			t.Fatal(err)
		}
	}
	if err := worker.handleTask(ctx, raw); err != nil {
		t.Fatalf("completed duplicate delivery: %v", err)
	}
	var status string
	var input, output int64
	if err := pool.QueryRow(ctx, `select status,input_tokens,output_tokens from ai_tasks where id=$1`, taskID).Scan(&status, &input, &output); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || status != "completed" || input != 12 || output != 8 {
		t.Fatalf("calls=%d status=%s input=%d output=%d", calls.Load(), status, input, output)
	}
}

func TestAIAdminRetryEnforcesOriginalCreatorDailyQuotaIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	cfg.NATS.OutboxEnabled = true
	settings := aiTestConfig("http://127.0.0.1")
	setAITestConfig(t, ctx, pool, cfg, settings)
	var actor, permission int64
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('retry-quota-'||gen_random_uuid()::text,gen_random_uuid()::text||'@test.invalid','synthetic-unused') returning id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into permissions(code,module,name) values('user.ai.daily_token_limit.3000','user','Synthetic quota') returning id`).Scan(&permission); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into user_permissions(user_id,permission_id) values($1,$2)`, actor, permission); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"sourceLocale": "zh-CN", "targetLocale": "en-US", "quotaBacked": true, "items": []map[string]string{{"key": "greeting", "text": "你好"}}}
	_, uid := insertAITestTask(t, ctx, pool, aiTaskI18nTranslation, "failed", payload, 2800)
	if _, err := pool.Exec(ctx, `update ai_tasks set created_by=$1,started_at=now() where task_uid=$2`, actor, uid); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: cfg}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks/"+uid+"/retry", strings.NewReader(`{}`))
	request.SetPathValue("id", uid)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor}))
	response := httptest.NewRecorder()
	server.retryAITask(response, request)
	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "AI_QUOTA_EXCEEDED") {
		t.Fatalf("daily quota bypass: status=%d body=%s", response.Code, response.Body.String())
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from ai_tasks where payload->>'retryOf'=$1`, uid).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected retry persisted count=%d err=%v", count, err)
	}
	// A known small usage replaces only the conservative old reservation; a new
	// retry may use the remaining quota and is durably queued in the same tx.
	if _, err := pool.Exec(ctx, `update ai_tasks set input_tokens=1 where task_uid=$1`, uid); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	server.retryAITask(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("eligible retry status=%d body=%s", response.Code, response.Body.String())
	}
	if err := pool.QueryRow(ctx, `select count(*) from ai_tasks where payload->>'retryOf'=$1 and created_by=$2`, uid, actor).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retry creator/count mismatch count=%d err=%v", count, err)
	}
	response = httptest.NewRecorder()
	server.retryAITask(response, request)
	if response.Code != http.StatusConflict && response.Code != http.StatusTooManyRequests {
		t.Fatalf("duplicate retry status=%d", response.Code)
	}
}

func TestAICatalogVisibilityWaitsForConcurrentGovernanceIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	var mod int64
	var publicID string
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'ai-visibility-'||gen_random_uuid()::text,'Synthetic visibility','approved') returning id,project_code`).Scan(&mod, &publicID); err != nil {
		t.Fatal(err)
	}
	governance, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer governance.Rollback(ctx)
	if _, err = governance.Exec(ctx, `update mods set review_status='rejected' where id=$1`, mod); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			result <- err
			return
		}
		defer tx.Rollback(ctx)
		_, _, err = loadEditableContentSubjectTx(ctx, tx, publicID)
		result <- err
	}()
	select {
	case err := <-result:
		t.Fatalf("visibility read escaped pending governance lock: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err = governance.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-result; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("rejected resource accepted after governance committed: %v", err)
	}
}

func TestAICatalogPublicationAndCancelHaveAtomicOrderingIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	worker := NewAIWorker(pool, nil, cfg.SettingsEncryptionKey)
	for _, cancelFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancel_first", false: "publish_first"}[cancelFirst], func(t *testing.T) {
			var mod int64
			var publicID string
			if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values(new_public_id(),'atomic-ai-'||gen_random_uuid()::text,'Synthetic atomic AI','approved') returning id,project_code`).Scan(&mod, &publicID); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,revision_no) values('mod',$1,'zh-CN','源','human',true,'approved',1)`, mod); err != nil {
				t.Fatal(err)
			}
			target := int64(0)
			payload := contentTranslationTaskPayload{EntityID: mod, PublicID: publicID, EntityType: "mod", SourceLocale: "zh-CN", SourceRevisionNo: 1, TargetLocale: "en-US", TargetRevisionNo: &target}
			raw, _ := json.Marshal(payload)
			task, uid := insertAITestTask(t, ctx, pool, aiTaskContentTranslation, "running", payload, 100)
			barrier, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer barrier.Rollback(ctx)
			if cancelFirst {
				_, err = barrier.Exec(ctx, `update ai_tasks set status='cancelled' where id=$1`, task)
			} else {
				_, err = barrier.Exec(ctx, `select id from mods where id=$1 for update`, mod)
			}
			if err != nil {
				t.Fatal(err)
			}
			persisted := make(chan error, 1)
			go func() {
				persisted <- worker.persistCatalogContentTranslation(ctx, task, 0, raw, map[string]any{"items": []any{map[string]any{"key": "name", "text": "Translated"}}})
			}()
			var cancelled chan int
			if !cancelFirst {
				// The resource barrier keeps actual persistence after its task lock,
				// allowing cancellation to contend against the same transaction.
				for {
					var waiting bool
					if err = pool.QueryRow(ctx, `select exists(select 1 from pg_stat_activity where datname=current_database() and wait_event_type='Lock' and query like 'select true from mods%')`).Scan(&waiting); err != nil {
						t.Fatal(err)
					}
					if waiting {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(time.Millisecond):
					}
				}
				cancelled = make(chan int, 1)
				go func() {
					request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/ai/tasks/"+uid+"/cancel", nil)
					request.SetPathValue("id", uid)
					request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{}))
					response := httptest.NewRecorder()
					(&Server{db: pool, cfg: cfg}).cancelAITask(response, request)
					cancelled <- response.Code
				}()
			}
			if err = barrier.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			err = <-persisted
			if cancelFirst && err == nil || !cancelFirst && err != nil {
				t.Fatalf("cancelFirst=%t publication error=%v", cancelFirst, err)
			}
			if !cancelFirst {
				if code := <-cancelled; code != http.StatusConflict {
					t.Fatalf("published task cancellation status=%d", code)
				}
			}
			var status string
			var count int
			if err = pool.QueryRow(ctx, `select status from ai_tasks where id=$1`, task).Scan(&status); err != nil {
				t.Fatal(err)
			}
			if err = pool.QueryRow(ctx, `select count(*) from content_localizations where subject_type='mod' and subject_id=$1 and locale='en-US'`, mod).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if cancelFirst && (status != "cancelled" || count != 0) || !cancelFirst && (status != "completed" || count != 1) {
				t.Fatalf("non-atomic outcome cancelFirst=%t status=%s targetCount=%d", cancelFirst, status, count)
			}
		})
	}
}
