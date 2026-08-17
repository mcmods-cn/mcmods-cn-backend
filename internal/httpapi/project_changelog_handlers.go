package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

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
	categories, err := loadProjectChangelogCategories(r.Context(), s.db, target.RouteID, locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load changelog categories")
		return
	}
	items, err := s.loadProjectChangelogs(r.Context(), target, locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load changelogs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"target": target, "categories": categories, "items": items})
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
	snapshotRaw, _ := json.Marshal(snapshot)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start changelog update")
		return
	}
	defer tx.Rollback(r.Context())
	requireReview := loadReviewConfig(r.Context(), s.db).ChangelogEdit && !canSkipChangelogReview(claims, target.PublicID)
	status := "approved"
	if requireReview {
		status = "pending"
	}
	entryID := entryInternalIDTx(r.Context(), tx, publicID)
	if entryID <= 0 {
		writeError(w, http.StatusNotFound, "changelog not found")
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
	// Source-managed entries become editor-owned after the first manual edit.
	// The update worker may still report upstream changes, but must never
	// overwrite this revision afterwards.
	if _, err = tx.Exec(r.Context(), `update external_release_bindings set manual_override=true,updated_at=now()
		where changelog_public_id=$1 and source_managed`, publicID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to protect manually edited changelog")
		return
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start changelog creation")
		return
	}
	defer tx.Rollback(r.Context())
	var internalID int64
	var publicID string
	if err = tx.QueryRow(r.Context(), `insert into project_changelogs(
		object_route_id,event_at,minecraft_versions,project_version,default_locale,created_by)
		values($1,$2,$3,$4,$5,$6) returning id,public_id`, target.RouteID, snapshot.EventAt,
		snapshot.MinecraftVersions, snapshot.ProjectVersion, snapshot.DefaultLocale, claims.Subject).Scan(&internalID, &publicID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create changelog")
		return
	}
	requireReview := loadReviewConfig(r.Context(), s.db).ChangelogCreate && !canSkipChangelogReview(claims, target.PublicID)
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
	entry, target, publishedRevisionID, err := s.loadProjectChangelogEditor(r.Context(), publicID, currentClaims(r))
	if err != nil {
		writeError(w, http.StatusNotFound, "changelog not found")
		return
	}
	includeUnpublished := target.CanEdit || claimsAllow(currentClaims(r), "content.review") || claimsAllow(currentClaims(r), "admin.*")
	items, err := contentRevisionHistory(r.Context(), s.db, projectChangelogAggregate, entry.ID, publishedRevisionID, includeUnpublished)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load changelog history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
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

func entryInternalIDTx(ctx context.Context, query databaseQuery, publicID string) int64 {
	var id int64
	_ = query.QueryRow(ctx, `select id from project_changelogs where public_id=$1`, publicID).Scan(&id)
	return id
}

func applyProjectChangelogSnapshotTx(ctx context.Context, tx pgx.Tx, changelogID, targetRouteID, revisionID, actorID int64, snapshot projectChangelogSnapshot) error {
	var categoryID *int64
	if snapshot.CategoryID != "" {
		var id int64
		if err := tx.QueryRow(ctx, `select id from project_changelog_categories where public_id=$1 and object_route_id=$2`, snapshot.CategoryID, targetRouteID).Scan(&id); err != nil {
			return err
		}
		categoryID = &id
	} else if snapshot.NewCategory != nil {
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
	return nil
}

func loadProjectChangelogCategories(ctx context.Context, query interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}, targetRouteID int64, locale string) ([]projectChangelogCategoryResponse, error) {
	rows, err := query.Query(ctx, `select category.public_id,category.default_locale,
		coalesce(jsonb_object_agg(localization.locale,localization.name) filter(where localization.locale is not null),'{}'::jsonb)
		from project_changelog_categories category
		left join project_changelog_category_localizations localization on localization.category_id=category.id
		where category.object_route_id=$1 group by category.id order by category.created_at,category.id`, targetRouteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projectChangelogCategoryResponse, 0)
	for rows.Next() {
		var item projectChangelogCategoryResponse
		var raw []byte
		if err = rows.Scan(&item.ID, &item.DefaultLocale, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &item.Names)
		item.Name = localizedChangelogValue(item.Names, locale, item.DefaultLocale)
		items = append(items, item)
	}
	return items, rows.Err()
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

func (s *Server) loadProjectChangelogs(ctx context.Context, target projectChangelogTarget, locale string) ([]projectChangelogResponse, error) {
	rows, err := s.db.Query(ctx, `select entry.public_id,entry.event_at,entry.minecraft_versions,entry.project_version,
		entry.default_locale,entry.review_status,entry.created_at,entry.updated_at,
		creator.public_id,creator.username,coalesce(category.public_id,''),coalesce(category.default_locale,''),
		coalesce(category_names.values,'{}'::jsonb),coalesce(body.values,'{}'::jsonb),
		exists(select 1 from change_requests request where request.aggregate_type=$2 and request.aggregate_key=entry.public_id and request.status='pending')
		from project_changelogs entry
		join users creator on creator.id=entry.created_by
		left join project_changelog_categories category on category.id=entry.category_id
		left join lateral (select jsonb_object_agg(value.locale,value.name) values
			from project_changelog_category_localizations value where value.category_id=category.id) category_names on true
		left join lateral (select jsonb_object_agg(value.locale,value.body_markdown) values
			from project_changelog_localizations value where value.changelog_id=entry.id) body on true
		where entry.object_route_id=$1 and entry.status='active' and entry.review_status='approved'
		order by entry.event_at desc,entry.id desc`, target.RouteID, projectChangelogAggregate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projectChangelogResponse, 0)
	for rows.Next() {
		var item projectChangelogResponse
		var categoryID, categoryDefaultLocale string
		var categoryRaw, bodyRaw []byte
		if err = rows.Scan(&item.ID, &item.EventAt, &item.MinecraftVersions, &item.ProjectVersion,
			&item.DefaultLocale, &item.ReviewStatus, &item.CreatedAt, &item.UpdatedAt,
			&item.CreatedByID, &item.CreatedByName, &categoryID, &categoryDefaultLocale,
			&categoryRaw, &bodyRaw, &item.PendingChange); err != nil {
			return nil, err
		}
		bodyValues := map[string]string{}
		_ = json.Unmarshal(bodyRaw, &bodyValues)
		item.Locale, item.BodyMarkdown = selectedChangelogLocalization(bodyValues, locale, item.DefaultLocale)
		item.AvailableLocales = sortedMapKeys(bodyValues)
		item.CanEdit = target.CanEdit
		if target.CanEdit {
			for _, key := range item.AvailableLocales {
				item.Localizations = append(item.Localizations, projectChangelogLocalization{Locale: key, BodyMarkdown: bodyValues[key]})
			}
		}
		if categoryID != "" {
			categoryNames := map[string]string{}
			_ = json.Unmarshal(categoryRaw, &categoryNames)
			item.Category = &projectChangelogCategoryResponse{ID: categoryID, DefaultLocale: categoryDefaultLocale,
				Names: categoryNames, Name: localizedChangelogValue(categoryNames, locale, categoryDefaultLocale)}
		}
		items = append(items, item)
	}
	return items, rows.Err()
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
		left join lateral (select jsonb_object_agg(value.locale,value.name) values
			from project_changelog_category_localizations value where value.category_id=category.id) category_names on true
		left join lateral (select jsonb_object_agg(value.locale,value.body_markdown) values
			from project_changelog_localizations value where value.changelog_id=entry.id) body on true
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
	moderator := claimsAllow(claims, "content.review") || claimsAllow(claims, "admin.*")
	if item.ReviewStatus != "approved" && claims.Subject != createdByInternal && !moderator {
		return item, target, nil, pgx.ErrNoRows
	}
	bodyValues := map[string]string{}
	_ = json.Unmarshal(bodyRaw, &bodyValues)
	item.Locale, item.BodyMarkdown = selectedChangelogLocalization(bodyValues, item.DefaultLocale, item.DefaultLocale)
	item.AvailableLocales = sortedMapKeys(bodyValues)
	for _, key := range item.AvailableLocales {
		item.Localizations = append(item.Localizations, projectChangelogLocalization{Locale: key, BodyMarkdown: bodyValues[key]})
	}
	item.CanEdit = target.CanEdit
	if categoryID != "" {
		categoryNames := map[string]string{}
		_ = json.Unmarshal(categoryRaw, &categoryNames)
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
