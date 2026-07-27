package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

const profileConfigSettingKey = "profile.config"

type profileConfigPayload struct {
	SignatureMaxBytes int `json:"signatureMaxBytes"`
}

type userProfileSettingsRequest struct {
	Signature      *string `json:"signature,omitempty"`
	MessageReceive *bool   `json:"messageReceive,omitempty"`
	AvatarFileID   *string `json:"avatarFileId,omitempty"`
	ClearAvatar    bool    `json:"clearAvatar,omitempty"`
}

type userProfileSettingsResponse struct {
	Signature            string `json:"signature"`
	SignatureMaxBytes    int    `json:"signatureMaxBytes"`
	AvatarURL            string `json:"avatarUrl"`
	ProfileBackgroundURL string `json:"profileBackgroundUrl"`
	MessageReceive       bool   `json:"messageReceive"`
	CanUpdateAvatar      bool   `json:"canUpdateAvatar"`
	CanUseAnimatedAvatar bool   `json:"canUseAnimatedAvatar"`
}

func (s *Server) updateProfileConfig(w http.ResponseWriter, r *http.Request) {
	var payload profileConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	payload = normalizeProfileConfig(payload)
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "用户资料配置格式不正确")
		return
	}
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ($1, $2::jsonb, $3, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		profileConfigSettingKey,
		string(raw),
		currentClaims(r).Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存用户资料配置失败")
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) getUserProfileSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.loadUserProfileSettings(r.Context(), currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户设置失败")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) updateUserProfileSettings(w http.ResponseWriter, r *http.Request) {
	var request userProfileSettingsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	claims := currentClaims(r)
	config := s.profileConfigFromSettings(r.Context())
	if request.Signature != nil {
		value := strings.TrimSpace(*request.Signature)
		if len([]byte(value)) > config.SignatureMaxBytes {
			writeError(w, http.StatusBadRequest, "签名超过最大字节数限制")
			return
		}
		request.Signature = &value
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存用户设置失败")
		return
	}
	defer tx.Rollback(r.Context())
	var baseRevisionID *int64
	if err = tx.QueryRow(r.Context(), `select profile_revision_id from users where id=$1 for update`, claims.Subject).Scan(&baseRevisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock user profile")
		return
	}

	if request.Signature != nil {
		if _, err := tx.Exec(r.Context(), `update users set signature = $2, updated_at = now() where id = $1`, claims.Subject, *request.Signature); err != nil {
			writeError(w, http.StatusInternalServerError, "保存用户签名失败")
			return
		}
	}
	if request.MessageReceive != nil {
		if err := ensurePermissionNode(r.Context(), tx, "user.message.receive"); err != nil {
			writeError(w, http.StatusInternalServerError, "保存私聊权限失败")
			return
		}
		if _, err := tx.Exec(
			r.Context(),
			`insert into user_permissions (user_id, permission_id, allow, updated_at)
			 select $1, id, $3, now() from permissions where code = $2
			 on conflict (user_id, permission_id) do update
			 set allow = excluded.allow, expires_at = null, updated_at = now()`,
			claims.Subject,
			"user.message.receive",
			*request.MessageReceive,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "保存私聊权限失败")
			return
		}
	}
	if request.ClearAvatar {
		if !s.userHasPermission(r.Context(), claims.Subject, "user.avatar.update") {
			writeError(w, http.StatusForbidden, "没有更换头像权限")
			return
		}
		if _, err := tx.Exec(r.Context(), `update users set avatar_file_id = null, avatar_url = '', updated_at = now() where id = $1`, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "清除头像失败")
			return
		}
	}
	if request.AvatarFileID != nil {
		if !s.userHasPermission(r.Context(), claims.Subject, "user.avatar.update") {
			writeError(w, http.StatusForbidden, "没有更换头像权限")
			return
		}
		fileID, resolveErr := resolveOSSFilePublicID(r.Context(), tx, *request.AvatarFileID)
		if resolveErr != nil {
			writeError(w, http.StatusBadRequest, "avatar file does not exist")
			return
		}
		var originalName, contentType, objectKey string
		err := tx.QueryRow(
			r.Context(),
			`select original_name, content_type, object_key
			 from oss_files
			 where id = $1 and uploader_id = $2 and status = 'active'`,
			fileID,
			claims.Subject,
		).Scan(&originalName, &contentType, &objectKey)
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusBadRequest, "头像文件不存在或不属于当前用户")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取头像文件失败")
			return
		}
		if !isSupportedAvatarImage(originalName, contentType) {
			writeError(w, http.StatusBadRequest, "头像仅支持 PNG、JPEG、WebP、GIF 或 APNG 图片")
			return
		}
		animated, err := s.isAnimatedAvatarObject(r.Context(), objectKey, originalName, contentType)
		if err != nil {
			writeError(w, http.StatusBadGateway, "无法验证头像文件是否为动态图")
			return
		}
		if animated && !s.userHasPermission(r.Context(), claims.Subject, "user.avatar.animated") {
			writeError(w, http.StatusForbidden, "没有使用动态头像权限")
			return
		}
		avatarURL := buildPublicOSSURL(s.ossConfigFromSettings(r.Context()), objectKey)
		if _, err := tx.Exec(
			r.Context(),
			`update users set avatar_file_id = $2, avatar_url = $3, updated_at = now() where id = $1`,
			claims.Subject,
			fileID,
			avatarURL,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "保存头像失败")
			return
		}
	}
	if request.Signature != nil || request.MessageReceive != nil || request.ClearAvatar || request.AvatarFileID != nil {
		var signature, avatarURL string
		var avatarFileID *string
		if err = tx.QueryRow(r.Context(), `select signature,avatar_url,(select public_id from oss_files where id=users.avatar_file_id) from users where id=$1`, claims.Subject).Scan(&signature, &avatarURL, &avatarFileID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read updated user profile")
			return
		}
		var messageReceive bool
		if err = tx.QueryRow(r.Context(), `select coalesce((select user_permission.allow from user_permissions user_permission
			join permissions permission on permission.id=user_permission.permission_id
			where user_permission.user_id=$1 and permission.code='user.message.receive' and (user_permission.expires_at is null or user_permission.expires_at>now())),false)`, claims.Subject).Scan(&messageReceive); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read updated message preference")
			return
		}
		snapshot, marshalErr := json.Marshal(map[string]any{
			"userId": claims.PublicSubject, "signature": signature, "avatarUrl": avatarURL,
			"avatarFileId": avatarFileID, "messageReceive": messageReceive,
		})
		if marshalErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode user profile revision")
			return
		}
		created, createErr := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
			EntityType: "user", EntityID: claims.Subject,
			AggregateType: "user_profile", AggregateKey: claims.PublicSubject,
			BaseRevision: baseRevisionID, Snapshot: snapshot, Reason: "Update user profile settings",
			ActorID: claims.Subject, Source: "user", Status: "approved",
			Metadata: map[string]any{"userId": claims.Subject}, Request: r,
		})
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to create user profile revision")
			return
		}
		if _, err = tx.Exec(r.Context(), `update users set profile_revision_id=$2 where id=$1`, claims.Subject, created.RevisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish user profile revision")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic profile approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record user profile revision")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存用户设置失败")
		return
	}
	settings, err := s.loadUserProfileSettings(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户设置失败")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) loadUserProfileSettings(ctx context.Context, userID int64) (userProfileSettingsResponse, error) {
	var response userProfileSettingsResponse
	if err := s.db.QueryRow(ctx, `select signature, avatar_url, profile_background_url from users where id = $1`, userID).
		Scan(&response.Signature, &response.AvatarURL, &response.ProfileBackgroundURL); err != nil {
		return response, err
	}
	response.SignatureMaxBytes = s.profileConfigFromSettings(ctx).SignatureMaxBytes
	response.MessageReceive = s.userHasPermission(ctx, userID, "user.message.receive")
	response.CanUpdateAvatar = s.userHasPermission(ctx, userID, "user.avatar.update")
	response.CanUseAnimatedAvatar = s.userHasPermission(ctx, userID, "user.avatar.animated")
	return response, nil
}

func (s *Server) profileConfigFromSettings(ctx context.Context) profileConfigPayload {
	payload := defaultProfileConfig()
	var raw []byte
	if err := s.db.QueryRow(ctx, `select value from system_settings where key = $1`, profileConfigSettingKey).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &payload)
	}
	return normalizeProfileConfig(payload)
}

func defaultProfileConfig() profileConfigPayload {
	return profileConfigPayload{SignatureMaxBytes: 256}
}

func normalizeProfileConfig(payload profileConfigPayload) profileConfigPayload {
	if payload.SignatureMaxBytes <= 0 {
		payload.SignatureMaxBytes = 256
	}
	if payload.SignatureMaxBytes > 4096 {
		payload.SignatureMaxBytes = 4096
	}
	return payload
}

func isSupportedAvatarImage(name string, contentType string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch extension {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".apng":
	default:
		return false
	}
	return strings.HasPrefix(contentType, "image/") && contentType != "image/svg+xml"
}

func isAnimatedAvatar(name string, contentType string) bool {
	extension := strings.ToLower(filepath.Ext(name))
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	return extension == ".gif" || extension == ".apng" || contentType == "image/gif" || contentType == "image/apng"
}

func (s *Server) isAnimatedAvatarObject(ctx context.Context, objectKey string, name string, contentType string) (bool, error) {
	if isAnimatedAvatar(name, contentType) {
		return true, nil
	}
	extension := strings.ToLower(filepath.Ext(name))
	contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if extension != ".png" && contentType != "image/png" {
		return false, nil
	}
	client, cfg, err := s.ossClient(ctx)
	if err != nil {
		return false, err
	}
	result, err := client.GetObject(ctx, &aliyunoss.GetObjectRequest{
		Bucket: aliyunoss.Ptr(cfg.Bucket),
		Key:    aliyunoss.Ptr(objectKey),
	})
	if err != nil {
		return false, err
	}
	defer result.Body.Close()
	return readAPNGAnimationControl(result.Body)
}

func readAPNGAnimationControl(reader io.Reader) (bool, error) {
	signature := make([]byte, 8)
	if _, err := io.ReadFull(reader, signature); err != nil {
		return false, err
	}
	want := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	if string(signature) != string(want) {
		return false, fmt.Errorf("invalid PNG signature")
	}
	for {
		var length uint32
		if err := binary.Read(reader, binary.BigEndian, &length); err != nil {
			return false, err
		}
		chunkType := make([]byte, 4)
		if _, err := io.ReadFull(reader, chunkType); err != nil {
			return false, err
		}
		switch string(chunkType) {
		case "acTL":
			return true, nil
		case "IDAT", "IEND":
			return false, nil
		}
		if _, err := io.CopyN(io.Discard, reader, int64(length)+4); err != nil {
			return false, err
		}
	}
}
