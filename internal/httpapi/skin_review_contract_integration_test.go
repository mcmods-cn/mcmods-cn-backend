package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestSkinReviewReturnsPublicRevisionIDIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	var ownerID, fileID, assetID int64
	var assetPublicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('skin-review-audit','skin-review-audit@example.invalid','synthetic') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, `insert into oss_files(bucket,object_key,original_name,content_type,size_bytes,uploader_id,status) values('synthetic','skin-review-audit.png','skin-review-audit.png','image/png',1,$1,'active') returning id`, ownerID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if _, err = tx.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes) values($1,$2,'skin-review-audit.png',64,64,1)`, hash, fileID); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,display_name,visibility) values($1,$2,'skin','Synthetic audit','private') returning id,public_id`, ownerID, hash).Scan(&assetID, &assetPublicID); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(skinAssetContentSnapshot{PublicID: assetPublicID, Name: "Synthetic audit", Model: "default", Visibility: "private", Tags: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: "skin", EntityID: assetID, AggregateType: "skin", AggregateKey: assetPublicID, Snapshot: raw, ActorID: ownerID, Source: "skin_metadata", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	server := &Server{cfg: cfg, db: pool}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/content/revisions/"+created.RevisionPublicID+"/review", bytes.NewBufferString(`{"status":"rejected","note":"Synthetic contract audit"}`))
	request.SetPathValue("revisionId", created.RevisionPublicID)
	request = request.WithContext(context.WithValue(request.Context(), claimsContextKey, security.Claims{Subject: ownerID, PermissionRules: []security.PermissionRule{{Code: "content.review", Allow: true}}}))
	response := httptest.NewRecorder()
	server.reviewContentRevision(response, request)
	var body struct {
		Data struct {
			RevisionID string `json:"revisionId"`
			Status     string `json:"status"`
		} `json:"data"`
	}
	if response.Code != http.StatusOK {
		t.Fatalf("review status: %d", response.Code)
	}
	if err = json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data.RevisionID != created.RevisionPublicID || body.Data.Status != "rejected" {
		t.Fatal("review did not return the public string revision ID")
	}
	var status string
	if err = pool.QueryRow(ctx, `select status from change_requests where id=$1`, created.ChangeRequestID).Scan(&status); err != nil || status != "rejected" {
		t.Fatal("review outcome was not persisted")
	}
	response = httptest.NewRecorder()
	request.Body = io.NopCloser(bytes.NewBufferString(`{"status":"rejected","note":"Synthetic contract audit"}`))
	server.reviewContentRevision(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate review status: %d", response.Code)
	}
}
