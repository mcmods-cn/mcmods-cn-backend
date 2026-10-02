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
	"unicode/utf8"
)

const projectFollowPageCursorVersion = 1

type projectFollowPageRequest struct {
	Query      string
	TargetType string
	Limit      int
	Scope      string
	Cursor     *projectFollowPageCursor
}

type projectFollowPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"createdAt"`
	RouteID   int64     `json:"routeId"`
}

func parseProjectFollowPageRequest(values url.Values, userID int64) (projectFollowPageRequest, error) {
	allowed := map[string]bool{"q": true, "type": true, "limit": true, "cursor": true}
	for key, items := range values {
		if !allowed[key] {
			return projectFollowPageRequest{}, fmt.Errorf("unsupported project follow query parameter %q", key)
		}
		if len(items) != 1 {
			return projectFollowPageRequest{}, fmt.Errorf("project follow query parameter %q must appear exactly once", key)
		}
	}
	request := projectFollowPageRequest{Query: strings.ToLower(strings.TrimSpace(values.Get("q"))), Limit: 40}
	if utf8.RuneCountInString(request.Query) > 200 || len(request.Query) > 800 {
		return projectFollowPageRequest{}, errors.New("project follow search query is too long")
	}
	rawType := strings.TrimSpace(values.Get("type"))
	request.TargetType = normalizeFollowableProjectType(rawType)
	if rawType != "" && request.TargetType == "" {
		return projectFollowPageRequest{}, errors.New("invalid followed project type")
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return projectFollowPageRequest{}, errors.New("invalid project follow page size")
		}
		request.Limit = parsed
	}
	request.Scope = projectFollowPageScope(request, userID)
	cursor, err := decodeProjectFollowPageCursor(values.Get("cursor"), request.Scope)
	if err != nil {
		return projectFollowPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func normalizeFollowableProjectType(value string) string {
	value = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	if value == "server" {
		value = "minecraft_server"
	}
	if _, exists := followableProjectTypes[value]; exists {
		return value
	}
	return ""
}

func projectFollowPageScope(request projectFollowPageRequest, userID int64) string {
	material, _ := json.Marshal(struct {
		Version    int    `json:"version"`
		UserID     int64  `json:"userId"`
		Query      string `json:"query"`
		TargetType string `json:"type"`
		Limit      int    `json:"limit"`
	}{projectFollowPageCursorVersion, userID, request.Query, request.TargetType, request.Limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeProjectFollowPageCursor(cursor projectFollowPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProjectFollowPageCursor(raw, scope string) (*projectFollowPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid project follow cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid project follow cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor projectFollowPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid project follow cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != projectFollowPageCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.RouteID <= 0 {
		return nil, errors.New("invalid project follow cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}
