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

func TestInvalidReportConclusionRejectsEveryPunitiveAction(t *testing.T) {
	t.Parallel()
	for name, request := range map[string]resolveUnifiedReportRequest{
		"delete": {Conclusion: "invalid", DeleteTarget: true, IdempotencyKey: "delete"},
		"ban":    {Conclusion: "invalid", BanUserID: "user00002", IdempotencyKey: "ban"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateReportResolutionActions(request); err == nil {
				t.Fatalf("invalid conclusion accepted punitive %s action", name)
			}
		})
	}
	for name, request := range map[string]resolveUnifiedReportRequest{
		"invalid without action": {Conclusion: "invalid", IdempotencyKey: "no-action"},
		"valid delete":           {Conclusion: "valid", DeleteTarget: true, IdempotencyKey: "delete"},
		"valid ban":              {Conclusion: "valid", BanUserID: "user00002", IdempotencyKey: "ban"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateReportResolutionActions(request); err != nil {
				t.Fatalf("valid resolution/action combination rejected: %v", err)
			}
		})
	}
}

func TestInvalidReportResolutionCannotHideTargetIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify invalid report resolution isolation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
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
			resolved_at timestamptz,updated_at timestamptz not null default now(),lock_version bigint not null default 1
		);
		create temporary table report_reviews(
			id bigserial primary key,report_id bigint not null,reviewer_id bigint not null,conclusion text not null,
			note text not null,idempotency_key text not null,created_at timestamptz not null default now(),unique(report_id,idempotency_key)
		);
		create temporary table report_snapshots(report_id bigint primary key,payload jsonb not null);
		create temporary table report_evidence(report_id bigint,status text not null,cleanup_after timestamptz not null);
		create temporary table moderation_actions(
			id bigserial primary key,public_id text not null default 'action001',report_id bigint,action_type text not null,
			target_type text not null,target_public_id text not null,actor_id bigint not null,before_summary jsonb not null,
			after_summary jsonb not null,reason text not null,idempotency_key text not null,
			unique(action_type,target_type,target_public_id,idempotency_key)
		);
		create temporary table mods(project_code text primary key,review_status text not null,updated_at timestamptz not null default now());
		create temporary table effective_project_access(user_id bigint not null,project_type text not null,project_public_id text not null,access_level text not null);
		create temporary table nats_outbox(
			id bigserial primary key,event_id text not null unique,event_type text not null,schema_version integer not null,
			subject text not null,aggregate_type text not null,aggregate_id text not null,trace_id text not null,
			payload jsonb not null,occurred_at timestamptz not null,status text not null,available_at timestamptz not null
		);
		create temporary table app_logs(
			category text,level text,actor_id bigint,action text,target text,ip text,user_agent text,method text,path text,
			status integer,latency_ms bigint,payload jsonb
		);
		insert into reports(id,public_id,target_type,target_public_id,reporter_id,target_actor_id,target_actor_role,status)
			values(1,'report001','mod','mod000001',1,10,'submitter','pending');
		insert into report_snapshots values(1,'{"targetType":"mod"}');
		insert into mods(project_code,review_status) values('mod000001','approved')
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/report001/resolve", bytes.NewBufferString(
		`{"conclusion":"invalid","note":"not upheld","deleteTarget":true,"idempotencyKey":"bug110-invalid-delete"}`,
	))
	request.SetPathValue("id", "report001")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{
		Subject:         99,
		PermissionRules: []security.PermissionRule{{Code: "report.action.delete", Allow: true, Priority: 1}},
	}))
	response := httptest.NewRecorder()
	server.resolveUnifiedReport(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid resolution status = %d body=%s", response.Code, response.Body.String())
	}

	var reportStatus, targetStatus string
	var reviews, actions, events int
	if err = pool.QueryRow(ctx, `select
		(select status from reports where public_id='report001'),
		(select review_status from mods where project_code='mod000001'),
		(select count(*) from report_reviews),
		(select count(*) from moderation_actions),
		(select count(*) from nats_outbox)`).Scan(&reportStatus, &targetStatus, &reviews, &actions, &events); err != nil {
		t.Fatal(err)
	}
	if reportStatus != "pending" || targetStatus != "approved" || reviews != 0 || actions != 0 || events != 0 {
		t.Fatalf("invalid resolution changed facts: report=%s target=%s reviews=%d actions=%d events=%d",
			reportStatus, targetStatus, reviews, actions, events)
	}
}
