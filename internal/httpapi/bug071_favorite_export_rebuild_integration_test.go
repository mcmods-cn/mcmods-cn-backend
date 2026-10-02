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

func TestFavoriteExportOriginalSnapshotRebuildSurvivesDetachedCollectionIntegration(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify export rebuild snapshots")
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
		values('bug071-owner','bug071-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b071hist1',$1,'BUG-071 history') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	var sourceTaskID string
	if err = pool.QueryRow(ctx, `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,collection_public_id_snapshot,
		pack_name,pack_version_id,minecraft_version,loader_type,loader_version,allow_compatible_only,status,stage,
		collection_item_count,exported_mod_count,skipped_item_count,final_file_count,report_version,created_at)
		values($1,$2,'b071hist1','BUG-071 history','original-version','1.21.1','fabric','0.16.14',true,'ready','completed',2,1,1,1,1,now()-interval '1 minute')
		returning public_id`, ownerID, collectionID).Scan(&sourceTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_modpack_export_items(task_id,source_project_type,source_project_name_snapshot,
		result_type,reason_code,modrinth_project_id,modrinth_version_id,selected_version_name,selected_file_name,minecraft_version,loader,release_type,
		env_client,env_server,file_size,sha1,sha512,download_url,dependency_of)
		select id,'mod','Original exported','exported','','project','version','1.0.0','original.jar','1.21.1','fabric','release',
		'required','required',123,repeat('1',40),repeat('2',128),
		'https://cdn.modrinth.com/data/project/versions/version/original.jar','[]'::jsonb
		from favorite_modpack_export_tasks where public_id=$1`, sourceTaskID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_modpack_export_items(task_id,source_project_type,source_project_name_snapshot,result_type,
		reason_code,reason_detail,dependency_of)
		select id,'blueprint','Original skipped','skipped','NOT_A_MOD','original report fact','[]'::jsonb
		from favorite_modpack_export_tasks where public_id=$1`, sourceTaskID); err != nil {
		t.Fatal(err)
	}
	catalog, err := loadMinecraftVersionConfig(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	for index := range catalog.Loaders {
		if catalog.Loaders[index].Code == "Fabric" {
			catalog.Loaders[index].Versions = []string{"1.21.1"}
		}
	}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, pool, catalog, []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14",
		SourceURL: fabricLoaderCatalogURL, ObservedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	claimsContext := context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID})
	rebuildRequest := func(source string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/modpack-exports/"+sourceTaskID+"/rebuild-preflight",
			strings.NewReader(`{"source":"`+source+`"}`))
		request.SetPathValue("taskId", sourceTaskID)
		request = request.WithContext(claimsContext)
		response := httptest.NewRecorder()
		server.rebuildFavoriteModpackExportPreview(response, request)
		return response
	}
	liveCurrentResponse := rebuildRequest("current_collection")
	if liveCurrentResponse.Code != http.StatusOK || !strings.Contains(liveCurrentResponse.Body.String(), `"rebuildSource":"current_collection"`) ||
		!strings.Contains(liveCurrentResponse.Body.String(), `"allowCompatibleOnly":true`) {
		t.Fatalf("live current rebuild status=%d body=%s", liveCurrentResponse.Code, liveCurrentResponse.Body.String())
	}
	if _, err = pool.Exec(ctx, `delete from favorite_collections where id=$1`, collectionID); err != nil {
		t.Fatal(err)
	}
	currentResponse := rebuildRequest("current_collection")
	if currentResponse.Code != http.StatusConflict || !strings.Contains(currentResponse.Body.String(), "MODPACK_EXPORT_REBUILD_SOURCE_UNAVAILABLE") {
		t.Fatalf("detached current rebuild status=%d body=%s", currentResponse.Code, currentResponse.Body.String())
	}
	originalResponse := rebuildRequest("original_snapshot")
	if originalResponse.Code != http.StatusOK {
		t.Fatalf("original rebuild preflight status=%d body=%s", originalResponse.Code, originalResponse.Body.String())
	}
	var previewResponse struct {
		Data favoriteModpackExportPreview `json:"data"`
	}
	if err = json.Unmarshal(originalResponse.Body.Bytes(), &previewResponse); err != nil {
		t.Fatal(err)
	}
	preview := previewResponse.Data
	if preview.CollectionPublicID != "b071hist1" || preview.MinecraftVersion != "1.21.1" || preview.Loader != "fabric" ||
		preview.LoaderVersion != "0.16.14" || !preview.AllowCompatibleOnly || preview.ReportVersion != 1 ||
		preview.RebuildSource != "original_snapshot" || preview.ExportedModCount != 1 || preview.SkippedItemCount != 1 || len(preview.Items) != 2 {
		t.Fatalf("original rebuild preview = %+v items=%+v body=%s", preview, preview.Items, originalResponse.Body.String())
	}
	var previewCollectionID *int64
	var collectionSnapshot, sourceMode string
	if err = pool.QueryRow(ctx, `select collection_id,collection_public_id_snapshot,source_mode
		from favorite_modpack_export_previews where public_id=$1`, preview.PreviewID).
		Scan(&previewCollectionID, &collectionSnapshot, &sourceMode); err != nil {
		t.Fatal(err)
	}
	if previewCollectionID != nil || collectionSnapshot != "b071hist1" || sourceMode != "original_snapshot" {
		t.Fatalf("stored rebuild preview collection=%v snapshot=%q mode=%q", previewCollectionID, collectionSnapshot, sourceMode)
	}

	createFromPreview := func(exportCompatibleOnly, confirmCompatibleOnly bool) *httptest.ResponseRecorder {
		createRequest := favoriteModpackExportRequest{MinecraftVersion: preview.MinecraftVersion, Loader: preview.Loader,
			PreviewID: preview.PreviewID, PreviewHash: preview.PreviewHash, ExportCompatibleOnly: exportCompatibleOnly,
			ConfirmCompatibleOnly: confirmCompatibleOnly}
		createBody, marshalErr := json.Marshal(createRequest)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		request := httptest.NewRequest(http.MethodPost, "/api/v1/users/me/favorite-collections/b071hist1/modpack-exports", bytes.NewReader(createBody))
		request.SetPathValue("id", "b071hist1")
		request = request.WithContext(claimsContext)
		response := httptest.NewRecorder()
		server.createFavoriteModpackExport(response, request)
		return response
	}
	mismatchResponse := createFromPreview(false, false)
	if mismatchResponse.Code != http.StatusConflict || !strings.Contains(mismatchResponse.Body.String(), "MODPACK_EXPORT_REBUILD_CONFIRMATION_MISMATCH") {
		t.Fatalf("original confirmation mismatch status=%d body=%s", mismatchResponse.Code, mismatchResponse.Body.String())
	}
	createResponse := createFromPreview(true, true)
	if createResponse.Code != http.StatusAccepted {
		t.Fatalf("detached original create status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
	var createResult struct {
		Data struct {
			TaskID string `json:"taskId"`
		} `json:"data"`
	}
	if err = json.Unmarshal(createResponse.Body.Bytes(), &createResult); err != nil {
		t.Fatal(err)
	}
	var rebuiltCollectionID *int64
	var rebuiltSnapshot string
	var rebuiltAllow bool
	var rebuiltItems int
	if err = pool.QueryRow(ctx, `select task.collection_id,task.collection_public_id_snapshot,task.allow_compatible_only,
		(select count(*) from favorite_modpack_export_items item where item.task_id=task.id)
		from favorite_modpack_export_tasks task where task.public_id=$1`, createResult.Data.TaskID).
		Scan(&rebuiltCollectionID, &rebuiltSnapshot, &rebuiltAllow, &rebuiltItems); err != nil {
		t.Fatal(err)
	}
	if rebuiltCollectionID != nil || rebuiltSnapshot != "b071hist1" || !rebuiltAllow || rebuiltItems != 2 {
		t.Fatalf("rebuilt task collection=%v snapshot=%q allow=%v items=%d", rebuiltCollectionID, rebuiltSnapshot, rebuiltAllow, rebuiltItems)
	}
}
