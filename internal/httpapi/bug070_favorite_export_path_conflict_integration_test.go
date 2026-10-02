package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteExportCreateRejectsConfirmedPathConflictIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify export path conflicts")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer database.DropEphemeralSchema(context.Background(), pool)

	var ownerID, collectionID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('bug070-owner','bug070-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b070path1',$1,'BUG-070 path conflicts') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	items := []favoriteModpackExportItem{
		{SourceProjectType: "mod", SourceProjectName: "Conflict A", ResultType: "exported", SelectedFileName: "Shared.jar"},
		{SourceProjectType: "mod", SourceProjectName: "Conflict B", ResultType: "exported", SelectedFileName: "shared.jar"},
	}
	markFavoriteExportPathConflicts(items)
	server := &Server{db: pool}
	saved, err := server.persistFavoriteModpackExportPreview(ctx, ownerID, favoriteModpackExportPreview{
		favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{
			CollectionID: collectionID, CollectionPublicID: "b070path1", CollectionName: "BUG-070 path conflicts",
			MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14",
			CollectionItemCount: 2, FailedItemCount: 2, Items: items,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	requestBody, err := json.Marshal(favoriteModpackExportRequest{
		MinecraftVersion: "1.21.1", Loader: "fabric", PreviewID: saved.PreviewID, PreviewHash: saved.PreviewHash,
		ExportCompatibleOnly: true, ConfirmCompatibleOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/favorite-collections/b070path1/modpack-exports", bytes.NewReader(requestBody))
	request.SetPathValue("id", "b070path1")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	response := httptest.NewRecorder()
	server.createFavoriteModpackExport(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "MODPACK_EXPORT_FILE_PATH_CONFLICT") {
		t.Fatalf("conflict create status=%d body=%s", response.Code, response.Body.String())
	}
	var tasks, consumed int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from favorite_modpack_export_tasks where owner_user_id=$1),
		(select count(*) from favorite_modpack_export_previews where public_id=$2 and consumed_at is not null)`, ownerID, saved.PreviewID).
		Scan(&tasks, &consumed); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || consumed != 0 {
		t.Fatalf("rejected conflict persisted tasks=%d consumed=%d", tasks, consumed)
	}
}
