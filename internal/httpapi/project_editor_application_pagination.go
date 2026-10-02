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

const projectEditorApplicationCursorVersion = 1

type projectEditorApplicationPageRequest struct {
	Status string
	Limit  int
	Scope  string
	Cursor *projectEditorApplicationPageCursor
}

type projectEditorApplicationPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

func parseProjectEditorApplicationPageRequest(values url.Values) (projectEditorApplicationPageRequest, error) {
	for key, entries := range values {
		if (key != "status" && key != "limit" && key != "cursor") || len(entries) != 1 {
			return projectEditorApplicationPageRequest{}, errors.New("invalid editor application page")
		}
	}
	status := strings.ToLower(strings.TrimSpace(values.Get("status")))
	if status == "" {
		status = "pending"
	}
	if status != "pending" && status != "approved" && status != "rejected" && status != "withdrawn" {
		return projectEditorApplicationPageRequest{}, errors.New("invalid editor application status")
	}
	limit := 50
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return projectEditorApplicationPageRequest{}, errors.New("invalid editor application page size")
		}
		limit = parsed
	}
	scope := projectEditorApplicationPageScope(status, limit)
	cursor, err := decodeProjectEditorApplicationPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return projectEditorApplicationPageRequest{}, err
	}
	return projectEditorApplicationPageRequest{Status: status, Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func projectEditorApplicationPageScope(status string, limit int) string {
	material, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Status  string `json:"status"`
		Limit   int    `json:"limit"`
	}{projectEditorApplicationCursorVersion, status, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeProjectEditorApplicationPageCursor(cursor projectEditorApplicationPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProjectEditorApplicationPageCursor(raw, scope string) (*projectEditorApplicationPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid editor application cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid editor application cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor projectEditorApplicationPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid editor application cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != projectEditorApplicationCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid editor application cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}
