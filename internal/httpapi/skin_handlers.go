package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
var errPrivateSkinProfileConflict = errors.New("private skin assets require private player profiles")

type skinAssetRecord struct {
	ID            int64
	PublicID      string
	OwnerID       int64
	OwnerPublicID string
	OwnerName     string
	BlobHash      string
	Kind          string
	Model         string
	Name          string
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
	Description   string                    `json:"description"`
	Tags          []string                  `json:"tags"`
	Visibility    string                    `json:"visibility"`
	DefaultLocale string                    `json:"defaultLocale"`
	Localizations []catalogLocalizationEdit `json:"localizations"`
}

type skinAssetUpdateRequest struct {
	Model         *string                   `json:"model,omitempty"`
	Name          *string                   `json:"name,omitempty"`
	Description   *string                   `json:"description,omitempty"`
	Tags          *[]string                 `json:"tags,omitempty"`
	Visibility    *string                   `json:"visibility,omitempty"`
	Reason        string                    `json:"reason,omitempty"`
	DefaultLocale string                    `json:"defaultLocale,omitempty"`
	Localizations []catalogLocalizationEdit `json:"localizations,omitempty"`
}

type skinAssetContentSnapshot struct {
	PublicID             string                    `json:"publicId"`
	Model                string                    `json:"model"`
	Name                 string                    `json:"name"`
	Description          string                    `json:"description"`
	Tags                 []string                  `json:"tags"`
	Visibility           string                    `json:"visibility"`
	DefaultLocale        string                    `json:"defaultLocale,omitempty"`
	Localizations        []catalogLocalizationEdit `json:"localizations,omitempty"`
	ReplaceLocalizations bool                      `json:"replaceLocalizations,omitempty"`
}

type createdSkinAsset struct {
	AssetID  int64
	PublicID string
	Revision createdContentRevision
}

type skinOwnerResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type playerProfileResponse struct {
	PublicID   string                 `json:"publicId"`
	UUID       string                 `json:"uuid"`
	Name       string                 `json:"name"`
	Bio        string                 `json:"bio"`
	Visibility string                 `json:"visibility"`
	IsDefault  bool                   `json:"isDefault"`
	Status     string                 `json:"status"`
	Skin       *playerTextureResponse `json:"skin"`
	Cape       *playerTextureResponse `json:"cape"`
	Owner      skinOwnerResponse      `json:"owner"`
	CreatedAt  time.Time              `json:"createdAt"`
	UpdatedAt  time.Time              `json:"updatedAt"`
}

type playerTextureResponse struct {
	OwnerInternalID int64             `json:"-"`
	PublicID        string            `json:"publicId"`
	Kind            string            `json:"kind"`
	Model           string            `json:"model"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Tags            []string          `json:"tags"`
	Visibility      string            `json:"visibility"`
	ReviewStatus    string            `json:"reviewStatus"`
	TextureHash     string            `json:"textureHash"`
	TextureURL      string            `json:"textureUrl"`
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
	SkinPublicID json.RawMessage `json:"skinPublicId"`
	CapePublicID json.RawMessage `json:"capePublicId"`
}

type playerTextureUpdate struct {
	Kind          string
	AssetPublicID string
	AssetID       int64
	Model         string
	Visibility    string
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
	yggdrasilConfig, service := s.yggdrasilRuntimeSnapshot()
	serviceEnabled := service != nil && service.privateKey != nil && service.disabledReason == nil
	serverName := strings.TrimSpace(yggdrasilConfig.ServerName)
	if serverName == "" {
		serverName = "Mcmods-cn"
	}
	apiRoot := strings.TrimSpace(yggdrasilConfig.PublicBaseURL)
	if apiRoot != "" {
		apiRoot = strings.TrimRight(apiRoot, "/") + "/"
	}
	textureBaseURL := strings.TrimSpace(yggdrasilConfig.TextureBaseURL)
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
	request, err := parseSkinCatalogPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pageSQL, arguments := skinCatalogPageSQL(request)
	rows, err := s.db.Query(r.Context(), pageSQL, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load skin library")
		return
	}
	pageRows := make([]skinCatalogPageRow, 0, request.Limit+1)
	for rows.Next() {
		var row skinCatalogPageRow
		if err = rows.Scan(&row.AssetID, &row.SortName, &row.CreatedAt, &row.UpdatedAt,
			&row.Heat, &row.Downloads, &row.Favorites, &row.Rating, &row.RatingCount,
			&row.Views, &row.Comments); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read skin library")
			return
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "failed to read skin library")
		return
	}
	rows.Close()
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	assetIDs := make([]int64, len(pageRows))
	for index, row := range pageRows {
		assetIDs[index] = row.AssetID
	}
	claims := currentClaims(r)
	viewerID := claims.Subject
	recordsByID := make(map[int64]skinAssetRecord, len(assetIDs))
	if len(assetIDs) != 0 {
		rows, err = s.db.Query(r.Context(), skinAssetSelectSQL+`
			where asset.id=any($2::bigint[]) and asset.status='active'
				and asset.review_status='approved' and asset.visibility='public'`, viewerID, assetIDs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load skin library")
			return
		}
		for rows.Next() {
			record, scanErr := scanSkinAsset(rows)
			if scanErr != nil {
				rows.Close()
				writeError(w, http.StatusInternalServerError, "failed to read skin library")
				return
			}
			recordsByID[record.ID] = record
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read skin library")
			return
		}
		rows.Close()
	}
	items := make([]map[string]any, 0, len(pageRows))
	for _, row := range pageRows {
		if record, exists := recordsByID[row.AssetID]; exists {
			items = append(items, skinAssetJSON(record, claims))
		}
	}
	nextCursor := ""
	if hasMore && len(pageRows) != 0 {
		nextCursor = skinCatalogNextCursor(request, pageRows[len(pageRows)-1])
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
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
	reviewRequired := catalogMutationReviewRequired(loadReviewConfig(r.Context(), s.db), "create") &&
		!catalogMutationBypassesReview(claims)
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
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
	var pendingTextureUpload *pendingTextureUpload
	defer func() { s.compensateUncommittedMinecraftTextureUpload(pendingTextureUpload) }()
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
	blob, pendingTextureUpload, err := persistSanitizedMinecraftTextureTx(r.Context(), tx, storageClient, storageConfig, texture)
	if err != nil {
		log.Printf("persist web skin texture for user %d: %v", claims.Subject, err)
		writeError(w, http.StatusServiceUnavailable, "texture storage is unavailable")
		return
	}
	created, err := persistSkinAssetCreationTx(r.Context(), tx, claims.Subject, blob.Hash, request, reviewRequired, r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record skin asset")
		return
	}
	if _, err = addSkinToWardrobeTx(r.Context(), tx, claims.Subject, created.AssetID, claims.Subject); errors.Is(err, errSkinWardrobeLimit) {
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
	pendingTextureUpload = nil
	record, err := s.skinAssetByPublicID(r.Context(), created.PublicID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load created skin asset")
		return
	}
	writeJSON(w, http.StatusCreated, skinAssetJSON(record, claims))
}

func persistSkinAssetCreationTx(
	ctx context.Context,
	tx pgx.Tx,
	ownerID int64,
	blobHash string,
	request skinAssetCreateRequest,
	reviewRequired bool,
	auditRequest *http.Request,
) (createdSkinAsset, error) {
	var result createdSkinAsset
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}
	if err := tx.QueryRow(ctx, `insert into skin_assets(
		owner_id,blob_hash,kind,model,display_name,description,tags,visibility,review_status,status)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,'active') returning id,public_id`, ownerID,
		blobHash, request.Kind, request.Model, request.Name, request.Description, request.Tags, request.Visibility, reviewStatus).
		Scan(&result.AssetID, &result.PublicID); err != nil {
		return result, fmt.Errorf("insert skin asset: %w", err)
	}
	snapshot := skinAssetContentSnapshot{
		PublicID: result.PublicID, Model: request.Model, Name: request.Name, Description: request.Description,
		Tags: request.Tags, Visibility: request.Visibility, DefaultLocale: request.DefaultLocale,
		Localizations: request.Localizations, ReplaceLocalizations: true,
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		return result, fmt.Errorf("encode skin creation revision: %w", err)
	}
	result.Revision, err = createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: "skin", EntityID: result.AssetID,
		AggregateType: "skin", AggregateKey: result.PublicID, Snapshot: snapshotRaw,
		Reason: "New skin", ActorID: ownerID, Status: reviewStatus, Source: "skin_upload",
		Metadata: map[string]any{"skinId": result.PublicID, "name": request.Name, "operation": "create"}, Request: auditRequest,
	})
	if err != nil {
		return result, fmt.Errorf("create skin revision: %w", err)
	}
	if !reviewRequired {
		if err = applySkinAssetSnapshotTx(ctx, tx, result.AssetID, ownerID, result.Revision.RevisionID, ownerID, snapshot); err != nil {
			return result, fmt.Errorf("publish skin creation: %w", err)
		}
		return result, nil
	}
	if _, err = tx.Exec(ctx, `update content_subjects set default_locale=$3,updated_at=now()
		where subject_type=$1 and subject_id=$2`, "skin", result.AssetID, request.DefaultLocale); err != nil {
		return result, fmt.Errorf("set pending skin content language: %w", err)
	}
	for _, localization := range request.Localizations {
		if _, err = tx.Exec(ctx, `insert into content_localizations(
			subject_type,subject_id,locale,name,summary,content_markdown,provenance,editable,review_status,updated_by)
			values('skin',$1,$2,$3,$4,$5,'human',true,'pending',$6)`, result.AssetID, localization.Locale,
			localization.Name, localization.Summary, localization.ContentMarkdown, ownerID); err != nil {
			return result, fmt.Errorf("insert pending skin localization: %w", err)
		}
	}
	return result, nil
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
	writeJSON(w, http.StatusOK, skinAssetJSON(record, claims))
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
	replaceLocalizations, normalizeErr := normalizeSkinAssetUpdateRequest(&request)
	if normalizeErr != nil {
		writeError(w, http.StatusBadRequest, normalizeErr.Error())
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
			&record.Name, &record.Description, &record.Tags, &record.Visibility, &record.ReviewStatus,
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
		PublicID: publicID, Model: record.Model, Name: record.Name,
		Description: record.Description, Tags: record.Tags, Visibility: record.Visibility,
		DefaultLocale: request.DefaultLocale, Localizations: request.Localizations, ReplaceLocalizations: replaceLocalizations,
	}
	snapshotRaw, err := json.Marshal(snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode skin revision")
		return
	}
	reviewConfig := loadReviewConfig(r.Context(), tx)
	operation := "edit"
	reviewRequired := catalogMutationReviewRequired(reviewConfig, operation) && !catalogMutationBypassesReview(claims)
	reviewStatus := "approved"
	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		reason = "Skin information update"
	}
	if baseRevisionID == nil {
		operation = "create"
		reviewRequired = catalogMutationReviewRequired(reviewConfig, operation) && !catalogMutationBypassesReview(claims)
		if strings.TrimSpace(request.Reason) == "" {
			reason = "New skin"
		}
	}
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	if reviewRequired {
		reviewStatus = "pending"
	}
	if reviewRequired && snapshot.Visibility == "private" {
		if _, err = lockSkinAssetProfilesForPrivateVisibility(r.Context(), tx, record.ID, record.OwnerID); err != nil {
			if errors.Is(err, errPrivateSkinProfileConflict) {
				writeError(w, http.StatusConflict, errPrivateSkinProfileConflict.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to validate player profile visibility")
			return
		}
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: "skin", EntityID: record.ID,
		AggregateType: "skin", AggregateKey: publicID, BaseRevision: baseRevisionID, Snapshot: snapshotRaw,
		Reason: reason, ActorID: claims.Subject, Status: reviewStatus, Source: "skin_metadata",
		Metadata: map[string]any{"skinId": publicID, "name": record.Name, "operation": operation}, Request: r,
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
		if err = applySkinAssetSnapshotTx(r.Context(), tx, record.ID, record.OwnerID, created.RevisionID, claims.Subject, snapshot); err != nil {
			if errors.Is(err, errPrivateSkinProfileConflict) {
				writeError(w, http.StatusConflict, errPrivateSkinProfileConflict.Error())
				return
			}
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
	writeJSON(w, http.StatusOK, skinAssetJSON(record, claims))
}

func applySkinAssetSnapshotTx(ctx context.Context, tx pgx.Tx, assetID, ownerID, revisionID, actorID int64, snapshot skinAssetContentSnapshot) error {
	var oldModel string
	var oldPublishedRevisionID *int64
	if err := tx.QueryRow(ctx, `select model,published_revision_id from skin_assets where id=$1 and status='active' for update`, assetID).
		Scan(&oldModel, &oldPublishedRevisionID); err != nil {
		return err
	}
	var profileIDs []int64
	var err error
	if snapshot.Visibility == "private" {
		profileIDs, err = lockSkinAssetProfilesForPrivateVisibility(ctx, tx, assetID, ownerID)
	} else {
		profileIDs, err = lockProfilesUsingAsset(ctx, tx, assetID)
	}
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
		assetID, snapshot.Model, snapshot.Name, snapshot.Description, snapshot.Tags, snapshot.Visibility, revisionID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("skin asset was not updated")
	}
	if snapshot.ReplaceLocalizations {
		return replaceOwnedAssetLocalizationsTx(ctx, tx, "skin", assetID, revisionID, actorID, snapshot.DefaultLocale, snapshot.Localizations)
	}
	if oldPublishedRevisionID == nil {
		if _, err = tx.Exec(ctx, `update content_localizations set review_status='approved',published_revision_id=$3,updated_by=$4,updated_at=now()
			where subject_type=$1 and subject_id=$2`, "skin", assetID, revisionID, actorID); err != nil {
			return err
		}
	}
	return nil
}

func lockSkinAssetProfilesForPrivateVisibility(ctx context.Context, tx pgx.Tx, assetID, ownerID int64) ([]int64, error) {
	profileIDs, err := lockProfilesUsingAsset(ctx, tx, assetID)
	if err != nil || len(profileIDs) == 0 {
		return profileIDs, err
	}
	var conflict bool
	if err = tx.QueryRow(ctx, `select exists(select 1 from player_profile_textures texture
		join player_profiles profile on profile.id=texture.profile_id
		where texture.asset_id=$1 and profile.user_id=$2 and profile.status='active' and profile.visibility<>'private')`,
		assetID, ownerID).Scan(&conflict); err != nil {
		return nil, err
	}
	if conflict {
		return nil, errPrivateSkinProfileConflict
	}
	return profileIDs, nil
}

func rejectSkinRevisionTx(ctx context.Context, tx pgx.Tx, assetID int64, initialSubmission bool) error {
	reviewStatus := "approved"
	if initialSubmission {
		reviewStatus = "rejected"
	}
	command, err := tx.Exec(ctx, `update skin_assets set review_status=$2,updated_at=now()
		where id=$1 and status='active'`, assetID, reviewStatus)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("skin asset was not rejected")
	}
	if initialSubmission {
		if _, err = tx.Exec(ctx, `update content_localizations set review_status='rejected',updated_at=now()
			where subject_type='skin' and subject_id=$1 and published_revision_id is null`, assetID); err != nil {
			return err
		}
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
	var blobHash string
	var status string
	err = tx.QueryRow(r.Context(), `select id,owner_id,blob_hash,status from skin_assets where public_id=$1 for update`, publicID).
		Scan(&assetID, &ownerID, &blobHash, &status)
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
	if err = s.softDeleteSkinAssetTx(r.Context(), tx, assetID, blobHash, "skin_asset_deleted"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skin asset")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete skin asset")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) softDeleteSkinAssetTx(ctx context.Context, tx pgx.Tx, assetID int64, blobHash, reason string) error {
	if err := lockMinecraftTextureLifecycleStore(ctx, tx, blobHash); err != nil {
		return fmt.Errorf("lock skin texture lifecycle: %w", err)
	}
	profileIDs, err := lockProfilesUsingAsset(ctx, tx, assetID)
	if err != nil {
		return fmt.Errorf("lock equipped player profiles: %w", err)
	}
	if _, err = tx.Exec(ctx, `delete from player_profile_textures where asset_id=$1`, assetID); err != nil {
		return fmt.Errorf("unlink equipped skin asset: %w", err)
	}
	if len(profileIDs) > 0 {
		if _, err = tx.Exec(ctx, `update player_profiles set updated_at=now() where id=any($1::bigint[])`, profileIDs); err != nil {
			return fmt.Errorf("update affected player profiles: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `delete from skin_wardrobe where asset_id=$1`, assetID); err != nil {
		return fmt.Errorf("update skin wardrobes: %w", err)
	}
	command, err := tx.Exec(ctx, `update skin_assets set status='deleted',updated_at=now() where id=$1 and status='active'`, assetID)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return errors.New("skin asset is not active")
	}
	if err = s.tombstoneUnreferencedMinecraftTextureBlobTx(ctx, tx, blobHash, reason); err != nil {
		return fmt.Errorf("retire unreferenced skin texture blob: %w", err)
	}
	return nil
}

func (s *Server) skinWardrobe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	claims := currentClaims(r)
	userID := claims.Subject
	if userID <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	request, err := parseSkinWardrobePageRequest(r.URL.Query(), userID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pageSQL, arguments := skinWardrobePageSQL(request)
	rows, err := s.db.Query(r.Context(), pageSQL, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load wardrobe")
		return
	}
	pageRows := make([]skinWardrobePageRow, 0, request.Limit+1)
	for rows.Next() {
		row, scanErr := scanSkinWardrobePageRow(rows)
		if scanErr != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to read wardrobe")
			return
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		writeError(w, http.StatusInternalServerError, "failed to read wardrobe")
		return
	}
	rows.Close()
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	items := make([]map[string]any, 0, len(pageRows))
	for _, row := range pageRows {
		items = append(items, skinAssetJSON(row.Asset, claims))
	}
	var total int64
	countSQL, countArguments := skinWardrobeCountSQL(request)
	if err = s.db.QueryRow(r.Context(), countSQL, countArguments...).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count wardrobe")
		return
	}
	nextCursor := ""
	if hasMore && len(pageRows) != 0 {
		nextCursor = skinWardrobeNextCursor(request, pageRows[len(pageRows)-1])
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "total": total, "limit": request.Limit,
		"hasMore": hasMore, "nextCursor": nextCursor,
	})
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
	if request.Visibility != nil && visibility != "private" {
		var hasPrivateTexture bool
		if err = tx.QueryRow(r.Context(), `select exists(select 1 from player_profile_textures texture
			join skin_assets asset on asset.id=texture.asset_id
			where texture.profile_id=$1 and asset.status='active' and asset.visibility='private')`, profileID).
			Scan(&hasPrivateTexture); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to validate player texture visibility")
			return
		}
		if hasPrivateTexture {
			writeError(w, http.StatusConflict, errPrivateSkinProfileConflict.Error())
			return
		}
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
