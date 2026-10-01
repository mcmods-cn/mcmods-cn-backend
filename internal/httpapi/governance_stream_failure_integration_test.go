package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestGovernanceListsRejectPostgreSQLStreamFailureIntegration(t *testing.T) {
	cases := []string{"own reports", "admin reports", "public blackroom"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			ctx, pool, cfg := isolatedAITestDatabase(t)
			var actor int64
			if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('stream-fixture','stream@example.invalid','synthetic') returning id`).Scan(&actor); err != nil {
				t.Fatal(err)
			}
			// Preserve the real table and its constraints in this ownership-verified
			// disposable database; a volatile view emits a real execution error.
			if _, err := pool.Exec(ctx, `create function audit_governance_failure() returns text language plpgsql volatile as $$ begin raise exception 'synthetic stream failure'; end $$`); err != nil {
				t.Fatal(err)
			}
			server := &Server{db: pool, cfg: cfg}
			handler := server.ownReports
			statement := fmt.Sprintf(`alter table reports rename to audit_preserved_reports;
create view reports as select 1::bigint id,audit_governance_failure() public_id,'user'::text target_type,'synthetic'::text target_public_id,'spam'::text reason_code,'pending'::text status,now() created_at,null::timestamptz resolved_at,null::timestamptz claimed_at,%d::bigint reporter_id`, actor)
			if name == "admin reports" {
				handler = server.adminReports
			} else if name == "public blackroom" {
				handler = server.publicBlackroom
				statement = fmt.Sprintf(`alter table ban_records rename to audit_preserved_bans;
create view ban_records as select 1::bigint id,audit_governance_failure() public_id,%d::bigint user_id,'synthetic'::text username_snapshot,''::text avatar_snapshot,'spam'::text reason_code,''::text custom_reason,'active'::text status,now() starts_at,null::timestamptz ends_at,null::timestamptz revoked_at,now() created_at`, actor)
			}
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actor}))
			response := httptest.NewRecorder()
			handler(response, request)
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("failed PostgreSQL stream returned %d, wanted 500", response.Code)
			}
		})
	}
}
