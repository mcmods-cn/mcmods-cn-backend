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

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestSEC034PrivateSkinProfilePrivacyMatrixIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify private skin profile boundaries")
	}
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop SEC-034 ephemeral schema: %v", dropErr)
		}
	})

	var ownerID, viewerID int64
	var ownerPublicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('Sec034Owner','sec034-owner@example.test','not-used','active') returning id,public_id`).
		Scan(&ownerID, &ownerPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('Sec034Viewer','sec034-viewer@example.test','not-used','active') returning id`).Scan(&viewerID); err != nil {
		t.Fatal(err)
	}
	privateHash := strings.Repeat("3", 64)
	publicHash := strings.Repeat("4", 64)
	for _, hash := range []string{privateHash, publicHash} {
		var fileID int64
		if err = pool.QueryRow(ctx, `insert into oss_files(
			object_key,category,source,original_name,source_original_name,content_type,size_bytes,source_size_bytes,
			sha256,uploader_id,status,scan_status)
			values($1,$2,'minecraft_texture','sec034.png','sec034.png','image/png',128,128,$3,$4,'active','trusted_generated') returning id`,
			"sec034/"+hash+".png", ossSharedSkinTextureCategory(), hash, ownerID).Scan(&fileID); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes)
			values($1,$2,$3,64,64,128)`, hash, fileID, "sec034/"+hash+".png"); err != nil {
			t.Fatal(err)
		}
	}
	var privateAssetID, publicAssetID int64
	var privateAssetPublicID string
	if err = pool.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
		values($1,$2,'skin','default','SEC-034 private','private','approved','active') returning id,public_id`, ownerID, privateHash).
		Scan(&privateAssetID, &privateAssetPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into skin_assets(owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
		values($1,$2,'skin','default','SEC-034 public','public','approved','active') returning id`, ownerID, publicHash).
		Scan(&publicAssetID); err != nil {
		t.Fatal(err)
	}

	profileIDs := make(map[string]int64)
	profilePublicIDs := make(map[string]string)
	for index, fixture := range []struct {
		key        string
		name       string
		visibility string
	}{
		{key: "legacy_public_private", name: "S34Legacy", visibility: "public"},
		{key: "private_private", name: "S34Private", visibility: "private"},
		{key: "empty_public", name: "S34Empty", visibility: "public"},
		{key: "public_public", name: "S34Public", visibility: "public"},
		{key: "legacy_unlisted_private", name: "S34Unlisted", visibility: "unlisted"},
	} {
		profileUUID := "00000000-0000-0000-0000-" + []string{"000000000341", "000000000342", "000000000343", "000000000344", "000000000345"}[index]
		var profileID int64
		var profilePublicID string
		if err = pool.QueryRow(ctx, `insert into player_profiles(user_id,uuid,name,visibility,is_default)
			values($1,$2::uuid,$3,$4,$5) returning id,public_id`, ownerID, profileUUID, fixture.name, fixture.visibility, index == 0).
			Scan(&profileID, &profilePublicID); err != nil {
			t.Fatal(err)
		}
		profileIDs[fixture.key] = profileID
		profilePublicIDs[fixture.key] = profilePublicID
	}
	if _, err = pool.Exec(ctx, `insert into player_profile_textures(profile_id,kind,asset_id,model) values
		($1,'skin',$5,'default'),($2,'skin',$5,'default'),($3,'skin',$6,'default'),($4,'skin',$5,'default')`,
		profileIDs["legacy_public_private"], profileIDs["private_private"], profileIDs["public_public"],
		profileIDs["legacy_unlisted_private"], privateAssetID, publicAssetID); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	legacyPublicID := profilePublicIDs["legacy_public_private"]
	for _, testCase := range []struct {
		name        string
		viewerID    int64
		wantTexture bool
	}{
		{name: "anonymous", wantTexture: false},
		{name: "unrelated authenticated viewer", viewerID: viewerID, wantTexture: false},
		{name: "asset owner", viewerID: ownerID, wantTexture: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			profile, loadErr := server.loadPlayerProfileByPublicIDForViewer(ctx, legacyPublicID, testCase.viewerID)
			if loadErr != nil {
				t.Fatal(loadErr)
			}
			if got := profile.Skin != nil; got != testCase.wantTexture {
				t.Fatalf("private texture visible=%t want=%t for %s", got, testCase.wantTexture, testCase.name)
			}
		})
	}
	unlistedProfile, err := server.loadPlayerProfileByPublicIDForViewer(ctx, profilePublicIDs["legacy_unlisted_private"], viewerID)
	if err != nil || unlistedProfile.Skin != nil {
		t.Fatalf("private texture visible through unlisted profile: skin=%v err=%v", unlistedProfile.Skin, err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/player-profiles/"+legacyPublicID, nil)
	request.SetPathValue("publicId", legacyPublicID)
	response := httptest.NewRecorder()
	server.playerProfileDetail(response, request)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), privateHash) || strings.Contains(response.Body.String(), privateAssetPublicID) {
		t.Fatalf("anonymous public profile response leaked private asset: status=%d body=%s", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/player-profiles/"+profilePublicIDs["private_private"], nil)
	request.SetPathValue("publicId", profilePublicIDs["private_private"])
	response = httptest.NewRecorder()
	server.playerProfileDetail(response, request)
	if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), privateHash) {
		t.Fatalf("anonymous private profile response status=%d body=%s", response.Code, response.Body.String())
	}

	publicProfile, err := server.loadPlayerProfileByPublicIDForViewer(ctx, profilePublicIDs["public_public"], viewerID)
	if err != nil || publicProfile.Skin == nil || publicProfile.Skin.TextureHash != publicHash {
		t.Fatalf("approved public texture was hidden: skin=%v err=%v", publicProfile.Skin, err)
	}
	if _, err = pool.Exec(ctx, `update skin_assets set review_status='pending' where id=$1`, publicAssetID); err != nil {
		t.Fatal(err)
	}
	pendingProfile, err := server.loadPlayerProfileByPublicIDForViewer(ctx, profilePublicIDs["public_public"], viewerID)
	if err != nil || pendingProfile.Skin != nil {
		t.Fatalf("pending public texture visible to unrelated viewer: skin=%v err=%v", pendingProfile.Skin, err)
	}
	pendingOwnerProfile, err := server.loadPlayerProfileByPublicIDForViewer(ctx, profilePublicIDs["public_public"], ownerID)
	if err != nil || pendingOwnerProfile.Skin == nil || pendingOwnerProfile.Skin.TextureHash != publicHash {
		t.Fatalf("pending texture was hidden from its owner: skin=%v err=%v", pendingOwnerProfile.Skin, err)
	}
	if _, err = pool.Exec(ctx, `update skin_assets set review_status='approved' where id=$1`, publicAssetID); err != nil {
		t.Fatal(err)
	}
	privacyTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lockSkinAssetProfilesForPrivateVisibility(ctx, privacyTx, privateAssetID, ownerID); !errors.Is(err, errPrivateSkinProfileConflict) {
		_ = privacyTx.Rollback(ctx)
		t.Fatalf("private asset transition conflict=%v, want %v", err, errPrivateSkinProfileConflict)
	}
	if err = privacyTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	privacyTx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = applySkinAssetSnapshotTx(ctx, privacyTx, publicAssetID, ownerID, 1, ownerID, skinAssetContentSnapshot{
		Model: "default", Name: "SEC-034 public", Tags: []string{}, Visibility: "private",
	})
	if !errors.Is(err, errPrivateSkinProfileConflict) {
		_ = privacyTx.Rollback(ctx)
		t.Fatalf("making an equipped public asset private err=%v, want %v", err, errPrivateSkinProfileConflict)
	}
	if err = privacyTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var storedAssetVisibility string
	if err = pool.QueryRow(ctx, `select visibility from skin_assets where id=$1`, publicAssetID).Scan(&storedAssetVisibility); err != nil || storedAssetVisibility != "public" {
		t.Fatalf("rejected asset privacy transition persisted visibility=%q err=%v", storedAssetVisibility, err)
	}

	visibilityBody := bytes.NewBufferString(`{"visibility":"public"}`)
	request = httptest.NewRequest(http.MethodPut, "/api/v1/me/player-profiles/"+profilePublicIDs["private_private"], visibilityBody)
	request.SetPathValue("publicId", profilePublicIDs["private_private"])
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID, PublicSubject: ownerPublicID}))
	response = httptest.NewRecorder()
	server.updatePlayerProfile(response, request, profilePublicIDs["private_private"])
	if response.Code != http.StatusConflict {
		t.Fatalf("publishing profile with private texture status=%d body=%s", response.Code, response.Body.String())
	}
	var storedVisibility string
	if err = pool.QueryRow(ctx, `select visibility from player_profiles where id=$1`, profileIDs["private_private"]).Scan(&storedVisibility); err != nil || storedVisibility != "private" {
		t.Fatalf("rejected profile publication persisted visibility=%q err=%v", storedVisibility, err)
	}

	textureBody, err := json.Marshal(map[string]string{"skinPublicId": privateAssetPublicID})
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPut, "/api/v1/me/player-profiles/"+profilePublicIDs["empty_public"]+"/texture", bytes.NewReader(textureBody))
	request.SetPathValue("publicId", profilePublicIDs["empty_public"])
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: ownerID, PublicSubject: ownerPublicID}))
	response = httptest.NewRecorder()
	server.setPlayerProfileTexture(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("equipping private texture on public profile status=%d body=%s", response.Code, response.Body.String())
	}
	var equipped bool
	if err = pool.QueryRow(ctx, `select exists(select 1 from player_profile_textures where profile_id=$1)`, profileIDs["empty_public"]).Scan(&equipped); err != nil || equipped {
		t.Fatalf("rejected private texture equip persisted=%t err=%v", equipped, err)
	}
}
