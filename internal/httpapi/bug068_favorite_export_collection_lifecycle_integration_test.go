package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestDeletingFavoriteCollectionPreservesReportsAndCancelsActiveExportsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite export collection deletion")
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

	var ownerID, collectionID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('bug068-owner','bug068-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b068keep1',$1,'BUG-068 retained collection') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_modpack_export_tasks(
		public_id,owner_user_id,collection_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,loader_version,
		status,stage,lease_token,lease_expires_at,finished_at,expires_at) values
		('b068pend1',$1,$2,'b068keep1','Pending report','v1','1.21.1','fabric','0.18.1','pending','queued','',null,null,null),
		('b068proc1',$1,$2,'b068keep1','Processing report','v1','1.21.1','fabric','0.18.1','processing','building','active-lease',now()+interval '1 minute',null,null),
		('b068ready',$1,$2,'b068keep1','Ready report','v1','1.21.1','fabric','0.18.1','ready','completed','',null,now(),now()+interval '1 day'),
		('b068expr1',$1,$2,'b068keep1','Expired report','v1','1.21.1','fabric','0.18.1','expired','expired','',null,now(),now()-interval '1 day')`, ownerID, collectionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_modpack_export_items(task_id,source_project_type,
		source_project_name_snapshot,result_type,reason_code,dependency_of)
		select id,'blueprint','Retained item '||public_id,'skipped','NOT_A_MOD','[]'::jsonb
		from favorite_modpack_export_tasks where owner_user_id=$1`, ownerID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/users/me/favorite-collections/b068keep1", nil)
	request.SetPathValue("id", "b068keep1")
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	response := httptest.NewRecorder()
	server.deleteFavoriteCollection(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", response.Code, response.Body.String())
	}

	var collectionCount, taskCount, detachedCount, itemCount int
	if err = pool.QueryRow(ctx, `select
		(select count(*) from favorite_collections where id=$1),
		(select count(*) from favorite_modpack_export_tasks where owner_user_id=$2),
		(select count(*) from favorite_modpack_export_tasks where owner_user_id=$2 and collection_id is null),
		(select count(*) from favorite_modpack_export_items item join favorite_modpack_export_tasks task on task.id=item.task_id where task.owner_user_id=$2)`,
		collectionID, ownerID).Scan(&collectionCount, &taskCount, &detachedCount, &itemCount); err != nil {
		t.Fatal(err)
	}
	if collectionCount != 0 || taskCount != 4 || detachedCount != 4 || itemCount != 4 {
		t.Fatalf("post-delete collection/tasks/detached/items=%d/%d/%d/%d want 0/4/4/4",
			collectionCount, taskCount, detachedCount, itemCount)
	}
	rows, err := pool.Query(ctx, `select public_id,status,stage,error_code,lease_token from favorite_modpack_export_tasks
		where owner_user_id=$1 order by public_id`, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	statuses := map[string]string{}
	for rows.Next() {
		var publicID, status, stage, errorCode, leaseToken string
		if err = rows.Scan(&publicID, &status, &stage, &errorCode, &leaseToken); err != nil {
			t.Fatal(err)
		}
		statuses[publicID] = status + "/" + stage + "/" + errorCode + "/" + leaseToken
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{"b068pend1", "b068proc1"} {
		if got := statuses[taskID]; got != "cancelled/cancelled/SOURCE_COLLECTION_DELETED/" {
			t.Errorf("%s state=%q", taskID, got)
		}
	}
	if statuses["b068ready"] != "ready/completed//" || statuses["b068expr1"] != "expired/expired//" {
		t.Errorf("terminal states changed: %#v", statuses)
	}

	historyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports?status=all&limit=10", nil)
	historyRequest = historyRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	historyResponse := httptest.NewRecorder()
	server.favoriteModpackExports(historyResponse, historyRequest)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", historyResponse.Code, historyResponse.Body.String())
	}
	var historyEnvelope struct {
		Data struct {
			Items []favoriteModpackExportSummary `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(historyResponse.Body.Bytes(), &historyEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(historyEnvelope.Data.Items) != 4 {
		t.Fatalf("history retained %d tasks, want 4", len(historyEnvelope.Data.Items))
	}
	for _, task := range historyEnvelope.Data.Items {
		if task.CollectionID != "b068keep1" {
			t.Errorf("task %s collection snapshot=%q", task.ID, task.CollectionID)
		}
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/modpack-exports/b068ready", nil)
	detailRequest.SetPathValue("taskId", "b068ready")
	detailRequest = detailRequest.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID}))
	detailResponse := httptest.NewRecorder()
	server.favoriteModpackExportDetail(detailResponse, detailRequest)
	if detailResponse.Code != http.StatusOK || !json.Valid(detailResponse.Body.Bytes()) {
		t.Fatalf("detail status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	if err = NewFavoriteModpackExportWorker(config.Config{}, pool, nil).process(ctx, "b068proc1"); err != nil {
		t.Fatalf("cancelled queued message did not become an idempotent no-op: %v", err)
	}
	var generatedFileID int64
	if err = pool.QueryRow(ctx, `insert into oss_files(bucket,endpoint,region,object_key,status,scan_status)
		values('bug068-bucket','https://oss.example.test','bug068-region','temporary/modpack-exports/b068proc1/generated.mrpack','active','trusted_generated') returning id`).Scan(&generatedFileID); err != nil {
		t.Fatal(err)
	}
	worker := NewFavoriteModpackExportWorker(config.Config{}, pool, nil)
	completed, err := worker.finalizeFavoriteExportArtifact(ctx, "b068proc1", "active-lease", generatedFileID, 2048, "bug068-sha", time.Now().Add(time.Hour))
	if err != nil || completed {
		t.Fatalf("cancelled completion result=%t error=%v", completed, err)
	}
	var fileStatus, cleanupStatus string
	var linkedFileID int64
	if err = pool.QueryRow(ctx, `select file.status,task.result_file_id,outbox.status
		from favorite_modpack_export_tasks task join oss_files file on file.id=task.result_file_id
		join oss_object_deletion_outbox outbox on outbox.oss_file_id=file.id
		where task.public_id='b068proc1'`).Scan(&fileStatus, &linkedFileID, &cleanupStatus); err != nil {
		t.Fatal(err)
	}
	if fileStatus != "deleted" || linkedFileID != generatedFileID || cleanupStatus != "pending" {
		t.Fatalf("cancelled artifact file/link/outbox=%s/%d/%s", fileStatus, linkedFileID, cleanupStatus)
	}
}
