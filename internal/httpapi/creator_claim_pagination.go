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

const creatorClaimCursorVersion = 1

type creatorClaimPageRequest struct {
	Limit  int
	Scope  string
	Cursor *creatorClaimPageCursor
}

type creatorClaimPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

func parseCreatorClaimPageRequest(values url.Values) (creatorClaimPageRequest, error) {
	for key, entries := range values {
		if (key != "limit" && key != "cursor") || len(entries) != 1 {
			return creatorClaimPageRequest{}, errors.New("invalid creator claim page")
		}
	}
	limit := 50
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			return creatorClaimPageRequest{}, errors.New("invalid creator claim page size")
		}
		limit = parsed
	}
	scope := creatorClaimPageScope(limit)
	cursor, err := decodeCreatorClaimPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return creatorClaimPageRequest{}, err
	}
	return creatorClaimPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func creatorClaimPageScope(limit int) string {
	material, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Queue   string `json:"queue"`
		Limit   int    `json:"limit"`
	}{creatorClaimCursorVersion, "pending-author", limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeCreatorClaimPageCursor(cursor creatorClaimPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCreatorClaimPageCursor(raw, scope string) (*creatorClaimPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid creator claim cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid creator claim cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor creatorClaimPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid creator claim cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != creatorClaimCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid creator claim cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}
