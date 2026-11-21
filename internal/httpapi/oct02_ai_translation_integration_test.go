package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"mcmods-cn-backend/internal/security"
)

func oct02AITestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MCMODS_TEST_DATABASE_URL is required for isolated PostgreSQL AI tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	owner, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "oct02_ai_" + randomHex(10)
	if _, err = owner.Exec(ctx, "create schema "+pgx.Identifier{schema}.Sanitize()); err != nil {
		owner.Close()
		t.Fatal(err)
	}
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 8
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		// schema is an unpredictable identifier successfully created by this helper;
		// cleanup never accepts a caller-supplied or shared schema name.
		if _, err := owner.Exec(cleanupCtx, "drop schema "+pgx.Identifier{schema}.Sanitize()+" cascade"); err != nil {
			t.Errorf("drop owned AI fixture schema: %v", err)
		}
		owner.Close()
	})
	if _, err = pool.Exec(ctx, `create table users(id bigint primary key,public_id text not null);
		insert into users values(42,'u00000042');
		create table ai_tasks(id bigint generated always as identity primary key,task_uid text not null unique,task_type text not null,
		 provider text not null default 'openai',model text not null default 'fixture',status text not null default 'queued',payload jsonb not null default '{}',
		 result jsonb not null default '{}',error text not null default '',created_by bigint default 42,started_at timestamptz,finished_at timestamptz,
		 updated_at timestamptz not null default now(),created_at timestamptz not null default now(),input_tokens bigint not null default 0,
		 output_tokens bigint not null default 0,cost_micros bigint not null default 0,quota_reserved_tokens bigint not null default 100);
		create table ai_task_logs(id bigint generated always as identity primary key,task_id bigint,level text,event text,message text,
		 payload jsonb not null default '{}',created_at timestamptz not null default now());
		create table app_logs(category text,level text,action text,target text,latency_ms bigint,payload jsonb);
		create table system_settings(key text primary key,value jsonb);
		create table notifications(id bigint primary key,recipient_id bigint,title text,body text,kind text,
            revision_xid xid8 not null default pg_current_xact_id());
		create table notification_translations(notification_id bigint,user_id bigint,locale text,title text,body text,
		 created_at timestamptz default now(),primary key(notification_id,user_id,locale));
		create table community_posts(id bigint primary key,published_revision_id bigint,status text,review_status text);
		create table community_post_translations(post_id bigint,locale text,title text,body_markdown text,source_revision_id bigint,ai_task_id bigint,
		 updated_at timestamptz default now(),primary key(post_id,locale));
		create table public_routes(internal_id bigint,public_id text,entity_type text);
		create table content_subjects(subject_id bigint,subject_type text,default_locale text);
		create table mods(id bigint,review_status text);
		create table content_localizations(subject_id bigint,subject_type text,locale text,revision_no bigint,review_status text,
		 published_revision_id bigint,provenance text,name text);
		create table content_revisions(id bigint,public_id text);
		create table change_requests(aggregate_type text,metadata jsonb)`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestOCT02AIProviderBudgetConcurrentAdmissionIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	worker := NewAIWorker(pool, nil, "")
	prepared := preparedAITask{provider: aiProviderConfig{Code: "openai"}, model: aiModelConfig{Model: "fixture"}, reservationTokens: 10,
		quotas: []aiQuotaConfig{{Scope: "site", Subject: "default", Period: "day", TokenLimit: 15}}}
	var accepted atomic.Int64
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, _, err := worker.reserveProviderRequestBudget(context.Background(), prepared)
			if err == nil {
				accepted.Add(1)
			} else if !strings.Contains(err.Error(), "budget is exhausted") {
				t.Errorf("admission error: %v", err)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("concurrent budget admissions=%d, want exactly one", accepted.Load())
	}
}

func TestOCT02AIInvalidResultStillConsumesSupplierUsageIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	worker := NewAIWorker(pool, nil, "")
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not JSON"}}],"usage":{"prompt_tokens":11,"completion_tokens":17}}`))
	}))
	defer provider.Close()
	prepared := preparedAITask{provider: aiProviderConfig{Code: "openai", BaseURL: provider.URL, APIKey: "fixture"}, model: aiModelConfig{Model: "fixture", InputPricePerMillion: 2, OutputPricePerMillion: 4}, timeout: time.Second, maxOutputTokens: 100, reservationTokens: 200,
		quotas: []aiQuotaConfig{{Scope: "site", Subject: "default", Period: "day", RequestLimit: 1}}}
	if _, usage, err := worker.executePreparedTask(context.Background(), prepared); err == nil || usage.InputTokens != 11 || usage.OutputTokens != 17 {
		t.Fatalf("invalid result usage=%#v error=%v", usage, err)
	}
	if _, _, err := worker.executePreparedTask(context.Background(), prepared); err == nil {
		t.Fatal("second supplier request exceeded the request quota")
	}
	if calls.Load() != 1 {
		t.Fatalf("provider calls=%d, want one", calls.Load())
	}
	var state string
	var tokens, cost, reserved int64
	if err := pool.QueryRow(context.Background(), `select payload->>'state',(payload->>'inputTokens')::bigint+(payload->>'outputTokens')::bigint,
	 (payload->>'costMicros')::bigint,(payload->>'reservedTokens')::bigint from ai_task_logs where event=$1`, aiRequestBudgetEvent).Scan(&state, &tokens, &cost, &reserved); err != nil {
		t.Fatal(err)
	}
	if state != "settled" || tokens != 28 || cost != 90 || reserved != 0 {
		t.Fatalf("settled ledger=%s tokens=%d cost=%d reserved=%d", state, tokens, cost, reserved)
	}
}

func TestOCT02AIUnknownUsageKeepsReservationIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	worker := NewAIWorker(pool, nil, "")
	prepared := preparedAITask{provider: aiProviderConfig{Code: "openai"}, model: aiModelConfig{Model: "fixture", InputPricePerMillion: 2}, reservationTokens: 200}
	id, budget, err := worker.reserveProviderRequestBudget(context.Background(), prepared)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = worker.settleProviderRequestBudget(canceled, id, budget, aiTaskUsage{}); err != nil {
		t.Fatal(err)
	}
	var state string
	var tokens int64
	if err = pool.QueryRow(context.Background(), `select payload->>'state',(payload->>'reservedTokens')::bigint from ai_task_logs where id=$1`, id).Scan(&state, &tokens); err != nil {
		t.Fatal(err)
	}
	if state != "usage_unknown" || tokens != 200 {
		t.Fatalf("unknown usage released the budget: %s/%d", state, tokens)
	}
}

func oct02AIWorkerTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := oct02AITestPool(t)
	if _, err := pool.Exec(context.Background(), `alter table users add column status text default 'active';
		alter table notifications add column source_locale text default 'en-US';
		alter table community_posts add column source_locale text, add column title text, add column body_markdown text;
		create table roles(id bigint,code text,weight int,parents text[],status text);
		create table role_permissions(role_id bigint,permission_id bigint,allow bool,expires_at timestamptz);
		create table user_role_bindings(user_id bigint,role_id bigint,expires_at timestamptz);
		create table effective_project_access(user_id bigint,access_level text,project_public_id text);
		create table creator_claims(user_id bigint,creator_id bigint,status text);
		create table creators(id bigint,public_id text,kind text);
		create table permissions(id bigint,code text);
		create table user_permissions(user_id bigint,permission_id bigint,allow bool,source text,expires_at timestamptz);
		insert into permissions values(1,'notification.translate'),(2,'content.translate'),(3,'user.ai.daily_token_limit.100000');
		insert into user_permissions select 42,id,true,'manual',null from permissions;`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestOCT02AIWorkerClaimAndNotificationPersistenceIntegration(t *testing.T) {
	pool := oct02AIWorkerTestPool(t)
	var calls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"content":"{\"items\":[{\"key\":\"title\",\"text\":\"你好 {name}\"},{\"key\":\"body\",\"text\":\"正文\"}]}"}}],"usage":{"prompt_tokens":11,"completion_tokens":17}}`))
	}))
	defer provider.Close()
	key := "fixture-encryption-key-at-least-32-characters"
	server := &Server{cfg: config.Config{SettingsEncryptionKey: key}, db: pool}
	cfg := defaultAIConfig()
	cfg.Providers[0].Enabled = true
	cfg.Providers[0].APIKey = "fixture"
	cfg.Providers[0].BaseURL = provider.URL
	cfg.Models[0].Enabled = true
	cfg.Models[0].Model = "fixture"
	cfg.Models[0].MaxOutputTokens = 100
	for i := range cfg.TaskModels {
		cfg.TaskModels[i].ModelKey = "openai/fixture"
	}
	sealed, err := server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `insert into system_settings values('ai.config',$1)`, sealed); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `insert into notifications values(17,42,'Hello {name}','Body','reply_mention')`); err != nil {
		t.Fatal(err)
	}
	var taskID int64
	if err = pool.QueryRow(context.Background(), `insert into ai_tasks(task_uid,task_type,payload,quota_reserved_tokens) values('ai-fixture',$1,
	 '{"notificationId":17,"targetLocale":"zh-CN","items":[{"key":"title","text":"Hello {name}"},{"key":"body","text":"Body"}]}',5000) returning id`, aiTaskNotificationTranslation).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(aiTaskMessage{TaskID: taskID, TaskUID: "ai-fixture", TaskType: aiTaskNotificationTranslation})
	worker := NewAIWorker(pool, nil, key)
	var group sync.WaitGroup
	for i := 0; i < 4; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := worker.handleTask(context.Background(), raw); err != nil {
				t.Errorf("worker: %v", err)
			}
		}()
	}
	group.Wait()
	var status, title string
	var used, reserved int64
	if err = pool.QueryRow(context.Background(), `select status,input_tokens+output_tokens,quota_reserved_tokens from ai_tasks where id=$1`, taskID).Scan(&status, &used, &reserved); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(context.Background(), `select title from notification_translations where notification_id=17`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || status != "completed" || title != "你好 {name}" || used != 28 || reserved != 0 {
		t.Fatalf("calls=%d task=%s title=%q usage=%d reservation=%d", calls.Load(), status, title, used, reserved)
	}
}

func TestAIQueuedCatalogSourceChecksVisibilityRevisionAndTextIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `alter table public_routes add column canonical_path text;
		alter table content_localizations add column summary text default '',add column content_markdown text default '';
		create table blueprints(id bigint,status text,review_status text);
		create table skin_assets(id bigint,status text,review_status text,visibility text);
		create table catalog_entities(id bigint,public_id text,status text,archived_at timestamptz);
		create table recipe_layout_templates(entity_id bigint,recipe_type_id bigint);
		create table recipes(entity_id bigint,recipe_type_id bigint);
		insert into public_routes values(7,'s00000007','skin','/skins/s00000007');
		insert into content_subjects values(7,'skin','en-US');
		insert into skin_assets values(7,'active','approved','public');
		insert into content_localizations(subject_id,subject_type,locale,revision_no,review_status,name)
		values(7,'skin','en-US',3,'approved','Example skin')`); err != nil {
		t.Fatal(err)
	}
	prepared := preparedAITask{taskType: aiTaskContentTranslation, rawPayload: []byte(`{
		"publicId":"s00000007","internalEntityId":7,"entityType":"skin","sourceLocale":"en-US","targetLocale":"zh-CN","sourceRevisionNo":3,
		"items":[{"key":"name","text":"Example skin"},{"key":"summary","text":""},{"key":"contentMarkdown","text":""}]}`)}
	for _, test := range []struct{ name, change string }{
		{"published source", ""},
		{"private skin", "update skin_assets set visibility='private'"},
		{"new source version", "update content_localizations set revision_no=4"},
		{"different text", "update content_localizations set summary='Updated'"},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if test.change != "" {
				if _, err := tx.Exec(ctx, test.change); err != nil {
					t.Fatal(err)
				}
			}
			err = verifyClaimedTranslationSourceTx(ctx, tx, security.Claims{Subject: 42}, prepared)
			if test.change == "" && err != nil || test.change != "" && !errors.Is(err, errAIExecutionPolicy) {
				t.Fatalf("translation source policy: %v", err)
			}
		})
	}
}

func TestAINotificationWithdrawalPreservesUnknownUsageBeforePublicationIntegration(t *testing.T) {
	pool := oct02AIWorkerTestPool(t)
	ctx := context.Background()
	worker := NewAIWorker(pool, nil, "")
	if _, err := pool.Exec(ctx, `insert into notifications(id,recipient_id,title,body,kind)
		values(17,42,'Hello','Example','reply_mention')`); err != nil {
		t.Fatal(err)
	}
	var taskID int64
	if err := pool.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type) values('ai-withdraw',$1) returning id`,
		aiTaskNotificationTranslation).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	run, err := claimAITaskExecution(ctx, pool, taskID)
	if err != nil {
		t.Fatal(err)
	}
	ctx = context.WithValue(ctx, aiTaskExecutionKey{}, run)
	raw := []byte(`{"notificationId":17,"sourceLocale":"en-US","targetLocale":"zh-CN","items":[{"key":"title","text":"Hello"},{"key":"body","text":"Example"}]}`)
	prepared := preparedAITask{taskType: aiTaskNotificationTranslation, rawPayload: raw, reservationTokens: 100,
		provider: aiProviderConfig{Code: "openai"}, model: aiModelConfig{Model: "fixture"}}
	ledgerID, budget, err := worker.reserveProviderRequestBudget(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.settleProviderRequestBudget(ctx, ledgerID, budget, aiTaskUsage{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update notifications set recipient_id=43 where id=17`); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"items": []any{map[string]any{"key": "title", "text": "你好"}, map[string]any{"key": "body", "text": "示例"}}}
	err = worker.persistNotificationTranslation(ctx, 42, raw, result)
	if !errors.Is(err, errAIExecutionPolicy) {
		t.Fatalf("publication after recipient change: %v", err)
	}
	if err := worker.failTask(ctx, taskID, err); err != nil {
		t.Fatal(err)
	}
	var status, usageState string
	var reserved, translations int
	if err := pool.QueryRow(ctx, `select status,quota_reserved_tokens,
		(select payload->>'state' from ai_task_logs where id=$2),(select count(*) from notification_translations)
		from ai_tasks where id=$1`, taskID, ledgerID).Scan(&status, &reserved, &usageState, &translations); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || reserved != 100 || usageState != "usage_unknown" || translations != 0 {
		t.Fatalf("task=%s reserved=%d supplier=%s translations=%d", status, reserved, usageState, translations)
	}
}

func TestOCT02AIExecutionFencingAndDeletedPostIntegration(t *testing.T) {
	pool := oct02AIWorkerTestPool(t)
	ctx := context.Background()
	worker := NewAIWorker(pool, nil, "")
	var taskID int64
	if err := pool.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type) values('ai-fencing',$1) returning id`, aiTaskContentTranslation).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	run, err := claimAITaskExecution(ctx, pool, taskID)
	if err != nil {
		t.Fatal(err)
	}
	oldCtx := context.WithValue(ctx, aiTaskExecutionKey{}, run)
	if _, err = pool.Exec(ctx, `update ai_tasks set status='retrying',started_at=null where id=$1`, taskID); err != nil {
		t.Fatal(err)
	}
	newRun, err := claimAITaskExecution(ctx, pool, taskID)
	if err != nil || newRun.Started.Equal(run.Started) {
		t.Fatalf("new execution=%#v error=%v", newRun, err)
	}
	if err = worker.failTask(oldCtx, taskID, errors.New("obsolete worker")); err == nil {
		t.Fatal("old worker changed a new task execution")
	}
	var status string
	if err = pool.QueryRow(ctx, `select status from ai_tasks where id=$1`, taskID).Scan(&status); err != nil || status != "running" {
		t.Fatalf("new task state=%s error=%v", status, err)
	}
	if _, err = pool.Exec(ctx, `insert into community_posts values(8,12,'deleted','approved')`); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"postInternalId":8,"sourceRevisionId":12,"targetLocale":"zh-CN","items":[{"key":"title","text":"title"},{"key":"bodyMarkdown","text":"body"}]}`)
	result := map[string]any{"items": []any{map[string]any{"key": "title", "text": "title"}, map[string]any{"key": "bodyMarkdown", "text": "body"}}}
	if err = worker.persistCommunityPostTranslation(context.WithValue(ctx, aiTaskExecutionKey{}, newRun), taskID, payload, result); !errors.Is(err, errAIExecutionPolicy) {
		t.Fatalf("deleted community post eligibility error = %v", err)
	}
	var count int
	if err = pool.QueryRow(ctx, `select count(*) from community_post_translations`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted post translation count=%d error=%v", count, err)
	}
}

func TestOCT02AICatalogProtectsHumanTargetAndChangedSourceIntegration(t *testing.T) {
	pool := oct02AITestPool(t)
	ctx := context.Background()
	worker := NewAIWorker(pool, nil, "")
	if _, err := pool.Exec(ctx, `insert into ai_tasks(task_uid,task_type) values('ai-catalog','content_translation_completion');
	 insert into public_routes values(7,'m00000007','mod');insert into content_subjects values(7,'mod','en-US');insert into mods values(7,'approved');
	 insert into content_localizations values(7,'mod','en-US',3,'approved',12,'human','source'),(7,'mod','zh-CN',2,'approved',13,'human_corrected','manual translation');
	 insert into content_revisions values(12,'r00000012'),(13,'r00000013')`); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"items": []any{map[string]any{"key": "name", "text": "AI title"}}}
	for _, sourceRevision := range []int{3, 2} {
		payload := []byte(fmt.Sprintf(`{"internalEntityId":7,"publicId":"m00000007","entityType":"mod","sourceLocale":"en-US","sourceRevisionNo":%d,"targetLocale":"zh-CN"}`, sourceRevision))
		if err := worker.persistCatalogContentTranslation(ctx, 1, 42, payload, result); err == nil {
			t.Fatalf("source revision %d replaced protected localization", sourceRevision)
		}
	}
	var target string
	if err := pool.QueryRow(ctx, `select name from content_localizations where locale='zh-CN'`).Scan(&target); err != nil || target != "manual translation" {
		t.Fatalf("human target=%q error=%v", target, err)
	}
}
