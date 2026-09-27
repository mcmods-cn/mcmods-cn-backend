package httpapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/database"
)

func TestSkinCreationReviewPolicyPersistsPendingAndBypassRevisionsIntegration(t *testing.T) {
	pool := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano()
	var ownerID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`,
		fmt.Sprintf("bug087_%d", suffix), fmt.Sprintf("bug087_%d@example.test", suffix)).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	hash := fmt.Sprintf("%064x", suffix)
	objectKey := fmt.Sprintf("bug087/%d.png", suffix)
	var fileID int64
	if err = tx.QueryRow(ctx, `insert into oss_files(
		object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,sha256,uploader_id,status,scan_status)
		values($1,$2,'minecraft_texture','bug087.png','bug087.png','image/png',128,128,$3,$4,'active','trusted_generated') returning id`,
		objectKey, ossSharedSkinTextureCategory(), hash, ownerID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
		values($1,$2,$3,64,64,128)`, hash, fileID, objectKey); err != nil {
		t.Fatal(err)
	}
	request := skinAssetCreateRequest{
		Kind: "skin", Model: "default", Name: "BUG-087 reviewed skin", Description: "review policy",
		Tags: []string{"bug087"}, Visibility: "public", DefaultLocale: "en-US",
		Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "BUG-087 reviewed skin", Summary: "review policy"}},
	}

	pending, err := persistSkinAssetCreationTx(ctx, tx, ownerID, hash, request, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSkinCreationReviewState(t, ctx, tx, pending, "pending", false, "pending", false, "pending", 0)
	if err = rejectSkinRevisionTx(ctx, tx, pending.AssetID, true); err != nil {
		t.Fatal(err)
	}
	assertSkinCreationReviewState(t, ctx, tx, pending, "rejected", false, "rejected", false, "pending", 0)

	approved, err := persistSkinAssetCreationTx(ctx, tx, ownerID, hash, request, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSkinCreationReviewState(t, ctx, tx, approved, "approved", true, "approved", true, "approved", 1)
}

func assertSkinCreationReviewState(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	created createdSkinAsset,
	wantAssetStatus string,
	wantAssetPublished bool,
	wantLocalizationStatus string,
	wantLocalizationPublished bool,
	wantRequestStatus string,
	wantCatalogRows int,
) {
	t.Helper()
	var assetStatus string
	var assetPublished bool
	if err := tx.QueryRow(ctx, `select review_status,published_revision_id is not null from skin_assets where id=$1`, created.AssetID).
		Scan(&assetStatus, &assetPublished); err != nil || assetStatus != wantAssetStatus || assetPublished != wantAssetPublished {
		t.Fatalf("asset state = %q/%t err=%v, want %q/%t", assetStatus, assetPublished, err, wantAssetStatus, wantAssetPublished)
	}
	var localizationStatus string
	var localizationPublished bool
	if err := tx.QueryRow(ctx, `select review_status,published_revision_id is not null from content_localizations
		where subject_type='skin' and subject_id=$1 and locale='en-US'`, created.AssetID).
		Scan(&localizationStatus, &localizationPublished); err != nil || localizationStatus != wantLocalizationStatus || localizationPublished != wantLocalizationPublished {
		t.Fatalf("localization state = %q/%t err=%v, want %q/%t", localizationStatus, localizationPublished, err, wantLocalizationStatus, wantLocalizationPublished)
	}
	var requestStatus, operation, source string
	var baseIsNull bool
	if err := tx.QueryRow(ctx, `select request.status,request.base_revision_id is null,request.metadata->>'operation',revision.source
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where request.aggregate_type='skin' and request.aggregate_key=$1`, created.PublicID).
		Scan(&requestStatus, &baseIsNull, &operation, &source); err != nil || requestStatus != wantRequestStatus || !baseIsNull || operation != "create" || source != "skin_upload" {
		t.Fatalf("review state = %q baseNull=%t operation=%q source=%q err=%v", requestStatus, baseIsNull, operation, source, err)
	}
	var catalogRows int
	if err := tx.QueryRow(ctx, `select count(*) from skin_public_catalog where asset_id=$1`, created.AssetID).Scan(&catalogRows); err != nil || catalogRows != wantCatalogRows {
		t.Fatalf("catalog rows = %d err=%v, want %d", catalogRows, err, wantCatalogRows)
	}
}
