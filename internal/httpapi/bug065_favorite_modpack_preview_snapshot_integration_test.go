package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestFavoriteModpackPreviewSnapshotFreezesConfirmedLoaderVersionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite MRPack preview snapshots")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var ownerID, collectionID, sourceRouteID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('bug065-owner','bug065-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b065snap1',$1,'BUG-065 snapshot') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into public_routes(public_id,entity_type,internal_id,canonical_path)
		values('b065rte01','mod',65001,'/mod/b065rte01') returning id`).Scan(&sourceRouteID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	preview := favoriteModpackExportPreview{favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{
		CollectionID: collectionID, CollectionPublicID: "b065snap1", CollectionName: "BUG-065 snapshot",
		MinecraftVersion: "1.21.1", Loader: "neoforge", LoaderVersion: "21.1.100",
		CollectionItemCount: 1, ExportedModCount: 1,
		Items: []favoriteModpackExportItem{{SourceProjectRouteID: int64Pointer(sourceRouteID), SourceProjectType: "mod", SourceProjectName: "Frozen Mod",
			ResultType: "exported", ModrinthVersionID: "confirmed-version"}},
	}}
	saved, err := server.persistFavoriteModpackExportPreview(ctx, ownerID, preview)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.PreviewID) != 9 || len(saved.PreviewHash) != 64 || !saved.ExpiresAt.After(time.Now()) {
		t.Fatalf("persisted preview identity = id:%q hash:%q expires:%v", saved.PreviewID, saved.PreviewHash, saved.ExpiresAt)
	}

	// Simulate authority drift after the user saw the preflight. Creation must
	// consume the immutable selection instead of rebuilding from this new value.
	preview.LoaderVersion = "21.1.999"
	request := favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge",
		PreviewID: saved.PreviewID, PreviewHash: saved.PreviewHash}
	createBody, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	invokeCreate := func() *httptest.ResponseRecorder {
		httpRequest := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/favorite-collections/b065snap1/modpack-exports", bytes.NewReader(createBody))
		httpRequest.SetPathValue("id", "b065snap1")
		httpRequest = httpRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
		response := httptest.NewRecorder()
		server.createFavoriteModpackExport(response, httpRequest)
		return response
	}
	response := invokeCreate()
	if response.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", response.Code, response.Body.String())
	}
	var loadedSourceRouteID *int64
	var loaderVersion, modrinthVersionID string
	if err = pool.QueryRow(ctx, `select task.loader_version,item.modrinth_version_id,item.source_project_route_id
		from favorite_modpack_export_tasks task join favorite_modpack_export_items item on item.task_id=task.id
		where task.owner_user_id=$1 order by task.id desc limit 1`, ownerID).Scan(&loaderVersion, &modrinthVersionID, &loadedSourceRouteID); err != nil {
		t.Fatal(err)
	}
	if loaderVersion != "21.1.100" || modrinthVersionID != "confirmed-version" || loadedSourceRouteID == nil || *loadedSourceRouteID != sourceRouteID {
		t.Fatalf("created task drifted to loader=%q item=%q route=%v", loaderVersion, modrinthVersionID, loadedSourceRouteID)
	}
	response = invokeCreate()
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "MODPACK_EXPORT_PREVIEW_CONSUMED") {
		t.Fatalf("second creation status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestFavoriteModpackPreviewSnapshotRejectsDigestDriftIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite MRPack preview digests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
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
		values('bug065-hash','bug065-hash@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b065hash1',$1,'BUG-065 hash') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	saved, err := server.persistFavoriteModpackExportPreview(ctx, ownerID, favoriteModpackExportPreview{
		favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{CollectionID: collectionID,
			CollectionPublicID: "b065hash1", CollectionName: "BUG-065 hash", MinecraftVersion: "1.21.1",
			Loader: "fabric", LoaderVersion: "0.18.1", Items: []favoriteModpackExportItem{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	request := favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "fabric", PreviewID: saved.PreviewID,
		PreviewHash: strings.Repeat("f", 64)}
	if _, _, err = loadFavoriteModpackExportPreviewForCreate(ctx, tx, ownerID, "b065hash1", request); !errors.Is(err, errFavoriteModpackExportPreviewMismatch) {
		t.Fatalf("digest drift error = %v, want mismatch", err)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("digest drift was incorrectly reported as a missing preview")
	}
}
