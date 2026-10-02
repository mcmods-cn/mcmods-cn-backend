package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultFavoriteExportPageLimit = 30
	maximumFavoriteExportPageLimit = 100
	maximumFavoriteExportCursor    = 2048
)

type favoriteExportPageRequest struct {
	OwnerUserID    int64
	Status         string
	Limit          int
	AfterCreatedAt time.Time
	AfterID        int64
}

type favoriteExportPageCursor struct {
	Version   int    `json:"v"`
	CreatedAt string `json:"createdAt"`
	ID        int64  `json:"id"`
	Scope     string `json:"scope"`
}

func parseFavoriteExportPageRequest(values url.Values, ownerUserID int64) (favoriteExportPageRequest, error) {
	request := favoriteExportPageRequest{OwnerUserID: ownerUserID, Status: "all", Limit: defaultFavoriteExportPageLimit}
	if ownerUserID <= 0 {
		return request, errors.New("invalid export history owner")
	}
	allowed := map[string]bool{"status": true, "limit": true, "cursor": true}
	for key, entries := range values {
		if !allowed[key] || len(entries) != 1 {
			return request, fmt.Errorf("invalid export history parameter %q", key)
		}
	}
	if raw := values.Get("status"); raw != "" {
		normalized := strings.ToLower(strings.TrimSpace(raw))
		if normalized != raw || !validFavoriteExportStatus(normalized) {
			return request, errors.New("invalid export history status")
		}
		request.Status = normalized
	}
	if raw := values.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || strconv.Itoa(limit) != raw || limit < 1 || limit > maximumFavoriteExportPageLimit {
			return request, errors.New("invalid export history limit")
		}
		request.Limit = limit
	}
	if raw := values.Get("cursor"); raw != "" {
		if len(raw) > maximumFavoriteExportCursor {
			return request, errors.New("export history cursor is too long")
		}
		payload, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil {
			return request, errors.New("invalid export history cursor")
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var cursor favoriteExportPageCursor
		if err = decoder.Decode(&cursor); err != nil {
			return request, errors.New("invalid export history cursor")
		}
		if err = ensureJSONEOF(decoder); err != nil {
			return request, errors.New("invalid export history cursor")
		}
		createdAt, err := time.Parse(time.RFC3339Nano, cursor.CreatedAt)
		if err != nil || createdAt.Location() != time.UTC || createdAt.Format(time.RFC3339Nano) != cursor.CreatedAt ||
			cursor.Version != 1 || cursor.ID <= 0 || cursor.Scope != favoriteExportPageScope(request) {
			return request, errors.New("invalid export history cursor")
		}
		request.AfterCreatedAt, request.AfterID = createdAt, cursor.ID
	}
	return request, nil
}

func validFavoriteExportStatus(status string) bool {
	switch status {
	case "all", "pending", "processing", "ready", "failed", "expired", "cancelled":
		return true
	default:
		return false
	}
}

func favoriteExportPageScope(request favoriteExportPageRequest) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%d", request.OwnerUserID, request.Status, request.Limit)))
	return hex.EncodeToString(digest[:])
}

func encodeFavoriteExportPageCursor(request favoriteExportPageRequest, createdAt time.Time, id int64) (string, error) {
	if id <= 0 || createdAt.IsZero() {
		return "", errors.New("invalid export history cursor anchor")
	}
	payload, err := json.Marshal(favoriteExportPageCursor{
		Version: 1, CreatedAt: createdAt.UTC().Format(time.RFC3339Nano), ID: id, Scope: favoriteExportPageScope(request),
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func favoriteExportPageSQL(request favoriteExportPageRequest) (string, []any) {
	query := `select task.id,task.public_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,
		loader_version,allow_compatible_only,report_version,status,collection_item_count,exported_mod_count,auto_dependency_count,skipped_item_count,
		failed_item_count,final_file_count,result_file_size,result_sha256,error_code,task.created_at,finished_at,expires_at
		from favorite_modpack_export_tasks task
		where task.owner_user_id=$1`
	arguments := []any{request.OwnerUserID}
	if request.Status != "all" {
		query += " and task.status=$2"
		arguments = append(arguments, request.Status)
	}
	if request.AfterID != 0 {
		query += fmt.Sprintf(" and (task.created_at,task.id)<($%d,$%d)", len(arguments)+1, len(arguments)+2)
		arguments = append(arguments, request.AfterCreatedAt, request.AfterID)
	}
	query += fmt.Sprintf(" order by task.created_at desc,task.id desc limit $%d", len(arguments)+1)
	arguments = append(arguments, request.Limit+1)
	return query, arguments
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("unexpected trailing JSON")
	}
	return nil
}
