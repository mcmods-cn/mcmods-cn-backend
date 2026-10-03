package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02ReportReopenRejectsPreviouslyUsedReviewKeyIntegration(t *testing.T) {
	ctx, server := newOCT02ReportReviewFixture(t, "resolved_invalid")
	before := captureOCT02ReportReviewFacts(t, ctx, server.db)
	response := invokeOCT02ReportReviewAction(ctx, server, "reopen", `{"reason":"new evidence","idempotencyKey":"old-review"}`)
	if response.Code != http.StatusConflict {
		t.Errorf("reopen reused key status=%d, want 409", response.Code)
	}
	if after := captureOCT02ReportReviewFacts(t, ctx, server.db); after != before {
		t.Errorf("reused key changed report, review, evidence, notification or audit facts: before=%+v after=%+v", before, after)
	}
}

func TestOCT02ReportResolveRejectsPreviouslyUsedReviewKeyIntegration(t *testing.T) {
	ctx, server := newOCT02ReportReviewFixture(t, "in_review")
	before := captureOCT02ReportReviewFacts(t, ctx, server.db)
	response := invokeOCT02ReportReviewAction(ctx, server, "resolve", `{"conclusion":"invalid","note":"new review","idempotencyKey":"old-review"}`)
	if response.Code != http.StatusConflict {
		t.Errorf("resolve reused key status=%d, want 409", response.Code)
	}
	if after := captureOCT02ReportReviewFacts(t, ctx, server.db); after != before {
		t.Errorf("reused key changed report, review, evidence, notification or audit facts: before=%+v after=%+v", before, after)
	}
}

func TestOCT02ReportFreshReviewKeysPreserveLifecycleIntegration(t *testing.T) {
	ctx, server := newOCT02ReportReviewFixture(t, "resolved_invalid")
	for _, step := range []struct {
		action string
		body   string
	}{
		{"reopen", `{"reason":"new evidence","idempotencyKey":"fresh-reopen"}`},
		{"claim", `{}`},
		{"resolve", `{"conclusion":"invalid","note":"reviewed again","idempotencyKey":"fresh-resolve"}`},
	} {
		response := invokeOCT02ReportReviewAction(ctx, server, step.action, step.body)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status=%d, want 200", step.action, response.Code)
		}
	}
	var status, conclusions, evidenceStatus string
	var claimant, version, notificationCount, assignmentCount, logCount int64
	var resolved, cleanupScheduled bool
	err := server.db.QueryRow(ctx, `select status,claimed_by,lock_version,resolved_at is not null,
		(select string_agg(conclusion,',' order by id) from report_reviews),
		(select status from report_evidence where report_id=1),
		(select isfinite(cleanup_after) and cleanup_after>now() from report_evidence where report_id=1),
		(select count(*) from nats_outbox),(select count(*) from report_assignment_events),(select count(*) from app_logs)
		from reports where id=1`).Scan(&status, &claimant, &version, &resolved, &conclusions, &evidenceStatus, &cleanupScheduled, &notificationCount, &assignmentCount, &logCount)
	if err != nil {
		t.Fatal(err)
	}
	if status != "resolved_invalid" || claimant != 101 || version != 4 || !resolved || conclusions != "invalid,reopened,invalid" || evidenceStatus != "pending_delete" || !cleanupScheduled || notificationCount != 1 || assignmentCount != 1 || logCount != 2 {
		t.Fatalf("unexpected lifecycle facts: status=%s claimant=%d version=%d resolved=%t reviews=%s evidence=%s cleanup=%t notifications=%d assignments=%d logs=%d", status, claimant, version, resolved, conclusions, evidenceStatus, cleanupScheduled, notificationCount, assignmentCount, logCount)
	}
}

func TestOCT02ReportReopenConflictingOpenTargetReturnsConflictIntegration(t *testing.T) {
	ctx, server := newOCT02ReportReviewFixture(t, "resolved_invalid")
	if _, err := server.db.Exec(ctx, `insert into reports(id,public_id,target_type,target_public_id,reporter_id,target_actor_role,status)
		values(2,'new-report','comment','comment01',10,'author','pending')`); err != nil {
		t.Fatal(err)
	}
	before := captureOCT02ReportReviewFacts(t, ctx, server.db)
	response := invokeOCT02ReportReviewAction(ctx, server, "reopen", `{"reason":"new evidence","idempotencyKey":"fresh-reopen"}`)
	if response.Code != http.StatusConflict {
		t.Errorf("reopen conflicting open target status=%d, want 409", response.Code)
	}
	if after := captureOCT02ReportReviewFacts(t, ctx, server.db); after != before {
		t.Errorf("conflicting open target changed durable facts: before=%+v after=%+v", before, after)
	}
}

func TestOCT02ReportReopenOtherDatabaseFailureRemainsServerErrorIntegration(t *testing.T) {
	ctx, server := newOCT02ReportReviewFixture(t, "resolved_invalid")
	// Inject a different unique constraint failure. Only the actual open-target
	// business constraint should be classified as a recoverable HTTP conflict.
	if _, err := server.db.Exec(ctx, `create unique index oct02_injected_status_unique on reports(status);
		insert into reports(id,public_id,target_type,target_public_id,reporter_id,target_actor_role,status)
		values(2,'unrelated-report','comment','other-comment',20,'author','pending')`); err != nil {
		t.Fatal(err)
	}
	before := captureOCT02ReportReviewFacts(t, ctx, server.db)
	response := invokeOCT02ReportReviewAction(ctx, server, "reopen", `{"reason":"new evidence","idempotencyKey":"fresh-reopen"}`)
	if response.Code != http.StatusInternalServerError {
		t.Errorf("reopen unrelated database failure status=%d, want 500", response.Code)
	}
	if after := captureOCT02ReportReviewFacts(t, ctx, server.db); after != before {
		t.Errorf("database failure changed durable facts: before=%+v after=%+v", before, after)
	}
}

// The explicit test URL is supplied by the isolated test environment. A single
// connection keeps this fixture's temporary tables local and exercises all
// transaction-owned queries without replacing the PostgreSQL implementation.
func newOCT02ReportReviewFixture(t *testing.T, status string) (context.Context, *Server) {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" || os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set an explicit isolated MCMODS_TEST_DATABASE_URL and MCMODS_RUN_DB_INTEGRATION=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal("invalid explicit test database configuration")
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("failed to create isolated test database pool")
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `
		create temporary table reports(
			id bigint primary key,public_id text not null unique,target_type text not null,target_public_id text not null,
			reporter_id bigint not null,target_actor_id bigint,target_actor_role text not null,status text not null,
			claimed_by bigint,claimed_at timestamptz,resolved_at timestamptz,
			updated_at timestamptz not null default now(),lock_version bigint not null default 1
		);
		create unique index uq_reports_open_reporter_target on reports(reporter_id,target_type,target_public_id)
			where status in ('pending','in_review');
		create temporary table report_assignment_events(
			id bigserial primary key,report_id bigint not null,action text not null,actor_id bigint not null,
			previous_assignee_id bigint,assignee_id bigint not null,reason text not null default '',
			created_at timestamptz not null default now()
		);
		create temporary table report_reviews(
			id bigserial primary key,report_id bigint not null,reviewer_id bigint not null,conclusion text not null,
			note text not null,idempotency_key text not null,created_at timestamptz not null default now(),unique(report_id,idempotency_key)
		);
		create temporary table report_evidence(report_id bigint,status text not null,cleanup_after timestamptz not null);
		create temporary table nats_outbox(
			id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null,
			subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null,
			payload jsonb not null,occurred_at timestamptz not null,status text not null,available_at timestamptz not null
		);
		create temporary table app_logs(
			category text,level text,actor_id bigint,action text,target text,ip text,user_agent text,method text,path text,
			status integer,latency_ms bigint,payload jsonb
		);
		insert into reports(id,public_id,target_type,target_public_id,reporter_id,target_actor_role,status,claimed_by,claimed_at,resolved_at,updated_at)
			values(1,'report-oct02','comment','comment01',10,'author','resolved_invalid',101,'2026-01-01Z','2026-01-01Z','2026-01-01Z');
		insert into report_reviews(report_id,reviewer_id,conclusion,note,idempotency_key)
			values(1,101,'invalid','old review','old-review');
		insert into report_evidence(report_id,status,cleanup_after) values(1,'pending_delete','2026-01-15Z')
	`); err != nil {
		t.Fatal(err)
	}
	if status == "in_review" {
		if _, err = pool.Exec(ctx, `update reports set status='in_review',resolved_at=null where id=1;
			update report_evidence set status='bound',cleanup_after='infinity' where report_id=1`); err != nil {
			t.Fatal(err)
		}
	}
	return ctx, &Server{db: pool}
}

func invokeOCT02ReportReviewAction(ctx context.Context, server *Server, action, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/report-oct02/"+action, bytes.NewBufferString(body))
	request.SetPathValue("id", "report-oct02")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: 101}))
	response := httptest.NewRecorder()
	switch action {
	case "resolve":
		server.resolveUnifiedReport(response, request)
	case "reopen":
		server.reopenUnifiedReport(response, request)
	case "claim":
		server.claimReport(response, request)
	default:
		panic("unknown report action in test")
	}
	return response
}

type oct02ReportReviewFacts struct {
	Report, Reviews, Evidence          string
	Notifications, Assignments, Audits int64
}

func captureOCT02ReportReviewFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool) oct02ReportReviewFacts {
	t.Helper()
	var facts oct02ReportReviewFacts
	err := pool.QueryRow(ctx, `select row_to_json(r)::text,
		(select coalesce(jsonb_agg(to_jsonb(v) order by id),'[]'::jsonb)::text from report_reviews v where report_id=r.id),
		(select coalesce(jsonb_agg(to_jsonb(e) order by report_id),'[]'::jsonb)::text from report_evidence e where report_id=r.id),
		(select count(*) from nats_outbox),(select count(*) from report_assignment_events),(select count(*) from app_logs)
		from reports r where id=1`).Scan(&facts.Report, &facts.Reviews, &facts.Evidence, &facts.Notifications, &facts.Assignments, &facts.Audits)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}
