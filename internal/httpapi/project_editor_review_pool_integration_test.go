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
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestProjectEditorReviewDoesNotReenterPoolDuringTransactionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolCfg, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolCfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Error(err)
		}
	}()
	if _, err = pool.Exec(ctx, `insert into users(id,username,email,password_hash) values
 (99480001,'editor-applicant','editor-applicant@example.invalid','test-only');
 insert into mods(id,project_code,slug,primary_name,review_status,submitted_by)
 values(99480003,'editpool1','editor-pool','Editor pool target','approved',99480001)`); err != nil {
		t.Fatal(err)
	}
	cfg := config.Load()
	reviewerID, _, token := createTEST044User(t, ctx, pool, cfg, "editor-pool-reviewer", "UTC")
	claims, err := security.ParseToken(cfg.JWTSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = reviewerID
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	for _, decision := range []string{"approved", "rejected"} {
		t.Run(decision, func(t *testing.T) {
			var applicationID string
			if err := pool.QueryRow(ctx, `insert into project_editor_applications(target_route_id,user_id,proof_markdown)
    select id,99480001,'synthetic proof' from public_routes where entity_type='mod' and internal_id=99480003 returning public_id`).Scan(&applicationID); err != nil {
				t.Fatal(err)
			}
			queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
			defer queryCancel()
			review := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/project-editor-applications/"+applicationID, strings.NewReader(`{"status":"`+decision+`","note":"synthetic review"}`))
				req.SetPathValue("id", applicationID)
				req = req.WithContext(context.WithValue(queryCtx, claimsContextKey, claims))
				response := httptest.NewRecorder()
				server.reviewProjectEditorApplication(response, req)
				return response
			}
			if decision == "approved" {
				if response := review(); response.Code != http.StatusForbidden {
					t.Fatalf("review without the permission returned %d", response.Code)
				}
				var pending bool
				if err := pool.QueryRow(ctx, `select status='pending' and reviewed_by is null from project_editor_applications where public_id=$1`, applicationID).Scan(&pending); err != nil || !pending {
					t.Fatalf("pending application changed before grant: pending=%t err=%v", pending, err)
				}
				grantTEST044Permissions(t, ctx, pool, reviewerID, "project.editor.review")
			}
			if response := review(); response.Code != http.StatusOK {
				t.Fatalf("single-connection review returned %d: %s", response.Code, response.Body.String())
			}
			if queryCtx.Err() != nil {
				t.Fatal("review held its transaction while waiting on the same pool")
			}
			var status string
			if err := pool.QueryRow(ctx, `select status from project_editor_applications where public_id=$1`, applicationID).Scan(&status); err != nil || status != decision {
				t.Fatal("review did not persist exact decision")
			}
		})
	}
}
