package httpapi

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSkinLocalizationRevisionIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set MCMODS_TEST_DATABASE_URL to an isolated migrated PostgreSQL database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	unique := "apia_skin_" + randomHex(8)
	var ownerID, fileID, assetID, revisionID int64
	var publicID string
	if err := tx.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'fixture') returning id`, unique, unique+"@example.test").Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 32) + randomHex(16)
	if err = tx.QueryRow(ctx, `insert into oss_files(bucket,object_key,original_name,content_type,size_bytes,uploader_id,status)
		values('fixture',$1,'fixture.png','image/png',1,$2,'active') returning id`, unique+".png", ownerID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
		values($1,$2,$3,64,64,1)`, hash, fileID, unique+".png"); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,display_name,visibility)
		values($1,$2,'skin','Original','public') returning id,public_id`, ownerID, hash).Scan(&assetID, &publicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into content_localizations(subject_type,subject_id,locale,name,provenance,editable,review_status,revision_no)
		values('skin',$1,'en-US','Machine name','ai',false,'approved',4),('skin',$1,'ja-JP','Protected human name','human',true,'approved',3)`, assetID); err != nil {
		t.Fatal(err)
	}
	request := skinAssetUpdateRequest{DefaultLocale: "zh-CN", Localizations: []catalogLocalizationEdit{
		{Locale: "zh-CN", Name: "中文名称", Summary: "中文简介"},
		{Locale: "en-US", Name: "English name", Summary: "English summary"},
	}}
	if err = normalizeSkinAssetUpdateLocalizations(&request); err != nil {
		t.Fatal(err)
	}
	publication := skinAssetContentSnapshot{PublicID: publicID, Model: "default", Name: *request.Name, Description: *request.Description,
		Tags: []string{}, Visibility: "public", DefaultLocale: request.DefaultLocale, Localizations: request.Localizations, UpdateLocalizations: true}
	raw, err := json.Marshal(publication)
	if err != nil {
		t.Fatal(err)
	}
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{EntityType: "skin", EntityID: assetID,
		AggregateType: "skin", AggregateKey: publicID, Snapshot: raw, ActorID: ownerID, Source: "skin_metadata", Status: "pending"})
	if err != nil {
		t.Fatal(err)
	}
	revisionID = created.RevisionID
	var stored skinAssetContentSnapshot
	if err = tx.QueryRow(ctx, `select snapshot from content_revisions where id=$1`, revisionID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Localizations) != 2 || !stored.UpdateLocalizations {
		t.Fatal("a single pending revision lost its language edits")
	}
	var legacyName string
	if err = tx.QueryRow(ctx, `select display_name from skin_assets where id=$1`, assetID).Scan(&legacyName); err != nil {
		t.Fatal(err)
	}
	if legacyName != "Original" {
		t.Fatal("pending revision changed published metadata")
	}
	if err = applySkinAssetSnapshotTx(ctx, tx, assetID, ownerID, revisionID, stored); err != nil {
		t.Fatal(err)
	}
	var defaultLocale string
	if err = tx.QueryRow(ctx, `select default_locale from content_subjects where subject_type='skin' and subject_id=$1`, assetID).Scan(&defaultLocale); err != nil {
		t.Fatal(err)
	}
	if defaultLocale != "zh-CN" {
		t.Fatalf("default locale=%q", defaultLocale)
	}
	var localeCount int
	if err = tx.QueryRow(ctx, `select count(*) from content_localizations where subject_type='skin' and subject_id=$1`, assetID).Scan(&localeCount); err != nil {
		t.Fatal(err)
	}
	if localeCount != 3 {
		t.Fatalf("language edit lost an omitted human locale: count=%d", localeCount)
	}
	var name, provenance string
	var number int64
	if err = tx.QueryRow(ctx, `select name,provenance,revision_no from content_localizations where subject_type='skin' and subject_id=$1 and locale='en-US'`, assetID).Scan(&name, &provenance, &number); err != nil {
		t.Fatal(err)
	}
	if name != "English name" || provenance != "human_corrected" || number != 5 {
		t.Fatalf("manual correction name=%q provenance=%q revision=%d", name, provenance, number)
	}
	var profileID int64
	var profilePublicID string
	if err = tx.QueryRow(ctx, `insert into player_profiles(user_id,uuid,name,visibility) values($1,$2::uuid,$3,'public') returning id,public_id`, ownerID, "01234567-1234-4321-9123-123456789abc", "P"+randomHex(6)).Scan(&profileID, &profilePublicID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into player_profile_textures(profile_id,kind,asset_id) values($1,'skin',$2)`, profileID, assetID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `update skin_assets set visibility='private' where id=$1`, assetID); err != nil {
		t.Fatal(err)
	}
	for _, access := range []struct {
		name           string
		viewerID       int64
		admin, allowed bool
	}{{"guest", 0, false, false}, {"owner", ownerID, false, true}, {"administrator", 0, true, true}, {"other-user", ownerID + 1, false, false}} {
		t.Run(access.name, func(t *testing.T) {
			rows, err := tx.Query(ctx, skinPlayerTextureSelectSQL, profilePublicID, access.viewerID, access.admin)
			if err != nil {
				t.Fatal(err)
			}
			visible := rows.Next()
			rowErr := rows.Err()
			rows.Close()
			if rowErr != nil {
				t.Fatal(rowErr)
			}
			if visible != access.allowed {
				t.Fatalf("private skin metadata visible=%v wanted=%v", visible, access.allowed)
			}
		})
	}
	// A PostgreSQL transaction rollback is the supported cleanup for these
	// immutable revisions; their append-only trigger is never disabled.
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err = pool.QueryRow(ctx, `select exists(select 1 from skin_assets where id=$1)`, assetID).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("fixture transaction did not roll back")
	}
}
