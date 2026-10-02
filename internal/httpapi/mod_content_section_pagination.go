package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	modContentSectionCursorVersion  = 1
	maxModContentSectionCursorBytes = 2048
	maxModContentSectionPageSize    = 200
	maxModContentLayoutPageSize     = 1000
)

type modContentSectionPageCursor struct {
	Version    int     `json:"v"`
	Scope      string  `json:"s"`
	SortPath   []int64 `json:"p"`
	Ordinal    int     `json:"o"`
	ResourceID int64   `json:"r"`
	Total      int     `json:"t"`
}

func modContentSectionCursorScope(modID, sectionID, versionID int64, revisionID, query string, limit int, mode string) string {
	raw := fmt.Sprintf("v1\x00%d\x00%d\x00%d\x00%s\x00%s\x00%d\x00%s",
		modID, sectionID, versionID, strings.TrimSpace(revisionID), strings.ToLower(strings.TrimSpace(query)), limit, mode)
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func encodeModContentSectionCursor(cursor modContentSectionPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeModContentSectionCursor(raw, scope string) (*modContentSectionPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > maxModContentSectionCursorBytes {
		return nil, errors.New("invalid mod-content section cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > maxModContentSectionCursorBytes {
		return nil, errors.New("invalid mod-content section cursor")
	}
	decoder := json.NewDecoder(strings.NewReader(string(decoded)))
	decoder.DisallowUnknownFields()
	var cursor modContentSectionPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid mod-content section cursor")
	}
	if err = decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) ||
		cursor.Version != modContentSectionCursorVersion || cursor.Scope != scope ||
		cursor.Ordinal < 0 || cursor.ResourceID <= 0 || cursor.Total < 0 ||
		len(cursor.SortPath) < 2 || len(cursor.SortPath) > 2*(maxModContentCategoryDepth+1) {
		return nil, errors.New("invalid mod-content section cursor")
	}
	for index, value := range cursor.SortPath {
		if value < 0 || (index%2 == 1 && value == 0) {
			return nil, errors.New("invalid mod-content section cursor")
		}
	}
	return &cursor, nil
}
