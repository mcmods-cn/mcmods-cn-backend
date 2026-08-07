package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

const simpleProjectAggregate = "simple_project"

var simpleProjectTypes = stringSet("plugin", "map", "resource_pack", "shader_pack", "datapack", "addon")
var simpleProjectParentTypes = stringSet("mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack")

type simpleProjectLocalization struct {
	Locale       string `json:"locale"`
	Name         string `json:"name"`
	Summary      string `json:"summary"`
	BodyMarkdown string `json:"bodyMarkdown"`
}

type simpleProjectParent struct {
	PublicID   string `json:"publicId,omitempty"`
	Type       string `json:"type"`
	Identifier string `json:"identifier,omitempty"`
	Name       string `json:"name,omitempty"`
	SiteID     string `json:"siteId,omitempty"`
	IconURL    string `json:"iconUrl,omitempty"`
	Unresolved bool   `json:"unresolved"`
}

type simpleProjectSnapshot struct {
	ProjectType         string                      `json:"projectType"`
	SiteID              string                      `json:"siteId"`
	DefaultLocale       string                      `json:"defaultLocale"`
	Localizations       []simpleProjectLocalization `json:"localizations"`
	Abbreviation        string                      `json:"abbreviation"`
	MinecraftVersions   []string                    `json:"minecraftVersions"`
	Loaders             []string                    `json:"loaders"`
	Categories          []string                    `json:"categories"`
	Features            []string                    `json:"features"`
	Resolution          string                      `json:"resolution"`
	Performance         string                      `json:"performance"`
	MapSize             string                      `json:"mapSize"`
	OfficialStatus      string                      `json:"officialStatus"`
	SourceStatus        string                      `json:"sourceStatus"`
	License             string                      `json:"license"`
	CurseForgeProjectID string                      `json:"curseforgeProjectId"`
	ModrinthProjectID   string                      `json:"modrinthProjectId"`
	IconURL             string                      `json:"iconUrl"`
	SearchKeywords      []string                    `json:"searchKeywords"`
	SubmissionMethod    string                      `json:"submissionMethod"`
	Authors             []modAuthorPayload          `json:"authors"`
	Links               []modLinkPayload            `json:"links"`
	GalleryImages       []modGalleryImagePayload    `json:"galleryImages"`
	ParentProjects      []simpleProjectParent       `json:"parentProjects"`
}

type simpleProjectResponse struct {
	ID       int64  `json:"-"`
	PublicID string `json:"id"`
	simpleProjectSnapshot
	ReviewStatus         string     `json:"reviewStatus"`
	CreatedByInternal    *int64     `json:"-"`
	CreatedBy            string     `json:"createdBy,omitempty"`
	PublishedRevisionID  *string    `json:"publishedRevisionId,omitempty"`
	SubmissionRevisionID string     `json:"submissionRevisionId,omitempty"`
	ChangeRequestID      string     `json:"changeRequestId,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
	PublishedAt          *time.Time `json:"publishedAt,omitempty"`
	CanEdit              bool       `json:"canEdit"`
}

const simpleProjectCatalogFilter = `where project.project_type=$1
	and (project.review_status='approved' or project.created_by=$2)
	and (($7 and project.id=any($8::bigint[])) or (not $7 and ($3='' or project.slug ilike '%%'||$3||'%%' or project.public_id ilike '%%'||$3||'%%'
		or project.primary_name ilike '%%'||$3||'%%' or project.summary ilike '%%'||$3||'%%'
		or exists(select 1 from simple_project_localizations localization where localization.project_id=project.id
			and (localization.name ilike '%%'||$3||'%%' or localization.summary ilike '%%'||$3||'%%')))))
	and ($4='' or $4=any(project.categories)) and ($5='' or $5=any(project.minecraft_versions))
	and ($6='' or $6=any(project.loaders))`

func (s *Server) simpleProjects(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	if projectType == "" {
		writeError(w, http.StatusNotFound, "project type not found")
		return
	}
	if r.Method == http.MethodPost {
		s.createSimpleProject(w, r, projectType)
		return
	}
	claims := currentClaims(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	loader := strings.TrimSpace(r.URL.Query().Get("loader"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	indexed := s.searchProjectPage(r.Context(), query, projectType, category, version, loader, claims, limit, offset)
	databaseOffset := offset
	if indexed.Used {
		databaseOffset = 0
	}
	var total int
	if indexed.Used {
		total = indexed.Total
	} else if err := s.db.QueryRow(r.Context(), `select count(*) from simple_projects project `+simpleProjectCatalogFilter,
		projectType, claims.Subject, query, category, version, loader, false, []int64{}).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count projects")
		return
	}
	rows, err := s.db.Query(r.Context(), `select project.id,project.public_id,project.project_type,project.slug,project.default_locale,
		project.abbreviation,project.minecraft_versions,project.loaders,project.categories,project.features,project.resolution,
		project.performance,project.map_size,project.official_status,project.source_status,project.license,
		project.curseforge_project_id,project.modrinth_project_id,project.icon_url,project.search_keywords,
		project.submission_method,project.review_status,project.created_by,coalesce(account.public_id,''),revision.public_id,
		project.created_at,project.updated_at,project.published_at from simple_projects project
		left join users account on account.id=project.created_by
		left join content_revisions revision on revision.id=project.published_revision_id
		left join public_routes popularity_route on popularity_route.entity_type=project.project_type and popularity_route.internal_id=project.id
		left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		`+simpleProjectCatalogFilter+`
		order by case when $7 then array_position($8::bigint[],project.id) end,
			case when not $7 then coalesce(popularity.heat_score,0) end desc,
			project.updated_at desc,project.id desc
		limit $9 offset $10`, projectType, claims.Subject, query, category, version, loader,
		indexed.Used, indexed.IDs, limit, databaseOffset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load projects")
		return
	}
	defer rows.Close()
	items := make([]simpleProjectResponse, 0)
	for rows.Next() {
		item, scanErr := scanSimpleProject(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode project")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil || s.loadSimpleProjectAssociations(r.Context(), items) != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project details")
		return
	}
	ossCfg := s.ossConfigFromSettings(r.Context())
	for index := range items {
		items[index].CanEdit = canEditSimpleProject(claims, items[index])
		items[index].IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, items[index].IconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate project icon URL")
			return
		}
		for parentIndex := range items[index].ParentProjects {
			items[index].ParentProjects[parentIndex].IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, items[index].ParentProjects[parentIndex].IconURL)
			if err != nil {
				writeError(w, http.StatusBadGateway, "failed to generate parent project icon URL")
				return
			}
		}
		if err = s.resolveModAuthorOSSURLsWithConfig(r.Context(), ossCfg, items[index].Authors); err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate project author avatar URL")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (s *Server) simpleProjectItem(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	if projectType == "" || siteID == "" {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if r.Method == http.MethodPut {
		s.createSimpleProjectRevision(w, r, projectType, siteID)
		return
	}
	item, err := s.simpleProjectBySiteID(r.Context(), projectType, siteID, currentClaims(r).Subject, false)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	item.CanEdit = canEditSimpleProject(currentClaims(r), item)
	ossCfg := s.ossConfigFromSettings(r.Context())
	item.IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.IconURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate project icon URL")
		return
	}
	for index := range item.ParentProjects {
		item.ParentProjects[index].IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.ParentProjects[index].IconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate parent project icon URL")
			return
		}
	}
	if err = s.resolveModAuthorOSSURLsWithConfig(r.Context(), ossCfg, item.Authors); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate project author avatar URL")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) simpleProjectIcon(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	if projectType == "" || siteID == "" {
		writeError(w, http.StatusBadRequest, "project icon path is invalid")
		return
	}
	var iconURL string
	err := s.db.QueryRow(r.Context(), `select icon_url from simple_projects
		where project_type=$1 and slug=$2 and (review_status='approved' or created_by=$3)`,
		projectType, siteID, currentClaims(r).Subject).Scan(&iconURL)
	if errors.Is(err, pgx.ErrNoRows) || strings.TrimSpace(iconURL) == "" {
		writeError(w, http.StatusNotFound, "project icon does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project icon")
		return
	}
	s.redirectStoredRasterURL(w, r, iconURL)
}

func (s *Server) simpleProjectEditor(w http.ResponseWriter, r *http.Request) {
	item, err := s.simpleProjectBySiteID(r.Context(), normalizeSimpleProjectType(r.PathValue("projectType")),
		normalizeModSiteID(r.PathValue("siteId")), currentClaims(r).Subject, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !canEditSimpleProject(currentClaims(r), item) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	item.CanEdit = true
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createSimpleProject(w http.ResponseWriter, r *http.Request, projectType string) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "project.create."+projectType) && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "project creation permission is required")
		return
	}
	var snapshot simpleProjectSnapshot
	if decodeJSON(r, &snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid project")
		return
	}
	snapshot.ProjectType = projectType
	if err := normalizeAndValidateSimpleProject(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	config := loadReviewConfig(r.Context(), s.db)
	reviewStatus := "approved"
	if simpleProjectReviewRequired(config, projectType, "create") && !claimsAllow(claims, "content.no-review") && !claimsAllow(claims, "admin.*") {
		reviewStatus = "pending"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start project creation")
		return
	}
	defer tx.Rollback(r.Context())
	if snapshot.SiteID == "" {
		snapshot.SiteID, err = availableSimpleProjectSiteID(r.Context(), tx, projectType, defaultSimpleProjectLocalization(snapshot).Name)
	} else {
		err = ensureSimpleProjectSiteIDAvailable(r.Context(), tx, projectType, snapshot.SiteID, 0)
	}
	if err != nil {
		writeError(w, http.StatusConflict, "project site ID is already used")
		return
	}
	publicID, err := availableModUniqueID(r.Context(), tx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate project ID")
		return
	}
	if err = s.prepareImportedSimpleProjectAssets(r.Context(), &snapshot, publicID, claims.Subject); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	localization := defaultSimpleProjectLocalization(snapshot)
	var projectID int64
	err = tx.QueryRow(r.Context(), `insert into simple_projects(public_id,project_type,slug,default_locale,primary_name,summary,
		body_markdown,abbreviation,minecraft_versions,loaders,categories,features,resolution,performance,map_size,
		official_status,source_status,license,curseforge_project_id,modrinth_project_id,icon_url,search_keywords,
		submission_method,review_status,created_by,published_at) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,
		$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,case when $24='approved' then now() else null end) returning id`,
		publicID, projectType, snapshot.SiteID, snapshot.DefaultLocale, localization.Name, localization.Summary,
		localization.BodyMarkdown, snapshot.Abbreviation, snapshot.MinecraftVersions, snapshot.Loaders, snapshot.Categories,
		snapshot.Features, snapshot.Resolution, snapshot.Performance, snapshot.MapSize, snapshot.OfficialStatus,
		snapshot.SourceStatus, snapshot.License, snapshot.CurseForgeProjectID, snapshot.ModrinthProjectID, snapshot.IconURL,
		snapshot.SearchKeywords, snapshot.SubmissionMethod, reviewStatus, claims.Subject).Scan(&projectID)
	if err != nil {
		writeError(w, http.StatusConflict, "failed to save project")
		return
	}
	if err = validateSimpleProjectGalleryTx(r.Context(), tx, 0, claims.Subject, snapshot.GalleryImages); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: projectType, EntityID: projectID, AggregateType: simpleProjectAggregate, AggregateKey: publicID,
		Snapshot: raw, ActorID: claims.Subject, Source: snapshot.SubmissionMethod, Status: reviewStatus,
		Metadata: map[string]any{"projectType": projectType, "siteId": snapshot.SiteID, "initial": true}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create project revision")
		}
		return
	}
	if reviewStatus == "approved" {
		if err = applySimpleProjectSnapshotTx(r.Context(), tx, projectID, created.RevisionID, claims.Subject, snapshot); err == nil {
			err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish project")
			return
		}
	} else if err = replaceSimpleProjectAssociationsTx(r.Context(), tx, projectID, projectType, created.RevisionID, claims.Subject, snapshot); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save project relations")
		return
	}
	defaults := s.permissionDefaultsFromSettings(r.Context())
	if defaults.DeveloperRole != "" {
		if err = s.bindProjectRoleTx(r.Context(), tx, claims.Subject, defaults.DeveloperRole, publicID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign project owner permissions")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project")
		return
	}
	item, err := s.simpleProjectBySiteID(r.Context(), projectType, snapshot.SiteID, claims.Subject, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read created project")
		return
	}
	item.CanEdit = true
	item.SubmissionRevisionID = created.RevisionPublicID
	item.ChangeRequestID = created.ChangeRequestPublicID
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createSimpleProjectRevision(w http.ResponseWriter, r *http.Request, projectType, siteID string) {
	claims := currentClaims(r)
	current, err := s.simpleProjectBySiteID(r.Context(), projectType, siteID, claims.Subject, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !canEditSimpleProject(claims, current) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	var request struct {
		Snapshot       simpleProjectSnapshot `json:"snapshot"`
		BaseRevisionID string                `json:"baseRevisionId"`
		ChangeReason   string                `json:"changeReason"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid project revision")
		return
	}
	request.Snapshot.ProjectType = projectType
	if err = normalizeAndValidateSimpleProject(&request.Snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err = ensureSimpleProjectSiteIDAvailable(r.Context(), s.db, projectType, request.Snapshot.SiteID, current.ID); err != nil {
		writeError(w, http.StatusConflict, "project site ID is already used")
		return
	}
	status := "approved"
	if simpleProjectReviewRequired(loadReviewConfig(r.Context(), s.db), projectType, "edit") && !claimsAllow(claims, "content.no-review") && !claimsAllow(claims, "admin.*") {
		status = "pending"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start project revision")
		return
	}
	defer tx.Rollback(r.Context())
	if err = validateSimpleProjectGalleryTx(r.Context(), tx, current.ID, claims.Subject, request.Snapshot.GalleryImages); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var baseRevision *int64
	if request.BaseRevisionID != "" {
		var value int64
		if err = tx.QueryRow(r.Context(), `select id from content_revisions where public_id=$1 and aggregate_type=$2 and aggregate_key=$3`,
			request.BaseRevisionID, simpleProjectAggregate, current.PublicID).Scan(&value); err != nil {
			writeError(w, http.StatusConflict, "base revision is invalid")
			return
		}
		baseRevision = &value
	}
	raw, _ := json.Marshal(request.Snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: projectType, EntityID: current.ID, AggregateType: simpleProjectAggregate, AggregateKey: current.PublicID,
		BaseRevision: baseRevision, Snapshot: raw, Reason: strings.TrimSpace(request.ChangeReason), ActorID: claims.Subject,
		Source: "user", Status: status, Metadata: map[string]any{"projectType": projectType, "siteId": request.Snapshot.SiteID}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create project revision")
		}
		return
	}
	if status == "approved" {
		if err = applySimpleProjectSnapshotTx(r.Context(), tx, current.ID, created.RevisionID, claims.Subject, request.Snapshot); err == nil {
			err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r)
		}
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to save project revision")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": created.RevisionPublicID, "status": status, "siteId": request.Snapshot.SiteID, "changeRequestId": created.ChangeRequestPublicID})
}

func normalizeAndValidateSimpleProject(snapshot *simpleProjectSnapshot) error {
	return normalizeAndValidateSimpleProjectDraft(snapshot, false)
}

func normalizeAndValidateSimpleProjectDraft(snapshot *simpleProjectSnapshot, allowMissingAddonParent bool) error {
	snapshot.ProjectType = normalizeSimpleProjectType(snapshot.ProjectType)
	snapshot.SiteID = normalizeModSiteID(snapshot.SiteID)
	snapshot.DefaultLocale = normalizeContentLocale(snapshot.DefaultLocale)
	snapshot.Abbreviation = strings.TrimSpace(snapshot.Abbreviation)
	snapshot.MinecraftVersions = uniqueTrimmed(snapshot.MinecraftVersions, 500)
	snapshot.Loaders = normalizedSimpleProjectOptions(snapshot.Loaders)
	snapshot.Categories = normalizedSimpleProjectOptions(snapshot.Categories)
	snapshot.Features = normalizedSimpleProjectOptions(snapshot.Features)
	snapshot.Resolution = strings.ToLower(strings.TrimSpace(snapshot.Resolution))
	snapshot.Performance = strings.ToLower(strings.TrimSpace(snapshot.Performance))
	snapshot.MapSize = strings.ToLower(strings.TrimSpace(snapshot.MapSize))
	snapshot.OfficialStatus = strings.TrimSpace(snapshot.OfficialStatus)
	snapshot.SourceStatus = strings.TrimSpace(snapshot.SourceStatus)
	snapshot.License = strings.TrimSpace(snapshot.License)
	snapshot.CurseForgeProjectID = strings.TrimSpace(snapshot.CurseForgeProjectID)
	snapshot.ModrinthProjectID = strings.TrimSpace(snapshot.ModrinthProjectID)
	snapshot.IconURL = strings.TrimSpace(snapshot.IconURL)
	snapshot.SearchKeywords = uniqueTrimmed(snapshot.SearchKeywords, 80)
	snapshot.SubmissionMethod = strings.ToLower(strings.TrimSpace(snapshot.SubmissionMethod))
	if snapshot.ProjectType == "" || snapshot.DefaultLocale == "" || snapshot.SiteID != "" && !validModSiteID(snapshot.SiteID) ||
		!allowedModStatuses[snapshot.OfficialStatus] || !allowedModSourceStatuses[snapshot.SourceStatus] ||
		!allowedModLicenses[snapshot.License] || !stringSet("manual", "modrinth", "curseforge")[snapshot.SubmissionMethod] {
		return errors.New("invalid project identity or status")
	}
	if len(snapshot.MinecraftVersions) == 0 {
		return errors.New("at least one Minecraft version is required")
	}
	if !validSimpleProjectOptions(snapshot.ProjectType, snapshot.Loaders, snapshot.Categories, snapshot.Features) {
		return errors.New("invalid project classification")
	}
	if (snapshot.ProjectType == "plugin" || snapshot.ProjectType == "shader_pack") && len(snapshot.Loaders) == 0 {
		return errors.New("at least one loader is required")
	}
	if snapshot.ProjectType == "resource_pack" && !stringSet("8x_or_lower", "16x", "32x", "48x", "64x", "128x", "256x", "512x_or_higher")[snapshot.Resolution] ||
		snapshot.ProjectType == "shader_pack" && !stringSet("potato", "low", "medium", "high", "cinematic", "supercomputer")[snapshot.Performance] ||
		snapshot.ProjectType == "map" && !stringSet("tiny", "small", "medium", "large", "huge")[snapshot.MapSize] {
		return errors.New("the project-specific classification is required")
	}
	if snapshot.IconURL != "" && !validHTTPURL(snapshot.IconURL) {
		return errors.New("invalid project icon URL")
	}
	localizations := make([]simpleProjectLocalization, 0, len(snapshot.Localizations))
	seenLocales := map[string]bool{}
	for _, localization := range snapshot.Localizations {
		localization.Locale = normalizeContentLocale(localization.Locale)
		localization.Name = strings.TrimSpace(localization.Name)
		localization.Summary = strings.TrimSpace(localization.Summary)
		localization.BodyMarkdown = strings.TrimSpace(localization.BodyMarkdown)
		if localization.Locale == "" || localization.Name == "" || seenLocales[localization.Locale] || len([]rune(localization.Name)) > 160 || len([]rune(localization.Summary)) > 500 || len(localization.BodyMarkdown) > 1<<20 {
			return errors.New("invalid project localization")
		}
		seenLocales[localization.Locale] = true
		localizations = append(localizations, localization)
	}
	snapshot.Localizations = localizations
	if !seenLocales[snapshot.DefaultLocale] {
		return errors.New("the default language content is required")
	}
	links := make([]modLinkPayload, 0, len(snapshot.Links))
	for _, link := range snapshot.Links {
		link.Type = strings.TrimSpace(link.Type)
		link.URL = strings.TrimSpace(link.URL)
		link.Note = strings.TrimSpace(link.Note)
		if link.URL == "" {
			continue
		}
		if !allowedModLinkTypes[link.Type] || !validHTTPURL(link.URL) || len(link.Note) > 240 {
			return errors.New("invalid project link")
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return errors.New("at least one related link is required")
	}
	snapshot.Links = links
	authors := make([]modAuthorPayload, 0, len(snapshot.Authors))
	for _, author := range snapshot.Authors {
		author.CreatorID = strings.ToLower(strings.TrimSpace(author.CreatorID))
		author.Kind = strings.ToLower(strings.TrimSpace(author.Kind))
		author.Name = strings.TrimSpace(author.Name)
		author.Role = strings.TrimSpace(author.Role)
		author.AvatarURL = strings.TrimSpace(author.AvatarURL)
		if author.CreatorID == "" && author.Name == "" {
			continue
		}
		if author.CreatorID != "" && !validCatalogPublicID(author.CreatorID) || author.Kind != "" && author.Kind != "author" && author.Kind != "team" || author.AvatarURL != "" && !validHTTPURL(author.AvatarURL) {
			return errors.New("invalid project author")
		}
		authors = append(authors, author)
	}
	snapshot.Authors = authors
	if len(snapshot.GalleryImages) > 32 {
		return errors.New("project contains too many gallery images")
	}
	parents := make([]simpleProjectParent, 0, len(snapshot.ParentProjects))
	seenParents := map[string]bool{}
	for _, parent := range snapshot.ParentProjects {
		parent.Type = strings.ToLower(strings.TrimSpace(parent.Type))
		parent.PublicID = strings.ToLower(strings.TrimSpace(parent.PublicID))
		parent.Identifier = strings.TrimSpace(parent.Identifier)
		if !simpleProjectParentTypes[parent.Type] || parent.PublicID == "" && parent.Identifier == "" || parent.PublicID != "" && !validCatalogPublicID(parent.PublicID) {
			return errors.New("invalid parent project reference")
		}
		key := parent.Type + "\x00" + parent.PublicID + "\x00" + strings.ToLower(parent.Identifier)
		if !seenParents[key] {
			seenParents[key] = true
			parents = append(parents, parent)
		}
	}
	if snapshot.ProjectType == "addon" && len(parents) == 0 && !allowMissingAddonParent {
		return errors.New("an add-on requires at least one parent project")
	}
	if snapshot.ProjectType != "addon" {
		parents = nil
	}
	snapshot.ParentProjects = parents
	return nil
}

func (s *Server) prepareImportedSimpleProjectAssets(ctx context.Context, snapshot *simpleProjectSnapshot, publicID string, actorID int64) error {
	if snapshot.SubmissionMethod == "manual" {
		return nil
	}
	for index := range snapshot.Authors {
		author := &snapshot.Authors[index]
		if author.CreatorID != "" || author.AvatarURL == "" {
			continue
		}
		kind := author.Kind
		if kind == "" {
			kind = "author"
		}
		avatar, err := s.mirrorExternalCreatorAvatar(ctx, author.AvatarURL, kind, author.Name, actorID)
		if err != nil {
			log.Printf("store imported %s author avatar: provider=%s author=%q: %v", snapshot.ProjectType, snapshot.SubmissionMethod, author.Name, err)
			author.AvatarURL = ""
			continue
		}
		author.AvatarURL = avatar.URL
		author.AvatarFileID, author.AvatarInternalID = &avatar.FilePublicID, &avatar.FileInternalID
	}
	if snapshot.IconURL != "" {
		iconURL, err := s.mirrorExternalProjectIcon(ctx, snapshot.IconURL, snapshot.ProjectType, publicID, actorID)
		if err != nil {
			return fmt.Errorf("failed to store imported project icon: %w", err)
		}
		snapshot.IconURL = iconURL
	}
	return nil
}

func normalizedSimpleProjectOptions(values []string) []string {
	for index := range values {
		values[index] = strings.ToLower(strings.TrimSpace(values[index]))
	}
	return uniqueTrimmed(values, 100)
}

func validSimpleProjectOptions(projectType string, loaders, categories, features []string) bool {
	loadersByType := map[string]map[string]bool{
		"plugin":        stringSet("bukkit", "spigot", "paper", "purpur", "folia", "sponge", "bungeecord", "waterfall", "velocity"),
		"map":           {},
		"resource_pack": {},
		"shader_pack":   stringSet("optifine", "iris", "oculus", "canvas"),
		"datapack":      stringSet("vanilla", "fabric", "forge", "neoforge", "quilt"),
		"addon":         stringSet("vanilla", "fabric", "forge", "neoforge", "quilt", "bukkit", "spigot", "paper"),
	}
	categoriesByType := map[string]map[string]bool{
		"plugin":        stringSet("administration", "chat", "economy", "gameplay", "minigame", "permissions", "protection", "roleplay", "utility", "world_management"),
		"map":           stringSet("puzzle", "parkour", "survival", "redstone", "city", "rpg", "adventure", "pvp", "minigame", "horror", "creation", "story", "education"),
		"resource_pack": stringSet("combat", "cursed", "decoration", "modded", "realistic", "simplistic", "themed", "tweaks", "utility", "vanilla_like"),
		"shader_pack":   stringSet("cartoon", "semi_realistic", "realistic", "vanilla", "functional"),
		"datapack":      stringSet("adventure", "building", "decoration", "game_mechanics", "magic", "technology", "utility", "world_generation", "challenge", "optimization"),
		"addon":         {},
	}
	featuresByType := map[string]map[string]bool{
		"resource_pack": stringSet("audio", "blocks", "core_shaders", "entities", "environment", "equipment", "fonts", "gui", "items", "locale", "models"),
		"shader_pack":   stringSet("ambient_light", "bloom", "colored_lighting", "pbr", "reflection", "shadows"),
	}
	return everySimpleProjectOptionAllowed(loaders, loadersByType[projectType]) &&
		everySimpleProjectOptionAllowed(categories, categoriesByType[projectType]) &&
		everySimpleProjectOptionAllowed(features, featuresByType[projectType])
}

func everySimpleProjectOptionAllowed(values []string, allowed map[string]bool) bool {
	for _, value := range values {
		if !allowed[value] {
			return false
		}
	}
	return true
}

func defaultSimpleProjectLocalization(snapshot simpleProjectSnapshot) simpleProjectLocalization {
	for _, localization := range snapshot.Localizations {
		if localization.Locale == snapshot.DefaultLocale {
			return localization
		}
	}
	return simpleProjectLocalization{}
}

func applySimpleProjectSnapshotTx(ctx context.Context, tx pgx.Tx, projectID, revisionID, actorID int64, snapshot simpleProjectSnapshot) error {
	localization := defaultSimpleProjectLocalization(snapshot)
	if _, err := tx.Exec(ctx, `update simple_projects set slug=$2,default_locale=$3,primary_name=$4,summary=$5,
		body_markdown=$6,abbreviation=$7,minecraft_versions=$8,loaders=$9,categories=$10,features=$11,resolution=$12,
		performance=$13,map_size=$14,official_status=$15,source_status=$16,license=$17,curseforge_project_id=$18,
		modrinth_project_id=$19,icon_url=$20,search_keywords=$21,submission_method=$22,review_status='approved',
		published_revision_id=$23,published_at=coalesce(published_at,now()),updated_at=now() where id=$1`, projectID,
		snapshot.SiteID, snapshot.DefaultLocale, localization.Name, localization.Summary, localization.BodyMarkdown,
		snapshot.Abbreviation, snapshot.MinecraftVersions, snapshot.Loaders, snapshot.Categories, snapshot.Features,
		snapshot.Resolution, snapshot.Performance, snapshot.MapSize, snapshot.OfficialStatus, snapshot.SourceStatus,
		snapshot.License, snapshot.CurseForgeProjectID, snapshot.ModrinthProjectID, snapshot.IconURL,
		snapshot.SearchKeywords, snapshot.SubmissionMethod, revisionID); err != nil {
		return err
	}
	if err := replaceSimpleProjectAssociationsTx(ctx, tx, projectID, snapshot.ProjectType, revisionID, actorID, snapshot); err != nil {
		return err
	}
	return resolvePendingSimpleProjectReferencesTx(ctx, tx, projectID, snapshot.ProjectType)
}

func resolvePendingSimpleProjectReferencesTx(ctx context.Context, tx pgx.Tx, projectID int64, projectType string) error {
	var slug, modrinthID, curseForgeID, reviewStatus string
	if err := tx.QueryRow(ctx, `select slug,modrinth_project_id,curseforge_project_id,review_status
		from simple_projects where id=$1 and project_type=$2`, projectID, projectType).
		Scan(&slug, &modrinthID, &curseForgeID, &reviewStatus); err != nil {
		return err
	}
	if reviewStatus != "approved" {
		return nil
	}
	matches := []string{strings.ToLower(slug)}
	if value := strings.ToLower(strings.TrimSpace(modrinthID)); value != "" {
		matches = append(matches, value)
	}
	if value := strings.ToLower(strings.TrimSpace(curseForgeID)); value != "" {
		matches = append(matches, value)
	}
	if _, err := tx.Exec(ctx, `update community_post_project_refs reference set target_id=$1,raw_identifier=''
		from unresolved_references unresolved
		where unresolved.source_type='community_post_project' and unresolved.source_id=reference.id
		  and unresolved.status='pending' and unresolved.reference_type=$2 and unresolved.normalized_identifier=any($3)
		  and reference.target_id is null`, projectID, projectType, matches); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `update simple_project_parent_refs reference set target_id=$1,raw_identifier=''
		from unresolved_references unresolved
		where unresolved.source_type='simple_project_parent' and unresolved.source_id=reference.id
		  and unresolved.status='pending' and unresolved.reference_type=$2 and unresolved.normalized_identifier=any($3)
		  and reference.target_id is null and reference.project_id<>$1`, projectID, projectType, matches); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update unresolved_references unresolved
		set status='resolved',resolved_type=$2,resolved_id=$1,resolved_at=now(),updated_at=now()
		where unresolved.status='pending' and unresolved.reference_type=$2 and unresolved.normalized_identifier=any($3)
		  and (unresolved.source_type='community_post_project' and exists(select 1 from community_post_project_refs reference
			where reference.id=unresolved.source_id and reference.target_id=$1)
		    or unresolved.source_type='simple_project_parent' and exists(select 1 from simple_project_parent_refs reference
			where reference.id=unresolved.source_id and reference.target_id=$1))`, projectID, projectType, matches)
	return err
}

func replaceSimpleProjectAssociationsTx(ctx context.Context, tx pgx.Tx, projectID int64, projectType string, revisionID, actorID int64, snapshot simpleProjectSnapshot) error {
	if _, err := tx.Exec(ctx, `delete from simple_project_localizations where project_id=$1`, projectID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from simple_project_links where project_id=$1`, projectID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from content_creator_bindings where subject_type=$1 and subject_id=$2`, projectType, projectID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from simple_project_parent_refs where project_id=$1`, projectID); err != nil {
		return err
	}
	for _, localization := range snapshot.Localizations {
		if _, err := tx.Exec(ctx, `insert into simple_project_localizations(project_id,locale,name,summary,body_markdown) values($1,$2,$3,$4,$5)`, projectID, localization.Locale, localization.Name, localization.Summary, localization.BodyMarkdown); err != nil {
			return err
		}
	}
	for index, link := range snapshot.Links {
		if _, err := tx.Exec(ctx, `insert into simple_project_links(project_id,link_type,url,note,display_order) values($1,$2,$3,$4,$5)`, projectID, link.Type, link.URL, link.Note, index); err != nil {
			return err
		}
	}
	for index, author := range snapshot.Authors {
		creatorID, name, role, err := resolveProjectAuthorForCreateTx(ctx, tx, author, actorID, nil)
		if err != nil {
			return err
		}
		roleID, err := creatorRoleInternalIDTx(ctx, tx, author.RoleID)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,role_id,name_snapshot,role_snapshot,display_order) values($1,$2,$3,$4,$5,$6,$7)`, projectType, projectID, creatorID, roleID, name, role, index); err != nil {
			return err
		}
	}
	for index, parent := range snapshot.ParentProjects {
		var targetID *int64
		if parent.PublicID != "" {
			var value int64
			if err := tx.QueryRow(ctx, `select internal_id from public_routes where public_id=$1 and entity_type=$2`, parent.PublicID, parent.Type).Scan(&value); err != nil {
				return errors.New("selected parent project does not exist")
			}
			targetID = &value
		}
		var referenceID int64
		if err := tx.QueryRow(ctx, `insert into simple_project_parent_refs(project_id,target_type,target_id,raw_identifier,display_order) values($1,$2,$3,$4,$5) returning id`, projectID, parent.Type, targetID, parent.Identifier, index).Scan(&referenceID); err != nil {
			return err
		}
		if targetID == nil {
			if _, err := tx.Exec(ctx, `insert into unresolved_references(source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata)
				values('simple_project_parent',$1,'parentProjects',$2,$3,lower($3),jsonb_build_object('projectId',$4::bigint))
				on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update set raw_identifier=excluded.raw_identifier,
				status='pending',resolved_type='',resolved_id=null,resolved_at=null,metadata=excluded.metadata,updated_at=now()`, referenceID, parent.Type, parent.Identifier, projectID); err != nil {
				return err
			}
		}
	}
	return replaceSimpleProjectGalleryTx(ctx, tx, projectID, revisionID, actorID, snapshot.GalleryImages)
}

func scanSimpleProject(row scanner) (simpleProjectResponse, error) {
	var item simpleProjectResponse
	err := row.Scan(&item.ID, &item.PublicID, &item.ProjectType, &item.SiteID, &item.DefaultLocale, &item.Abbreviation,
		&item.MinecraftVersions, &item.Loaders, &item.Categories, &item.Features, &item.Resolution, &item.Performance,
		&item.MapSize, &item.OfficialStatus, &item.SourceStatus, &item.License, &item.CurseForgeProjectID,
		&item.ModrinthProjectID, &item.IconURL, &item.SearchKeywords, &item.SubmissionMethod, &item.ReviewStatus,
		&item.CreatedByInternal, &item.CreatedBy, &item.PublishedRevisionID, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt)
	item.Localizations = []simpleProjectLocalization{}
	item.Authors = []modAuthorPayload{}
	item.Links = []modLinkPayload{}
	item.GalleryImages = []modGalleryImagePayload{}
	item.ParentProjects = []simpleProjectParent{}
	return item, err
}

func (s *Server) simpleProjectBySiteID(ctx context.Context, projectType, siteID string, viewerID int64, editor bool) (simpleProjectResponse, error) {
	row := s.db.QueryRow(ctx, `select project.id,project.public_id,project.project_type,project.slug,project.default_locale,
		project.abbreviation,project.minecraft_versions,project.loaders,project.categories,project.features,project.resolution,
		project.performance,project.map_size,project.official_status,project.source_status,project.license,
		project.curseforge_project_id,project.modrinth_project_id,project.icon_url,project.search_keywords,
		project.submission_method,project.review_status,project.created_by,coalesce(account.public_id,''),revision.public_id,
		project.created_at,project.updated_at,project.published_at from simple_projects project
		left join users account on account.id=project.created_by left join content_revisions revision on revision.id=project.published_revision_id
		where project.project_type=$1 and project.slug=$2 and ($3 or project.review_status='approved' or project.created_by=$4)`, projectType, siteID, editor, viewerID)
	item, err := scanSimpleProject(row)
	if err != nil {
		return item, err
	}
	values := []simpleProjectResponse{item}
	if err = s.loadSimpleProjectAssociations(ctx, values); err != nil {
		return item, err
	}
	return values[0], nil
}

func (s *Server) loadSimpleProjectAssociations(ctx context.Context, items []simpleProjectResponse) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]*simpleProjectResponse, len(items))
	ids := make([]int64, 0, len(items))
	for index := range items {
		byID[items[index].ID] = &items[index]
		ids = append(ids, items[index].ID)
	}
	rows, err := s.db.Query(ctx, `select project_id,locale,name,summary,body_markdown from simple_project_localizations where project_id=any($1) order by project_id,locale`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value simpleProjectLocalization
		if err = rows.Scan(&id, &value.Locale, &value.Name, &value.Summary, &value.BodyMarkdown); err != nil {
			rows.Close()
			return err
		}
		byID[id].Localizations = append(byID[id].Localizations, value)
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select project_id,link_type,url,note from simple_project_links where project_id=any($1) order by project_id,display_order,id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value modLinkPayload
		if err = rows.Scan(&id, &value.Type, &value.URL, &value.Note); err != nil {
			rows.Close()
			return err
		}
		byID[id].Links = append(byID[id].Links, value)
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select binding.subject_id,creator.public_id,creator.kind,creator.name,creator.avatar_url,
		coalesce(role.public_id,''),coalesce(role.name,binding.role_snapshot),coalesce((select jsonb_agg(jsonb_build_object(
		'creatorId',member.public_id,'kind',member.kind,'name',member.name,'avatarUrl',member.avatar_url,
		'roleId',member_role.public_id,'role',member_role.name,'title',membership.title) order by membership.display_order,membership.created_at)
		from creator_team_members membership join creators member on member.id=membership.member_creator_id
		join creator_role_definitions member_role on member_role.id=membership.role_id where membership.team_id=creator.id),'[]'::jsonb)
		from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
		left join creator_role_definitions role on role.id=binding.role_id
		where binding.subject_type=any($2) and binding.subject_id=any($1) order by binding.subject_id,binding.display_order,binding.id`, ids, simpleProjectTypeValues())
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value modAuthorPayload
		var raw []byte
		if err = rows.Scan(&id, &value.CreatorID, &value.Kind, &value.Name, &value.AvatarURL, &value.RoleID, &value.Role, &raw); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(raw, &value.Members)
		byID[id].Authors = append(byID[id].Authors, value)
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select gallery.project_id,gallery.public_id,file.public_id,file.original_name,file.content_type,file.size_bytes
		from simple_project_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id and file.status='active'
		where gallery.project_id=any($1) order by gallery.project_id,gallery.display_order,gallery.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value modGalleryImagePayload
		if err = rows.Scan(&id, &value.PublicID, &value.FileID, &value.Name, &value.ContentType, &value.SizeBytes); err != nil {
			rows.Close()
			return err
		}
		value.URL = "/api/v1/content-projects/" + url.PathEscape(byID[id].ProjectType) + "/" + url.PathEscape(byID[id].SiteID) + "/gallery/" + value.PublicID
		byID[id].GalleryImages = append(byID[id].GalleryImages, value)
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select ref.project_id,coalesce(route.public_id,''),ref.target_type,ref.raw_identifier,
		coalesce(mod.slug,modpack.slug,target.slug,''),coalesce(mod.primary_name,modpack.primary_name,target.primary_name,ref.raw_identifier),
		coalesce(mod.icon_url,modpack.icon_url,target.icon_url,''),ref.target_id is null
		from simple_project_parent_refs ref left join public_routes route on route.entity_type=ref.target_type and route.internal_id=ref.target_id
		left join mods mod on ref.target_type='mod' and mod.id=ref.target_id
		left join modpacks modpack on ref.target_type='modpack' and modpack.id=ref.target_id
		left join simple_projects target on target.project_type=ref.target_type and target.id=ref.target_id
		where ref.project_id=any($1) order by ref.project_id,ref.display_order,ref.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value simpleProjectParent
		if err = rows.Scan(&id, &value.PublicID, &value.Type, &value.Identifier, &value.SiteID, &value.Name, &value.IconURL, &value.Unresolved); err != nil {
			rows.Close()
			return err
		}
		byID[id].ParentProjects = append(byID[id].ParentProjects, value)
	}
	rows.Close()
	return rows.Err()
}

func simpleProjectTypeValues() []string {
	return []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"}
}

func canEditSimpleProject(claims security.Claims, item simpleProjectResponse) bool {
	return claims.Subject > 0 && (item.CreatedByInternal != nil && *item.CreatedByInternal == claims.Subject || claimsAllow(claims, "project.edit."+item.PublicID) || claimsAllow(claims, "admin.*"))
}

func normalizeSimpleProjectType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	if value == "resourcepack" || value == "texture_pack" || value == "texturepack" {
		value = "resource_pack"
	}
	if value == "shader" || value == "shaderpack" {
		value = "shader_pack"
	}
	if simpleProjectTypes[value] {
		return value
	}
	return ""
}

func simpleProjectWebPath(projectType, siteID string) string {
	prefixes := map[string]string{
		"plugin":        "/plugins/",
		"map":           "/maps/",
		"resource_pack": "/resource-packs/",
		"shader_pack":   "/shaders/",
		"datapack":      "/datapacks/",
		"addon":         "/addons/",
	}
	return prefixes[projectType] + siteID
}

func availableSimpleProjectSiteID(ctx context.Context, query databaseQuery, projectType, name string) (string, error) {
	base := modSiteIDBase(name)
	for suffix := 0; suffix < 1000; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s_%d", base, suffix+1)
		}
		if ensureSimpleProjectSiteIDAvailable(ctx, query, projectType, candidate, 0) == nil {
			return candidate, nil
		}
	}
	return "", errors.New("unable to allocate project site ID")
}

func ensureSimpleProjectSiteIDAvailable(ctx context.Context, query databaseQuery, projectType, siteID string, excludeID int64) error {
	var exists bool
	if err := query.QueryRow(ctx, `select exists(select 1 from simple_projects where project_type=$1 and slug=$2 and id<>$3)`, projectType, siteID, excludeID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errors.New("project site ID exists")
	}
	return nil
}

func validateSimpleProjectGalleryTx(ctx context.Context, tx pgx.Tx, projectID, actorID int64, images []modGalleryImagePayload) error {
	seen := map[string]bool{}
	for _, image := range images {
		if image.FileID == "" || seen[image.FileID] {
			return errors.New("project gallery contains an invalid or duplicate image")
		}
		seen[image.FileID] = true
		allowExisting := false
		if image.PublicID != "" {
			var existingProjectID int64
			var existingFileID string
			err := tx.QueryRow(ctx, `select gallery.project_id,file.public_id from simple_project_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id where gallery.public_id=$1`, image.PublicID).Scan(&existingProjectID, &existingFileID)
			allowExisting = err == nil && existingProjectID == projectID && existingFileID == image.FileID
			if err != nil && !errors.Is(err, pgx.ErrNoRows) || err == nil && !allowExisting {
				return errors.New("project gallery image reference is invalid")
			}
		}
		file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, image.FileID, ossRasterBindingScope{UploaderID: actorID, AllowAnyUploader: allowExisting})
		if err != nil {
			return errors.New("project gallery image is unavailable or not owned by the editor")
		}
		var used bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from simple_project_gallery_images where oss_file_id=$1 and project_id<>$2)`, file.ID, projectID).Scan(&used); err != nil || used {
			return errors.New("project gallery image is already used")
		}
	}
	return nil
}

func replaceSimpleProjectGalleryTx(ctx context.Context, tx pgx.Tx, projectID, revisionID, actorID int64, images []modGalleryImagePayload) error {
	if _, err := tx.Exec(ctx, `delete from simple_project_gallery_images where project_id=$1`, projectID); err != nil {
		return err
	}
	for index, image := range images {
		publicID := strings.TrimSpace(image.PublicID)
		if publicID == "" {
			if _, err := tx.Exec(ctx, `insert into simple_project_gallery_images(project_id,oss_file_id,display_order,created_by,published_revision_id) select $1,id,$3,$4,$5 from oss_files where public_id=$2`, projectID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `insert into simple_project_gallery_images(public_id,project_id,oss_file_id,display_order,created_by,published_revision_id) select $1,$2,id,$4,$5,$6 from oss_files where public_id=$3`, publicID, projectID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) simpleProjectGalleryImage(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	var objectKey, contentType string
	err := s.db.QueryRow(r.Context(), `select file.object_key,file.content_type from simple_project_gallery_images gallery
		join simple_projects project on project.id=gallery.project_id and project.project_type=$1 and project.slug=$2
		join oss_files file on file.id=gallery.oss_file_id and file.status='active'
		where gallery.public_id=$3 and (project.review_status='approved' or project.created_by=$4)`, projectType,
		normalizeModSiteID(r.PathValue("siteId")), strings.ToLower(r.PathValue("publicId")), currentClaims(r).Subject).Scan(&objectKey, &contentType)
	if err != nil {
		writeError(w, http.StatusNotFound, "project gallery image not found")
		return
	}
	s.redirectCatalogOSSAsset(w, r, objectKey, contentType)
}

func (s *Server) simpleProjectHistory(w http.ResponseWriter, r *http.Request) {
	item, err := s.simpleProjectBySiteID(r.Context(), normalizeSimpleProjectType(r.PathValue("projectType")), normalizeModSiteID(r.PathValue("siteId")), currentClaims(r).Subject, false)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	include := canEditSimpleProject(currentClaims(r), item) || claimsAllow(currentClaims(r), "content.review") || claimsAllow(currentClaims(r), "admin.*")
	var published *int64
	if item.PublishedRevisionID != nil {
		var id int64
		if s.db.QueryRow(r.Context(), `select id from content_revisions where public_id=$1`, *item.PublishedRevisionID).Scan(&id) == nil {
			published = &id
		}
	}
	items, err := contentRevisionHistory(r.Context(), s.db, simpleProjectAggregate, item.PublicID, published, include)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
