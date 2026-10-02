package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

func TestReportModerationRecipientsDistinguishSubmittersFromDevelopersIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify report governance recipients")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `create temporary table effective_project_access(
		user_id bigint not null,project_type text not null,project_public_id text not null,access_level text not null
	);
	insert into effective_project_access values
		(10,'mod','mod000001','editor'),
		(20,'mod','mod000001','developer'),
		(20,'mod','mod000001','developer'),
		(21,'mod','mod000001','developer'),
		(22,'plugin','mod000001','developer'),
		(23,'mod','mod000002','developer')`); err != nil {
		t.Fatal(err)
	}

	submitterID := int64(10)
	recipients, err := reportModerationRecipientIDs(ctx, tx, "mod", "mod000001", reportTargetActor{
		ID: &submitterID, Role: reportTargetActorSubmitter,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recipients, []int64{20, 21}) {
		t.Fatalf("project moderation recipients = %v, want approved developers only", recipients)
	}

	ownerID := int64(30)
	recipients, err = reportModerationRecipientIDs(ctx, tx, "comment", "comment01", reportTargetActor{
		ID: &ownerID, Role: reportTargetActorAuthor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recipients, []int64{30}) {
		t.Fatalf("direct-content moderation recipients = %v, want the real author", recipients)
	}
}

func TestReportProjectTypesUseCanonicalAccessIdentities(t *testing.T) {
	t.Parallel()
	for reportType, projectType := range map[string]string{
		"mod": "mod", "modpack": "modpack", "plugin": "plugin", "map": "map", "shader": "shader_pack",
		"resource_pack": "resource_pack", "datapack": "datapack", "addon": "addon", "server": "minecraft_server",
	} {
		if actual, ok := reportProjectAccessType(reportType); !ok || actual != projectType {
			t.Fatalf("reportProjectAccessType(%q) = %q, %v; want %q, true", reportType, actual, ok, projectType)
		}
	}
	if _, ok := reportProjectAccessType("comment"); ok {
		t.Fatal("comment was treated as a relationship-backed project")
	}
}

func TestResolveProjectReportNotifiesDevelopersInsteadOfSubmitterIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify report resolution notifications")
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
			claimed_by bigint,claimed_at timestamptz,resolved_at timestamptz,
			updated_at timestamptz not null default now(),lock_version bigint not null default 1
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
		insert into reports(id,public_id,target_type,target_public_id,reporter_id,target_actor_id,target_actor_role,status,claimed_by,claimed_at)
			values(1,'report001','mod','mod000001',1,10,'submitter','in_review',99,now());
		insert into report_snapshots values(1,'{"targetType":"mod","submitter":{"id":"user00010"}}');
		insert into mods(project_code,review_status) values('mod000001','approved');
		insert into effective_project_access values
			(10,'mod','mod000001','editor'),(20,'mod','mod000001','developer'),
			(20,'mod','mod000001','developer'),(21,'mod','mod000001','developer')
	`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/reports/report001/resolve", bytes.NewBufferString(
		`{"conclusion":"valid","note":"confirmed","deleteTarget":true,"idempotencyKey":"bug109-resolution"}`,
	))
	request.SetPathValue("id", "report001")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{
		Subject:         99,
		PermissionRules: []security.PermissionRule{{Code: "report.action.delete", Allow: true, Priority: 1}},
	}))
	response := httptest.NewRecorder()
	server.resolveUnifiedReport(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("resolve status = %d body=%s", response.Code, response.Body.String())
	}

	rows, err := pool.Query(ctx, `select aggregate_id from nats_outbox where event_type='report.target_action' order by aggregate_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actual := make([]string, 0)
	for rows.Next() {
		var recipient string
		if err = rows.Scan(&recipient); err != nil {
			t.Fatal(err)
		}
		actual = append(actual, recipient)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, []string{"20", "21"}) {
		t.Fatalf("moderation notification recipients = %v, want developers 20 and 21 (not submitter 10)", actual)
	}
}
