package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/jackc/pgx/v5"
)

type yggdrasilTexturePayload struct {
	Timestamp   int64                       `json:"timestamp"`
	ProfileID   string                      `json:"profileId"`
	ProfileName string                      `json:"profileName"`
	Textures    map[string]yggdrasilTexture `json:"textures"`
}

type yggdrasilTexture struct {
	URL      string            `json:"url"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

func (s *Server) buildYggdrasilProfile(ctx context.Context, profileID int64, signed bool) (yggdrasilProfile, error) {
	yggdrasilConfig, service := s.yggdrasilRuntimeSnapshot()
	profile, err := s.yggdrasilProfileRecordByID(ctx, profileID)
	if err != nil {
		return yggdrasilProfile{}, err
	}
	unsignedID, ok := unsignedYggdrasilUUID(profile.UUID)
	if !ok {
		return yggdrasilProfile{}, errors.New("invalid profile UUID in database")
	}

	rows, err := s.db.Query(ctx,
		`select t.kind,b.hash,coalesce(a.model,'default')
		 from player_profile_textures t
		 join skin_assets a on a.id=t.asset_id and a.status='active'
		 join skin_texture_blobs b on b.hash=a.blob_hash
		 where t.profile_id=$1`, profileID)
	if err != nil {
		return yggdrasilProfile{}, err
	}
	defer rows.Close()
	textures := make(map[string]yggdrasilTexture, 2)
	for rows.Next() {
		var kind, hash, model string
		if err := rows.Scan(&kind, &hash, &model); err != nil {
			return yggdrasilProfile{}, err
		}
		if !yggdrasilTextureHashPattern.MatchString(hash) {
			continue
		}
		textureURL, urlErr := yggdrasilTextureURL(yggdrasilConfig.TextureBaseURL, hash)
		if urlErr != nil {
			return yggdrasilProfile{}, urlErr
		}
		texture := yggdrasilTexture{URL: textureURL}
		switch strings.ToLower(kind) {
		case "skin":
			if strings.EqualFold(model, "slim") {
				texture.Metadata = map[string]string{"model": "slim"}
			}
			textures["SKIN"] = texture
		case "cape":
			textures["CAPE"] = texture
		}
	}
	if err := rows.Err(); err != nil {
		return yggdrasilProfile{}, err
	}

	payload := yggdrasilTexturePayload{
		Timestamp:   time.Now().UnixMilli(),
		ProfileID:   unsignedID,
		ProfileName: profile.Name,
		Textures:    textures,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return yggdrasilProfile{}, err
	}
	textureValue := base64.StdEncoding.EncodeToString(raw)
	properties := []yggdrasilProperty{
		{Name: "textures", Value: textureValue},
		{Name: "uploadableTextures", Value: "skin,cape"},
	}
	if signed {
		properties[0].Signature, err = service.signPropertyValue(properties[0].Value)
		if err != nil {
			return yggdrasilProfile{}, err
		}
	}
	return yggdrasilProfile{ID: unsignedID, Name: profile.Name, Properties: properties}, nil
}

func (s *Server) yggdrasilProfileByUUID(w http.ResponseWriter, r *http.Request) {
	databaseUUID, ok := databaseYggdrasilUUID(r.PathValue("uuid"))
	if !ok {
		writeYggdrasilNoContent(w)
		return
	}
	var profileID int64
	err := s.db.QueryRow(r.Context(), `select id from player_profiles where uuid=$1 and status='active'`, databaseUUID).Scan(&profileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilNoContent(w)
		return
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	signed := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("unsigned")), "false")
	profile, err := s.buildYggdrasilProfile(r.Context(), profileID, signed)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	writeYggdrasilJSON(w, http.StatusOK, profile)
}

func (s *Server) yggdrasilProfilesByName(w http.ResponseWriter, r *http.Request) {
	var names []string
	if err := decodeYggdrasilJSON(w, r, &names); err != nil || len(names) > 100 {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid profile query.", "")
		return
	}
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		lower := strings.ToLower(name)
		if name == "" {
			continue
		}
		if _, exists := seen[lower]; exists {
			continue
		}
		seen[lower] = struct{}{}
		normalized = append(normalized, lower)
	}
	if len(normalized) == 0 {
		writeYggdrasilJSON(w, http.StatusOK, []yggdrasilProfile{})
		return
	}
	rows, err := s.db.Query(r.Context(),
		`select id,uuid::text,name from player_profiles
		 where status='active' and lower(name)=any($1::text[])`, normalized)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	defer rows.Close()
	profiles := make([]yggdrasilProfile, 0, len(normalized))
	for rows.Next() {
		var record yggdrasilProfileRecord
		if err := rows.Scan(&record.InternalID, &record.UUID, &record.Name); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		profiles = append(profiles, record.publicProfile())
	}
	if rows.Err() != nil {
		writeYggdrasilInternalError(w)
		return
	}
	writeYggdrasilJSON(w, http.StatusOK, profiles)
}

func (s *Server) yggdrasilProfileByName(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("username"))
	if name == "" {
		writeYggdrasilNoContent(w)
		return
	}
	var record yggdrasilProfileRecord
	err := s.db.QueryRow(r.Context(),
		`select id,uuid::text,name from player_profiles where status='active' and lower(name)=lower($1)`, name).
		Scan(&record.InternalID, &record.UUID, &record.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilNoContent(w)
		return
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	writeYggdrasilJSON(w, http.StatusOK, record.publicProfile())
}

func (s *Server) yggdrasilSetTexture(w http.ResponseWriter, r *http.Request) {
	kind := strings.ToLower(strings.TrimSpace(r.PathValue("textureType")))
	if kind != "skin" && kind != "cape" {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid texture type.", "")
		return
	}
	userID, profileID, ok := s.authorizeYggdrasilTextureChange(r, r.PathValue("uuid"))
	if !ok {
		writeYggdrasilError(w, http.StatusUnauthorized, "UnauthorizedException", "Invalid access token.", "")
		return
	}
	assetLimit, err := s.userSkinAssetLimit(r.Context(), userID)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	var preflightAssetCount int
	if err = s.db.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and status='active'`, userID).Scan(&preflightAssetCount); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	var preflightRecentCreates int
	if err = s.db.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and created_at>=now()-interval '24 hours'`, userID).Scan(&preflightRecentCreates); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	yggdrasilConfig, _ := s.yggdrasilRuntimeSnapshot()
	maximum := yggdrasilConfig.TextureMaxBytes
	if maximum <= 0 {
		maximum = 2 * 1024 * 1024
	}
	r.Body = http.MaxBytesReader(w, r.Body, maximum+(1<<20))
	if err := r.ParseMultipartForm(maximum); err != nil {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid texture upload.", "")
		return
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Texture file is required.", "")
		return
	}
	defer file.Close()
	model := strings.ToLower(strings.TrimSpace(r.FormValue("model")))
	if kind == "cape" {
		model = "default"
	} else if model == "" {
		model = "default"
	} else if model != "default" && model != "slim" {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid skin model.", "")
		return
	}
	releaseTextureWorker, acquired := acquireMinecraftTextureProcessing(r.Context())
	if !acquired {
		writeYggdrasilError(w, http.StatusServiceUnavailable, "ServiceUnavailableException", "Texture processing service is busy.", "")
		return
	}
	defer releaseTextureWorker()
	texture, err := sanitizeMinecraftTexture(file, kind, maximum)
	if err != nil {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid texture image.", "")
		return
	}
	if preflightAssetCount >= assetLimit || preflightRecentCreates >= maximumSkinAssetCreatesPerDay {
		var reusable bool
		if err = s.db.QueryRow(r.Context(), `select exists(select 1 from skin_assets
			where owner_id=$1 and blob_hash=$2 and kind=$3 and model=$4 and status='active')`,
			userID, texture.Hash, kind, model).Scan(&reusable); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		if !reusable {
			writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Skin library creation limit reached.", "")
			return
		}
	}
	storageClient, storageConfig, err := s.ossClient(r.Context())
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	var pendingTextureUpload *pendingTextureUpload
	defer func() { s.compensateUncommittedMinecraftTextureUpload(pendingTextureUpload) }()
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, userID).Scan(&userStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Account is unavailable.", "")
		} else {
			writeYggdrasilInternalError(w)
		}
		return
	}
	if userStatus != "active" {
		writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Account is unavailable.", "")
		return
	}
	var profileStatus string
	if err = tx.QueryRow(r.Context(), `select status from player_profiles where id=$1 and user_id=$2 for update`, profileID, userID).Scan(&profileStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Player profile is unavailable.", "")
		} else {
			writeYggdrasilInternalError(w)
		}
		return
	}
	if profileStatus != "active" {
		writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Player profile is unavailable.", "")
		return
	}
	var assetID int64
	err = tx.QueryRow(r.Context(), `select id from skin_assets
		where owner_id=$1 and blob_hash=$2 and kind=$3 and model=$4 and status='active' and review_status='approved'
		order by id desc limit 1 for update`, userID, texture.Hash, kind, model).Scan(&assetID)
	if errors.Is(err, pgx.ErrNoRows) {
		var assetCount, recentCreates int
		if err = tx.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and status='active'`, userID).Scan(&assetCount); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		if err = tx.QueryRow(r.Context(), `select count(*) from skin_assets where owner_id=$1 and created_at>=now()-interval '24 hours'`, userID).Scan(&recentCreates); err != nil {
			writeYggdrasilInternalError(w)
			return
		}
		if assetCount >= assetLimit || recentCreates >= maximumSkinAssetCreatesPerDay {
			writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Skin library creation limit reached.", "")
			return
		}
		blob, uploadedTexture, persistErr := persistSanitizedMinecraftTextureTx(r.Context(), tx, storageClient, storageConfig, texture)
		pendingTextureUpload = uploadedTexture
		if persistErr != nil {
			log.Printf("persist launcher skin texture for user %d: %v", userID, persistErr)
			writeYggdrasilInternalError(w)
			return
		}
		err = tx.QueryRow(r.Context(),
			`insert into skin_assets
			 (owner_id,blob_hash,kind,model,display_name,visibility,review_status,status)
			 values ($1,$2,$3,$4,$5,'private','approved','active')
			 returning id`, userID, blob.Hash, kind, model, "Launcher "+kind+" upload").Scan(&assetID)
	}
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	if _, err = addSkinToWardrobeTx(r.Context(), tx, userID, assetID, userID); errors.Is(err, errSkinWardrobeLimit) {
		writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Skin wardrobe item limit reached.", "")
		return
	} else if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	command, err := tx.Exec(r.Context(),
		`update player_profiles set updated_at=now()
		 where id=$1 and user_id=$2 and status='active'`, profileID, userID)
	if err != nil || command.RowsAffected() != 1 {
		writeYggdrasilInternalError(w)
		return
	}
	command, err = tx.Exec(r.Context(),
		`insert into player_profile_textures (profile_id,kind,asset_id,model,equipped_at)
		 values ($1,$2,$3,$4,now())
		 on conflict (profile_id,kind) do update set
		 asset_id=excluded.asset_id,model=excluded.model,equipped_at=now()`,
		profileID, kind, assetID, model)
	if err != nil || command.RowsAffected() != 1 {
		writeYggdrasilInternalError(w)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	pendingTextureUpload = nil
	writeYggdrasilNoContent(w)
}

func (s *Server) yggdrasilDeleteTexture(w http.ResponseWriter, r *http.Request) {
	kind := strings.ToLower(strings.TrimSpace(r.PathValue("textureType")))
	if kind != "skin" && kind != "cape" {
		writeYggdrasilError(w, http.StatusBadRequest, "IllegalArgumentException", "Invalid texture type.", "")
		return
	}
	userID, profileID, ok := s.authorizeYggdrasilTextureChange(r, r.PathValue("uuid"))
	if !ok {
		writeYggdrasilError(w, http.StatusUnauthorized, "UnauthorizedException", "Invalid access token.", "")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	defer tx.Rollback(r.Context())
	var userStatus string
	if err = tx.QueryRow(r.Context(), `select status from users where id=$1 for update`, userID).Scan(&userStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Account is unavailable.", "")
		} else {
			writeYggdrasilInternalError(w)
		}
		return
	}
	if userStatus != "active" {
		writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Account is unavailable.", "")
		return
	}
	var profileStatus string
	if err = tx.QueryRow(r.Context(), `select status from player_profiles where id=$1 and user_id=$2 for update`, profileID, userID).Scan(&profileStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Player profile is unavailable.", "")
		} else {
			writeYggdrasilInternalError(w)
		}
		return
	}
	if profileStatus != "active" {
		writeYggdrasilError(w, http.StatusForbidden, "ForbiddenOperationException", "Player profile is unavailable.", "")
		return
	}
	if _, err = tx.Exec(r.Context(), `delete from player_profile_textures where profile_id=$1 and kind=$2`, profileID, kind); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	command, err := tx.Exec(r.Context(), `update player_profiles set updated_at=now()
		where id=$1 and user_id=$2 and status='active'`, profileID, userID)
	if err != nil || command.RowsAffected() != 1 {
		writeYggdrasilInternalError(w)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	writeYggdrasilNoContent(w)
}

func (s *Server) authorizeYggdrasilTextureChange(r *http.Request, uuid string) (int64, int64, bool) {
	token := yggdrasilBearerToken(r)
	databaseUUID, uuidOK := databaseYggdrasilUUID(uuid)
	if token == "" || !uuidOK {
		return 0, 0, false
	}
	var userID, profileID int64
	err := s.db.QueryRow(r.Context(),
		`select t.user_id,p.id
		 from yggdrasil_tokens t
		 join yggdrasil_accounts a on a.user_id=t.user_id and a.enabled=true
		 join users u on u.id=t.user_id and u.status='active'
		 join player_profiles p on p.id=t.player_profile_id and p.user_id=t.user_id
		 where t.access_token_hash=$1 and t.status='active' and t.expires_at>now()
		   and p.uuid=$2 and p.status='active'`, hashYggdrasilToken(token), databaseUUID).
		Scan(&userID, &profileID)
	return userID, profileID, err == nil && s.yggdrasilUserAllowed(r.Context(), userID)
}

func (s *Server) yggdrasilTextureContent(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(strings.TrimSpace(r.PathValue("hash")))
	if !yggdrasilTextureHashPattern.MatchString(hash) {
		writeYggdrasilError(w, http.StatusNotFound, "NotFoundException", "Texture not found.", "")
		return
	}
	var objectKey string
	var sizeBytes int64
	err := s.db.QueryRow(r.Context(), `select blob.object_key,blob.size_bytes
		from skin_texture_blobs blob join oss_files file on file.id=blob.oss_file_id and file.status='active'
		where blob.hash=$1`, hash).
		Scan(&objectKey, &sizeBytes)
	if errors.Is(err, pgx.ErrNoRows) {
		writeYggdrasilError(w, http.StatusNotFound, "NotFoundException", "Texture not found.", "")
		return
	}
	if err != nil || objectKey == "" || sizeBytes <= 0 {
		writeYggdrasilInternalError(w)
		return
	}
	etag := `"` + hash + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	cfg := s.ossConfigFromSettings(r.Context())
	client, err := s.ossDownloadClient(r.Context(), cfg)
	if err != nil {
		writeYggdrasilInternalError(w)
		return
	}
	result, err := client.GetObject(r.Context(), &aliyunoss.GetObjectRequest{Bucket: aliyunoss.Ptr(cfg.Bucket), Key: aliyunoss.Ptr(objectKey)})
	if err != nil {
		writeYggdrasilError(w, http.StatusBadGateway, "ServiceUnavailableException", "Texture storage is unavailable.", "")
		return
	}
	defer result.Body.Close()
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Length", strconv.FormatInt(sizeBytes, 10))
	w.WriteHeader(http.StatusOK)
	written, copyErr := io.Copy(w, io.LimitReader(result.Body, sizeBytes))
	if copyErr != nil || written != sizeBytes {
		log.Printf("stream Yggdrasil texture %s: wrote %d of %d bytes: %v", hash, written, sizeBytes, copyErr)
	}
}

func yggdrasilTextureURL(baseURL, hash string) (string, error) {
	if !yggdrasilTextureHashPattern.MatchString(hash) {
		return "", fmt.Errorf("invalid texture hash")
	}
	if err := validateYggdrasilEndpoint(baseURL, "texture", false); err != nil {
		return "", err
	}
	return normalizedYggdrasilBaseURL(baseURL) + hash, nil
}
