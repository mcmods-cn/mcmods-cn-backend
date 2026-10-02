package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type projectChangelogCategoryPage struct {
	Categories []projectChangelogCategoryResponse `json:"categories"`
	Limit      int                                `json:"limit"`
	HasMore    bool                               `json:"hasMore"`
	NextCursor string                             `json:"nextCursor"`
}

func projectChangelogCategoryScope(target projectChangelogTarget, locale string) string {
	return "categories:" + projectChangelogPageScope(target, locale, projectChangelogMaximumCategories)
}

func loadProjectChangelogCategoryPage(ctx context.Context, query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, target projectChangelogTarget, locale, rawCursor string) (projectChangelogCategoryPage, error) {
	page := projectChangelogCategoryPage{Categories: []projectChangelogCategoryResponse{}, Limit: projectChangelogMaximumCategories}
	scope := projectChangelogCategoryScope(target, locale)
	cursor, err := decodeProjectChangelogPageCursor(rawCursor, scope)
	if err != nil {
		return page, err
	}
	var after time.Time
	var afterID int64
	if cursor != nil {
		after, afterID = cursor.EventAt, cursor.ID
	}
	rows, err := query.Query(ctx, `with selected as (
  select id,public_id,default_locale,created_at from project_changelog_categories
  where object_route_id=$1 and ($3::bigint=0 or (created_at,id)>($2,$3))
  order by created_at,id limit $4
) select category.public_id,category.default_locale,category.created_at,category.id,
  coalesce((select jsonb_object_agg(value.locale,value.name) from (
    select locale,name from project_changelog_category_localizations
    where category_id=category.id order by locale limit 9
  ) value),'{}'::jsonb)
from selected category order by category.created_at,category.id`, target.RouteID, after, afterID, page.Limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	var lastCreated time.Time
	var lastID int64
	for rows.Next() {
		var item projectChangelogCategoryResponse
		var raw []byte
		var created time.Time
		var id int64
		if err = rows.Scan(&item.ID, &item.DefaultLocale, &created, &id, &raw); err != nil {
			return page, err
		}
		if len(page.Categories) == page.Limit {
			page.HasMore = true
			continue
		}
		if err = json.Unmarshal(raw, &item.Names); err != nil || len(item.Names) > projectChangelogMaximumLocalizations {
			return page, errors.New("changelog category localization limit exceeded")
		}
		item.Name = localizedChangelogValue(item.Names, locale, item.DefaultLocale)
		page.Categories = append(page.Categories, item)
		lastCreated, lastID = created, id
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if page.HasMore {
		page.NextCursor = encodeProjectChangelogPageCursor(projectChangelogPageCursor{
			Version: projectChangelogCursorVersion, Scope: scope, EventAt: lastCreated, ID: lastID,
		})
	}
	return page, err
}

func (s *Server) projectChangelogCategories(w http.ResponseWriter, r *http.Request) {
	targetType := normalizeChangelogTargetType(r.URL.Query().Get("targetType"))
	targetID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("targetId")))
	if targetType == "" || !validCatalogPublicID(targetID) {
		writeError(w, http.StatusBadRequest, "invalid changelog target")
		return
	}
	target, err := s.resolveProjectChangelogTarget(r.Context(), targetType, targetID, currentClaims(r))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "changelog target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load changelog target")
		return
	}
	locale := normalizeContentLocale(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = "zh-CN"
	}
	if _, err = decodeProjectChangelogPageCursor(r.URL.Query().Get("cursor"), projectChangelogCategoryScope(target, locale)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid changelog category cursor")
		return
	}
	page, err := loadProjectChangelogCategoryPage(r.Context(), s.db, target, locale, r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load changelog categories")
		return
	}
	writeBoundedCatalogJSON(w, map[string]any{"target": target, "categories": page.Categories,
		"limit": page.Limit, "hasMore": page.HasMore, "nextCursor": page.NextCursor})
}
