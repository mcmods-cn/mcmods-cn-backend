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
	skinWardrobePageCursorVersion = 1
	defaultSkinWardrobePageLimit  = 50
	maximumSkinWardrobePageLimit  = 100
)

type skinWardrobePageRequest struct {
	UserID int64
	Kind   string
	Model  string
	Limit  int
	Scope  string
	Cursor *skinWardrobePageCursor
}

type skinWardrobePageCursor struct {
	Version int       `json:"v"`
	Scope   string    `json:"scope"`
	AddedAt time.Time `json:"addedAt"`
	AssetID int64     `json:"assetId"`
}

type skinWardrobePageRow struct {
	AddedAt time.Time
	Asset   skinAssetRecord
}

func parseSkinWardrobePageRequest(values url.Values, userID int64) (skinWardrobePageRequest, error) {
	if userID <= 0 {
		return skinWardrobePageRequest{}, errors.New("invalid wardrobe owner")
	}
	allowed := map[string]bool{"kind": true, "model": true, "limit": true, "cursor": true}
	for key, items := range values {
		if !allowed[key] {
			return skinWardrobePageRequest{}, fmt.Errorf("unsupported wardrobe query parameter %q", key)
		}
		if len(items) != 1 {
			return skinWardrobePageRequest{}, fmt.Errorf("wardrobe query parameter %q must appear exactly once", key)
		}
	}
	request := skinWardrobePageRequest{
		UserID: userID,
		Kind:   strings.ToLower(strings.TrimSpace(values.Get("kind"))),
		Model:  strings.ToLower(strings.TrimSpace(values.Get("model"))),
		Limit:  defaultSkinWardrobePageLimit,
	}
	if request.Kind != "" && request.Kind != "skin" && request.Kind != "cape" {
		return skinWardrobePageRequest{}, errors.New("wardrobe kind must be skin or cape")
	}
	if request.Model != "" && request.Model != "default" && request.Model != "slim" {
		return skinWardrobePageRequest{}, errors.New("wardrobe model must be default or slim")
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > maximumSkinWardrobePageLimit {
			return skinWardrobePageRequest{}, errors.New("invalid wardrobe page size")
		}
		request.Limit = parsed
	}
	request.Scope = skinWardrobePageScope(request)
	cursor, err := decodeSkinWardrobePageCursor(values.Get("cursor"), request.Scope)
	if err != nil {
		return skinWardrobePageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func skinWardrobePageScope(request skinWardrobePageRequest) string {
	payload, _ := json.Marshal(struct {
		Version int    `json:"version"`
		UserID  int64  `json:"userId"`
		Kind    string `json:"kind"`
		Model   string `json:"model"`
		Limit   int    `json:"limit"`
	}{skinWardrobePageCursorVersion, request.UserID, request.Kind, request.Model, request.Limit})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func encodeSkinWardrobePageCursor(cursor skinWardrobePageCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeSkinWardrobePageCursor(raw, scope string) (*skinWardrobePageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid wardrobe cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > 2048 {
		return nil, errors.New("invalid wardrobe cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor skinWardrobePageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid wardrobe cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) ||
		cursor.Version != skinWardrobePageCursorVersion || cursor.Scope != scope ||
		cursor.AddedAt.IsZero() || cursor.AssetID <= 0 {
		return nil, errors.New("invalid wardrobe cursor")
	}
	cursor.AddedAt = cursor.AddedAt.UTC()
	return &cursor, nil
}

func skinWardrobePageSQL(request skinWardrobePageRequest) (string, []any) {
	query := skinWardrobePageSelectSQL + `
		join skin_wardrobe wardrobe on wardrobe.asset_id=asset.id`
	where, arguments := skinWardrobeWhere(request)
	query += " where " + strings.Join(where, " and ")
	if request.Cursor != nil {
		arguments = append(arguments, request.Cursor.AddedAt, request.Cursor.AssetID)
		query += fmt.Sprintf(" and (wardrobe.added_at<$%d or (wardrobe.added_at=$%d and wardrobe.asset_id>$%d))",
			len(arguments)-1, len(arguments)-1, len(arguments))
	}
	arguments = append(arguments, request.Limit+1)
	query += fmt.Sprintf(" order by wardrobe.added_at desc,wardrobe.asset_id limit $%d", len(arguments))
	return query, arguments
}

func skinWardrobeCountSQL(request skinWardrobePageRequest) (string, []any) {
	where, arguments := skinWardrobeWhere(request)
	return `select count(*) from skin_wardrobe wardrobe
		join skin_assets asset on asset.id=wardrobe.asset_id
		where ` + strings.Join(where, " and "), arguments
}

func skinWardrobeWhere(request skinWardrobePageRequest) ([]string, []any) {
	where := []string{
		"wardrobe.user_id=$1",
		"asset.status='active'",
		"(asset.owner_id=$1 or (asset.review_status='approved' and asset.visibility in ('public','unlisted')))",
	}
	arguments := []any{request.UserID}
	if request.Kind != "" {
		arguments = append(arguments, request.Kind)
		where = append(where, fmt.Sprintf("asset.kind=$%d", len(arguments)))
	}
	if request.Model != "" {
		arguments = append(arguments, request.Model)
		where = append(where, fmt.Sprintf("asset.model=$%d", len(arguments)))
	}
	return where, arguments
}

func skinWardrobeNextCursor(request skinWardrobePageRequest, row skinWardrobePageRow) string {
	return encodeSkinWardrobePageCursor(skinWardrobePageCursor{
		Version: skinWardrobePageCursorVersion, Scope: request.Scope,
		AddedAt: row.AddedAt.UTC(), AssetID: row.Asset.ID,
	})
}

func scanSkinWardrobePageRow(row skinRowScanner) (skinWardrobePageRow, error) {
	var pageRow skinWardrobePageRow
	destinations := append([]any{&pageRow.AddedAt}, skinAssetScanDestinations(&pageRow.Asset)...)
	return pageRow, row.Scan(destinations...)
}
