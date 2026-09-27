package httpapi

import (
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

func TestGovernanceHandlersFailClosedOnDatabaseAndSnapshotErrorsIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to verify governance read failures")
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
		create temporary table users(
			id bigint primary key,public_id text not null,username text not null
		);
		create temporary table reports(
			id bigint primary key,public_id text not null,target_type text not null,target_public_id text not null,
			reporter_id bigint not null,target_actor_id bigint,target_actor_role text not null,reason_code text not null,reason_version integer not null,
			custom_reason text not null,detail text not null,status text not null,created_at timestamptz not null,
			resolved_at timestamptz,claimed_by bigint,claimed_at timestamptz
		);
		create temporary table report_snapshots(report_id bigint not null,payload jsonb not null);
		create temporary table report_evidence(
			id bigint primary key,public_id text not null,report_id bigint not null,original_name text not null,
			content_type text not null,byte_size bigint not null,sha256 text not null,scan_status text not null,
			status text not null,created_at timestamptz not null,deleted_at timestamptz
		);
		create temporary table report_reviews(
			id bigint primary key,report_id bigint not null,reviewer_id bigint not null,conclusion text not null,
			note text not null,created_at timestamptz not null
		);
		create temporary table moderation_actions(
			id bigint primary key,public_id text not null,report_id bigint not null,actor_id bigint not null,
			action_type text not null,target_type text not null,target_public_id text not null,reason text not null,
			created_at timestamptz not null
		);
		create temporary table ban_records(
			id bigint primary key,public_id text not null,user_id bigint not null,username_snapshot text not null,
			avatar_snapshot text not null,reason_code text not null,custom_reason text not null,status text not null,
			starts_at timestamptz not null,ends_at timestamptz,revoked_at timestamptz,created_at timestamptz not null
		);
		insert into users values(1,'user00001','reporter');
		insert into reports values(
			1,'report001','mod','mod000001',1,null,'submitter','spam',1,'','details','pending',now(),null,null,null
		);
		insert into report_snapshots values(1,'{"targetType":"mod","id":"mod000001"}'::jsonb);
		insert into ban_records values(
			1,'ban000001',1,'reporter','','spam','','active',now(),null,null,now()
		)
	`); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	claims := security.Claims{Subject: 1, PermissionRules: []security.PermissionRule{{
		Code: "report.snapshot.view", Allow: true, Priority: 1,
	}}}

	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/reports", nil)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		server.ownReports(response, request)
	})
	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.adminReports(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports", nil).WithContext(ctx))
	})
	assertHandlerStatus(t, http.StatusOK, func(response *httptest.ResponseRecorder) {
		server.publicBlackroom(response, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/blackroom", nil).WithContext(ctx))
	})
	invokeDetail := func(response *httptest.ResponseRecorder) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports/report001", nil)
		request.SetPathValue("id", "report001")
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		server.adminReportDetail(response, request)
	}
	assertHandlerStatus(t, http.StatusOK, invokeDetail)

	if _, err = pool.Exec(ctx, `update report_snapshots set payload='null'::jsonb`); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, invokeDetail)
	if _, err = pool.Exec(ctx, `update report_snapshots set payload='{"targetType":"mod"}'::jsonb`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table reports rename column reason_code to arch030_broken_reason`); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/reports", nil)
		request = request.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		server.ownReports(response, request)
	})
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.adminReports(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/reports", nil).WithContext(ctx))
	})
	if _, err = pool.Exec(ctx, `alter table reports rename column arch030_broken_reason to reason_code`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table ban_records rename column reason_code to arch030_broken_reason`); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, func(response *httptest.ResponseRecorder) {
		server.publicBlackroom(response, httptest.NewRequest(http.MethodGet, "/api/v1/site-affairs/blackroom", nil).WithContext(ctx))
	})
	if _, err = pool.Exec(ctx, `alter table ban_records rename column arch030_broken_reason to reason_code`); err != nil {
		t.Fatal(err)
	}

	if _, err = pool.Exec(ctx, `alter table report_reviews rename column note to arch030_broken_note`); err != nil {
		t.Fatal(err)
	}
	assertHandlerStatus(t, http.StatusInternalServerError, invokeDetail)
}
