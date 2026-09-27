package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const projectAuthorshipCursorVersion = 1

type projectAuthorshipPageRequest struct {
	Status string
	Limit  int
	Scope  string
	Cursor *projectAuthorshipPageCursor
}

type projectAuthorshipPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

func parseProjectAuthorshipPageRequest(values url.Values, userID int64, canManageAuthors, canManageTeams bool) (projectAuthorshipPageRequest, error) {
	status := strings.ToLower(strings.TrimSpace(values.Get("status")))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" && status != "revoked" {
		return projectAuthorshipPageRequest{}, errors.New("relationship status is invalid")
	}
	limit := 50
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return projectAuthorshipPageRequest{}, errors.New("relationship page limit is invalid")
		}
		limit = parsed
	}
	scope := projectAuthorshipPageScope(status, limit, userID, canManageAuthors, canManageTeams)
	cursor, err := decodeProjectAuthorshipPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return projectAuthorshipPageRequest{}, err
	}
	return projectAuthorshipPageRequest{Status: status, Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func projectAuthorshipPageScope(status string, limit int, userID int64, canManageAuthors, canManageTeams bool) string {
	material, _ := json.Marshal(struct {
		Version          int    `json:"version"`
		Status           string `json:"status"`
		Limit            int    `json:"limit"`
		UserID           int64  `json:"userId"`
		CanManageAuthors bool   `json:"canManageAuthors"`
		CanManageTeams   bool   `json:"canManageTeams"`
	}{projectAuthorshipCursorVersion, status, limit, userID, canManageAuthors, canManageTeams})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeProjectAuthorshipPageCursor(cursor projectAuthorshipPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProjectAuthorshipPageCursor(raw, scope string) (*projectAuthorshipPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid project authorship cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid project authorship cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor projectAuthorshipPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid project authorship cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != projectAuthorshipCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid project authorship cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}
