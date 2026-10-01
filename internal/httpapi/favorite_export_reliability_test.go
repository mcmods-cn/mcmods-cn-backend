package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteExportDatabaseFailureIsNotExpired(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://fixture:fixture@127.0.0.1:1/unused")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	server := &Server{db: pool}
	for _, target := range []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"history", server.favoriteModpackExports},
		{"detail", server.favoriteModpackExportDetail},
		{"download", server.downloadFavoriteModpackExport},
	} {
		t.Run(target.name, func(t *testing.T) {
			res := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/exports/fixture", nil)
			req.SetPathValue("taskId", "fixture00")
			target.handler(res, req)
			if res.Code != http.StatusInternalServerError {
				t.Fatalf("database outage became missing/expired task: %d %s", res.Code, res.Body.String())
			}
		})
	}
}

func TestFavoriteExportRejectsInvalidDependencyShapeIntegration(t *testing.T) {
	url := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("requires dedicated migrated PostgreSQL via MCMODS_TEST_DATABASE_URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	key := "apia_export_" + randomHex(8)
	var userID, collectionID, taskID, itemID int64
	var taskPublicID string
	defer func() {
		if userID != 0 {
			if _, err := pool.Exec(context.Background(), `delete from users where id=$1`, userID); err != nil {
				t.Errorf("allocated export fixture cleanup: %v", err)
			}
		}
	}()
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, key, key+"@example.test").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(user_id,name) values($1,$2) returning id`, userID, key).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	// A failed task is never picked up by the real export worker.
	if err = pool.QueryRow(ctx, `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,pack_name,pack_version_id,minecraft_version,loader_type,status)
		values($1,$2,'Fixture','fixture-1','1.21.1','fabric','failed') returning id,public_id`, userID, collectionID).Scan(&taskID, &taskPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_modpack_export_items(task_id,source_project_type,source_project_name_snapshot,result_type,dependency_of)
		values($1,'mod','Fixture','failed','"invalid-shape"'::jsonb) returning id`, taskID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	load := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/exports/"+taskPublicID, nil)
		req.SetPathValue("taskId", taskPublicID)
		req = req.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: userID}))
		res := httptest.NewRecorder()
		server.favoriteModpackExportDetail(res, req)
		return res
	}
	if res := load(); res.Code != http.StatusInternalServerError {
		t.Fatalf("invalid dependency JSON became a successful partial report: %d %s", res.Code, res.Body.String())
	}
	if _, err = pool.Exec(ctx, `update favorite_modpack_export_items set dependency_of='[]'::jsonb where id=$1`, itemID); err != nil {
		t.Fatal(err)
	}
	if res := load(); res.Code != http.StatusOK {
		t.Fatalf("valid dependency array cannot be read: %d %s", res.Code, res.Body.String())
	}
}
