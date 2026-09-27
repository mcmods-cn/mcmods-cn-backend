package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

func (s *Server) setPlayerProfileTexture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	publicID := normalizedSkinPublicID(r.PathValue("publicId"))
	if publicID == "" {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	var request playerTextureRequest
	if !decodeSkinJSON(w, r, &request) {
		return
	}
	updates, err := parsePlayerTextureUpdates(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update player texture")
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&userStatus); err != nil || userStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	for index := range updates {
		update := &updates[index]
		if update.AssetPublicID == "" {
			continue
		}
		var assetID, assetOwnerID int64
		var assetKind, assetModel, status, reviewStatus, visibility string
		err = tx.QueryRow(r.Context(), `select asset.id,asset.owner_id,asset.kind,asset.model,asset.status,asset.review_status,asset.visibility
			from skin_assets asset where asset.public_id=$1 for update`, update.AssetPublicID).
			Scan(&assetID, &assetOwnerID, &assetKind, &assetModel, &status, &reviewStatus, &visibility)
		if err != nil || status != "active" || reviewStatus != "approved" || assetKind != update.Kind ||
			(visibility == "private" && assetOwnerID != claims.Subject) {
			writeError(w, http.StatusBadRequest, "skin asset is unavailable")
			return
		}
		model := update.Model
		if model == "" {
			model = assetModel
		}
		if update.Kind == "cape" {
			model = "default"
		}
		update.AssetID = assetID
		update.Model = model
		update.Visibility = visibility
	}
	var profileID int64
	var profileVisibility string
	err = tx.QueryRow(r.Context(), `select id,visibility from player_profiles
		where public_id=$1 and user_id=$2 and status='active' for update`, publicID, claims.Subject).
		Scan(&profileID, &profileVisibility)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profile")
		return
	}
	if profileVisibility != "private" {
		for _, update := range updates {
			if update.AssetPublicID != "" && update.Visibility == "private" {
				writeError(w, http.StatusConflict, errPrivateSkinProfileConflict.Error())
				return
			}
		}
	}
	for _, update := range updates {
		if update.AssetPublicID == "" {
			if _, err = tx.Exec(r.Context(), `delete from player_profile_textures where profile_id=$1 and kind=$2`, profileID, update.Kind); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to clear player texture")
				return
			}
			continue
		}
		if _, err = addSkinToWardrobeTx(r.Context(), tx, claims.Subject, update.AssetID, 0); errors.Is(err, errSkinWardrobeLimit) {
			writeError(w, http.StatusForbidden, errSkinWardrobeLimit.Error())
			return
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update wardrobe")
			return
		}
		_, err = tx.Exec(r.Context(), `insert into player_profile_textures(profile_id,kind,asset_id,model,equipped_at)
			values($1,$2,$3,$4,now()) on conflict(profile_id,kind) do update
			set asset_id=excluded.asset_id,model=excluded.model,equipped_at=now()`, profileID, update.Kind, update.AssetID, update.Model)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to equip player texture")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `update player_profiles set updated_at=now() where id=$1`, profileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update player profile")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update player texture")
		return
	}
	profile, err := s.loadPlayerProfileByPublicIDForViewer(r.Context(), publicID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated player profile")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func parsePlayerTextureUpdates(request playerTextureRequest) ([]playerTextureUpdate, error) {
	updates := make([]playerTextureUpdate, 0, 2)
	for _, value := range []struct {
		kind string
		raw  json.RawMessage
	}{{kind: "skin", raw: request.SkinPublicID}, {kind: "cape", raw: request.CapePublicID}} {
		if len(value.raw) == 0 {
			continue
		}
		assetPublicID := ""
		if string(value.raw) != "null" {
			if err := json.Unmarshal(value.raw, &assetPublicID); err != nil {
				return nil, fmt.Errorf("%sPublicId must be a string or null", value.kind)
			}
			assetPublicID = strings.TrimSpace(assetPublicID)
			if assetPublicID != "" {
				assetPublicID = normalizedSkinPublicID(assetPublicID)
				if assetPublicID == "" {
					return nil, fmt.Errorf("%sPublicId is invalid", value.kind)
				}
			}
		}
		updates = append(updates, playerTextureUpdate{Kind: value.kind, AssetPublicID: assetPublicID})
	}
	if len(updates) == 0 {
		return nil, fmt.Errorf("skinPublicId or capePublicId is required")
	}
	return updates, nil
}

func (s *Server) publicUserPlayerProfiles(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	userID := identity.InternalID
	claims := currentClaims(r)
	includePrivate := claims.Subject == userID || isSkinAdmin(claims)
	profiles, err := s.loadPlayerProfiles(r.Context(), userID, claims, includePrivate)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profiles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": profiles})
}

func (s *Server) playerProfileDetail(w http.ResponseWriter, r *http.Request) {
	publicID := normalizedSkinPublicID(r.PathValue("publicId"))
	claims := currentClaims(r)
	profile, err := s.loadPlayerProfileByPublicIDForViewer(r.Context(), publicID, claims.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profile")
		return
	}
	if profile.Status != "active" {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if profile.Visibility == "private" && profile.Owner.ID != claims.PublicSubject && !isSkinAdmin(claims) {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) launcherCredential(w http.ResponseWriter, r *http.Request) {
	userID := currentClaims(r).Subject
	if userID <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !s.yggdrasilUserAllowed(r.Context(), userID) {
			writeError(w, http.StatusForbidden, "launcher login permission is required")
			return
		}
		var request launcherCredentialRequest
		if !decodeSkinJSON(w, r, &request) {
			return
		}
		if len(request.Password) < 12 || len(request.Password) > 256 {
			writeError(w, http.StatusBadRequest, "launcher password must be between 12 and 256 bytes")
			return
		}
		hash, acquired, err := hashYggdrasilPassword(r.Context(), request.Password)
		if !acquired {
			writeError(w, http.StatusTooManyRequests, "launcher password service is busy")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to hash launcher password")
			return
		}
		tx, err := s.db.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update launcher credential")
			return
		}
		defer tx.Rollback(r.Context())
		var status string
		if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, userID).Scan(&status); err != nil || status != "active" {
			writeError(w, http.StatusForbidden, "account is unavailable")
			return
		}
		accountUUID, err := ensureYggdrasilAccountTx(r.Context(), tx, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create launcher account identity")
			return
		}
		if _, err = tx.Exec(r.Context(), `delete from yggdrasil_join_sessions
			where token_id in (select id from yggdrasil_tokens where user_id=$1)`, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke existing launcher sessions")
			return
		}
		if _, err = tx.Exec(r.Context(), `update yggdrasil_tokens set status='revoked',revoked_at=now()
			where user_id=$1 and status<>'revoked'`, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke existing launcher sessions")
			return
		}
		if _, err = tx.Exec(r.Context(), `update yggdrasil_accounts set launcher_password_hash=$2,enabled=true,updated_at=now() where user_id=$1`, userID, hash); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update launcher credential")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update launcher credential")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "accountUuid": accountUUID})
	case http.MethodDelete:
		tx, err := s.db.Begin(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to disable launcher credential")
			return
		}
		defer tx.Rollback(r.Context())
		var status string
		if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, userID).Scan(&status); err != nil || status != "active" {
			writeError(w, http.StatusForbidden, "account is unavailable")
			return
		}
		var accountUserID int64
		err = tx.QueryRow(r.Context(), `select user_id from yggdrasil_accounts where user_id=$1 for update`, userID).Scan(&accountUserID)
		if errors.Is(err, pgx.ErrNoRows) {
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to disable launcher credential")
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock launcher credential")
			return
		}
		if _, err = tx.Exec(r.Context(), `update yggdrasil_accounts set launcher_password_hash='',enabled=false,updated_at=now() where user_id=$1`, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to disable launcher credential")
			return
		}
		if _, err = tx.Exec(r.Context(), `delete from yggdrasil_join_sessions where token_id in (select id from yggdrasil_tokens where user_id=$1)`, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke launcher sessions")
			return
		}
		if _, err = tx.Exec(r.Context(), `update yggdrasil_tokens set status='revoked',revoked_at=now() where user_id=$1 and status<>'revoked'`, userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to revoke launcher sessions")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to disable launcher credential")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) launcherSessions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	userID := currentClaims(r).Subject
	if userID <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	rows, err := s.db.Query(r.Context(), `select token.public_id,token.status,token.issued_at,token.expires_at,token.last_used_at,
		token.ip,token.user_agent,coalesce(profile.public_id,''),coalesce(profile.uuid::text,''),coalesce(profile.name,'')
		from yggdrasil_tokens token left join player_profiles profile on profile.id=token.player_profile_id
		where token.user_id=$1 and token.status in ('active','stale') and token.expires_at>now()
		order by token.issued_at desc,token.id desc limit 100`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load launcher sessions")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, status, ip, userAgent, profileID, profileUUID, profileName string
		var issuedAt, expiresAt time.Time
		var lastUsedAt *time.Time
		if err = rows.Scan(&id, &status, &issuedAt, &expiresAt, &lastUsedAt, &ip, &userAgent, &profileID, &profileUUID, &profileName); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read launcher sessions")
			return
		}
		items = append(items, map[string]any{"id": id, "status": status, "issuedAt": issuedAt, "createdAt": issuedAt,
			"expiresAt": expiresAt, "lastUsedAt": lastUsedAt, "lastSeenAt": lastUsedAt, "ip": ip, "userAgent": userAgent,
			"profile": map[string]any{"publicId": profileID, "uuid": profileUUID, "name": profileName}})
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read launcher sessions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) launcherSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessionID := strings.ToLower(strings.TrimSpace(r.PathValue("sessionId")))
	if !validCatalogPublicID(sessionID) {
		writeError(w, http.StatusBadRequest, "launcher session ID is invalid")
		return
	}
	userID := currentClaims(r).Subject
	if userID <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke launcher session")
		return
	}
	defer tx.Rollback(r.Context())
	tag, err := tx.Exec(r.Context(), `update yggdrasil_tokens set status='revoked',revoked_at=now()
		where public_id=$1 and user_id=$2 and status<>'revoked'`, sessionID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke launcher session")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from yggdrasil_join_sessions where token_id=
		(select id from yggdrasil_tokens where public_id=$1 and user_id=$2)`, sessionID, userID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke launcher session")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke launcher session")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"revoked": tag.RowsAffected() > 0})
}

const skinAssetCoreSelectColumnsSQL = `asset.id,asset.public_id,asset.owner_id,owner.public_id,owner.username,
	asset.blob_hash,asset.kind,asset.model,asset.display_name,asset.description,asset.tags,asset.visibility,
	asset.review_status,asset.status,asset.downloads,blob.width,blob.height,blob.size_bytes,asset.created_at,asset.updated_at,
	`

const skinAssetSelectColumnsSQL = skinAssetCoreSelectColumnsSQL +
	`exists(select 1 from skin_wardrobe viewer_wardrobe where viewer_wardrobe.user_id=$1 and viewer_wardrobe.asset_id=asset.id)`

const skinAssetFromSQL = ` from skin_assets asset join users owner on owner.id=asset.owner_id
	join skin_texture_blobs blob on blob.hash=asset.blob_hash `

const skinAssetSelectSQL = `select ` + skinAssetSelectColumnsSQL + skinAssetFromSQL

const skinWardrobePageSelectSQL = `select wardrobe.added_at,` + skinAssetCoreSelectColumnsSQL + `true` + skinAssetFromSQL

func scanSkinAsset(row skinRowScanner) (skinAssetRecord, error) {
	var record skinAssetRecord
	err := row.Scan(skinAssetScanDestinations(&record)...)
	return record, err
}

func skinAssetScanDestinations(record *skinAssetRecord) []any {
	return []any{&record.ID, &record.PublicID, &record.OwnerID, &record.OwnerPublicID, &record.OwnerName,
		&record.BlobHash, &record.Kind, &record.Model, &record.Name, &record.Description, &record.Tags,
		&record.Visibility, &record.ReviewStatus, &record.Status, &record.Downloads, &record.Width, &record.Height,
		&record.SizeBytes, &record.CreatedAt, &record.UpdatedAt, &record.InWardrobe}
}

func (s *Server) skinAssetByPublicID(ctx context.Context, publicID string, viewerID int64) (skinAssetRecord, error) {
	return scanSkinAsset(s.db.QueryRow(ctx, skinAssetSelectSQL+` where asset.public_id=$2`, viewerID, publicID))
}

func lockProfilesUsingAsset(ctx context.Context, tx pgx.Tx, assetID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, `select profile.id from player_profiles profile
		join player_profile_textures texture on texture.profile_id=profile.id
		where texture.asset_id=$1 order by profile.id for update of profile`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profileIDs := make([]int64, 0)
	for rows.Next() {
		var profileID int64
		if err = rows.Scan(&profileID); err != nil {
			return nil, err
		}
		profileIDs = append(profileIDs, profileID)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return profileIDs, nil
}

func addSkinToWardrobeTx(ctx context.Context, tx pgx.Tx, userID, assetID, knownOwnerID int64) (bool, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `select exists(select 1 from skin_wardrobe where user_id=$1 and asset_id=$2)`, userID, assetID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	var count int
	if err := tx.QueryRow(ctx, `select count(*) from skin_wardrobe where user_id=$1`, userID).Scan(&count); err != nil {
		return false, err
	}
	if count >= maximumSkinWardrobeItems {
		return false, errSkinWardrobeLimit
	}
	command, err := tx.Exec(ctx, `insert into skin_wardrobe(user_id,asset_id) values($1,$2) on conflict do nothing`, userID, assetID)
	if err != nil {
		return false, err
	}
	added := command.RowsAffected() == 1
	if !added {
		return false, nil
	}
	ownerID := knownOwnerID
	if ownerID <= 0 {
		if err = tx.QueryRow(ctx, `select owner_id from skin_assets where id=$1`, assetID).Scan(&ownerID); err != nil {
			return false, err
		}
	}
	if ownerID != userID {
		adoption, adoptionErr := tx.Exec(ctx, `insert into skin_asset_adoptions(user_id,asset_id) values($1,$2) on conflict do nothing`, userID, assetID)
		if adoptionErr != nil {
			return false, adoptionErr
		}
		if adoption.RowsAffected() == 1 {
			if _, err = tx.Exec(ctx, `update skin_assets set downloads=downloads+1 where id=$1 and status='active'`, assetID); err != nil {
				return false, err
			}
		}
	}
	return true, nil
}

func skinAssetJSON(record skinAssetRecord, claims security.Claims) map[string]any {
	isOwner := claims.Subject > 0 && claims.Subject == record.OwnerID
	canEdit := isOwner || isSkinAdmin(claims)
	canUse := record.Status == "active" && (isOwner || (record.ReviewStatus == "approved" && record.Visibility != "private"))
	return map[string]any{
		"publicId":    record.PublicID,
		"owner":       map[string]any{"id": record.OwnerPublicID, "username": record.OwnerName},
		"textureHash": record.BlobHash, "kind": record.Kind, "model": record.Model,
		"name": record.Name, "description": record.Description, "tags": record.Tags,
		"visibility": record.Visibility, "reviewStatus": record.ReviewStatus, "status": record.Status,
		"downloads": record.Downloads, "width": record.Width, "height": record.Height, "sizeBytes": record.SizeBytes,
		"textureUrl": "/api/yggdrasil/textures/" + record.BlobHash,
		"inWardrobe": record.InWardrobe, "canEdit": canEdit, "canUse": canUse,
		"createdAt": record.CreatedAt, "updatedAt": record.UpdatedAt,
	}
}

func canReadSkinAsset(record skinAssetRecord, claims security.Claims) bool {
	if record.Status != "active" {
		return false
	}
	if claims.Subject == record.OwnerID || isSkinAdmin(claims) {
		return true
	}
	return record.ReviewStatus == "approved" && record.Visibility != "private"
}

func isSkinAdmin(claims security.Claims) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "skin.admin")
}

func normalizeSkinAssetCreate(request *skinAssetCreateRequest) error {
	request.FileID = strings.ToLower(strings.TrimSpace(request.FileID))
	if !validCatalogPublicID(request.FileID) {
		return fmt.Errorf("fileId is required")
	}
	kind, err := normalizeMinecraftTextureKind(request.Kind)
	if err != nil {
		return err
	}
	request.Kind = kind
	request.Model = strings.ToLower(strings.TrimSpace(request.Model))
	if request.Model == "" || request.Kind == "cape" {
		request.Model = "default"
	}
	if request.Model != "default" && request.Model != "slim" {
		return fmt.Errorf("model must be default or slim")
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		if request.Kind == "cape" {
			request.Name = "Cape"
		} else {
			request.Name = "Skin"
		}
	}
	if len([]rune(request.Name)) > 80 {
		return fmt.Errorf("name is too long")
	}
	request.Description = strings.TrimSpace(request.Description)
	if len([]rune(request.Description)) > 1000 {
		return fmt.Errorf("description is too long")
	}
	request.DefaultLocale, request.Localizations, err = normalizeCatalogLocalizations(request.DefaultLocale, request.Localizations)
	if err != nil {
		return fmt.Errorf("localized skin content is invalid")
	}
	if len(request.Localizations) == 0 {
		request.Localizations = []catalogLocalizationEdit{{Locale: request.DefaultLocale, Name: request.Name, Summary: request.Description}}
		request.DefaultLocale, request.Localizations, err = normalizeCatalogLocalizations(request.DefaultLocale, request.Localizations)
	}
	if err != nil || requireCatalogCreateDefaultLocalization(request.DefaultLocale, request.Localizations) != nil {
		return fmt.Errorf("the default language must have a localized skin name")
	}
	for _, localization := range request.Localizations {
		if localization.Locale == request.DefaultLocale {
			request.Name = localization.Name
			request.Description = localization.Summary
			break
		}
	}
	request.Tags, err = normalizeSkinTags(request.Tags)
	if err != nil {
		return err
	}
	request.Visibility, err = normalizeSkinVisibility(request.Visibility, "private")
	return err
}

func applySkinAssetUpdate(record *skinAssetRecord, request skinAssetUpdateRequest) error {
	if request.Model != nil {
		model := strings.ToLower(strings.TrimSpace(*request.Model))
		if record.Kind == "cape" {
			model = "default"
		}
		if model != "default" && model != "slim" {
			return fmt.Errorf("model must be default or slim")
		}
		record.Model = model
	}
	if request.Name != nil {
		value := strings.TrimSpace(*request.Name)
		if value == "" || len([]rune(value)) > 80 {
			return fmt.Errorf("name must contain between 1 and 80 characters")
		}
		record.Name = value
	}
	if request.Description != nil {
		value := strings.TrimSpace(*request.Description)
		if len([]rune(value)) > 1000 {
			return fmt.Errorf("description is too long")
		}
		record.Description = value
	}
	if request.Tags != nil {
		value, err := normalizeSkinTags(*request.Tags)
		if err != nil {
			return err
		}
		record.Tags = value
	}
	if request.Visibility != nil {
		value, err := normalizeSkinVisibility(*request.Visibility, record.Visibility)
		if err != nil {
			return err
		}
		record.Visibility = value
	}
	return nil
}

func normalizeSkinAssetUpdateRequest(request *skinAssetUpdateRequest) (bool, error) {
	if request.Localizations == nil {
		return false, nil
	}
	defaultLocale, localizations, err := normalizeCatalogLocalizations(request.DefaultLocale, request.Localizations)
	if err != nil || requireCatalogCreateDefaultLocalization(defaultLocale, localizations) != nil {
		return false, fmt.Errorf("the default language must have a localized skin name")
	}
	request.DefaultLocale, request.Localizations = defaultLocale, localizations
	for _, localization := range localizations {
		if localization.Locale == defaultLocale {
			name, description := localization.Name, localization.Summary
			request.Name, request.Description = &name, &description
			break
		}
	}
	return true, nil
}

func normalizeSkinTags(tags []string) ([]string, error) {
	if len(tags) > 16 {
		return nil, fmt.Errorf("at most 16 tags are allowed")
	}
	result := make([]string, 0, len(tags))
	seen := map[string]struct{}{}
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		if len([]rune(tag)) > 32 {
			return nil, fmt.Errorf("tag is too long")
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		result = append(result, tag)
	}
	return result, nil
}

func normalizeSkinVisibility(value, fallback string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = fallback
	}
	switch value {
	case "public", "unlisted", "private":
		return value, nil
	default:
		return "", fmt.Errorf("visibility must be public, unlisted or private")
	}
}

func normalizePlayerProfileCreate(request *playerProfileCreateRequest) error {
	request.Name = strings.TrimSpace(request.Name)
	if !minecraftProfileNamePattern.MatchString(request.Name) {
		return fmt.Errorf("player name must contain 3-16 ASCII letters, digits or underscores")
	}
	request.Bio = strings.TrimSpace(request.Bio)
	if len([]rune(request.Bio)) > 500 {
		return fmt.Errorf("bio is too long")
	}
	visibility, err := normalizeSkinVisibility(request.Visibility, "public")
	request.Visibility = visibility
	return err
}

func normalizePlayerProfileUpdate(request *playerProfileUpdateRequest) error {
	if request.Name != nil {
		value := strings.TrimSpace(*request.Name)
		if !minecraftProfileNamePattern.MatchString(value) {
			return fmt.Errorf("player name must contain 3-16 ASCII letters, digits or underscores")
		}
		request.Name = &value
	}
	if request.Bio != nil {
		value := strings.TrimSpace(*request.Bio)
		if len([]rune(value)) > 500 {
			return fmt.Errorf("bio is too long")
		}
		request.Bio = &value
	}
	if request.Visibility != nil {
		value, err := normalizeSkinVisibility(*request.Visibility, "public")
		if err != nil {
			return err
		}
		request.Visibility = &value
	}
	return nil
}

func playerProfileLimit(permissions []security.PermissionRule) int {
	value := permissionRulesNumericValue(permissions, "skin.profile.limit")
	if value <= 0 {
		return defaultPlayerProfileLimit
	}
	if value > maximumPlayerProfileLimit {
		return maximumPlayerProfileLimit
	}
	return int(value)
}

func skinAssetLimit(permissions []security.PermissionRule) int {
	value := permissionRulesNumericValue(permissions, "skin.library.limit")
	if value <= 0 {
		return defaultSkinAssetLimit
	}
	if value > maximumSkinAssetLimit {
		return maximumSkinAssetLimit
	}
	return int(value)
}

func (s *Server) userSkinAssetLimit(ctx context.Context, userID int64) (int, error) {
	_, permissions, err := s.resolveUserRootPermissions(ctx, userID)
	if err != nil {
		return 0, err
	}
	return skinAssetLimit(permissions), nil
}

func normalizedSkinPublicID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 9 {
		return ""
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return ""
		}
	}
	return value
}

func decodeSkinJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxSkinRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON value")
		return false
	}
	return true
}

func ensureYggdrasilAccountTx(ctx context.Context, tx pgx.Tx, userID int64) (string, error) {
	var accountUUID string
	err := tx.QueryRow(ctx, `select account_uuid::text from yggdrasil_accounts where user_id=$1 for update`, userID).Scan(&accountUUID)
	if err == nil {
		return accountUUID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	accountUUID, err = newMinecraftUUID()
	if err != nil {
		return "", err
	}
	err = tx.QueryRow(ctx, `insert into yggdrasil_accounts(user_id,account_uuid) values($1,$2::uuid)
		on conflict(user_id) do update set updated_at=now() returning account_uuid::text`, userID, accountUUID).Scan(&accountUUID)
	return accountUUID, err
}

func (s *Server) loadPlayerProfiles(ctx context.Context, userID int64, claims security.Claims, includePrivate bool) ([]playerProfileResponse, error) {
	rows, err := s.db.Query(ctx, `select profile.public_id,profile.uuid::text,profile.name,profile.bio,
		profile.visibility,profile.is_default,profile.status,profile.created_at,profile.updated_at,
		owner.public_id,owner.username
		from player_profiles profile join users owner on owner.id=profile.user_id
		where profile.user_id=$1 and profile.status='active' and ($2 or profile.visibility='public')
		order by profile.is_default desc,profile.created_at,profile.id`, userID, includePrivate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]playerProfileResponse, 0)
	for rows.Next() {
		var profile playerProfileResponse
		if err = rows.Scan(&profile.PublicID, &profile.UUID, &profile.Name, &profile.Bio,
			&profile.Visibility, &profile.IsDefault, &profile.Status, &profile.CreatedAt, &profile.UpdatedAt,
			&profile.Owner.ID, &profile.Owner.Username); err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	profilePointers := make([]*playerProfileResponse, 0, len(profiles))
	for index := range profiles {
		profilePointers = append(profilePointers, &profiles[index])
	}
	if err = s.loadPlayerTexturesForProfiles(ctx, profilePointers, claims.Subject); err != nil {
		return nil, err
	}
	return profiles, nil
}

func (s *Server) loadPlayerProfileByPublicIDForViewer(ctx context.Context, publicID string, viewerID int64) (playerProfileResponse, error) {
	var profile playerProfileResponse
	err := s.db.QueryRow(ctx, `select profile.public_id,profile.uuid::text,profile.name,profile.bio,
		profile.visibility,profile.is_default,profile.status,profile.created_at,profile.updated_at,
		owner.public_id,owner.username
		from player_profiles profile join users owner on owner.id=profile.user_id where profile.public_id=$1`, publicID).
		Scan(&profile.PublicID, &profile.UUID, &profile.Name, &profile.Bio, &profile.Visibility,
			&profile.IsDefault, &profile.Status, &profile.CreatedAt, &profile.UpdatedAt,
			&profile.Owner.ID, &profile.Owner.Username)
	if err != nil {
		return profile, err
	}
	return profile, s.loadPlayerTextures(ctx, &profile, viewerID)
}

func (s *Server) loadPlayerTextures(ctx context.Context, profile *playerProfileResponse, viewerID int64) error {
	return s.loadPlayerTexturesForProfiles(ctx, []*playerProfileResponse{profile}, viewerID)
}

func (s *Server) loadPlayerTexturesForProfiles(ctx context.Context, profiles []*playerProfileResponse, viewerID int64) error {
	profileIDs := make([]string, 0, len(profiles))
	profilesByID := make(map[string]*playerProfileResponse, len(profiles))
	for _, profile := range profiles {
		if profile == nil {
			continue
		}
		profile.Skin = nil
		profile.Cape = nil
		if _, exists := profilesByID[profile.PublicID]; exists {
			continue
		}
		profileIDs = append(profileIDs, profile.PublicID)
		profilesByID[profile.PublicID] = profile
	}
	if len(profileIDs) == 0 {
		return nil
	}
	rows, err := s.db.Query(ctx, `select profile.public_id,texture.kind,asset.model,asset.public_id,asset.display_name,
		asset.description,asset.tags,asset.visibility,asset.review_status,asset.status,asset.downloads,asset.blob_hash,
		asset.created_at,asset.updated_at,owner.id,owner.public_id,owner.username,
		exists(select 1 from skin_wardrobe wardrobe where wardrobe.user_id=$2 and wardrobe.asset_id=asset.id)
		from player_profile_textures texture join player_profiles profile on profile.id=texture.profile_id
		join skin_assets asset on asset.id=texture.asset_id
		join users owner on owner.id=asset.owner_id
		where profile.public_id=any($1::text[]) and asset.status='active'
		and (asset.owner_id=$2 or (asset.review_status='approved' and asset.visibility<>'private'))
		order by profile.public_id,texture.kind`, profileIDs, viewerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var profileID string
		var texture playerTextureResponse
		var assetStatus string
		if err = rows.Scan(&profileID, &texture.Kind, &texture.Model, &texture.PublicID, &texture.Name, &texture.Description,
			&texture.Tags, &texture.Visibility, &texture.ReviewStatus, &assetStatus, &texture.Downloads,
			&texture.TextureHash, &texture.CreatedAt, &texture.UpdatedAt, &texture.OwnerInternalID, &texture.Owner.ID,
			&texture.Owner.Username, &texture.InWardrobe); err != nil {
			return err
		}
		profile := profilesByID[profileID]
		if profile == nil {
			continue
		}
		texture.TextureURL = "/api/yggdrasil/textures/" + texture.TextureHash
		texture.CanEdit = viewerID > 0 && viewerID == texture.OwnerInternalID
		texture.CanUse = assetStatus == "active" && (texture.CanEdit ||
			(texture.ReviewStatus == "approved" && texture.Visibility != "private"))
		textureCopy := texture
		if texture.Kind == "skin" {
			profile.Skin = &textureCopy
		} else if texture.Kind == "cape" {
			profile.Cape = &textureCopy
		}
	}
	return rows.Err()
}
