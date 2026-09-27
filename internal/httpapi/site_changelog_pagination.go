package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	siteChangelogPageCursorVersion      = 1
	siteChangelogDefaultPageLimit       = 30
	siteChangelogMaximumPageLimit       = 100
	siteChangelogMaximumPageCursorBytes = 2048
)

type siteChangelogPageRequest struct {
	Scope  string
	Locale string
	Limit  int
	Cursor *siteChangelogPageCursor
}

type siteChangelogPageCursor struct {
	Version    int       `json:"v"`
	Scope      string    `json:"scope"`
	Limit      int       `json:"limit"`
	ChangeDate time.Time `json:"changeDate"`
	ID         int64     `json:"id"`
}

type siteChangelogPublicItem struct {
	InternalID   int64     `json:"-"`
	ID           string    `json:"id"`
	ChangeDate   time.Time `json:"changeDate"`
	Locale       string    `json:"locale"`
	Title        string    `json:"title"`
	BodyMarkdown string    `json:"bodyMarkdown"`
}

type siteChangelogTranslation struct {
	Title        string `json:"title"`
	BodyMarkdown string `json:"bodyMarkdown"`
	Status       string `json:"status"`
}

type siteChangelogAdminItem struct {
	InternalID   int64                               `json:"-"`
	ID           string                              `json:"id"`
	ChangeDate   string                              `json:"changeDate"`
	Status       string                              `json:"status"`
	Translations map[string]siteChangelogTranslation `json:"translations"`
	UpdatedAt    time.Time                           `json:"updatedAt"`
}

type siteChangelogPublicPage struct {
	Items      []siteChangelogPublicItem `json:"items"`
	Limit      int                       `json:"limit"`
	HasMore    bool                      `json:"hasMore"`
	NextCursor string                    `json:"nextCursor"`
}

type siteChangelogAdminPage struct {
	Items      []siteChangelogAdminItem `json:"items"`
	Limit      int                      `json:"limit"`
	HasMore    bool                     `json:"hasMore"`
	NextCursor string                   `json:"nextCursor"`
}

const siteChangelogPublicPageSQL = `with page as materialized (
	select changelog.id,changelog.public_id,changelog.change_date
	from site_changelogs changelog
	where changelog.status='published'
	  and exists(select 1 from site_changelog_translations visible where visible.changelog_id=changelog.id and visible.status='published')
	  and ($2::date is null or (changelog.change_date,changelog.id)<($2::date,$3::bigint))
	order by changelog.change_date desc,changelog.id desc
	limit $4
)
select page.id,page.public_id,page.change_date,translation.locale,translation.title,translation.body_markdown
from page
join lateral (
	select value.locale,value.title,value.body_markdown
	from site_changelog_translations value
	where value.changelog_id=page.id and value.status='published'
	order by case value.locale when $1 then 0 when 'zh-CN' then 1 when 'en-US' then 2 else 3 end,value.locale
	limit 1
) translation on true
order by page.change_date desc,page.id desc`

const siteChangelogAdminPageSQL = `with page as materialized (
	select changelog.id,changelog.public_id,changelog.change_date,changelog.status,changelog.updated_at
	from site_changelogs changelog
	where ($1::date is null or (changelog.change_date,changelog.id)<($1::date,$2::bigint))
	order by changelog.change_date desc,changelog.id desc
	limit $3
)
select page.id,page.public_id,page.change_date,page.status,translation.translations,page.updated_at
from page
join lateral (
	select coalesce(jsonb_object_agg(value.locale,jsonb_build_object(
		'title',value.title,'bodyMarkdown',value.body_markdown,'status',value.status)),'{}'::jsonb) translations
	from site_changelog_translations value
	where value.changelog_id=page.id
) translation on true
order by page.change_date desc,page.id desc`

func parseSiteChangelogPageRequest(values url.Values, admin bool) (siteChangelogPageRequest, error) {
	allowed := map[string]bool{"limit": true, "cursor": true}
	if !admin {
		allowed["locale"] = true
	}
	for name, entries := range values {
		if !allowed[name] || len(entries) != 1 {
			return siteChangelogPageRequest{}, errors.New("站点更新分页参数不正确")
		}
	}
	limit := siteChangelogDefaultPageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > siteChangelogMaximumPageLimit {
			return siteChangelogPageRequest{}, errors.New("站点更新分页大小不正确")
		}
		limit = parsed
	}
	locale := ""
	scope := "admin"
	if !admin {
		locale = normalizedSiteAffairsLocale(values.Get("locale"))
		scope = "public:" + locale
	}
	cursor, err := decodeSiteChangelogPageCursor(values.Get("cursor"), scope, limit)
	if err != nil {
		return siteChangelogPageRequest{}, err
	}
	return siteChangelogPageRequest{Scope: scope, Locale: locale, Limit: limit, Cursor: cursor}, nil
}

func encodeSiteChangelogPageCursor(cursor siteChangelogPageCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeSiteChangelogPageCursor(raw, scope string, limit int) (*siteChangelogPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > siteChangelogMaximumPageCursorBytes*2 {
		return nil, errors.New("站点更新分页游标不正确")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > siteChangelogMaximumPageCursorBytes {
		return nil, errors.New("站点更新分页游标不正确")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor siteChangelogPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("站点更新分页游标不正确")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != siteChangelogPageCursorVersion ||
		cursor.Scope != scope || cursor.Limit != limit || cursor.ChangeDate.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("站点更新分页游标不正确")
	}
	canonicalDate := time.Date(cursor.ChangeDate.Year(), cursor.ChangeDate.Month(), cursor.ChangeDate.Day(), 0, 0, 0, 0, time.UTC)
	if cursor.ChangeDate.Format(time.RFC3339Nano) != canonicalDate.Format(time.RFC3339Nano) {
		return nil, errors.New("站点更新分页游标不正确")
	}
	cursor.ChangeDate = canonicalDate
	return &cursor, nil
}

func siteChangelogPagePosition(request siteChangelogPageRequest) (any, int64) {
	if request.Cursor == nil {
		return nil, 0
	}
	return request.Cursor.ChangeDate, request.Cursor.ID
}

func collectPublicSiteChangelogRows(rows checkedRows, capacity int) ([]siteChangelogPublicItem, error) {
	defer rows.Close()
	items := make([]siteChangelogPublicItem, 0, capacity)
	for rows.Next() {
		var item siteChangelogPublicItem
		if err := rows.Scan(&item.InternalID, &item.ID, &item.ChangeDate, &item.Locale, &item.Title, &item.BodyMarkdown); err != nil {
			return nil, fmt.Errorf("scan public site changelog: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public site changelogs: %w", err)
	}
	return items, nil
}

func decodeSiteChangelogTranslations(raw []byte) (map[string]siteChangelogTranslation, error) {
	if _, err := decodeStoredJSONObject(raw, "site changelog translations"); err != nil {
		return nil, err
	}
	translations := make(map[string]siteChangelogTranslation)
	if err := json.Unmarshal(raw, &translations); err != nil {
		return nil, fmt.Errorf("decode site changelog translations: %w", err)
	}
	return translations, nil
}

func collectAdminSiteChangelogRows(rows checkedRows, capacity int) ([]siteChangelogAdminItem, error) {
	defer rows.Close()
	items := make([]siteChangelogAdminItem, 0, capacity)
	for rows.Next() {
		var item siteChangelogAdminItem
		var date time.Time
		var translations []byte
		if err := rows.Scan(&item.InternalID, &item.ID, &date, &item.Status, &translations, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan admin site changelog: %w", err)
		}
		var err error
		item.Translations, err = decodeSiteChangelogTranslations(translations)
		if err != nil {
			return nil, fmt.Errorf("site changelog %s: %w", item.ID, err)
		}
		item.ChangeDate = date.Format("2006-01-02")
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate admin site changelogs: %w", err)
	}
	return items, nil
}

type siteChangelogRowQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type adminSiteChangelogDetailValue struct {
	ChangeDate   string
	Status       string
	Translations map[string]siteChangelogTranslation
	UpdatedAt    time.Time
}

func loadAdminSiteChangelogDetail(
	ctx context.Context,
	queryer siteChangelogRowQueryer,
	publicID string,
) (adminSiteChangelogDetailValue, error) {
	var detail adminSiteChangelogDetailValue
	var changeDate time.Time
	var translations []byte
	err := queryer.QueryRow(ctx, `select changelog.change_date,changelog.status,changelog.updated_at,
		coalesce(jsonb_object_agg(translation.locale,jsonb_build_object('title',translation.title,'bodyMarkdown',translation.body_markdown,'status',translation.status)) filter(where translation.locale is not null),'{}'::jsonb)
		from site_changelogs changelog left join site_changelog_translations translation on translation.changelog_id=changelog.id
		where changelog.public_id=$1 group by changelog.id`, publicID).Scan(&changeDate, &detail.Status, &detail.UpdatedAt, &translations)
	if err != nil {
		return adminSiteChangelogDetailValue{}, err
	}
	detail.ChangeDate = changeDate.Format("2006-01-02")
	detail.Translations, err = decodeSiteChangelogTranslations(translations)
	if err != nil {
		return adminSiteChangelogDetailValue{}, fmt.Errorf("site changelog %s: %w", publicID, err)
	}
	return detail, nil
}

func (s *Server) queryPublicSiteChangelogPage(ctx context.Context, request siteChangelogPageRequest) (siteChangelogPublicPage, error) {
	changeDate, id := siteChangelogPagePosition(request)
	rows, err := s.db.Query(ctx, siteChangelogPublicPageSQL, request.Locale, changeDate, id, request.Limit+1)
	if err != nil {
		return siteChangelogPublicPage{}, err
	}
	items, err := collectPublicSiteChangelogRows(rows, request.Limit+1)
	if err != nil {
		return siteChangelogPublicPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeSiteChangelogPageCursor(siteChangelogPageCursor{
			Version: siteChangelogPageCursorVersion, Scope: request.Scope, Limit: request.Limit,
			ChangeDate: last.ChangeDate, ID: last.InternalID,
		})
	}
	return siteChangelogPublicPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (s *Server) queryAdminSiteChangelogPage(ctx context.Context, request siteChangelogPageRequest) (siteChangelogAdminPage, error) {
	changeDate, id := siteChangelogPagePosition(request)
	rows, err := s.db.Query(ctx, siteChangelogAdminPageSQL, changeDate, id, request.Limit+1)
	if err != nil {
		return siteChangelogAdminPage{}, err
	}
	items, err := collectAdminSiteChangelogRows(rows, request.Limit+1)
	if err != nil {
		return siteChangelogAdminPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		date, _ := time.Parse("2006-01-02", last.ChangeDate)
		nextCursor = encodeSiteChangelogPageCursor(siteChangelogPageCursor{
			Version: siteChangelogPageCursorVersion, Scope: request.Scope, Limit: request.Limit,
			ChangeDate: date.UTC(), ID: last.InternalID,
		})
	}
	return siteChangelogAdminPage{Items: items, Limit: request.Limit, HasMore: hasMore, NextCursor: nextCursor}, nil
}
