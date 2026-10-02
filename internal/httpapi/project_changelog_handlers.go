package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"
)

const projectChangelogAggregate = "project_changelog"

var changelogTargetTypes = map[string]bool{
	"mod": true, "modpack": true, "plugin": true, "map": true,
	"resource_pack": true, "shader_pack": true, "datapack": true,
	"addon": true, "minecraft_server": true,
}

type projectChangelogLocalization struct {
	Locale       string `json:"locale"`
	BodyMarkdown string `json:"bodyMarkdown"`
}

type projectChangelogCategoryLocalization struct {
	Locale string `json:"locale"`
	Name   string `json:"name"`
}

type projectChangelogNewCategory struct {
	DefaultLocale string                                 `json:"defaultLocale"`
	Localizations []projectChangelogCategoryLocalization `json:"localizations"`
}

type projectChangelogSnapshot struct {
	EventAt           time.Time                      `json:"eventAt"`
	MinecraftVersions []string                       `json:"minecraftVersions"`
	ProjectVersion    string                         `json:"projectVersion"`
	DefaultLocale     string                         `json:"defaultLocale"`
	CategoryID        string                         `json:"categoryId,omitempty"`
	NewCategory       *projectChangelogNewCategory   `json:"newCategory,omitempty"`
	Localizations     []projectChangelogLocalization `json:"localizations"`
	Reason            string                         `json:"reason,omitempty"`
}

type projectChangelogTarget struct {
	RouteID       int64  `json:"-"`
	InternalID    int64  `json:"-"`
	EntityType    string `json:"type"`
	PublicID      string `json:"id"`
	Name          string `json:"name"`
	CanonicalPath string `json:"url"`
	CanEdit       bool   `json:"canEdit"`
}

type projectChangelogCategoryResponse struct {
	ID            string            `json:"id"`
	DefaultLocale string            `json:"defaultLocale"`
	Names         map[string]string `json:"names"`
	Name          string            `json:"name"`
}

type projectChangelogResponse struct {
	ID                string                            `json:"id"`
	EventAt           time.Time                         `json:"eventAt"`
	MinecraftVersions []string                          `json:"minecraftVersions"`
	ProjectVersion    string                            `json:"projectVersion"`
	DefaultLocale     string                            `json:"defaultLocale"`
	Category          *projectChangelogCategoryResponse `json:"category,omitempty"`
	BodyMarkdown      string                            `json:"bodyMarkdown"`
	Locale            string                            `json:"locale"`
	AvailableLocales  []string                          `json:"availableLocales"`
	Localizations     []projectChangelogLocalization    `json:"localizations,omitempty"`
	ReviewStatus      string                            `json:"reviewStatus"`
	PendingChange     bool                              `json:"pendingChange"`
	CanEdit           bool                              `json:"canEdit"`
	CreatedByID       string                            `json:"createdById"`
	CreatedByName     string                            `json:"createdByName"`
	CreatedAt         time.Time                         `json:"createdAt"`
	UpdatedAt         time.Time                         `json:"updatedAt"`
}

func (s *Server) projectChangelogs(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, "failed to load changelog target")
		return
	}
	if r.Method == http.MethodPost {
		s.createProjectChangelog(w, r, target)
		return
	}
	locale := normalizeContentLocale(r.URL.Query().Get("locale"))
	if locale == "" {
		locale = "zh-CN"
	}
	pageRequest, err := parseProjectChangelogPageRequest(r.URL.Query(), target, locale)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	categories := make([]projectChangelogCategoryResponse, 0)
	categoryPage := projectChangelogCategoryPage{}
	if pageRequest.Cursor == nil {
		categoryPage, err = loadProjectChangelogCategoryPage(r.Context(), s.db, target, locale, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load changelog categories")
			return
		}
		categories = categoryPage.Categories
	}
	page, err := s.loadProjectChangelogs(r.Context(), target, locale, pageRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load changelogs")
		return
	}
	writeBoundedCatalogJSON(w, map[string]any{
		"target": target, "categories": categories, "items": page.Items, "limit": pageRequest.Limit,
		"categoriesHasMore": categoryPage.HasMore, "categoriesNextCursor": categoryPage.NextCursor,
		"hasMore": page.HasMore, "nextCursor": page.NextCursor,
	})
}

func (s *Server) projectChangelogItem(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid changelog")
		return
	}
	claims := currentClaims(r)
	entry, target, publishedRevisionID, err := s.loadProjectChangelogEditor(r.Context(), publicID, claims)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "changelog not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load changelog")
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"target": target, "item": entry})
		return
	}
	if !target.CanEdit {
		writeError(w, http.StatusForbidden, "changelog edit permission required")
		return
	}
	var snapshot projectChangelogSnapshot
	if err = decodeJSON(r, &snapshot); err != nil || normalizeProjectChangelogSnapshot(&snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid changelog")
		return
	}
	if !s.validateProjectChangelogMinecraftVersions(w, r, &snapshot) {
		return
	}
	snapshotRaw, _ := json.Marshal(snapshot)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start changelog update")
		return
	}
	defer tx.Rollback(r.Context())
	if !validateProjectChangelogCategory(w, r, tx, snapshot.CategoryID, target.RouteID) {
		return
	}
	requireReview := loadReviewConfig(r.Context(), tx).ChangelogEdit && !canSkipChangelogReview(claims, target.PublicID)
	status := "approved"
	if requireReview {
		status = "pending"
	}
	entryID, err := entryInternalIDTx(r.Context(), tx, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "changelog not found")
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "CHANGELOG_LOOKUP_FAILED", "failed to load changelog", 0, nil)
		return
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: projectChangelogAggregate, EntityID: entryID,
		AggregateType: projectChangelogAggregate, AggregateKey: publicID, BaseRevision: publishedRevisionID,
		Snapshot: snapshotRaw, Reason: snapshot.Reason, ActorID: claims.Subject, Source: "user", Status: status,
		Metadata: map[string]any{"targetLabel": target.Name + " - " + snapshot.ProjectVersion, "targetURL": target.CanonicalPath + "?tab=changelog"}, Request: r,
	})
	if errors.Is(err, errReviewInProgress) {
		writeError(w, http.StatusConflict, "changelog already has a pending review")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create changelog revision")
		return
	}
	if status == "approved" {
		if err = applyProjectChangelogSnapshotTx(r.Context(), tx, entryID, target.RouteID, created.RevisionID, claims.Subject, snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish changelog")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save changelog")
		return
	}
	annotateActivity(r, activity.ActionEdit, activity.ObjectChangelog, publicID, 0)
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "reviewStatus": status, "changeRequestId": created.ChangeRequestPublicID})
}

func (s *Server) createProjectChangelog(w http.ResponseWriter, r *http.Request, target projectChangelogTarget) {
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	if !target.CanEdit {
		writeError(w, http.StatusForbidden, "changelog edit permission required")
		return
	}
	var snapshot projectChangelogSnapshot
	if decodeJSON(r, &snapshot) != nil || normalizeProjectChangelogSnapshot(&snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid changelog")
		return
	}
	if !s.validateProjectChangelogMinecraftVersions(w, r, &snapshot) {
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start changelog creation")
		return
	}
	defer tx.Rollback(r.Context())
	if !validateProjectChangelogCategory(w, r, tx, snapshot.CategoryID, target.RouteID) {
		return
	}
	var internalID int64
	var publicID string
	if err = tx.QueryRow(r.Context(), `insert into project_changelogs(
		object_route_id,event_at,minecraft_versions,project_version,default_locale,created_by)
		values($1,$2,$3,$4,$5,$6) returning id,public_id`, target.RouteID, snapshot.EventAt,
		snapshot.MinecraftVersions, snapshot.ProjectVersion, snapshot.DefaultLocale, claims.Subject).Scan(&internalID, &publicID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create changelog")
		return
	}
	requireReview := loadReviewConfig(r.Context(), tx).ChangelogCreate && !canSkipChangelogReview(claims, target.PublicID)
	status := "approved"
	if requireReview {
		status = "pending"
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: projectChangelogAggregate, EntityID: internalID, AggregateType: projectChangelogAggregate,
		AggregateKey: publicID, Snapshot: raw, Reason: snapshot.Reason, ActorID: claims.Subject, Source: "user", Status: status,
		Metadata: map[string]any{"targetLabel": target.Name + " - " + snapshot.ProjectVersion, "targetURL": target.CanonicalPath + "?tab=changelog"}, Request: r,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create changelog revision")
		return
	}
	if status == "approved" {
		if err = applyProjectChangelogSnapshotTx(r.Context(), tx, internalID, target.RouteID, created.RevisionID, claims.Subject, snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish changelog")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save changelog")
		return
	}
	annotateActivity(r, activity.ActionCreate, activity.ObjectChangelog, publicID, 0)
	writeJSON(w, http.StatusCreated, map[string]any{"id": publicID, "reviewStatus": status, "changeRequestId": created.ChangeRequestPublicID})
}

func (s *Server) projectChangelogHistory(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	pageRequest, err := parseContentHistoryPageRequest(r.URL.Query(), contentHistoryScope("changelog", publicID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry, target, publishedRevisionID, err := s.loadProjectChangelogEditor(r.Context(), publicID, currentClaims(r))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "changelog not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load changelog history")
		}
		return
	}
	visibility := projectPendingReviewVisibility(currentClaims(r), target.PublicID, target.CanEdit)
	items, err := contentRevisionHistoryPage(r.Context(), s.db, projectChangelogAggregate, entry.ID, publishedRevisionID, visibility, pageRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load changelog history")
		return
	}
	writeJSON(w, http.StatusOK, mergeContentHistoryPage(items, nil, pageRequest))
}

func normalizeProjectChangelogSnapshot(snapshot *projectChangelogSnapshot) error {
	snapshot.ProjectVersion = strings.TrimSpace(snapshot.ProjectVersion)
	snapshot.DefaultLocale = normalizeContentLocale(snapshot.DefaultLocale)
	snapshot.CategoryID = strings.ToLower(strings.TrimSpace(snapshot.CategoryID))
	snapshot.Reason = strings.TrimSpace(snapshot.Reason)
	if snapshot.EventAt.IsZero() || len(snapshot.ProjectVersion) == 0 || len(snapshot.ProjectVersion) > 120 ||
		!isEditableContentLocale(snapshot.DefaultLocale) || len(snapshot.Reason) > 500 {
		return errors.New("invalid changelog")
	}
	versions, err := normalizeStringList(snapshot.MinecraftVersions, 100)
	if err != nil || len(versions) == 0 {
		return errors.New("invalid minecraft versions")
	}
	snapshot.MinecraftVersions = versions
	seen := map[string]bool{}
	localizations := make([]projectChangelogLocalization, 0, len(snapshot.Localizations))
	for _, value := range snapshot.Localizations {
		value.Locale = normalizeContentLocale(value.Locale)
		value.BodyMarkdown = strings.TrimSpace(value.BodyMarkdown)
		if !isEditableContentLocale(value.Locale) || seen[value.Locale] || len(value.BodyMarkdown) > 200000 {
			return errors.New("invalid changelog localization")
		}
		if value.BodyMarkdown != "" {
			seen[value.Locale] = true
			localizations = append(localizations, value)
		}
	}
	if !seen[snapshot.DefaultLocale] {
		return errors.New("default changelog localization is required")
	}
	snapshot.Localizations = localizations
	if snapshot.CategoryID != "" && snapshot.NewCategory != nil || snapshot.CategoryID != "" && !validCatalogPublicID(snapshot.CategoryID) {
		return errors.New("invalid changelog category")
	}
	if snapshot.NewCategory != nil {
		snapshot.NewCategory.DefaultLocale = normalizeContentLocale(snapshot.NewCategory.DefaultLocale)
		if !isEditableContentLocale(snapshot.NewCategory.DefaultLocale) {
			return errors.New("invalid changelog category locale")
		}
		categorySeen := map[string]bool{}
		values := make([]projectChangelogCategoryLocalization, 0, len(snapshot.NewCategory.Localizations))
		for _, value := range snapshot.NewCategory.Localizations {
			value.Locale = normalizeContentLocale(value.Locale)
			value.Name = strings.TrimSpace(value.Name)
			if !isEditableContentLocale(value.Locale) || categorySeen[value.Locale] || len(value.Name) > 80 {
				return errors.New("invalid changelog category localization")
			}
			if value.Name != "" {
				categorySeen[value.Locale] = true
				values = append(values, value)
			}
		}
		if !categorySeen[snapshot.NewCategory.DefaultLocale] {
			return errors.New("default changelog category name is required")
		}
		snapshot.NewCategory.Localizations = values
	}
	return nil
}

func (s *Server) validateProjectChangelogMinecraftVersions(w http.ResponseWriter, r *http.Request, snapshot *projectChangelogSnapshot) bool {
	versions, err := authoritativeMinecraftVersionCodes(r.Context(), s.db, snapshot.MinecraftVersions, 100)
	if err != nil || len(versions) == 0 {
		if err != nil && !errors.Is(err, errInvalidMinecraftVersionCodes) && !errors.Is(err, errUnknownMinecraftVersionCodes) {
			writeError(w, http.StatusInternalServerError, "failed to load Minecraft version settings")
			return false
		}
		writeError(w, http.StatusBadRequest, "unknown or invalid Minecraft version")
		return false
	}
	snapshot.MinecraftVersions = versions
	return true
}

func (s *Server) resolveProjectChangelogTarget(ctx context.Context, entityType, publicID string, claims security.Claims) (projectChangelogTarget, error) {
	var target projectChangelogTarget
	entityType = normalizeChangelogTargetType(entityType)
	if entityType == "" {
		return target, pgx.ErrNoRows
	}
	err := s.db.QueryRow(ctx, `select id,internal_id,entity_type,public_id,canonical_path from public_routes
		where entity_type=$1 and public_id=$2`, entityType, publicID).Scan(&target.RouteID, &target.InternalID, &target.EntityType, &target.PublicID, &target.CanonicalPath)
	if err != nil {
		return target, err
	}
	switch entityType {
	case "mod":
		err = s.db.QueryRow(ctx, `select primary_name from mods where id=$1 and review_status='approved'`, target.InternalID).Scan(&target.Name)
	case "modpack":
		err = s.db.QueryRow(ctx, `select primary_name from modpacks where id=$1 and review_status='approved'`, target.InternalID).Scan(&target.Name)
	case "minecraft_server":
		err = s.db.QueryRow(ctx, `select name from minecraft_servers where id=$1 and review_status='approved'`, target.InternalID).Scan(&target.Name)
	default:
		err = s.db.QueryRow(ctx, `select primary_name from simple_projects where id=$1 and project_type=$2 and review_status='approved'`, target.InternalID, entityType).Scan(&target.Name)
	}
	if err != nil {
		return target, err
	}
	target.CanEdit = canEditReviewTarget(ctx, s.db, claims, entityType, target.InternalID, target.PublicID)
	return target, nil
}

func normalizeChangelogTargetType(value string) string {
	value = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	if value == "server" {
		value = "minecraft_server"
	}
	if changelogTargetTypes[value] {
		return value
	}
	return ""
}

func canSkipChangelogReview(claims security.Claims, targetPublicID string) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.no-review") ||
		claimsAllow(claims, "project.no-review") || claimsAllow(claims, "project.no-review."+targetPublicID)
}

func entryInternalIDTx(ctx context.Context, query databaseQuery, publicID string) (int64, error) {
	var id int64
	err := query.QueryRow(ctx, `select id from project_changelogs where public_id=$1`, publicID).Scan(&id)
	return id, err
}

func projectChangelogCategoryID(ctx context.Context, query databaseQuery, publicID string, targetRouteID int64) (*int64, error) {
	if publicID == "" {
		return nil, nil
	}
	var id int64
	if err := query.QueryRow(ctx, `select id from project_changelog_categories where public_id=$1 and object_route_id=$2 for share`, publicID, targetRouteID).Scan(&id); err != nil {
		return nil, err
	}
	return &id, nil
}

func validateProjectChangelogCategory(w http.ResponseWriter, r *http.Request, tx pgx.Tx, publicID string, targetRouteID int64) bool {
	if _, err := projectChangelogCategoryID(r.Context(), tx, publicID, targetRouteID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "unknown changelog category for this project")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load changelog category")
		}
		return false
	}
	return true
}

func applyProjectChangelogSnapshotTx(ctx context.Context, tx pgx.Tx, changelogID, targetRouteID, revisionID, actorID int64, snapshot projectChangelogSnapshot) error {
	versions, err := authoritativeMinecraftVersionCodes(ctx, tx, snapshot.MinecraftVersions, 100)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return errInvalidMinecraftVersionCodes
	}
	snapshot.MinecraftVersions = versions
	categoryID, err := projectChangelogCategoryID(ctx, tx, snapshot.CategoryID, targetRouteID)
	if err != nil {
		return err
	}
	if snapshot.NewCategory != nil {
		var id int64
		if err := tx.QueryRow(ctx, `insert into project_changelog_categories(object_route_id,default_locale,created_by)
			values($1,$2,$3) returning id`, targetRouteID, snapshot.NewCategory.DefaultLocale, actorID).Scan(&id); err != nil {
			return err
		}
		for _, value := range snapshot.NewCategory.Localizations {
			if _, err := tx.Exec(ctx, `insert into project_changelog_category_localizations(category_id,locale,name) values($1,$2,$3)`, id, value.Locale, value.Name); err != nil {
				return err
			}
		}
		categoryID = &id
	}
	if _, err := tx.Exec(ctx, `update project_changelogs set category_id=$2,event_at=$3,minecraft_versions=$4,
		project_version=$5,default_locale=$6,review_status='approved',published_revision_id=$7,
		published_at=coalesce(published_at,now()),updated_at=now() where id=$1`, changelogID, categoryID,
		snapshot.EventAt, snapshot.MinecraftVersions, snapshot.ProjectVersion, snapshot.DefaultLocale, revisionID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from project_changelog_localizations where changelog_id=$1`, changelogID); err != nil {
		return err
	}
	for _, value := range snapshot.Localizations {
		if _, err := tx.Exec(ctx, `insert into project_changelog_localizations(
			changelog_id,locale,body_markdown,source_revision_id,updated_by) values($1,$2,$3,$4,$5)`,
			changelogID, value.Locale, value.BodyMarkdown, revisionID, actorID); err != nil {
			return err
		}
	}
	if err := recordApprovedChangelogManualOverrideTx(ctx, tx, changelogID, revisionID); err != nil {
		return err
	}
	return enqueueProjectUpdateEventTx(ctx, tx, targetRouteID, revisionID, actorID, "changelog", []string{"changelog", "minecraft_versions", "project_version"}, "changelog-revision:"+strconv.FormatInt(revisionID, 10))
}

func recordApprovedChangelogManualOverrideTx(ctx context.Context, tx pgx.Tx, changelogID, revisionID int64) error {
	_, err := tx.Exec(ctx, `update external_release_bindings binding set
		manual_override=true,manual_override_revision_id=revision.id,manual_override_source=revision.source,updated_at=now()
		from content_revisions revision,project_changelogs changelog
		where changelog.id=$1 and revision.id=$2 and revision.aggregate_type=$3
		  and revision.aggregate_key=changelog.public_id and revision.source='user'
		  and binding.changelog_public_id=changelog.public_id and binding.source_managed and not binding.manual_override`,
		changelogID, revisionID, projectChangelogAggregate)
	return err
}

func localizedChangelogValue(values map[string]string, locale, fallback string) string {
	for _, key := range []string{normalizeContentLocale(locale), normalizeContentLocale(fallback), "zh-CN", "en-US"} {
		if strings.TrimSpace(values[key]) != "" {
			return values[key]
		}
	}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (s *Server) loadProjectChangelogs(ctx context.Context, target projectChangelogTarget, locale string, request projectChangelogPageRequest) (projectChangelogPage, error) {
	query, args := projectChangelogPageSQL(target.RouteID, locale, request)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return projectChangelogPage{}, err
	}
	defer rows.Close()
	items := make([]projectChangelogSummary, 0, request.Limit+1)
	for rows.Next() {
		var item projectChangelogSummary
		var categoryID, categoryDefaultLocale, categoryName string
		if err = rows.Scan(&item.InternalID, &item.ID, &item.EventAt, &item.MinecraftVersions, &item.ProjectVersion,
			&item.DefaultLocale, &item.ReviewStatus, &item.CreatedAt, &item.UpdatedAt,
			&item.CreatedByID, &item.CreatedByName, &categoryID, &categoryDefaultLocale,
			&categoryName, &item.Locale, &item.BodyExcerpt, &item.AvailableLocales, &item.PendingChange); err != nil {
			return projectChangelogPage{}, err
		}
		if len(item.AvailableLocales) > projectChangelogMaximumLocalizations {
			return projectChangelogPage{}, errors.New("changelog localization limit exceeded")
		}
		preview := []rune(item.BodyExcerpt)
		item.BodyTruncated = len(preview) > projectChangelogPreviewCharacterLimit
		if item.BodyTruncated {
			item.BodyExcerpt = string(preview[:projectChangelogPreviewCharacterLimit])
		}
		item.CanEdit = target.CanEdit
		if categoryID != "" {
			item.Category = &projectChangelogCategorySummary{ID: categoryID, DefaultLocale: categoryDefaultLocale, Name: categoryName}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return projectChangelogPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeProjectChangelogPageCursor(projectChangelogPageCursor{
			Version: projectChangelogCursorVersion, Scope: request.Scope, EventAt: last.EventAt, ID: last.InternalID,
		})
	}
	return projectChangelogPage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func (s *Server) loadProjectChangelogEditor(ctx context.Context, publicID string, claims security.Claims) (projectChangelogResponse, projectChangelogTarget, *int64, error) {
	var item projectChangelogResponse
	var targetType, targetPublicID string
	var categoryID, categoryDefaultLocale string
	var categoryRaw, bodyRaw []byte
	var createdByInternal int64
	var publishedRevisionID *int64
	err := s.db.QueryRow(ctx, `select entry.public_id,target.entity_type,target.public_id,
		entry.event_at,entry.minecraft_versions,entry.project_version,entry.default_locale,entry.review_status,
		entry.created_at,entry.updated_at,entry.created_by,creator.public_id,creator.username,entry.published_revision_id,
		coalesce(category.public_id,''),coalesce(category.default_locale,''),
		coalesce(category_names.values,'{}'::jsonb),coalesce(body.values,'{}'::jsonb),
		exists(select 1 from change_requests request where request.aggregate_type=$2 and request.aggregate_key=entry.public_id and request.status='pending')
		from project_changelogs entry
		join public_routes target on target.id=entry.object_route_id
		join users creator on creator.id=entry.created_by
		left join project_changelog_categories category on category.id=entry.category_id
		left join lateral (select jsonb_object_agg(value.locale,value.name) values from (
			select localization.locale,localization.name from project_changelog_category_localizations localization
			where localization.category_id=category.id order by localization.locale limit 9) value) category_names on true
		left join lateral (select jsonb_object_agg(value.locale,value.body_markdown) values from (
			select localization.locale,localization.body_markdown from project_changelog_localizations localization
			where localization.changelog_id=entry.id order by localization.locale limit 9) value) body on true
		where entry.public_id=$1 and entry.status='active'`, publicID, projectChangelogAggregate).Scan(
		&item.ID, &targetType, &targetPublicID, &item.EventAt, &item.MinecraftVersions,
		&item.ProjectVersion, &item.DefaultLocale, &item.ReviewStatus, &item.CreatedAt, &item.UpdatedAt,
		&createdByInternal, &item.CreatedByID, &item.CreatedByName, &publishedRevisionID,
		&categoryID, &categoryDefaultLocale, &categoryRaw, &bodyRaw, &item.PendingChange)
	if err != nil {
		return item, projectChangelogTarget{}, nil, err
	}
	target, err := s.resolveProjectChangelogTarget(ctx, targetType, targetPublicID, claims)
	if err != nil {
		return item, target, nil, err
	}
	canReview := canReviewProjectSubmission(claims, target.PublicID, createdByInternal)
	if item.ReviewStatus != "approved" && claims.Subject != createdByInternal && !target.CanEdit && !canReview {
		return item, target, nil, pgx.ErrNoRows
	}
	bodyValues := map[string]string{}
	if err = json.Unmarshal(bodyRaw, &bodyValues); err != nil || len(bodyValues) > projectChangelogMaximumLocalizations {
		return item, target, nil, errors.New("changelog localization limit exceeded")
	}
	for _, body := range bodyValues {
		if utf8.RuneCountInString(body) > 200000 {
			return item, target, nil, errors.New("changelog body limit exceeded")
		}
	}
	item.Locale, item.BodyMarkdown = selectedChangelogLocalization(bodyValues, item.DefaultLocale, item.DefaultLocale)
	item.AvailableLocales = sortedMapKeys(bodyValues)
	for _, key := range item.AvailableLocales {
		item.Localizations = append(item.Localizations, projectChangelogLocalization{Locale: key, BodyMarkdown: bodyValues[key]})
	}
	item.CanEdit = target.CanEdit
	if categoryID != "" {
		categoryNames := map[string]string{}
		if err = json.Unmarshal(categoryRaw, &categoryNames); err != nil || len(categoryNames) > projectChangelogMaximumLocalizations {
			return item, target, nil, errors.New("changelog category localization limit exceeded")
		}
		item.Category = &projectChangelogCategoryResponse{ID: categoryID, DefaultLocale: categoryDefaultLocale,
			Names: categoryNames, Name: localizedChangelogValue(categoryNames, item.DefaultLocale, categoryDefaultLocale)}
	}
	return item, target, publishedRevisionID, nil
}

func selectedChangelogLocalization(values map[string]string, locale, fallback string) (string, string) {
	for _, key := range []string{normalizeContentLocale(locale), normalizeContentLocale(fallback), "zh-CN", "en-US"} {
		if strings.TrimSpace(values[key]) != "" {
			return key, values[key]
		}
	}
	keys := sortedMapKeys(values)
	if len(keys) > 0 {
		return keys[0], values[keys[0]]
	}
	return normalizeContentLocale(fallback), ""
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
