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

const (
	projectChangelogCursorVersion         = 1
	projectChangelogDefaultPageLimit      = 20
	projectChangelogMaximumPageLimit      = 50
	projectChangelogMaximumCursorBytes    = 2048
	projectChangelogPreviewCharacterLimit = 4000
	projectChangelogMaximumLocalizations  = 8
	projectChangelogMaximumCategories     = 100
)

type projectChangelogPageRequest struct {
	Limit  int
	Scope  string
	Cursor *projectChangelogPageCursor
}

type projectChangelogPageCursor struct {
	Version int       `json:"v"`
	Scope   string    `json:"s"`
	EventAt time.Time `json:"eventAt"`
	ID      int64     `json:"id"`
}

type projectChangelogCategorySummary struct {
	ID            string `json:"id"`
	DefaultLocale string `json:"defaultLocale"`
	Name          string `json:"name"`
}

type projectChangelogSummary struct {
	InternalID        int64                            `json:"-"`
	ID                string                           `json:"id"`
	EventAt           time.Time                        `json:"eventAt"`
	MinecraftVersions []string                         `json:"minecraftVersions"`
	ProjectVersion    string                           `json:"projectVersion"`
	DefaultLocale     string                           `json:"defaultLocale"`
	Category          *projectChangelogCategorySummary `json:"category,omitempty"`
	BodyExcerpt       string                           `json:"bodyExcerpt"`
	BodyTruncated     bool                             `json:"bodyTruncated"`
	Locale            string                           `json:"locale"`
	AvailableLocales  []string                         `json:"availableLocales"`
	ReviewStatus      string                           `json:"reviewStatus"`
	PendingChange     bool                             `json:"pendingChange"`
	CanEdit           bool                             `json:"canEdit"`
	CreatedByID       string                           `json:"createdById"`
	CreatedByName     string                           `json:"createdByName"`
	CreatedAt         time.Time                        `json:"createdAt"`
	UpdatedAt         time.Time                        `json:"updatedAt"`
}

type projectChangelogPage struct {
	Items      []projectChangelogSummary
	HasMore    bool
	NextCursor string
}

func parseProjectChangelogPageRequest(values url.Values, target projectChangelogTarget, locale string) (projectChangelogPageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return projectChangelogPageRequest{}, errors.New("offset pagination is not supported")
	}
	limit := projectChangelogDefaultPageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > projectChangelogMaximumPageLimit {
			return projectChangelogPageRequest{}, errors.New("changelog page limit is invalid")
		}
		limit = parsed
	}
	scope := projectChangelogPageScope(target, locale, limit)
	cursor, err := decodeProjectChangelogPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return projectChangelogPageRequest{}, err
	}
	return projectChangelogPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func projectChangelogPageScope(target projectChangelogTarget, locale string, limit int) string {
	material, _ := json.Marshal(struct {
		Version    int    `json:"version"`
		RouteID    int64  `json:"routeId"`
		EntityType string `json:"entityType"`
		PublicID   string `json:"publicId"`
		Locale     string `json:"locale"`
		Limit      int    `json:"limit"`
	}{projectChangelogCursorVersion, target.RouteID, target.EntityType, target.PublicID, normalizeContentLocale(locale), limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeProjectChangelogPageCursor(cursor projectChangelogPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProjectChangelogPageCursor(raw, scope string) (*projectChangelogPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > projectChangelogMaximumCursorBytes {
		return nil, errors.New("invalid changelog cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid changelog cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor projectChangelogPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid changelog cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != projectChangelogCursorVersion ||
		cursor.Scope != scope || cursor.EventAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid changelog cursor")
	}
	cursor.EventAt = cursor.EventAt.UTC()
	return &cursor, nil
}

func projectChangelogPageSQL(targetRouteID int64, locale string, request projectChangelogPageRequest) (string, []any) {
	query := `select entry.id,entry.public_id,entry.event_at,entry.minecraft_versions,entry.project_version,
		entry.default_locale,entry.review_status,entry.created_at,entry.updated_at,
		creator.public_id,creator.username,coalesce(category.public_id,''),coalesce(category.default_locale,''),
		coalesce(category_name.name,''),coalesce(selected_body.locale,entry.default_locale),
		coalesce(selected_body.body_excerpt,''),coalesce(available_locales.values,'{}'::text[]),
		exists(select 1 from change_requests request where request.aggregate_type=$2 and request.aggregate_key=entry.public_id and request.status='pending')
		from project_changelogs entry
		join users creator on creator.id=entry.created_by
		left join project_changelog_categories category on category.id=entry.category_id
		left join lateral (select value.name
			from project_changelog_category_localizations value where value.category_id=category.id
			order by case value.locale when $3 then 0 when category.default_locale then 1 when 'zh-CN' then 2 when 'en-US' then 3 else 4 end,value.locale
			limit 1) category_name on true
		left join lateral (select value.locale,left(value.body_markdown,$4) body_excerpt
			from project_changelog_localizations value where value.changelog_id=entry.id
			order by case value.locale when $3 then 0 when entry.default_locale then 1 when 'zh-CN' then 2 when 'en-US' then 3 else 4 end,value.locale
			limit 1) selected_body on true
		left join lateral (select coalesce(array_agg(locales.locale order by locales.locale),'{}'::text[]) values
			from (select value.locale from project_changelog_localizations value
				where value.changelog_id=entry.id order by value.locale limit $5) locales) available_locales on true
		where entry.object_route_id=$1 and entry.status='active' and entry.review_status='approved'`
	args := []any{targetRouteID, projectChangelogAggregate, normalizeContentLocale(locale), projectChangelogPreviewCharacterLimit + 1, projectChangelogMaximumLocalizations + 1}
	if request.Cursor != nil {
		query += ` and (entry.event_at,entry.id)<($6,$7)`
		args = append(args, request.Cursor.EventAt, request.Cursor.ID)
	}
	query += ` order by entry.event_at desc,entry.id desc limit $` + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit+1)
	return query, args
}
