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
)

const (
	modRevisionHistoryCursorVersion = 1
	modRevisionHistoryDefaultLimit  = 50
	modRevisionHistoryMaxLimit      = 100
	modRevisionHistoryMaxCursorSize = 2048
)

type modRevisionHistoryPageRequest struct {
	Limit  int
	Scope  string
	Cursor *modRevisionHistoryPageCursor
}

type modRevisionHistoryPageCursor struct {
	Version    int    `json:"v"`
	Scope      string `json:"s"`
	RevisionNo int64  `json:"revisionNo"`
}

func parseModRevisionHistoryPageRequest(values url.Values, projectID string) (modRevisionHistoryPageRequest, error) {
	limit := modRevisionHistoryDefaultLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > modRevisionHistoryMaxLimit {
			return modRevisionHistoryPageRequest{}, errors.New("invalid revision history page size")
		}
		limit = parsed
	}
	scope := modRevisionHistoryPageScope(projectID, limit)
	cursor, err := decodeModRevisionHistoryPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return modRevisionHistoryPageRequest{}, err
	}
	return modRevisionHistoryPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func modRevisionHistoryPageScope(projectID string, limit int) string {
	material, _ := json.Marshal(struct {
		Version   int    `json:"version"`
		ProjectID string `json:"projectId"`
		Limit     int    `json:"limit"`
	}{modRevisionHistoryCursorVersion, strings.ToLower(strings.TrimSpace(projectID)), limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeModRevisionHistoryPageCursor(cursor modRevisionHistoryPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeModRevisionHistoryPageCursor(raw, scope string) (*modRevisionHistoryPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > modRevisionHistoryMaxCursorSize {
		return nil, errors.New("invalid revision history cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid revision history cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor modRevisionHistoryPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid revision history cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) ||
		cursor.Version != modRevisionHistoryCursorVersion || cursor.Scope != scope || cursor.RevisionNo <= 0 {
		return nil, errors.New("invalid revision history cursor")
	}
	return &cursor, nil
}

func modRevisionHistoryPageSQL(projectID string, visibility pendingReviewVisibility, request modRevisionHistoryPageRequest) (string, []any) {
	query := modRevisionSelect + `
		where revision.aggregate_type='mod' and revision.aggregate_key=$1
		  and ($2 or request.status='approved' or ($3 and coalesce(request.submitted_by,0)>0 and request.submitted_by<>$4))`
	arguments := []any{projectID, visibility.includeAll, visibility.includeScoped, visibility.reviewerID}
	if request.Cursor != nil {
		query += ` and revision.revision_no<$5`
		arguments = append(arguments, request.Cursor.RevisionNo)
	}
	query += fmt.Sprintf(` order by revision.revision_no desc limit $%d`, len(arguments)+1)
	arguments = append(arguments, request.Limit+1)
	return query, arguments
}
