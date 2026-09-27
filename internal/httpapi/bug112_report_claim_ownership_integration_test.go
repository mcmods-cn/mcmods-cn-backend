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

func TestReportResolutionRequiresClaimOwnerAndAuditedTakeoverIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify report claim ownership")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, `
		create temporary table reports(
			id bigint primary key,public_id text not null unique,target_type text not null,target_public_id text not null,
			reporter_id bigint not null,target_actor_id bigint,target_actor_role text not null,status text not null,
			claimed_by bigint,claimed_at timestamptz,resolved_at timestamptz,
			updated_at timestamptz not null default now(),lock_version bigint not null default 1
		);
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
		insert into reports(id,public_id,target_type,target_public_id,reporter_id,target_actor_role,status)
			values(1,'report112','comment','comment01',10,'author','pending')
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	reviewerOne := security.Claims{Subject: 101}
	reviewerTwo := security.Claims{Subject: 202}

	unclaimed := invokeBUG112ReportResolve(ctx, server, reviewerOne, "unclaimed")
	if unclaimed.Code != http.StatusConflict {
		t.Fatalf("unclaimed resolution status=%d body=%s", unclaimed.Code, unclaimed.Body.String())
	}

	claimed := invokeBUG112ReportAction(ctx, server, reviewerOne, "claim", `{}`)
	if claimed.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", claimed.Code, claimed.Body.String())
	}

	otherReviewer := invokeBUG112ReportResolve(ctx, server, reviewerTwo, "other-reviewer")
	if otherReviewer.Code != http.StatusConflict {
		t.Fatalf("non-owner resolution status=%d body=%s", otherReviewer.Code, otherReviewer.Body.String())
	}
	assertBUG112ReportState(t, ctx, pool, "in_review", 101, 0, 0, 1)

	forbiddenTakeover := invokeBUG112ReportAction(ctx, server, reviewerTwo, "takeover", `{"reason":"urgent handoff"}`)
	if forbiddenTakeover.Code != http.StatusForbidden {
		t.Fatalf("unprivileged takeover status=%d body=%s", forbiddenTakeover.Code, forbiddenTakeover.Body.String())
	}

	reviewerTwo.PermissionRules = []security.PermissionRule{{Code: "report.action.takeover", Allow: true, Priority: 1}}
	takenOver := invokeBUG112ReportAction(ctx, server, reviewerTwo, "takeover", `{"reason":"urgent handoff"}`)
	if takenOver.Code != http.StatusOK {
		t.Fatalf("takeover status=%d body=%s", takenOver.Code, takenOver.Body.String())
	}
	assertBUG112ReportState(t, ctx, pool, "in_review", 202, 0, 0, 2)

	oldOwner := invokeBUG112ReportResolve(ctx, server, reviewerOne, "old-owner")
	if oldOwner.Code != http.StatusConflict {
		t.Fatalf("old owner resolution status=%d body=%s", oldOwner.Code, oldOwner.Body.String())
	}
	newOwner := invokeBUG112ReportResolve(ctx, server, reviewerTwo, "new-owner")
	if newOwner.Code != http.StatusOK {
		t.Fatalf("new owner resolution status=%d body=%s", newOwner.Code, newOwner.Body.String())
	}
	assertBUG112ReportState(t, ctx, pool, "resolved_invalid", 202, 1, 1, 2)

	var firstAction, secondAction, secondReason string
	var firstAssignee, previousAssignee, secondAssignee int64
	if err = pool.QueryRow(ctx, `select
		(select action from report_assignment_events where id=1),
		(select assignee_id from report_assignment_events where id=1),
		(select action from report_assignment_events where id=2),
		(select previous_assignee_id from report_assignment_events where id=2),
		(select assignee_id from report_assignment_events where id=2),
		(select reason from report_assignment_events where id=2)`).
		Scan(&firstAction, &firstAssignee, &secondAction, &previousAssignee, &secondAssignee, &secondReason); err != nil {
		t.Fatal(err)
	}
	if firstAction != "claim" || firstAssignee != 101 || secondAction != "takeover" || previousAssignee != 101 || secondAssignee != 202 || secondReason != "urgent handoff" {
		t.Fatalf("assignment audit mismatch: first=%s/%d second=%s/%d/%d/%q", firstAction, firstAssignee, secondAction, previousAssignee, secondAssignee, secondReason)
	}
}

func invokeBUG112ReportAction(ctx context.Context, server *Server, claims security.Claims, action, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/report112/"+action, bytes.NewBufferString(body))
	request.SetPathValue("id", "report112")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	if action == "claim" {
		server.claimReport(response, request)
	} else {
		server.takeoverReport(response, request)
	}
	return response
}

func invokeBUG112ReportResolve(ctx context.Context, server *Server, claims security.Claims, key string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/report112/resolve", bytes.NewBufferString(
		`{"conclusion":"invalid","note":"not upheld","idempotencyKey":"`+key+`"}`,
	))
	request.SetPathValue("id", "report112")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
	response := httptest.NewRecorder()
	server.resolveUnifiedReport(response, request)
	return response
}

func assertBUG112ReportState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, status string, claimant int64, reviews, events, assignments int) {
	t.Helper()
	var gotStatus string
	var gotClaimant int64
	var gotReviews, gotEvents, gotAssignments int
	if err := pool.QueryRow(ctx, `select status,claimed_by,
		(select count(*) from report_reviews),(select count(*) from nats_outbox),(select count(*) from report_assignment_events)
		from reports where public_id='report112'`).Scan(&gotStatus, &gotClaimant, &gotReviews, &gotEvents, &gotAssignments); err != nil {
		t.Fatal(err)
	}
	if gotStatus != status || gotClaimant != claimant || gotReviews != reviews || gotEvents != events || gotAssignments != assignments {
		t.Fatalf("report facts status=%s claimant=%d reviews=%d events=%d assignments=%d", gotStatus, gotClaimant, gotReviews, gotEvents, gotAssignments)
	}
}
