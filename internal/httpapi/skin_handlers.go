package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

const (
	maxSkinRequestBodyBytes       int64 = 64 << 10
	defaultPlayerProfileLimit           = 5
	maximumPlayerProfileLimit           = 100
	defaultSkinAssetLimit               = 500
	maximumSkinAssetLimit               = 5000
	maximumSkinAssetCreatesPerDay       = 100
	maximumSkinWardrobeItems            = 5000
)

var minecraftProfileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
var errSkinWardrobeLimit = errors.New("skin wardrobe item limit reached")

type skinAssetRecord struct {
	ID            int64
	PublicID      string
	OwnerID       int64
	OwnerPublicID string
	OwnerName     string
	BlobHash      string
	Kind          string
	Model         string
	DisplayName   string
	Description   string
	Tags          []string
	Visibility    string
	ReviewStatus  string
	Status        string
	Downloads     int64
	Width         int
	Height        int
	SizeBytes     int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	InWardrobe    bool
}

type skinAssetCreateRequest struct {
	FileID        string                    `json:"fileId"`
	Kind          string                    `json:"kind"`
	Model         string                    `json:"model"`
	Name          string                    `json:"name"`
	DisplayName   string                    `json:"displayName"`
	Description   string                    `json:"description"`
	Tags          []string                  `json:"tags"`
	Visibility    string                    `json:"visibility"`
	DefaultLocale string                    `json:"defaultLocale"`
	Localizations []catalogLocalizationEdit `json:"localizations"`
}

type skinAssetUpdateRequest struct {
	Model       *string   `json:"model,omitempty"`
	Name        *string   `json:"name,omitempty"`
	DisplayName *string   `json:"displayName,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
	Visibility  *string   `json:"visibility,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

type skinAssetContentSnapshot struct {
	PublicID    string   `json:"publicId"`
	Model       string   `json:"model"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Visibility  string   `json:"visibility"`
}

type skinOwnerResponse struct {
	ID       string `json:"id"`
	PublicID string `json:"publicId"`
	Username string `json:"username"`
}

type playerProfileResponse struct {
	PublicID   string                           `json:"publicId"`
	UserID     string                           `json:"userId"`
	UUID       string                           `json:"uuid"`
	Name       string                           `json:"name"`
	Bio        string                           `json:"bio"`
	Visibility string                           `json:"visibility"`
	IsDefault  bool                             `json:"isDefault"`
	Status     string                           `json:"status"`
	Textures   map[string]playerTextureResponse `json:"textures"`
	Skin       *playerTextureResponse           `json:"skin"`
	Cape       *playerTextureResponse           `json:"cape"`
	Owner      skinOwnerResponse                `json:"owner"`
	CreatedAt  time.Time                        `json:"createdAt"`
	UpdatedAt  time.Time                        `json:"updatedAt"`
}

type playerTextureResponse struct {
	OwnerInternalID int64             `json:"-"`
	PublicID        string            `json:"publicId"`
	AssetPublicID   string            `json:"assetPublicId"`
	Kind            string            `json:"kind"`
	Model           string            `json:"model"`
	Name            string            `json:"name"`
	DisplayName     string            `json:"displayName"`
	Description     string            `json:"description"`
	Tags            []string          `json:"tags"`
	Visibility      string            `json:"visibility"`
	ReviewStatus    string            `json:"reviewStatus"`
	TextureHash     string            `json:"textureHash"`
	Hash            string            `json:"hash"`
	TextureURL      string            `json:"textureUrl"`
	URL             string            `json:"url"`
	Owner           skinOwnerResponse `json:"owner"`
	Downloads       int64             `json:"downloads"`
	CanEdit         bool              `json:"canEdit"`
	CanUse          bool              `json:"canUse"`
	InWardrobe      bool              `json:"inWardrobe"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type playerProfileCreateRequest struct {
	Name       string `json:"name"`
	Bio        string `json:"bio"`
	Visibility string `json:"visibility"`
	IsDefault  bool   `json:"isDefault"`
}

type playerProfileUpdateRequest struct {
	Name       *string `json:"name,omitempty"`
	Bio        *string `json:"bio,omitempty"`
	Visibility *string `json:"visibility,omitempty"`
	IsDefault  *bool   `json:"isDefault,omitempty"`
}

type playerTextureRequest struct {
	Kind          string          `json:"kind"`
	AssetPublicID string          `json:"assetPublicId"`
	Model         string          `json:"model"`
	SkinPublicID  json.RawMessage `json:"skinPublicId"`
	CapePublicID  json.RawMessage `json:"capePublicId"`
}

type playerTextureUpdate struct {
	Kind          string
	AssetPublicID string
	AssetID       int64
	Model         string
}

type launcherCredentialRequest struct {
	Password string `json:"password"`
}

type skinRowScanner interface {
	Scan(dest ...any) error
}

func (s *Server) skinService(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	w.Header().Set("Cache-Control", "private, no-store")
	serviceEnabled := s.ygg != nil && s.ygg.privateKey != nil && s.ygg.disabledReason == nil
	serverName := strings.TrimSpace(s.cfg.Yggdrasil.ServerName)
	if serverName == "" {
		serverName = "Mcmods-cn"
	}
	apiRoot := strings.TrimSpace(s.cfg.Yggdrasil.PublicBaseURL)
	if apiRoot != "" {
		apiRoot = strings.TrimRight(apiRoot, "/") + "/"
	}
	textureBaseURL := strings.TrimSpace(s.cfg.Yggdrasil.TextureBaseURL)
	if textureBaseURL != "" {
		textureBaseURL = strings.TrimRight(textureBaseURL, "/") + "/"
	}
	if !serviceEnabled {
		apiRoot = ""
		textureBaseURL = ""
	}
	launcherEnabled := false
	accountUUID := ""
	profileCount := int64(0)
	assetCount := int64(0)
	assetLimit := skinAssetLimit(claims.PermissionRules)
	if claims.Subject > 0 {
		err := s.db.QueryRow(r.Context(), `select enabled,account_uuid::text from yggdrasil_accounts where user_id=$1`, claims.Subject).
			Scan(&launcherEnabled, &accountUUID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load launcher service status")
			return
		}
		if err = s.db.QueryRow(r.Context(), `select count(*) from player_profiles where user_id=$1 and status='active'`, claims.Subject).
			Scan(&profileCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load player profile usage")
			return
		}
		if err = s.db.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and status='active'`, claims.Subject).
			Scan(&assetCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load skin library usage")
			return
		}
		assetLimit, err = s.userSkinAssetLimit(r.Context(), claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load skin library limit")
			return
		}
		launcherEnabled = launcherEnabled && s.yggdrasilUserAllowed(r.Context(), claims.Subject)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":              serviceEnabled,
		"name":                 serverName,
		"serverName":           serverName,
		"yggdrasilApiRoot":     apiRoot,
		"textureBaseUrl":       textureBaseURL,
		"textureUploadEnabled": true,
		"registrationEnabled":  true,
		"textureKinds":         []string{"skin", "cape"},
		"skinModels":           []string{"default", "slim"},
		"maxUploadBytes":       maxMinecraftTextureUploadBytes,
		"maxPixels":            maxMinecraftTexturePixels,
		"profileNamePattern":   minecraftProfileNamePattern.String(),
		"profileLimit":         playerProfileLimit(claims.PermissionRules),
		"profileCount":         profileCount,
		"assetLimit":           assetLimit,
		"assetCount":           assetCount,
		"launcher":             map[string]any{"enabled": launcherEnabled, "accountUuid": accountUUID},
	})
}

func (s *Server) skins(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPublicSkins(w, r)
	case http.MethodPost:
		s.createSkin(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listPublicSkins(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	kind := strings.ToLower(strings.TrimSpace(query.Get("kind")))
	if kind != "" {
		if _, err := normalizeMinecraftTextureKind(kind); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	model := strings.ToLower(strings.TrimSpace(query.Get("model")))
	if model != "" && model != "default" && model != "slim" {
		writeError(w, http.StatusBadRequest, "model must be default or slim")
		return
	}
	search := strings.TrimSpace(query.Get("q"))
	if len([]rune(search)) > 80 {
		writeError(w, http.StatusBadRequest, "search query is too long")
		return
	}
	limit := boundedLimit(query.Get("limit"), 24, 100)
	offset := boundedOffset(query.Get("offset"))
	var orderBy string
	switch strings.ToLower(strings.TrimSpace(query.Get("sort"))) {
	case "", "latest":
		orderBy = "asset.created_at desc,asset.id desc"
	case "downloads":
		orderBy = "asset.downloads desc,asset.created_at desc,asset.id desc"
	case "name":
		orderBy = "lower(asset.display_name),asset.id"
	default:
		writeError(w, http.StatusBadRequest, "sort must be latest, downloads or name")
		return
	}
	viewerID := currentClaims(r).Subject
	rows, err := s.db.Query(r.Context(), skinAssetSelectSQL+`
		where asset.status='active' and asset.review_status='approved' and asset.visibility='public'
		and ($2='' or asset.kind=$2) and ($3='' or asset.model=$3)
		and ($4='' or asset.display_name ilike '%'||$4||'%' or asset.description ilike '%'||$4||'%'
			or array_to_string(asset.tags,' ') ilike '%'||$4||'%')
		order by `+orderBy+` limit $5 offset $6`, viewerID, kind, model, search, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skin library")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		record, scanErr := scanSkinAsset(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read skin library")
			return
		}
		items = append(items, skinAssetJSON(record, viewerID))
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read skin library")
		return
	}
	var total int64
	if err = s.db.QueryRow(r.Context(), `select count(*) from skin_assets asset
		where asset.status='active' and asset.review_status='approved' and asset.visibility='public'
		and ($1='' or asset.kind=$1) and ($2='' or asset.model=$2)
		and ($3='' or asset.display_name ilike '%'||$3||'%' or asset.description ilike '%'||$3||'%'
			or array_to_string(asset.tags,' ') ilike '%'||$3||'%')`, kind, model, search).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count skin library")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) createSkin(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var request skinAssetCreateRequest
	if !decodeSkinJSON(w, r, &request) {
		return
	}
	if err := normalizeSkinAssetCreate(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	releaseTextureWorker, acquired := acquireMinecraftTextureProcessing(r.Context())
	if !acquired {
		writeError(w, http.StatusTooManyRequests, "texture processing service is busy")
		return
	}
	defer releaseTextureWorker()
	assetLimit, err := s.userSkinAssetLimit(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect skin library limit")
		return
	}
	var assetCount int
	if err = s.db.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and status='active'`, claims.Subject).Scan(&assetCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect skin library limit")
		return
	}
	if assetCount >= assetLimit {
		writeError(w, http.StatusForbidden, "skin library asset limit reached")
		return
	}
	var recentCreates int
	if err = s.db.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and created_at>=now()-interval '24 hours'`, claims.Subject).Scan(&recentCreates); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect skin library creation rate")
		return
	}
	if recentCreates >= maximumSkinAssetCreatesPerDay {
		writeError(w, http.StatusTooManyRequests, "skin library daily creation limit reached")
		return
	}
	file, err := resolveTrustedRasterOSSFilePublicID(r.Context(), s.db, request.FileID, ossRasterBindingScope{UploaderID: claims.Subject})
	if err != nil {
		writeError(w, http.StatusBadRequest, "texture file is invalid or unavailable")
		return
	}
	texture, storageClient, storageConfig, err := s.loadMinecraftTextureUpload(r.Context(), file, request.Kind)
	if err != nil {
		log.Printf("import web skin texture for user %d: %v", claims.Subject, err)
		writeError(w, http.StatusBadRequest, "texture file is invalid or unavailable")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create skin asset")
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&userStatus); err != nil || userStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	if err = tx.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and status='active'`, claims.Subject).Scan(&assetCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect skin library limit")
		return
	}
	if assetCount >= assetLimit {
		writeError(w, http.StatusForbidden, "skin library asset limit reached")
		return
	}
	if err = tx.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and created_at>=now()-interval '24 hours'`, claims.Subject).Scan(&recentCreates); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect skin library creation rate")
		return
	}
	if recentCreates >= maximumSkinAssetCreatesPerDay {
		writeError(w, http.StatusTooManyRequests, "skin library daily creation limit reached")
		return
	}
	blob, err := persistSanitizedMinecraftTextureTx(r.Context(), tx, storageClient, storageConfig, claims.Subject, texture)
	if err != nil {
		log.Printf("persist web skin texture for user %d: %v", claims.Subject, err)
		writeError(w, http.StatusServiceUnavailable, "texture storage is unavailable")
		return
	}
	var assetID int64
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into skin_assets(
		owner_id,blob_hash,kind,model,display_name,description,tags,visibility,review_status,status)
		values($1,$2,$3,$4,$5,$6,$7,$8,'approved','active') returning id,public_id`, claims.Subject,
		blob.Hash, request.Kind, request.Model, request.DisplayName, request.Description, request.Tags, request.Visibility).
		Scan(&assetID, &publicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record skin asset")
		return
	}
	if _, err = tx.Exec(r.Context(), `update content_subjects set default_locale=$2,updated_at=now()
		where subject_id=$1 and subject_type='skin'`, assetID, request.DefaultLocale); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set skin content language")
		return
	}
	for _, localization := range request.Localizations {
		if _, err = tx.Exec(r.Context(), `insert into content_localizations(
			subject_type,subject_id,locale,name,summary,content_markdown,provenance,editable,review_status,updated_by)
			values('skin',$1,$2,$3,$4,$5,'human',true,'approved',$6)`, assetID, localization.Locale,
			localization.Name, localization.Summary, localization.ContentMarkdown, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record localized skin content")
			return
		}
	}
	if _, err = addSkinToWardrobeTx(r.Context(), tx, claims.Subject, assetID, claims.Subject); errors.Is(err, errSkinWardrobeLimit) {
		writeError(w, http.StatusForbidden, errSkinWardrobeLimit.Error())
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add skin asset to wardrobe")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create skin asset")
		return
	}
	record, err := s.skinAssetByPublicID(r.Context(), publicID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load created skin asset")
		return
	}
	writeJSON(w, http.StatusCreated, skinAssetJSON(record, claims.Subject))
}

func (s *Server) skinDetail(w http.ResponseWriter, r *http.Request) {
	publicID := normalizedSkinPublicID(r.PathValue("publicId"))
	if publicID == "" {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.getSkinDetail(w, r, publicID)
	case http.MethodPut:
		s.updateSkinDetail(w, r, publicID)
	case http.MethodDelete:
		s.deleteSkin(w, r, publicID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) getSkinDetail(w http.ResponseWriter, r *http.Request, publicID string) {
	claims := currentClaims(r)
	record, err := s.skinAssetByPublicID(r.Context(), publicID, claims.Subject)
	if err != nil || !canReadSkinAsset(record, claims) {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	writeJSON(w, http.StatusOK, skinAssetJSON(record, claims.Subject))
}

func (s *Server) updateSkinDetail(w http.ResponseWriter, r *http.Request, publicID string) {
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var request skinAssetUpdateRequest
	if !decodeSkinJSON(w, r, &request) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update skin asset")
		return
	}
	defer tx.Rollback(r.Context())
	var actorStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&actorStatus); err != nil || actorStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	var record skinAssetRecord
	var baseRevisionID *int64
	err = tx.QueryRow(r.Context(), `select id,public_id,owner_id,blob_hash,kind,model,display_name,description,tags,
		visibility,review_status,status,downloads,created_at,updated_at,published_revision_id
		from skin_assets where public_id=$1 for update`, publicID).
		Scan(&record.ID, &record.PublicID, &record.OwnerID, &record.BlobHash, &record.Kind, &record.Model,
			&record.DisplayName, &record.Description, &record.Tags, &record.Visibility, &record.ReviewStatus,
			&record.Status, &record.Downloads, &record.CreatedAt, &record.UpdatedAt, &baseRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skin asset")
		return
	}
	if record.Status != "active" {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	if claims.Subject != record.OwnerID && !isSkinAdmin(claims) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	if err = applySkinAssetUpdate(&record, request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshot := skinAssetContentSnapshot{
		PublicID: publicID, Model: record.Model, DisplayName: record.DisplayName,
		Description: record.Description, Tags: record.Tags, Visibility: record.Visibility,
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode skin revision")
		return
	}
	reviewRequired := loadReviewConfig(r.Context(), s.db).CatalogEdit && !catalogMutationBypassesReview(claims)
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "Skin information update"
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: "skin", EntityID: record.ID,
		AggregateType: "skin", AggregateKey: publicID, BaseRevision: baseRevisionID, Snapshot: snapshotRaw,
		Reason: reason, ActorID: claims.Subject, Status: reviewStatus, Source: "skin_metadata",
		Metadata: map[string]any{"skinId": publicID, "name": record.DisplayName, "operation": "edit"}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create skin revision")
		return
	}
	if !reviewRequired {
		if err = applySkinAssetSnapshotTx(r.Context(), tx, record.ID, record.OwnerID, created.RevisionID, snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish skin revision")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update skin asset")
		return
	}
	if reviewRequired {
		writeJSON(w, http.StatusOK, map[string]any{
			"updated": false, "reviewRequired": true, "revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID,
		})
		return
	}
	record, err = s.skinAssetByPublicID(r.Context(), publicID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated skin asset")
		return
	}
	writeJSON(w, http.StatusOK, skinAssetJSON(record, claims.Subject))
}

func applySkinAssetSnapshotTx(ctx context.Context, tx pgx.Tx, assetID, ownerID, revisionID int64, snapshot skinAssetContentSnapshot) error {
	var oldModel string
	if err := tx.QueryRow(ctx, `select model from skin_assets where id=$1 and status='active' for update`, assetID).Scan(&oldModel); err != nil {
		return err
	}
	profileIDs, err := lockProfilesUsingAsset(ctx, tx, assetID)
	if err != nil {
		return err
	}
	if oldModel != snapshot.Model {
		if _, err = tx.Exec(ctx, `update player_profile_textures set model=$2 where asset_id=$1`, assetID, snapshot.Model); err != nil {
			return err
		}
	}
	if snapshot.Visibility == "private" {
		if _, err = tx.Exec(ctx, `delete from player_profile_textures texture using player_profiles profile
			where texture.profile_id=profile.id and texture.asset_id=$1 and profile.user_id<>$2`, assetID, ownerID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `delete from skin_wardrobe where asset_id=$1 and user_id<>$2`, assetID, ownerID); err != nil {
			return err
		}
	}
	if (oldModel != snapshot.Model || snapshot.Visibility == "private") && len(profileIDs) > 0 {
		if _, err = tx.Exec(ctx, `update player_profiles set updated_at=now() where id=any($1::bigint[])`, profileIDs); err != nil {
			return err
		}
	}
	command, err := tx.Exec(ctx, `update skin_assets set model=$2,display_name=$3,description=$4,tags=$5,
		visibility=$6,review_status='approved',published_revision_id=$7,updated_at=now() where id=$1 and status='active'`,
		assetID, snapshot.Model, snapshot.DisplayName, snapshot.Description, snapshot.Tags, snapshot.Visibility, revisionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("skin asset was not updated")
	}
	return nil
}

func (s *Server) deleteSkin(w http.ResponseWriter, r *http.Request, publicID string) {
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skin asset")
		return
	}
	defer tx.Rollback(r.Context())
	var actorStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&actorStatus); err != nil || actorStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	var assetID, ownerID int64
	var status string
	err = tx.QueryRow(r.Context(), `select id,owner_id,status from skin_assets where public_id=$1 for update`, publicID).
		Scan(&assetID, &ownerID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skin asset")
		return
	}
	if status != "active" {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	if claims.Subject != ownerID && !isSkinAdmin(claims) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	profileIDs, err := lockProfilesUsingAsset(r.Context(), tx, assetID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock equipped player profiles")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from player_profile_textures where asset_id=$1`, assetID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to unlink equipped skin asset")
		return
	}
	if len(profileIDs) > 0 {
		if _, err = tx.Exec(r.Context(), `update player_profiles set updated_at=now() where id=any($1::bigint[])`, profileIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update affected player profiles")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `delete from skin_wardrobe where asset_id=$1`, assetID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update wardrobes")
		return
	}
	command, err := tx.Exec(r.Context(), `update skin_assets set status='deleted',updated_at=now() where id=$1 and status='active'`, assetID)
	if err != nil || command.RowsAffected() != 1 {
		writeError(w, http.StatusInternalServerError, "failed to delete skin asset")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skin asset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) skinWardrobe(w http.ResponseWriter, r *http.Request) {
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
	rows, err := s.db.Query(r.Context(), skinAssetSelectSQL+`
		join skin_wardrobe wardrobe on wardrobe.asset_id=asset.id and wardrobe.user_id=$1
		where asset.status='active' and (asset.owner_id=$1 or
			(asset.review_status='approved' and asset.visibility in ('public','unlisted')))
		order by wardrobe.added_at desc,asset.id desc limit $2`, userID, maximumSkinWardrobeItems)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load wardrobe")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		record, scanErr := scanSkinAsset(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to read wardrobe")
			return
		}
		items = append(items, skinAssetJSON(record, userID))
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read wardrobe")
		return
	}
	var total int64
	if err = s.db.QueryRow(r.Context(), `select count(*) from skin_wardrobe where user_id=$1`, userID).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count wardrobe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": maximumSkinWardrobeItems})
}

func (s *Server) skinWardrobeItem(w http.ResponseWriter, r *http.Request) {
	userID := currentClaims(r).Subject
	if userID <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	publicID := normalizedSkinPublicID(r.PathValue("publicId"))
	if publicID == "" {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update wardrobe")
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, userID).Scan(&userStatus); err != nil || userStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	var assetID, ownerID int64
	var visibility, reviewStatus, status string
	err = tx.QueryRow(r.Context(), `select id,owner_id,visibility,review_status,status from skin_assets where public_id=$1 for update`, publicID).
		Scan(&assetID, &ownerID, &visibility, &reviewStatus, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skin asset")
		return
	}
	if status != "active" || (ownerID != userID && (reviewStatus != "approved" || visibility == "private")) {
		writeError(w, http.StatusNotFound, "skin asset was not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		_, err = addSkinToWardrobeTx(r.Context(), tx, userID, assetID, ownerID)
		if errors.Is(err, errSkinWardrobeLimit) {
			writeError(w, http.StatusForbidden, errSkinWardrobeLimit.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add wardrobe item")
			return
		}
	case http.MethodDelete:
		var equipped bool
		if err = tx.QueryRow(r.Context(), `select exists(select 1 from player_profile_textures texture
			join player_profiles profile on profile.id=texture.profile_id
			where texture.asset_id=$1 and profile.user_id=$2 and profile.status='active')`, assetID, userID).Scan(&equipped); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to inspect equipped wardrobe item")
			return
		}
		if equipped {
			writeError(w, http.StatusConflict, "skin asset is equipped by one of your player profiles")
			return
		}
		_, err = tx.Exec(r.Context(), `delete from skin_wardrobe where user_id=$1 and asset_id=$2`, userID, assetID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove wardrobe item")
			return
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update wardrobe")
		return
	}
	if r.Method == http.MethodPut {
		writeJSON(w, http.StatusOK, map[string]bool{"added": true})
	} else {
		writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
	}
}

func (s *Server) myPlayerProfiles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listMyPlayerProfiles(w, r)
	case http.MethodPost:
		s.createPlayerProfile(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) listMyPlayerProfiles(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	profiles, err := s.loadPlayerProfiles(r.Context(), claims.Subject, claims, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profiles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": profiles, "limit": playerProfileLimit(claims.PermissionRules)})
}

func (s *Server) createPlayerProfile(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	var request playerProfileCreateRequest
	if !decodeSkinJSON(w, r, &request) {
		return
	}
	if err := normalizePlayerProfileCreate(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	profileUUID, err := newMinecraftUUID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate player UUID")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create player profile")
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&userStatus); err != nil || userStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	var count int
	if err = tx.QueryRow(r.Context(), `select count(*) from player_profiles where user_id=$1 and status='active'`, claims.Subject).Scan(&count); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect player profile limit")
		return
	}
	if count >= playerProfileLimit(claims.PermissionRules) {
		writeError(w, http.StatusForbidden, "player profile limit reached")
		return
	}
	request.IsDefault = request.IsDefault || count == 0
	if request.IsDefault {
		if _, err = tx.Exec(r.Context(), `update player_profiles set is_default=false,updated_at=now() where user_id=$1 and status='active'`, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update default player profile")
			return
		}
	}
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into player_profiles(user_id,uuid,name,bio,visibility,is_default)
		values($1,$2::uuid,$3,$4,$5,$6) returning public_id`, claims.Subject, profileUUID, request.Name,
		request.Bio, request.Visibility, request.IsDefault).Scan(&publicID)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "player name is already in use")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create player profile")
		return
	}
	if _, err = ensureYggdrasilAccountTx(r.Context(), tx, claims.Subject); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create launcher account identity")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create player profile")
		return
	}
	profile, err := s.loadPlayerProfileByPublicIDForViewer(r.Context(), publicID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load created player profile")
		return
	}
	writeJSON(w, http.StatusCreated, profile)
}

func (s *Server) myPlayerProfile(w http.ResponseWriter, r *http.Request) {
	publicID := normalizedSkinPublicID(r.PathValue("publicId"))
	if publicID == "" {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	switch r.Method {
	case http.MethodPut:
		s.updatePlayerProfile(w, r, publicID)
	case http.MethodDelete:
		s.deletePlayerProfile(w, r, publicID)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) updatePlayerProfile(w http.ResponseWriter, r *http.Request, publicID string) {
	claims := currentClaims(r)
	var request playerProfileUpdateRequest
	if !decodeSkinJSON(w, r, &request) {
		return
	}
	if err := normalizePlayerProfileUpdate(&request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update player profile")
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&userStatus); err != nil || userStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	var profileID, ownerID int64
	var name, bio, visibility, status string
	var isDefault bool
	err = tx.QueryRow(r.Context(), `select id,user_id,name,bio,visibility,is_default,status from player_profiles
		where public_id=$1 for update`, publicID).Scan(&profileID, &ownerID, &name, &bio, &visibility, &isDefault, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profile")
		return
	}
	if status != "active" {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if claims.Subject != ownerID {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	oldName := name
	if request.Name != nil {
		name = *request.Name
	}
	if request.Bio != nil {
		bio = *request.Bio
	}
	if request.Visibility != nil {
		visibility = *request.Visibility
	}
	if request.IsDefault != nil && *request.IsDefault {
		if _, err = tx.Exec(r.Context(), `update player_profiles set is_default=false,updated_at=now()
			where user_id=$1 and id<>$2 and status='active'`, ownerID, profileID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update default player profile")
			return
		}
		isDefault = true
	} else if request.IsDefault != nil && !*request.IsDefault && isDefault {
		var replacementID int64
		replacementErr := tx.QueryRow(r.Context(), `select id from player_profiles where user_id=$1 and id<>$2 and status='active'
			order by created_at,id limit 1 for update`, ownerID, profileID).Scan(&replacementID)
		if replacementErr == nil {
			if _, err = tx.Exec(r.Context(), `update player_profiles set is_default=false,updated_at=now() where id=$1`, profileID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update default player profile")
				return
			}
			if _, err = tx.Exec(r.Context(), `update player_profiles set is_default=true,updated_at=now() where id=$1`, replacementID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update default player profile")
				return
			}
			isDefault = false
		} else if !errors.Is(replacementErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to find replacement default player profile")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `update player_profiles set name=$2,bio=$3,visibility=$4,is_default=$5,updated_at=now()
		where id=$1`, profileID, name, bio, visibility, isDefault)
	if isUniqueViolation(err) {
		writeError(w, http.StatusConflict, "player name is already in use")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update player profile")
		return
	}
	if name != oldName {
		if _, err = tx.Exec(r.Context(), `insert into player_profile_name_history(profile_id,old_name,new_name) values($1,$2,$3)`, profileID, oldName, name); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record player name history")
			return
		}
		if _, err = tx.Exec(r.Context(), `update yggdrasil_tokens set status='stale' where player_profile_id=$1 and status='active'`, profileID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to invalidate renamed player sessions")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update player profile")
		return
	}
	profile, err := s.loadPlayerProfileByPublicIDForViewer(r.Context(), publicID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load updated player profile")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) deletePlayerProfile(w http.ResponseWriter, r *http.Request, publicID string) {
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete player profile")
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, claims.Subject).Scan(&userStatus); err != nil || userStatus != "active" {
		writeError(w, http.StatusForbidden, "account is unavailable")
		return
	}
	var profileID, ownerID int64
	var isDefault bool
	var status string
	err = tx.QueryRow(r.Context(), `select id,user_id,is_default,status from player_profiles where public_id=$1 for update`, publicID).
		Scan(&profileID, &ownerID, &isDefault, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profile")
		return
	}
	if status != "active" {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if claims.Subject != ownerID {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from player_profile_textures where profile_id=$1`, profileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear player textures")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from yggdrasil_join_sessions where player_profile_id=$1`, profileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear player joins")
		return
	}
	if _, err = tx.Exec(r.Context(), `update yggdrasil_tokens set status='revoked',revoked_at=now()
		where player_profile_id=$1 and status<>'revoked'`, profileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke player sessions")
		return
	}
	if _, err = tx.Exec(r.Context(), `update player_profiles set status='deleted',is_default=false,updated_at=now() where id=$1`, profileID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete player profile")
		return
	}
	if isDefault {
		var replacementID int64
		replacementErr := tx.QueryRow(r.Context(), `select id from player_profiles where user_id=$1 and status='active'
			order by created_at,id limit 1 for update`, ownerID).Scan(&replacementID)
		if replacementErr == nil {
			if _, err = tx.Exec(r.Context(), `update player_profiles set is_default=true,updated_at=now() where id=$1`, replacementID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update default player profile")
				return
			}
		} else if !errors.Is(replacementErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to find replacement default player profile")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete player profile")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

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
	}
	var profileID int64
	err = tx.QueryRow(r.Context(), `select id from player_profiles where public_id=$1 and user_id=$2 and status='active' for update`, publicID, claims.Subject).
		Scan(&profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "player profile was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load player profile")
		return
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
	if len(updates) > 0 {
		if strings.TrimSpace(request.Kind) != "" || strings.TrimSpace(request.AssetPublicID) != "" || strings.TrimSpace(request.Model) != "" {
			return nil, fmt.Errorf("do not mix skinPublicId/capePublicId with the legacy texture fields")
		}
		return updates, nil
	}
	kind, err := normalizeMinecraftTextureKind(request.Kind)
	if err != nil {
		return nil, err
	}
	assetPublicID := strings.TrimSpace(request.AssetPublicID)
	if assetPublicID != "" {
		assetPublicID = normalizedSkinPublicID(assetPublicID)
		if assetPublicID == "" {
			return nil, fmt.Errorf("assetPublicId is invalid")
		}
	}
	model := strings.ToLower(strings.TrimSpace(request.Model))
	if model != "" && model != "default" && model != "slim" {
		return nil, fmt.Errorf("model must be default or slim")
	}
	return []playerTextureUpdate{{Kind: kind, AssetPublicID: assetPublicID, Model: model}}, nil
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
	if profile.Visibility == "private" && profile.UserID != claims.PublicSubject && !isSkinAdmin(claims) {
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

const skinAssetSelectSQL = `select asset.id,asset.public_id,asset.owner_id,owner.public_id,owner.username,
	asset.blob_hash,asset.kind,asset.model,asset.display_name,asset.description,asset.tags,asset.visibility,
	asset.review_status,asset.status,asset.downloads,blob.width,blob.height,blob.size_bytes,asset.created_at,asset.updated_at,
	exists(select 1 from skin_wardrobe viewer_wardrobe where viewer_wardrobe.user_id=$1 and viewer_wardrobe.asset_id=asset.id)
	from skin_assets asset join users owner on owner.id=asset.owner_id
	join skin_texture_blobs blob on blob.hash=asset.blob_hash `

func scanSkinAsset(row skinRowScanner) (skinAssetRecord, error) {
	var record skinAssetRecord
	err := row.Scan(&record.ID, &record.PublicID, &record.OwnerID, &record.OwnerPublicID, &record.OwnerName,
		&record.BlobHash, &record.Kind, &record.Model, &record.DisplayName, &record.Description, &record.Tags,
		&record.Visibility, &record.ReviewStatus, &record.Status, &record.Downloads, &record.Width, &record.Height,
		&record.SizeBytes, &record.CreatedAt, &record.UpdatedAt, &record.InWardrobe)
	return record, err
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

func skinAssetJSON(record skinAssetRecord, viewerID int64) map[string]any {
	canEdit := viewerID > 0 && viewerID == record.OwnerID
	canUse := record.Status == "active" && (canEdit || (record.ReviewStatus == "approved" && record.Visibility != "private"))
	return map[string]any{
		"publicId": record.PublicID, "ownerId": record.OwnerPublicID,
		"owner": map[string]any{"id": record.OwnerPublicID, "publicId": record.OwnerPublicID, "username": record.OwnerName},
		"hash":  record.BlobHash, "textureHash": record.BlobHash, "kind": record.Kind, "model": record.Model,
		"name": record.DisplayName, "displayName": record.DisplayName, "description": record.Description, "tags": record.Tags,
		"visibility": record.Visibility, "reviewStatus": record.ReviewStatus, "status": record.Status,
		"downloads": record.Downloads, "width": record.Width, "height": record.Height, "sizeBytes": record.SizeBytes,
		"textureUrl": "/api/yggdrasil/textures/" + record.BlobHash,
		"inWardrobe": record.InWardrobe, "isOwner": canEdit, "canEdit": canEdit, "canUse": canUse,
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
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	if request.Name != "" && request.DisplayName != "" && request.Name != request.DisplayName {
		return fmt.Errorf("name and displayName must match when both are provided")
	}
	if request.Name != "" {
		request.DisplayName = request.Name
	}
	if request.DisplayName == "" {
		if request.Kind == "cape" {
			request.DisplayName = "Cape"
		} else {
			request.DisplayName = "Skin"
		}
	}
	if len([]rune(request.DisplayName)) > 80 {
		return fmt.Errorf("displayName is too long")
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
		request.Localizations = []catalogLocalizationEdit{{Locale: request.DefaultLocale, Name: request.DisplayName, Summary: request.Description}}
		request.DefaultLocale, request.Localizations, err = normalizeCatalogLocalizations(request.DefaultLocale, request.Localizations)
	}
	if err != nil || requireCatalogCreateDefaultLocalization(request.DefaultLocale, request.Localizations) != nil {
		return fmt.Errorf("the default language must have a localized skin name")
	}
	for _, localization := range request.Localizations {
		if localization.Locale == request.DefaultLocale {
			request.DisplayName = localization.Name
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
	if request.Name != nil {
		if request.DisplayName != nil && strings.TrimSpace(*request.Name) != strings.TrimSpace(*request.DisplayName) {
			return fmt.Errorf("name and displayName must match when both are provided")
		}
		request.DisplayName = request.Name
	}
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
	if request.DisplayName != nil {
		value := strings.TrimSpace(*request.DisplayName)
		if value == "" || len([]rune(value)) > 80 {
			return fmt.Errorf("displayName must contain between 1 and 80 characters")
		}
		record.DisplayName = value
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
	rows, err := s.db.Query(ctx, `select profile.public_id,owner.public_id,profile.uuid::text,profile.name,profile.bio,
		profile.visibility,profile.is_default,profile.status,profile.created_at,profile.updated_at,
		owner.public_id,owner.public_id,owner.username
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
		if err = rows.Scan(&profile.PublicID, &profile.UserID, &profile.UUID, &profile.Name, &profile.Bio,
			&profile.Visibility, &profile.IsDefault, &profile.Status, &profile.CreatedAt, &profile.UpdatedAt,
			&profile.Owner.ID, &profile.Owner.PublicID, &profile.Owner.Username); err != nil {
			return nil, err
		}
		profile.Textures = map[string]playerTextureResponse{}
		profiles = append(profiles, profile)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for index := range profiles {
		if err = s.loadPlayerTextures(ctx, &profiles[index], claims.Subject); err != nil {
			return nil, err
		}
	}
	return profiles, nil
}

func (s *Server) loadPlayerProfileByPublicIDForViewer(ctx context.Context, publicID string, viewerID int64) (playerProfileResponse, error) {
	var profile playerProfileResponse
	err := s.db.QueryRow(ctx, `select profile.public_id,owner.public_id,profile.uuid::text,profile.name,profile.bio,
		profile.visibility,profile.is_default,profile.status,profile.created_at,profile.updated_at,
		owner.public_id,owner.public_id,owner.username
		from player_profiles profile join users owner on owner.id=profile.user_id where profile.public_id=$1`, publicID).
		Scan(&profile.PublicID, &profile.UserID, &profile.UUID, &profile.Name, &profile.Bio, &profile.Visibility,
			&profile.IsDefault, &profile.Status, &profile.CreatedAt, &profile.UpdatedAt,
			&profile.Owner.ID, &profile.Owner.PublicID, &profile.Owner.Username)
	if err != nil {
		return profile, err
	}
	profile.Textures = map[string]playerTextureResponse{}
	return profile, s.loadPlayerTextures(ctx, &profile, viewerID)
}

func (s *Server) loadPlayerTextures(ctx context.Context, profile *playerProfileResponse, viewerID int64) error {
	rows, err := s.db.Query(ctx, `select texture.kind,asset.model,asset.public_id,asset.display_name,
		asset.description,asset.tags,asset.visibility,asset.review_status,asset.status,asset.downloads,asset.blob_hash,
		asset.created_at,asset.updated_at,owner.id,owner.public_id,owner.username,
		exists(select 1 from skin_wardrobe wardrobe where wardrobe.user_id=$2 and wardrobe.asset_id=asset.id)
		from player_profile_textures texture join player_profiles profile on profile.id=texture.profile_id
		join skin_assets asset on asset.id=texture.asset_id
		join users owner on owner.id=asset.owner_id
		where profile.public_id=$1 and asset.status='active' order by texture.kind`, profile.PublicID, viewerID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var texture playerTextureResponse
		var assetStatus string
		if err = rows.Scan(&texture.Kind, &texture.Model, &texture.PublicID, &texture.Name, &texture.Description,
			&texture.Tags, &texture.Visibility, &texture.ReviewStatus, &assetStatus, &texture.Downloads,
			&texture.TextureHash, &texture.CreatedAt, &texture.UpdatedAt, &texture.OwnerInternalID, &texture.Owner.ID,
			&texture.Owner.Username, &texture.InWardrobe); err != nil {
			return err
		}
		texture.AssetPublicID = texture.PublicID
		texture.DisplayName = texture.Name
		texture.Hash = texture.TextureHash
		texture.TextureURL = "/api/yggdrasil/textures/" + texture.TextureHash
		texture.URL = texture.TextureURL
		texture.Owner.PublicID = texture.Owner.ID
		texture.CanEdit = viewerID > 0 && viewerID == texture.OwnerInternalID
		texture.CanUse = assetStatus == "active" && (texture.CanEdit ||
			(texture.ReviewStatus == "approved" && texture.Visibility != "private"))
		profile.Textures[texture.Kind] = texture
		textureCopy := texture
		if texture.Kind == "skin" {
			profile.Skin = &textureCopy
		} else if texture.Kind == "cape" {
			profile.Cape = &textureCopy
		}
	}
	return rows.Err()
}
