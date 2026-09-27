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
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	contentHistoryCursorVersion = 1
	contentHistoryDefaultLimit  = 50
	contentHistoryMaxLimit      = 100
	contentHistoryMaxCursorSize = 2048
)

type contentHistoryPageRequest struct {
	Limit  int
	Scope  string
	Cursor *contentHistoryPageCursor
}

type contentHistoryPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"at"`
	Origin    string    `json:"o"`
	ID        string    `json:"id"`
}

type contentHistoryPage struct {
	Items      []contentHistoryItem `json:"items"`
	Limit      int                  `json:"limit"`
	HasMore    bool                 `json:"hasMore"`
	NextCursor string               `json:"nextCursor"`
}

func parseContentHistoryPageRequest(values url.Values, target string, allowedExtra ...string) (contentHistoryPageRequest, error) {
	allowed := map[string]struct{}{"limit": {}, "cursor": {}}
	for _, key := range allowedExtra {
		allowed[key] = struct{}{}
	}
	for key, entries := range values {
		if _, ok := allowed[key]; !ok || len(entries) != 1 {
			return contentHistoryPageRequest{}, errors.New("invalid content history query")
		}
	}
	limit := contentHistoryDefaultLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > contentHistoryMaxLimit {
			return contentHistoryPageRequest{}, errors.New("invalid content history page size")
		}
		limit = parsed
	}
	scope := contentHistoryPageScope(target, limit)
	cursor, err := decodeContentHistoryPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return contentHistoryPageRequest{}, err
	}
	return contentHistoryPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func contentHistoryPageScope(target string, limit int) string {
	material, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Target  string `json:"target"`
		Limit   int    `json:"limit"`
	}{contentHistoryCursorVersion, strings.ToLower(strings.TrimSpace(target)), limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeContentHistoryPageCursor(cursor contentHistoryPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeContentHistoryPageCursor(raw, scope string) (*contentHistoryPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > contentHistoryMaxCursorSize {
		return nil, errors.New("invalid content history cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid content history cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor contentHistoryPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid content history cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != contentHistoryCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || (cursor.Origin != "manual" && cursor.Origin != "import") ||
		strings.TrimSpace(cursor.ID) == "" || len(cursor.ID) > 256 {
		return nil, errors.New("invalid content history cursor")
	}
	return &cursor, nil
}

func mergeContentHistoryPage(manual, imports []contentHistoryItem, request contentHistoryPageRequest) contentHistoryPage {
	items := make([]contentHistoryItem, 0, len(manual)+len(imports))
	items = append(items, manual...)
	items = append(items, imports...)
	sort.Slice(items, func(left, right int) bool {
		if !items[left].CreatedAt.Equal(items[right].CreatedAt) {
			return items[left].CreatedAt.After(items[right].CreatedAt)
		}
		leftRank, rightRank := contentHistoryOriginRank(items[left].Origin), contentHistoryOriginRank(items[right].Origin)
		if leftRank != rightRank {
			return leftRank > rightRank
		}
		return items[left].ID > items[right].ID
	})
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeContentHistoryPageCursor(contentHistoryPageCursor{
			Version: contentHistoryCursorVersion, Scope: request.Scope, CreatedAt: last.CreatedAt, Origin: last.Origin, ID: last.ID,
		})
	}
	return contentHistoryPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}
}

func contentHistoryOriginRank(origin string) int {
	if origin == "manual" {
		return 1
	}
	return 0
}

func contentRevisionHistoryPageSQL(
	aggregateType string,
	aggregateKey string,
	publishedRevisionID *int64,
	visibility pendingReviewVisibility,
	request contentHistoryPageRequest,
) (string, []any) {
	var cursorAt any
	cursorRank := 0
	cursorID := ""
	if request.Cursor != nil {
		cursorAt = request.Cursor.CreatedAt
		cursorRank = contentHistoryOriginRank(request.Cursor.Origin)
		cursorID = request.Cursor.ID
	}
	query := `select revision.public_id,revision.revision_no,request.status,revision.source,request.reason,
		coalesce(account.public_id,''),coalesce(nullif(request.submitted_by_snapshot,''),nullif(revision.created_by_snapshot,''),account.username,'system'),
		revision.created_at,coalesce(revision.id=$3::bigint,false)
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		left join users account on account.id=coalesce(request.submitted_by,revision.created_by)
		where revision.aggregate_type=$1 and revision.aggregate_key=$2
		  and ($4 or request.status='approved' or ($5 and coalesce(request.submitted_by,0)>0 and request.submitted_by<>$6))
		  and ($7::timestamptz is null or (revision.created_at,1,revision.public_id)<($7::timestamptz,$8::integer,$9::text))
		order by revision.created_at desc,revision.public_id desc limit $10`
	arguments := []any{aggregateType, aggregateKey, publishedRevisionID, visibility.includeAll, visibility.includeScoped,
		visibility.reviewerID, cursorAt, cursorRank, cursorID, request.Limit + 1}
	return query, arguments
}

func importedResourceHistoryPageSQL(resourceID, versionID int64, includeUnpublished bool, request contentHistoryPageRequest) (string, []any) {
	var cursorAt any
	cursorRank := 0
	cursorID := ""
	if request.Cursor != nil {
		cursorAt = request.Cursor.CreatedAt
		cursorRank = contentHistoryOriginRank(request.Cursor.Origin)
		cursorID = request.Cursor.ID
	}
	query := `select revision.id,revision.revision_no,revision.status,revision.source_kind,revision.source_namespace,
		coalesce(account.public_id,''),coalesce(nullif(revision.submitted_by_snapshot,''),account.username,'system'),
		snapshot.created_at,revision.is_active
		from resource_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		left join users account on account.id=revision.submitted_by
		where snapshot.resource_id=$1 and revision.target_version_id=$2
		  and ($3 or revision.status in ('ready','partial','superseded'))
		  and ($4::timestamptz is null or (snapshot.created_at,0,snapshot.revision_id)<($4::timestamptz,$5::integer,$6::text))
		order by snapshot.created_at desc,snapshot.revision_id desc limit $7`
	return query, []any{resourceID, versionID, includeUnpublished, cursorAt, cursorRank, cursorID, request.Limit + 1}
}

func contentHistoryScope(parts ...any) string {
	var builder strings.Builder
	for index, part := range parts {
		if index > 0 {
			builder.WriteByte(':')
		}
		builder.WriteString(fmt.Sprint(part))
	}
	return builder.String()
}
