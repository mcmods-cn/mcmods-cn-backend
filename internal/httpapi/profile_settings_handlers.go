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
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

const profileConfigSettingKey = "profile.config"

type profileConfigPayload struct {
	SignatureMaxBytes int `json:"signatureMaxBytes"`
}

type userProfileSettingsRequest struct {
	Username            *string   `json:"username,omitempty"`
	Signature           *string   `json:"signature,omitempty"`
	Timezone            *string   `json:"timezone,omitempty"`
	MessageReceive      *bool     `json:"messageReceive,omitempty"`
	ShowOnlineStatus    *bool     `json:"showOnlineStatus,omitempty"`
	PublicCardStatSlots *[]string `json:"publicCardStatSlots,omitempty"`
	AvatarFileID        *string   `json:"avatarFileId,omitempty"`
	ClearAvatar         bool      `json:"clearAvatar,omitempty"`
}

type userProfileSettingsResponse struct {
	PublicID             string             `json:"publicId"`
	Username             string             `json:"username"`
	Signature            string             `json:"signature"`
	SignatureMaxBytes    int                `json:"signatureMaxBytes"`
	AvatarURL            string             `json:"avatarUrl"`
	ProfileBackgroundURL string             `json:"profileBackgroundUrl"`
	Timezone             string             `json:"timezone"`
	MessageReceive       bool               `json:"messageReceive"`
	ShowOnlineStatus     bool               `json:"showOnlineStatus"`
	OnlineStatus         publicOnlineStatus `json:"onlineStatus"`
	PublicCardStatSlots  []string           `json:"publicCardStatSlots"`
	CardStatisticOptions []string           `json:"cardStatisticOptions"`
	CanUpdateAvatar      bool               `json:"canUpdateAvatar"`
	CanUseAnimatedAvatar bool               `json:"canUseAnimatedAvatar"`
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
	s.invalidateSettingsCache(r.Context())
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
	if request.Username != nil {
		value := strings.TrimSpace(*request.Username)
		if err := validateUsername(value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		request.Username = &value
	}
	if request.Signature != nil {
		value := strings.TrimSpace(*request.Signature)
		if len([]byte(value)) > config.SignatureMaxBytes {
			writeError(w, http.StatusBadRequest, "签名超过最大字节数限制")
			return
		}
		request.Signature = &value
	}
	if request.Timezone != nil {
		value, normalizeErr := normalizeTimezone(*request.Timezone)
		if normalizeErr != nil {
			writeError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		request.Timezone = &value
	}
	if request.PublicCardStatSlots != nil {
		values, normalizeErr := normalizePublicCardSlots(*request.PublicCardStatSlots)
		if normalizeErr != nil {
			writeError(w, http.StatusBadRequest, normalizeErr.Error())
			return
		}
		request.PublicCardStatSlots = &values
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存用户设置失败")
		return
	}
	defer tx.Rollback(r.Context())
	var baseRevisionID *int64
	var currentTimezone string
	if err = tx.QueryRow(r.Context(), `select profile_revision_id,timezone from users where id=$1 for update`, claims.Subject).Scan(&baseRevisionID, &currentTimezone); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock user profile")
		return
	}
	timezoneChanged := request.Timezone != nil && *request.Timezone != currentTimezone
	if request.Username != nil {
		if _, err := tx.Exec(r.Context(), `update users set username=$2,updated_at=now() where id=$1`, claims.Subject, *request.Username); err != nil {
			writeError(w, http.StatusConflict, "用户名已被占用")
			return
		}
	}

	if request.Signature != nil {
		if _, err := tx.Exec(r.Context(), `update users set signature = $2, updated_at = now() where id = $1`, claims.Subject, *request.Signature); err != nil {
			writeError(w, http.StatusInternalServerError, "保存用户签名失败")
			return
		}
	}
	if timezoneChanged {
		if _, err := tx.Exec(r.Context(), `update users set timezone=$2,updated_at=now() where id=$1`, claims.Subject, *request.Timezone); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save user timezone")
			return
		}
		if _, err := tx.Exec(r.Context(), `insert into user_timezone_changes(user_id,old_timezone,new_timezone) values($1,$2,$3)`, claims.Subject, currentTimezone, *request.Timezone); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record user timezone change")
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
			`insert into user_permissions (user_id,permission_id,allow,source,source_key,updated_at)
			 select $1,id,$3,$4,'profile_settings',now() from permissions where code=$2
			 on conflict (user_id,permission_id,source,source_key) do update
			 set allow = excluded.allow, expires_at = null, updated_at = now()`,
			claims.Subject,
			"user.message.receive",
			*request.MessageReceive,
			directPermissionSourcePreference,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "保存私聊权限失败")
			return
		}
	}
	if request.ShowOnlineStatus != nil {
		if _, err := tx.Exec(r.Context(), `update users set show_online_status=$2,updated_at=now() where id=$1`, claims.Subject, *request.ShowOnlineStatus); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save online status privacy")
			return
		}
	}
	if request.PublicCardStatSlots != nil {
		if _, err := tx.Exec(r.Context(), `update users set public_card_stat_slots=$2,updated_at=now() where id=$1`, claims.Subject, *request.PublicCardStatSlots); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save public card statistics")
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
		file, resolveErr := resolveTrustedRasterOSSFilePublicID(r.Context(), tx, *request.AvatarFileID, ossRasterBindingScope{UploaderID: claims.Subject})
		if resolveErr != nil {
			writeError(w, http.StatusBadRequest, "avatar file does not exist")
			return
		}
		if !isSupportedAvatarImage(file.OriginalName, file.ContentType) {
			writeError(w, http.StatusBadRequest, "头像仅支持 PNG、JPEG、WebP、GIF 或 APNG 图片")
			return
		}
		animated, err := s.isAnimatedAvatarObject(r.Context(), file.ObjectKey, file.OriginalName, file.ContentType)
		if err != nil {
			writeError(w, http.StatusBadGateway, "无法验证头像文件是否为动态图")
			return
		}
		if animated && !s.userHasPermission(r.Context(), claims.Subject, "user.avatar.animated") {
			writeError(w, http.StatusForbidden, "没有使用动态头像权限")
			return
		}
		avatarURL := ossStoredObjectURL(s.ossConfigFromSettings(r.Context()), file.ObjectKey)
		if _, err := tx.Exec(
			r.Context(),
			`update users set avatar_file_id = $2, avatar_url = $3, updated_at = now() where id = $1`,
			claims.Subject,
			file.ID,
			avatarURL,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "保存头像失败")
			return
		}
	}
	if request.Username != nil || request.Signature != nil || timezoneChanged || request.MessageReceive != nil || request.ShowOnlineStatus != nil || request.PublicCardStatSlots != nil || request.ClearAvatar || request.AvatarFileID != nil {
		var username, signature, avatarURL, timezone string
		var showOnlineStatus bool
		var publicCardStatSlots []string
		var avatarFileID *string
		if err = tx.QueryRow(r.Context(), `select username,signature,avatar_url,(select public_id from oss_files where id=users.avatar_file_id),timezone,show_online_status,public_card_stat_slots from users where id=$1`, claims.Subject).Scan(&username, &signature, &avatarURL, &avatarFileID, &timezone, &showOnlineStatus, &publicCardStatSlots); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read updated user profile")
			return
		}
		var messageReceive bool
		if err = tx.QueryRow(r.Context(), `select coalesce((select bool_and(user_permission.allow) from user_permissions user_permission
			join permissions permission on permission.id=user_permission.permission_id
			where user_permission.user_id=$1 and permission.code='user.message.receive' and (user_permission.expires_at is null or user_permission.expires_at>now())),false)`, claims.Subject).Scan(&messageReceive); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read updated message preference")
			return
		}
		snapshot, marshalErr := json.Marshal(map[string]any{
			"userId": claims.PublicSubject, "username": username, "signature": signature, "avatarUrl": avatarURL,
			"avatarFileId": avatarFileID, "timezone": timezone, "messageReceive": messageReceive,
			"showOnlineStatus": showOnlineStatus, "publicCardStatSlots": publicCardStatSlots,
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
	if request.MessageReceive != nil {
		if !s.requireSecurityVersionRefresh(w, r, "update_message_receive_permission", claims.Subject,
			s.refreshPermissionVersion(r.Context(), claims.Subject)) {
			return
		}
	}
	s.cache.Delete(r.Context(), publicUserCardCacheKey(claims.Subject))
	settings, err := s.loadUserProfileSettings(r.Context(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取用户设置失败")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) loadUserProfileSettings(ctx context.Context, userID int64) (userProfileSettingsResponse, error) {
	var response userProfileSettingsResponse
	if err := s.db.QueryRow(ctx, `select public_id,username,signature,avatar_url,profile_background_url,timezone,show_online_status,
		public_card_stat_slots from users where id=$1`, userID).
		Scan(&response.PublicID, &response.Username, &response.Signature, &response.AvatarURL, &response.ProfileBackgroundURL, &response.Timezone, &response.ShowOnlineStatus, &response.PublicCardStatSlots); err != nil {
		return response, err
	}
	ossCfg := s.ossConfigFromSettings(ctx)
	var err error
	response.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, response.AvatarURL)
	if err != nil {
		return response, err
	}
	response.ProfileBackgroundURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, response.ProfileBackgroundURL)
	if err != nil {
		return response, err
	}
	response.SignatureMaxBytes = s.profileConfigFromSettings(ctx).SignatureMaxBytes
	onlineActive := s.cache.UsersOnline(ctx, []int64{userID}, time.Now(), s.cache.Config().PresenceTTL)[userID]
	response.OnlineStatus = mapPublicOnlineVisibility(response.ShowOnlineStatus, onlineActive)
	response.CardStatisticOptions = append([]string(nil), publicCardStatisticOptionKeys...)
	response.MessageReceive = s.userHasPermission(ctx, userID, "user.message.receive")
	response.CanUpdateAvatar = s.userHasPermission(ctx, userID, "user.avatar.update")
	response.CanUseAnimatedAvatar = s.userHasPermission(ctx, userID, "user.avatar.animated")
	return response, nil
}

func (s *Server) profileConfigFromSettings(ctx context.Context) profileConfigPayload {
	payload := defaultProfileConfig()
	var raw []byte
	if cached, err := s.loadCachedPublicSetting(ctx, profileConfigSettingKey); err == nil {
		raw = cached
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
