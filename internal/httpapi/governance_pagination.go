package httpapi

import (
	"bytes"
	"context"
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

const (
	governancePageCursorVersion     = 1
	governanceMaximumPageLimit      = 100
	governanceMaximumPageCursorSize = 2048
)

type governancePageRequest struct {
	Scope  string
	Status string
	Limit  int
	Cursor *governancePageCursor
}

type governancePageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

type ownReportListItem struct {
	InternalID int64      `json:"-"`
	ID         string     `json:"id"`
	TargetType string     `json:"targetType"`
	TargetID   string     `json:"targetId"`
	ReasonCode string     `json:"reasonCode"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"createdAt"`
	ResolvedAt *time.Time `json:"resolvedAt"`
}

type adminReportListItem struct {
	InternalID   int64      `json:"-"`
	ID           string     `json:"id"`
	TargetType   string     `json:"targetType"`
	TargetID     string     `json:"targetId"`
	ReasonCode   string     `json:"reasonCode"`
	Status       string     `json:"status"`
	ReporterID   string     `json:"reporterId"`
	ReporterName string     `json:"reporterName"`
	CreatedAt    time.Time  `json:"createdAt"`
	ClaimedAt    *time.Time `json:"claimedAt"`
}

type blackroomListItem struct {
	InternalID   int64      `json:"-"`
	ID           string     `json:"id"`
	UserID       string     `json:"userId"`
	Username     string     `json:"username"`
	AvatarURL    string     `json:"avatarUrl"`
	ReasonCode   string     `json:"reasonCode"`
	CustomReason string     `json:"customReason"`
	Status       string     `json:"status"`
	StartsAt     time.Time  `json:"startsAt"`
	EndsAt       *time.Time `json:"endsAt"`
	RevokedAt    *time.Time `json:"revokedAt"`
	CreatedAt    time.Time  `json:"-"`
}

type adminBlackroomListItem struct {
	InternalID           int64      `json:"-"`
	ID                   string     `json:"id"`
	UserID               string     `json:"userId"`
	Username             string     `json:"username"`
	AvatarURL            string     `json:"avatarUrl"`
	ReasonCode           string     `json:"reasonCode"`
	CustomReason         string     `json:"customReason"`
	Status               string     `json:"status"`
	StartsAt             time.Time  `json:"startsAt"`
	EndsAt               *time.Time `json:"endsAt"`
	RevokedAt            *time.Time `json:"revokedAt"`
	PublicRecordMarkdown string     `json:"publicRecordMarkdown"`
	InternalNote         string     `json:"internalNote"`
	ModeratorID          string     `json:"moderatorId"`
	ModeratorName        string     `json:"moderatorName"`
	RevokedByID          string     `json:"revokedById"`
	RevokedByName        string     `json:"revokedByName"`
	RevokeReason         string     `json:"revokeReason"`
	CreatedAt            time.Time  `json:"-"`
}

type ownReportPage struct {
	Items      []ownReportListItem `json:"items"`
	Limit      int                 `json:"limit"`
	HasMore    bool                `json:"hasMore"`
	NextCursor string              `json:"nextCursor"`
}

type adminReportPage struct {
	Items      []adminReportListItem `json:"items"`
	Limit      int                   `json:"limit"`
	HasMore    bool                  `json:"hasMore"`
	NextCursor string                `json:"nextCursor"`
}

type blackroomPage struct {
	Items      []blackroomListItem `json:"items"`
	Limit      int                 `json:"limit"`
	HasMore    bool                `json:"hasMore"`
	NextCursor string              `json:"nextCursor"`
}

type adminBlackroomPage struct {
	Items      []adminBlackroomListItem `json:"items"`
	Limit      int                      `json:"limit"`
	HasMore    bool                     `json:"hasMore"`
	NextCursor string                   `json:"nextCursor"`
}

const ownReportPageSQL = `select report.id,report.public_id,report.target_type,report.target_public_id,
	report.reason_code,report.status,report.created_at,report.resolved_at
from reports report
where report.reporter_id=$1
	and ($2::timestamptz is null or (report.created_at,report.id)<($2::timestamptz,$3::bigint))
order by report.created_at desc,report.id desc
limit $4`

const adminReportPageSQL = `select report.id,report.public_id,report.target_type,report.target_public_id,
	report.reason_code,report.status,reporter.public_id,reporter.username,report.created_at,report.claimed_at
from reports report
join users reporter on reporter.id=report.reporter_id
where report.status=$1
	and ($2::timestamptz is null or (report.created_at,report.id)>($2::timestamptz,$3::bigint))
order by report.created_at,report.id
limit $4`

const blackroomPageSQL = `select ban.id,ban.public_id,u.public_id,ban.username_snapshot,ban.avatar_snapshot,
	ban.reason_code,ban.custom_reason,ban.status,ban.starts_at,ban.ends_at,ban.revoked_at,ban.created_at
from ban_records ban
join users u on u.id=ban.user_id
where ($1::timestamptz is null or (ban.created_at,ban.id)<($1::timestamptz,$2::bigint))
order by ban.created_at desc,ban.id desc
limit $3`

const adminBlackroomPageSQL = `select ban.id,ban.public_id,u.public_id,ban.username_snapshot,ban.avatar_snapshot,
	ban.reason_code,ban.custom_reason,ban.status,ban.starts_at,ban.ends_at,ban.revoked_at,ban.created_at,
	ban.public_record_markdown,ban.internal_note,moderator.public_id,moderator.username,
	coalesce(revoker.public_id,''),coalesce(revoker.username,''),ban.revoke_reason
from ban_records ban
join users u on u.id=ban.user_id
join users moderator on moderator.id=ban.moderator_id
left join users revoker on revoker.id=ban.revoked_by
where ($1::timestamptz is null or (ban.created_at,ban.id)<($1::timestamptz,$2::bigint))
order by ban.created_at desc,ban.id desc
limit $3`

func parseOwnReportPageRequest(values url.Values, reporterID int64) (governancePageRequest, error) {
	return parseGovernancePageRequest(values, "reports:own", strconv.FormatInt(reporterID, 10), 20, false)
}

func parseAdminReportPageRequest(values url.Values) (governancePageRequest, error) {
	status := strings.TrimSpace(values.Get("status"))
	if status == "" {
		status = "pending"
	}
	if !validReportPageStatus(status) {
		return governancePageRequest{}, errors.New("举报状态不正确")
	}
	request, err := parseGovernancePageRequest(values, "reports:admin", status, 50, true)
	request.Status = status
	return request, err
}

func parsePublicBlackroomPageRequest(values url.Values) (governancePageRequest, error) {
	return parseGovernancePageRequest(values, "blackroom", "public", 30, false)
}

func parseAdminBlackroomPageRequest(values url.Values) (governancePageRequest, error) {
	return parseGovernancePageRequest(values, "blackroom", "admin", 30, false)
}

func parseGovernancePageRequest(values url.Values, kind, qualifier string, defaultLimit int, allowStatus bool) (governancePageRequest, error) {
	allowed := map[string]bool{"limit": true, "cursor": true}
	if allowStatus {
		allowed["status"] = true
	}
	for name, entries := range values {
		if !allowed[name] || len(entries) != 1 {
			return governancePageRequest{}, errors.New("治理列表分页参数不正确")
		}
	}
	limit := defaultLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > governanceMaximumPageLimit {
			return governancePageRequest{}, errors.New("治理列表分页大小不正确")
		}
		limit = parsed
	}
	scope := governancePageScope(kind, qualifier, limit)
	cursor, err := decodeGovernancePageCursor(values.Get("cursor"), scope)
	if err != nil {
		return governancePageRequest{}, err
	}
	return governancePageRequest{Scope: scope, Limit: limit, Cursor: cursor}, nil
}

func validReportPageStatus(status string) bool {
	switch status {
	case "pending", "in_review", "resolved_valid", "resolved_invalid", "cancelled":
		return true
	default:
		return false
	}
}

func governancePageScope(kind, qualifier string, limit int) string {
	payload, _ := json.Marshal(struct {
		Version   int    `json:"version"`
		Kind      string `json:"kind"`
		Qualifier string `json:"qualifier"`
		Limit     int    `json:"limit"`
	}{governancePageCursorVersion, kind, qualifier, limit})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func encodeGovernancePageCursor(cursor governancePageCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeGovernancePageCursor(raw, scope string) (*governancePageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > governanceMaximumPageCursorSize*2 {
		return nil, errors.New("治理列表分页游标不正确")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > governanceMaximumPageCursorSize {
		return nil, errors.New("治理列表分页游标不正确")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor governancePageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("治理列表分页游标不正确")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != governancePageCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("治理列表分页游标不正确")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

func governancePagePosition(request governancePageRequest) (any, int64) {
	if request.Cursor == nil {
		return nil, 0
	}
	return request.Cursor.CreatedAt, request.Cursor.ID
}

func governanceNextCursor(request governancePageRequest, createdAt time.Time, id int64) string {
	return encodeGovernancePageCursor(governancePageCursor{
		Version: governancePageCursorVersion, Scope: request.Scope, CreatedAt: createdAt.UTC(), ID: id,
	})
}

func (s *Server) queryOwnReportPage(ctx context.Context, reporterID int64, request governancePageRequest) (ownReportPage, error) {
	createdAt, id := governancePagePosition(request)
	rows, err := s.db.Query(ctx, ownReportPageSQL, reporterID, createdAt, id, request.Limit+1)
	if err != nil {
		return ownReportPage{}, err
	}
	items, err := collectOwnReportRows(rows, request.Limit+1)
	if err != nil {
		return ownReportPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = governanceNextCursor(request, last.CreatedAt, last.InternalID)
	}
	return ownReportPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func collectOwnReportRows(rows checkedRows, capacity int) ([]ownReportListItem, error) {
	defer rows.Close()
	items := make([]ownReportListItem, 0, capacity)
	for rows.Next() {
		var item ownReportListItem
		if err := rows.Scan(&item.InternalID, &item.ID, &item.TargetType, &item.TargetID, &item.ReasonCode,
			&item.Status, &item.CreatedAt, &item.ResolvedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) queryAdminReportPage(ctx context.Context, request governancePageRequest) (adminReportPage, error) {
	createdAt, id := governancePagePosition(request)
	rows, err := s.db.Query(ctx, adminReportPageSQL, request.Status, createdAt, id, request.Limit+1)
	if err != nil {
		return adminReportPage{}, err
	}
	items, err := collectAdminReportRows(rows, request.Limit+1)
	if err != nil {
		return adminReportPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = governanceNextCursor(request, last.CreatedAt, last.InternalID)
	}
	return adminReportPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func collectAdminReportRows(rows checkedRows, capacity int) ([]adminReportListItem, error) {
	defer rows.Close()
	items := make([]adminReportListItem, 0, capacity)
	for rows.Next() {
		var item adminReportListItem
		if err := rows.Scan(&item.InternalID, &item.ID, &item.TargetType, &item.TargetID, &item.ReasonCode,
			&item.Status, &item.ReporterID, &item.ReporterName, &item.CreatedAt, &item.ClaimedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) queryPublicBlackroomPage(ctx context.Context, request governancePageRequest, now time.Time) (blackroomPage, error) {
	createdAt, id := governancePagePosition(request)
	rows, err := s.db.Query(ctx, blackroomPageSQL, createdAt, id, request.Limit+1)
	if err != nil {
		return blackroomPage{}, err
	}
	items, err := collectPublicBlackroomRows(rows, request.Limit+1, now)
	if err != nil {
		return blackroomPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = governanceNextCursor(request, last.CreatedAt, last.InternalID)
	}
	return blackroomPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func collectPublicBlackroomRows(rows checkedRows, capacity int, now time.Time) ([]blackroomListItem, error) {
	defer rows.Close()
	items := make([]blackroomListItem, 0, capacity)
	for rows.Next() {
		var item blackroomListItem
		var storedStatus string
		if err := rows.Scan(&item.InternalID, &item.ID, &item.UserID, &item.Username, &item.AvatarURL,
			&item.ReasonCode, &item.CustomReason, &storedStatus, &item.StartsAt, &item.EndsAt, &item.RevokedAt,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		item.Status = publicBanStatus(storedStatus, item.EndsAt, item.RevokedAt, now)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) queryAdminBlackroomPage(ctx context.Context, request governancePageRequest, now time.Time) (adminBlackroomPage, error) {
	createdAt, id := governancePagePosition(request)
	rows, err := s.db.Query(ctx, adminBlackroomPageSQL, createdAt, id, request.Limit+1)
	if err != nil {
		return adminBlackroomPage{}, err
	}
	items, err := collectAdminBlackroomRows(rows, request.Limit+1, now)
	if err != nil {
		return adminBlackroomPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = governanceNextCursor(request, last.CreatedAt, last.InternalID)
	}
	return adminBlackroomPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func collectAdminBlackroomRows(rows checkedRows, capacity int, now time.Time) ([]adminBlackroomListItem, error) {
	defer rows.Close()
	items := make([]adminBlackroomListItem, 0, capacity)
	for rows.Next() {
		var item adminBlackroomListItem
		var storedStatus string
		if err := rows.Scan(&item.InternalID, &item.ID, &item.UserID, &item.Username, &item.AvatarURL,
			&item.ReasonCode, &item.CustomReason, &storedStatus, &item.StartsAt, &item.EndsAt, &item.RevokedAt,
			&item.CreatedAt, &item.PublicRecordMarkdown, &item.InternalNote, &item.ModeratorID, &item.ModeratorName,
			&item.RevokedByID, &item.RevokedByName, &item.RevokeReason); err != nil {
			return nil, err
		}
		item.Status = publicBanStatus(storedStatus, item.EndsAt, item.RevokedAt, now)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}
