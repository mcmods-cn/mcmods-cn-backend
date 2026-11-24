package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/progression"
)

type auditCatalogTranslationFaultTracer struct{ failures chan *pgconn.PgError }

func (tracer *auditCatalogTranslationFaultTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, query pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (tracer *auditCatalogTranslationFaultTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, query pgx.TraceQueryEndData) {
	var failure *pgconn.PgError
	if errors.As(query.Err, &failure) && failure.Code == "23514" && failure.ConstraintName == "audit_reject_activity_boundary" {
		select {
		case tracer.failures <- failure:
		default:
		}
	}
}

type auditCatalogTranslationFixture struct {
	f         auditFixGRFixture
	api       *Server
	path      string
	completed chan struct{}
	faults    *auditCatalogTranslationFaultTracer
	monitor   *activity.Monitor
}

type auditCatalogTranslationHTTPResult struct {
	TaskUID string `json:"taskId"`
}

func auditCatalogTranslationMonitor(t *testing.T, f auditFixGRFixture, api *Server) *activity.Monitor {
	t.Helper()
	m := activity.NewMonitor(f.db, progression.NewService(f.db, api.cache), activity.Options{BatchSize: 16, QueueCapacity: 32, FlushInterval: 20 * time.Millisecond, RetryMinDelay: 20 * time.Millisecond, RetryMaxDelay: 50 * time.Millisecond, WriteTimeout: time.Second, DurableEnqueueTimeout: time.Second})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := m.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return m
}

func newAuditCatalogTranslationFixture(t *testing.T) auditCatalogTranslationFixture {
	t.Helper()
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "content.translate", "user.ai.daily_token_limit.100000")
	auditFixConfigureEnqueueAI(t, f.test013Fixture)
	if _, err := f.db.Exec(f.ctx, `insert into content_subjects(subject_type,subject_id,default_locale) values('mod',$1,'en-US') on conflict do nothing`, f.modID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_localizations(subject_type,subject_id,locale,name,summary,content_markdown,provenance,review_status) values('mod',$1,'en-US','Owned catalog source','Source summary','Source body','human','approved') on conflict do nothing`, f.modID); err != nil {
		t.Fatal(err)
	}
	faults := &auditCatalogTranslationFaultTracer{failures: make(chan *pgconn.PgError, 8)}
	cfg := f.db.Config()
	cfg.MaxConns, cfg.MinConns = 2, 1
	cfg.ConnConfig.Tracer = faults
	db, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	f.origin.Close()
	api := NewServer(f.ctx, f.server.cfg, db, nil, nil, nil, nil)
	path := "/api/v1/content/" + f.modCode + "/translations"
	completed := make(chan struct{}, 16)
	// Observe real middleware completion without changing any header, response,
	// claims, moderation context or production route behavior.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.ServeHTTP(w, r)
		if r.Method == http.MethodPost && r.URL.Path == path {
			completed <- struct{}{}
		}
	}))
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := api.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		api.cache.Close()
	})
	f.db, f.origin, f.server = db, origin, api
	monitor := auditCatalogTranslationMonitor(t, f, api)
	api.activity = monitor // Assigned before the first client request, never swapped during HTTP.
	return auditCatalogTranslationFixture{f: f, api: api, path: path, completed: completed, faults: faults, monitor: monitor}
}

func (fixture auditCatalogTranslationFixture) request(t *testing.T, body any) auditCompletionHTTPResult {
	t.Helper()
	result := <-auditCompletionAsyncJSON(fixture.f.test013Fixture, fixture.f.editor, http.MethodPost, fixture.path, body)
	if result.err != nil {
		t.Fatal(result.err)
	}
	select {
	case <-fixture.completed:
	case <-fixture.f.ctx.Done():
		t.Fatal("actual HTTP middleware did not finish")
	}
	return result
}

func TestAuditCMP003CatalogTranslationActivityFailureRollsBackTaskQuotaAndPublisherHTTPPostgresIntegration(t *testing.T) {
	x := newAuditCatalogTranslationFixture(t)
	f := x.f
	body := map[string]string{"targetLocale": "fr-FR", "sourceLocale": "en-US"}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := auditFixAIActivityFacts(t, f.test013Fixture)
	result := x.request(t, body)
	select {
	case failure := <-x.faults.failures:
		if failure.Code != "23514" || failure.ConstraintName != "audit_reject_activity_boundary" {
			t.Fatal("wrong database fault satisfied rollback case")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP path never observed the exact owned activity CHECK23514")
	}
	if result.status != http.StatusServiceUnavailable || !bytes.Contains(result.raw, []byte("failed to persist AI translation activity")) {
		t.Fatalf("catalog HTTP status=%d with task=%d and activityIntent=%d after exact activity23514; want503 and zero business writes", result.status, f.number(t, `select count(*) from ai_tasks`), f.number(t, `select count(*) from activity_event_outbox`))
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAIActivityFacts(t, f.test013Fixture))
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	recovered := x.request(t, body)
	if recovered.status != 202 {
		t.Fatalf("catalog recoveryHTTP%d", recovered.status)
	}
	first := decodeTEST022Data[auditCatalogTranslationHTTPResult](t, recovered.raw)
	if first.TaskUID == "" {
		t.Fatal("catalog recovery lost task identity")
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	replay := x.request(t, body)
	if replay.status != 202 || !bytes.Contains(replay.raw, []byte(first.TaskUID)) {
		t.Fatal("pending catalog task reuse depended on creating a second activity")
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	auditFixWaitActivity(t, func() bool {
		return f.number(t, `select count(*) from user_activity_events where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectMod) == 1
	})
	if f.number(t, `select count(*) from ai_tasks`) != 1 || f.number(t, `select count(*) from nats_outbox where aggregate_type='ai_task'`) != 1 || f.number(t, `select count(*) from ai_tasks where quota_reserved_tokens>0`) != 1 {
		t.Fatal("retry changed task, quota reservation or publisher identity")
	}
	t.Log("actual catalogHTTP exact23514→503/allfactsrollback; recovery202 onequota/task/publisher; reuse202 duringactivityoutage one actual persistedCreate/Mod; no provider execution claim")
}

func TestAuditCMP003CatalogTranslationReuseCannotCreateExtraTaskRewardHTTPPostgresIntegration(t *testing.T) {
	x := newAuditCatalogTranslationFixture(t)
	f := x.f
	var currency, firstTask, secondTask int64
	if err := f.db.QueryRow(f.ctx, `insert into currencies(code,name) values('catalog_cmp003','Owned catalog reward currency') returning id`).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('catalog_once','Owned first translation','{"action":"create","objectType":"mod","metric":"count","target":1}','{"experience":25,"currencies":{"catalog_cmp003":3}}','active') returning id`).Scan(&firstTask); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into task_definitions(code,name,condition,rewards,status) values('catalog_second','Owned second real task only','{"action":"create","objectType":"mod","metric":"count","target":2}','{"experience":50,"currencies":{"catalog_cmp003":6}}','active') returning id`).Scan(&secondTask); err != nil {
		t.Fatal(err)
	}
	body := map[string]string{"targetLocale": "fr-FR", "sourceLocale": "en-US"}
	first := x.request(t, body)
	if first.status != 202 {
		t.Fatalf("firstHTTP%d", first.status)
	}
	created := decodeTEST022Data[auditCatalogTranslationHTTPResult](t, first.raw)
	for range 2 {
		next := x.request(t, body)
		if next.status != 202 || !bytes.Contains(next.raw, []byte(created.TaskUID)) {
			t.Fatal("ordinary retry did not reuse the actual task")
		}
	}
	// Observe committed consumption after every actual middleware has finished;
	// Close alone does not certify draining all persistent outbox rows.
	auditFixWaitActivity(t, func() bool {
		return f.number(t, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.editor]) == 0 && f.number(t, `select count(*) from user_activity_events where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectMod) > 0
	})
	closed, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := x.monitor.Close(closed); err != nil {
		cancel()
		t.Fatal(err)
	}
	cancel()
	var events, progress, rewarded, balance, ledgers int64
	if err := f.db.QueryRow(f.ctx, `select
 (select count(*) from user_activity_events where user_id=$1 and action_id=$2 and object_type_id=$3),
 coalesce((select progress from user_task_progress where user_id=$1 and task_id=$4),0),
 (select count(*) from user_task_progress where user_id=$1 and task_id=$4 and rewarded_at is not null),
 coalesce((select balance from user_currency_balances where user_id=$1 and currency_id=$5),0),
 (select count(*) from currency_transactions where user_id=$1 and currency_id=$5 and transaction_type='task_reward')`, f.userIDs[f.editor], activity.ActionCreate, activity.ObjectMod, secondTask, currency).Scan(&events, &progress, &rewarded, &balance, &ledgers); err != nil {
		t.Fatal(err)
	}
	if events != 1 || progress != 1 || rewarded != 0 || balance != 3 || ledgers != 1 {
		t.Fatalf("same catalog task produced events/secondprogress/secondrewarded/coin/ledger=%d/%d/%d/%d/%d want1/1/0/3/1", events, progress, rewarded, balance, ledgers)
	}
	_ = auditCatalogTranslationMonitor(t, f, x.api)
	if f.number(t, `select count(*) from experience_transactions where user_id=$1 and reason='task_reward' and reference_key like $2 and amount_delta=25`, f.userIDs[f.editor], strconv.FormatInt(firstTask, 10)+":%") != 1 || f.number(t, `select count(*) from ai_tasks`) != 1 || f.number(t, `select count(*) from nats_outbox where aggregate_type='ai_task'`) != 1 {
		t.Fatal("actual consumer restart changed one real task, publisher or first reward")
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,review_status) values('mod',$1,'fr-FR','Owned cached translation','ai','approved')`, f.modID); err != nil {
		t.Fatal(err)
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	cached := x.request(t, body)
	if cached.status != 200 || !bytes.Contains(cached.raw, []byte(`"cached":true`)) {
		t.Fatal("typed cached HTTP branch created new work")
	}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	if strings.Contains(string(cached.raw), `"cached":false`) {
		t.Fatal("cache fixture mistaken for actual provider execution")
	}
	t.Log("actual catalogHTTP one task + two reuse202 + typedcache200; real middleware completion/consumer drain/restart; oneCreate/Mod,25XP+3coin once, second real-task reward remains locked; existing policy unchanged")
}

func TestCatalogTranslationEnqueueReportsStorageFailureWithoutDatabaseDetails(t *testing.T) {
	x := newAuditCatalogTranslationFixture(t)
	f := x.f
	const fault = "synthetic_catalog_outbox_failure"
	if _, err := f.db.Exec(f.ctx, `alter table nats_outbox add constraint synthetic_catalog_outbox_failure
		check (event_type<>'ai.content_translation.requested') not valid`); err != nil {
		t.Fatal(err)
	}
	result := x.request(t, map[string]string{"targetLocale": "fr-FR", "sourceLocale": "en-US"})
	if result.status != http.StatusServiceUnavailable || bytes.Contains(result.raw, []byte(fault)) || !bytes.Contains(result.raw, []byte("Could not queue the translation")) {
		t.Errorf("catalog enqueue HTTP %d: %s", result.status, result.raw)
	}
	if f.number(t, `select count(*) from ai_tasks`) != 0 {
		t.Fatal("failed outbox write left an AI task")
	}
}
