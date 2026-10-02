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
	"mcmods-cn-backend/internal/catalogpolicy"
	"mcmods-cn-backend/internal/security"
)

const modpackAggregate = "modpack"

const modpackCatalogCompatibilitiesSQL = `select modpack_id,loader,array_agg(minecraft_version order by minecraft_version desc) from (
	select modpack_id,loader,minecraft_version,
		dense_rank() over(partition by modpack_id order by loader) loader_position,
		row_number() over(partition by modpack_id,loader order by minecraft_version desc) version_position
	from modpack_loader_compatibilities where modpack_id=any($1)
) ranked where loader_position<=16 and version_position<=32 group by modpack_id,loader order by modpack_id,loader`

type modpackModPayload struct {
	ModPublicID       string `json:"modPublicId,omitempty"`
	ModSiteID         string `json:"modSiteId,omitempty"`
	ModName           string `json:"modName"`
	IconURL           string `json:"iconUrl,omitempty"`
	Provider          string `json:"provider"`
	ProviderProjectID string `json:"providerProjectId,omitempty"`
	ProviderVersionID string `json:"providerVersionId,omitempty"`
	Identifier        string `json:"identifier,omitempty"`
	FileName          string `json:"fileName,omitempty"`
	ClientRequired    bool   `json:"clientRequired"`
	ServerRequired    bool   `json:"serverRequired"`
	Resolved          bool   `json:"resolved"`
}

type modpackImportSelection struct {
	Provider    string `json:"provider"`
	ProjectID   string `json:"projectId"`
	VersionID   string `json:"versionId"`
	VersionName string `json:"versionName"`
	FileID      string `json:"fileId"`
	FileName    string `json:"fileName"`
	ReleaseType string `json:"releaseType"`
	PublishedAt string `json:"publishedAt"`
}

type createModpackRequest struct {
	SiteID              string                          `json:"siteId"`
	PrimaryName         string                          `json:"primaryName"`
	SecondaryName       string                          `json:"secondaryName"`
	Abbreviation        string                          `json:"abbreviation"`
	Summary             string                          `json:"summary"`
	DefaultLocale       string                          `json:"defaultLocale"`
	Environment         string                          `json:"environment"`
	PrimaryCategory     string                          `json:"primaryCategory"`
	PackType            string                          `json:"packType"`
	PackagingMethod     string                          `json:"packagingMethod"`
	Compatibilities     []modLoaderCompatibilityPayload `json:"compatibilities"`
	Tags                []string                        `json:"tags"`
	SearchKeywords      []string                        `json:"searchKeywords"`
	Authors             []modAuthorPayload              `json:"authors"`
	OfficialStatus      string                          `json:"officialStatus"`
	SourceStatus        string                          `json:"sourceStatus"`
	License             string                          `json:"license"`
	CurseForgeProjectID string                          `json:"curseforgeProjectId"`
	ModrinthProjectID   string                          `json:"modrinthProjectId"`
	IconURL             string                          `json:"iconUrl"`
	BodyMarkdown        string                          `json:"bodyMarkdown"`
	SubmissionMethod    string                          `json:"submissionMethod"`
	Links               []modLinkPayload                `json:"links"`
	GalleryImages       []modGalleryImagePayload        `json:"galleryImages"`
	Mods                []modpackModPayload             `json:"mods"`
	ImportSelection     *modpackImportSelection         `json:"importSelection,omitempty"`
}

type modpackResponse struct {
	ID                   int64                           `json:"-"`
	PublicID             string                          `json:"id"`
	SiteID               string                          `json:"siteId"`
	PrimaryName          string                          `json:"primaryName"`
	SecondaryName        string                          `json:"secondaryName"`
	Abbreviation         string                          `json:"abbreviation"`
	Summary              string                          `json:"summary"`
	DefaultLocale        string                          `json:"defaultLocale"`
	Environment          string                          `json:"environment"`
	PrimaryCategory      string                          `json:"primaryCategory"`
	PackType             string                          `json:"packType"`
	PackagingMethod      string                          `json:"packagingMethod"`
	OfficialStatus       string                          `json:"officialStatus"`
	SourceStatus         string                          `json:"sourceStatus"`
	License              string                          `json:"license"`
	CurseForgeProjectID  string                          `json:"curseforgeProjectId"`
	ModrinthProjectID    string                          `json:"modrinthProjectId"`
	IconURL              string                          `json:"iconUrl"`
	BodyMarkdown         string                          `json:"bodyMarkdown"`
	SearchKeywords       []string                        `json:"searchKeywords"`
	SubmissionMethod     string                          `json:"submissionMethod"`
	ReviewStatus         string                          `json:"reviewStatus"`
	SubmittedByInternal  *int64                          `json:"-"`
	SubmittedBy          string                          `json:"submittedBy,omitempty"`
	PublishedRevisionID  *string                         `json:"publishedRevisionId,omitempty"`
	SubmissionRevisionID string                          `json:"submissionRevisionId,omitempty"`
	ChangeRequestID      string                          `json:"changeRequestId,omitempty"`
	CreatedAt            time.Time                       `json:"createdAt"`
	UpdatedAt            time.Time                       `json:"updatedAt"`
	PublishedAt          *time.Time                      `json:"publishedAt,omitempty"`
	Compatibilities      []modLoaderCompatibilityPayload `json:"compatibilities"`
	Tags                 []string                        `json:"tags"`
	Authors              []modAuthorPayload              `json:"authors"`
	Links                []modLinkPayload                `json:"links"`
	GalleryImages        []modGalleryImagePayload        `json:"galleryImages"`
	Mods                 []modpackModPayload             `json:"mods"`
	CanEdit              bool                            `json:"canEdit"`
}

type modpackCatalogCard struct {
	ID                  int64                           `json:"-"`
	PublicID            string                          `json:"id"`
	SiteID              string                          `json:"siteId"`
	PrimaryName         string                          `json:"primaryName"`
	SecondaryName       string                          `json:"secondaryName"`
	Abbreviation        string                          `json:"abbreviation"`
	Summary             string                          `json:"summary"`
	DefaultLocale       string                          `json:"defaultLocale"`
	Environment         string                          `json:"environment"`
	PrimaryCategory     string                          `json:"primaryCategory"`
	OfficialStatus      string                          `json:"officialStatus"`
	SourceStatus        string                          `json:"sourceStatus"`
	License             string                          `json:"license"`
	CurseForgeProjectID string                          `json:"curseforgeProjectId"`
	ModrinthProjectID   string                          `json:"modrinthProjectId"`
	IconURL             string                          `json:"iconUrl"`
	SearchKeywords      []string                        `json:"searchKeywords"`
	ReviewStatus        string                          `json:"reviewStatus"`
	CreatedAt           time.Time                       `json:"createdAt"`
	UpdatedAt           time.Time                       `json:"updatedAt"`
	PublishedAt         *time.Time                      `json:"publishedAt,omitempty"`
	Compatibilities     []modLoaderCompatibilityPayload `json:"compatibilities"`
	Tags                []string                        `json:"tags"`
	Authors             []catalogCardAuthorPayload      `json:"authors"`
	HasGallery          bool                            `json:"hasGallery"`
}

type modpackListResponse struct {
	Items []modpackCatalogCard `json:"items"`
	Total int                  `json:"total"`
}

var allowedModpackCategories = stringSet(catalogpolicy.ModpackCategories()...)

const publicModpackCatalogFilter = `where (pack.review_status='approved' or pack.submitted_by=$1)
	and (($3 and pack.id=any($4::bigint[])) or (not $3 and ($2='' or pack.slug ilike '%%'||$2||'%%'
		or pack.public_id ilike '%%'||$2||'%%' or pack.primary_name ilike '%%'||$2||'%%'
		or pack.secondary_name ilike '%%'||$2||'%%' or pack.summary ilike '%%'||$2||'%%'
		or $2=any(pack.search_keywords)
		or exists(select 1 from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
			where binding.subject_type='modpack' and binding.subject_id=pack.id and binding.status='approved'
			  and creator.name ilike '%%'||$2||'%%'))))
	and (cardinality($5::text[])=0 or (
		$9='all' and not exists(select 1 from unnest($5::text[]) requested(value) where not exists(
			select 1 from modpack_loader_compatibilities compatibility where compatibility.modpack_id=pack.id and compatibility.minecraft_version=requested.value))
		or $9<>'all' and exists(select 1 from modpack_loader_compatibilities compatibility
			where compatibility.modpack_id=pack.id and compatibility.minecraft_version=any($5::text[]))))
	and (cardinality($6::text[])=0 or exists(select 1 from modpack_loader_compatibilities compatibility
		where compatibility.modpack_id=pack.id and compatibility.loader=any($6::text[])))
	and (cardinality($7::text[])=0 or pack.primary_category=any($7::text[]))
	and (cardinality($8::text[])=0 or not exists(select 1 from unnest($8::text[]) requested(value) where not exists(
		select 1 from modpack_tags tag where tag.modpack_id=pack.id and tag.tag=requested.value)))
	and (cardinality($10::text[])=0 or pack.environment=any($10::text[]))
	and (cardinality($11::text[])=0 or pack.official_status=any($11::text[]))
	and (cardinality($12::text[])=0 or pack.source_status=any($12::text[]))
	and (cardinality($13::text[])=0 or pack.license=any($13::text[]))
	and (cardinality($14::text[])=0 or not exists(select 1 from unnest($14::text[]) requested(value) where not case requested.value
		when 'gallery' then exists(select 1 from modpack_gallery_images gallery where gallery.modpack_id=pack.id)
		when 'downloads' then true
		when 'reviewed' then pack.review_status='approved'
		else false end))
	and ($15::integer=0 or $15>0 and pack.updated_at>=now()-make_interval(days=>$15)
		or $15<0 and pack.updated_at<now()-make_interval(days=>abs($15)))
	and (cardinality($16::text[])=0 or not exists(
		select 1 from unnest($16::text[]) requested(value) where not exists(
			select 1 from modpack_mods entry where entry.modpack_id=pack.id and (
				lower(entry.identifier)=requested.value
				or lower(entry.provider_project_id)=requested.value
				or exists(select 1 from mods candidate where candidate.review_status='approved' and (
					candidate.id=entry.mod_id
					or entry.provider='modrinth' and entry.provider_project_id<>'' and candidate.modrinth_project_id=entry.provider_project_id
					or entry.provider='curseforge' and entry.provider_project_id<>'' and candidate.curseforge_project_id=entry.provider_project_id
					or entry.identifier<>'' and exists(select 1 from mod_identifiers source_identifier
						where source_identifier.mod_id=candidate.id and lower(source_identifier.identifier)=lower(entry.identifier))
				) and (lower(candidate.project_code)=requested.value or lower(candidate.slug)=requested.value
					or exists(select 1 from mod_identifiers requested_identifier
						where requested_identifier.mod_id=candidate.id and lower(requested_identifier.identifier)=requested.value)))
			))))`

func (s *Server) modpacks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.createModpack(w, r)
		return
	}
	claims := currentClaims(r)
	query, validQuery := parseCatalogQuery(r.URL.Query().Get("q"))
	excludeSiteID, exclusionErr := parseCatalogSlugExclusion(r.URL.Query())
	versions, validVersions := parseCatalogList(r.URL.Query().Get("version"), 20)
	loaders, validLoaders := parseCatalogList(r.URL.Query().Get("loader"), 20)
	primaryCategories, validPrimary := parseCatalogList(r.URL.Query().Get("primary"), 1)
	tags, validTags := parseCatalogList(r.URL.Query().Get("tag"), 20)
	environments, validEnvironments := parseCatalogList(r.URL.Query().Get("environment"), 10)
	statuses, validStatuses := parseCatalogList(r.URL.Query().Get("status"), 10)
	sources, validSources := parseCatalogList(r.URL.Query().Get("source"), 10)
	licenses, validLicenses := parseCatalogList(r.URL.Query().Get("license"), 20)
	features, validFeatures := parseCatalogList(r.URL.Query().Get("feature"), 20)
	modFilters := parseCatalogModFilters(r.URL.Query().Get("mods"))
	updatedDays, validUpdated := parseCatalogUpdatedRange(r.URL.Query().Get("updated"))
	if !validQuery || exclusionErr != nil || !validVersions || !validLoaders || !validPrimary || !validTags || !validEnvironments ||
		!validStatuses || !validSources || !validLicenses || !validFeatures || !validCatalogFeatures(features) || !validUpdated ||
		!everyCatalogValueAllowed(primaryCategories, allowedModpackCategories) || !everyCatalogValueAllowed(tags, allowedModpackCategories) ||
		!everyCatalogValueAllowed(environments, stringSet("clientOnly", "serverOnly", "bothRequired")) ||
		!everyCatalogValueAllowed(statuses, allowedModStatuses) || !everyCatalogValueAllowed(sources, allowedModSourceStatuses) ||
		!everyCatalogValueAllowed(licenses, allowedModLicenses) {
		writeError(w, http.StatusBadRequest, "invalid catalog filter")
		return
	}
	versionMode, validVersionMode := parseCatalogVersionMode(r.URL.Query().Get("versionMode"))
	if !validVersionMode {
		writeError(w, http.StatusBadRequest, "invalid version mode")
		return
	}
	rawSort := r.URL.Query().Get("sort")
	sort, validSort := parseCatalogSort(rawSort)
	direction, validDirection := parseCatalogSortDirection(r.URL.Query().Get("order"), sort)
	if !validSort || !validDirection {
		writeError(w, http.StatusBadRequest, "invalid catalog sort")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	indexed := indexedSearchPage{}
	filtered := len(versions)+len(loaders)+len(primaryCategories)+len(tags)+len(environments)+len(statuses)+len(sources)+len(licenses)+len(features)+len(modFilters) > 0 || updatedDays != 0
	if catalogSortUsesSearchIndex(sort) && !filtered && excludeSiteID == "" {
		indexed = s.searchProjectPage(r.Context(), query, "modpack", "", "", "", claims, limit, offset)
	}
	databaseOffset := offset
	if indexed.Used {
		databaseOffset = 0
	}
	var total int
	if indexed.Used {
		total = indexed.Total
	} else if err := s.db.QueryRow(r.Context(), `select count(*) from modpacks pack `+publicModpackCatalogFilter+` and ($17='' or pack.slug<>$17)`,
		claims.Subject, query, false, []int64{}, versions, loaders, primaryCategories, tags, versionMode,
		environments, statuses, sources, licenses, features, updatedDays, modFilters, excludeSiteID).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count modpacks")
		return
	}
	orderSQL := catalogOrderSQL(sort, direction, indexed.Used, 4,
		"coalesce(pack.published_at,pack.created_at)", "pack.updated_at", "pack.id", "pack.primary_name")
	rows, err := s.db.Query(r.Context(), `select pack.id,pack.public_id,pack.slug,pack.primary_name,pack.secondary_name,
		pack.abbreviation,pack.summary,pack.default_locale,pack.environment,pack.primary_category,pack.official_status,
		pack.source_status,pack.license,pack.curseforge_project_id,pack.modrinth_project_id,pack.icon_url,
		pack.search_keywords,pack.review_status,pack.created_at,pack.updated_at,pack.published_at,
		exists(select 1 from modpack_gallery_images gallery where gallery.modpack_id=pack.id)
		from modpacks pack
		left join public_routes popularity_route on popularity_route.entity_type='modpack' and popularity_route.internal_id=pack.id
		left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		`+publicModpackCatalogFilter+` and ($17='' or pack.slug<>$17)
		order by `+orderSQL+`
		limit $18 offset $19`, claims.Subject, query, indexed.Used, indexed.IDs, versions, loaders,
		primaryCategories, tags, versionMode, environments, statuses, sources, licenses, features, updatedDays, modFilters, excludeSiteID, limit, databaseOffset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load modpacks")
		return
	}
	defer rows.Close()
	items := make([]modpackCatalogCard, 0)
	for rows.Next() {
		item, scanErr := scanModpackCatalogCard(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode modpack")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load modpacks")
		return
	}
	rows.Close()
	if err = s.loadModpackCatalogAssociations(r.Context(), items); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load modpack relations")
		return
	}
	ossCfg := s.ossConfigFromSettings(r.Context())
	for index := range items {
		items[index].IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, items[index].IconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate modpack icon URL")
			return
		}
	}
	writeBoundedCatalogJSON(w, modpackListResponse{Items: items, Total: total})
}

func (s *Server) modpackItem(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	if r.Method == http.MethodPut {
		s.createModpackRevision(w, r, siteID)
		return
	}
	item, err := s.modpackBySiteID(r.Context(), siteID, claims.Subject, false)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "modpack not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load modpack")
		return
	}
	item.CanEdit = canEditModpack(claims, item)
	ossCfg := s.ossConfigFromSettings(r.Context())
	item.IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.IconURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate modpack icon URL")
		return
	}
	for index := range item.Mods {
		item.Mods[index].IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.Mods[index].IconURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate contained mod icon URL")
			return
		}
	}
	if err = s.resolveModAuthorOSSURLsWithConfig(r.Context(), ossCfg, item.Authors); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate modpack author avatar URL")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) modpackIcon(w http.ResponseWriter, r *http.Request) {
	s.publicProjectIcon(w, r, "modpack")
}

func (s *Server) modpackEditor(w http.ResponseWriter, r *http.Request) {
	item, err := s.modpackBySiteID(r.Context(), normalizeModSiteID(r.PathValue("siteId")), currentClaims(r).Subject, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "modpack not found")
		return
	}
	if !canEditModpack(currentClaims(r), item) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	item.CanEdit = true
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createModpack(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !claimsAllow(claims, "modpack.create") && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "modpack creation permission is required")
		return
	}
	var snapshot createModpackRequest
	if decodeJSON(r, &snapshot) != nil || normalizeAndValidateModpackRequest(&snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid modpack")
		return
	}
	reviewRequired := loadReviewConfig(r.Context(), s.db).ModpackCreate && !claimsAllow(claims, "content.no-review") && !claimsAllow(claims, "admin.*")
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	reviewStatus := "approved"
	if reviewRequired {
		reviewStatus = "pending"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start modpack creation")
		return
	}
	defer tx.Rollback(r.Context())
	if snapshot.SiteID == "" {
		snapshot.SiteID, err = availableModpackSiteID(r.Context(), tx, snapshot.PrimaryName)
	} else {
		err = ensureModpackSiteIDAvailable(r.Context(), tx, snapshot.SiteID, 0)
	}
	if err != nil {
		writeError(w, http.StatusConflict, "modpack site ID is already used")
		return
	}
	publicID, err := availableModUniqueID(r.Context(), tx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to allocate modpack ID")
		return
	}
	if err = s.prepareImportedModpackAssets(r.Context(), &snapshot, publicID, claims.Subject); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var modpackID int64
	err = tx.QueryRow(r.Context(), `insert into modpacks(public_id,slug,primary_name,secondary_name,abbreviation,summary,
		default_locale,environment,primary_category,pack_type,packaging_method,official_status,source_status,license,curseforge_project_id,
		modrinth_project_id,icon_url,body_markdown,search_keywords,submission_method,review_status,submitted_by,published_at)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,
		case when $21='approved' then now() else null end) returning id`, publicID, snapshot.SiteID, snapshot.PrimaryName,
		snapshot.SecondaryName, snapshot.Abbreviation, snapshot.Summary, snapshot.DefaultLocale, snapshot.Environment,
		snapshot.PrimaryCategory, snapshot.PackType, snapshot.PackagingMethod, snapshot.OfficialStatus, snapshot.SourceStatus, snapshot.License,
		snapshot.CurseForgeProjectID, snapshot.ModrinthProjectID, snapshot.IconURL, snapshot.BodyMarkdown,
		snapshot.SearchKeywords, snapshot.SubmissionMethod, reviewStatus, claims.Subject).Scan(&modpackID)
	if err != nil {
		writeError(w, http.StatusConflict, "failed to save modpack")
		return
	}
	if err = validateModpackGalleryFilesTx(r.Context(), tx, 0, claims.Subject, snapshot.GalleryImages); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
	if modpackCreationNeedsPreviewAssociations(reviewStatus) {
		if err = replaceModpackAssociationsTx(r.Context(), tx, modpackID, 0, claims.Subject,
			false, canManageAuthors, canManageTeams, snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save modpack relations")
			return
		}
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: "modpack", EntityID: modpackID, AggregateType: modpackAggregate, AggregateKey: publicID,
		Snapshot: raw, ActorID: claims.Subject, Source: snapshot.SubmissionMethod, Status: reviewStatus,
		Metadata: map[string]any{"siteId": snapshot.SiteID, "initial": true}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create modpack revision")
		return
	}
	if reviewStatus == "approved" {
		if err = applyModpackSnapshotTx(r.Context(), tx, modpackID, created.RevisionID, claims.Subject,
			canManageAuthors, canManageTeams, snapshot); err == nil {
			err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r)
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish modpack")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit modpack")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "create_modpack", 0, s.refreshProjectACLVersion(r.Context())) {
		return
	}
	item, err := s.modpackBySiteID(r.Context(), snapshot.SiteID, claims.Subject, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read created modpack")
		return
	}
	item.CanEdit = canEditModpack(claims, item)
	item.SubmissionRevisionID = created.RevisionPublicID
	item.ChangeRequestID = created.ChangeRequestPublicID
	writeJSON(w, http.StatusCreated, item)
}

func modpackCreationNeedsPreviewAssociations(reviewStatus string) bool {
	return reviewStatus != "approved"
}

func (s *Server) createModpackRevision(w http.ResponseWriter, r *http.Request, siteID string) {
	claims := currentClaims(r)
	current, err := s.modpackBySiteID(r.Context(), siteID, claims.Subject, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "modpack not found")
		return
	}
	if !canEditModpack(claims, current) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	var request struct {
		Snapshot       createModpackRequest `json:"snapshot"`
		BaseRevisionID string               `json:"baseRevisionId"`
		ChangeReason   string               `json:"changeReason"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid modpack revision")
		return
	}
	request.Snapshot.SiteID = normalizeModSiteID(request.Snapshot.SiteID)
	if normalizeAndValidateModpackRequest(&request.Snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid modpack")
		return
	}
	reviewRequired := loadReviewConfig(r.Context(), s.db).ModpackEdit && !claimsAllow(claims, "content.no-review") && !claimsAllow(claims, "admin.*")
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	status := "approved"
	if reviewRequired {
		status = "pending"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start modpack revision")
		return
	}
	defer tx.Rollback(r.Context())
	if err = ensureModpackSiteIDAvailable(r.Context(), tx, request.Snapshot.SiteID, current.ID); err != nil {
		writeError(w, http.StatusConflict, "modpack site ID is already used")
		return
	}
	if err = validateModpackGalleryFilesTx(r.Context(), tx, current.ID, claims.Subject, request.Snapshot.GalleryImages); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var baseRevision *int64
	if request.BaseRevisionID != "" {
		var value int64
		if err = tx.QueryRow(r.Context(), `select id from content_revisions where public_id=$1 and aggregate_type=$2 and aggregate_key=$3`,
			request.BaseRevisionID, modpackAggregate, current.PublicID).Scan(&value); err != nil {
			writeError(w, http.StatusConflict, "base revision is invalid")
			return
		}
		baseRevision = &value
	}
	raw, _ := json.Marshal(request.Snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: "modpack", EntityID: current.ID, AggregateType: modpackAggregate, AggregateKey: current.PublicID,
		BaseRevision: baseRevision, Snapshot: raw, Reason: strings.TrimSpace(request.ChangeReason), ActorID: claims.Subject,
		Source: "user", Status: status, Metadata: map[string]any{"siteId": request.Snapshot.SiteID}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create modpack revision")
		return
	}
	if status == "approved" {
		canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
		if err = applyModpackSnapshotTx(r.Context(), tx, current.ID, created.RevisionID, claims.Subject,
			canManageAuthors, canManageTeams, request.Snapshot); err == nil {
			err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r)
		}
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to save modpack revision")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "create_modpack_revision", 0,
		s.refreshProjectACLVersion(r.Context())) {
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": created.RevisionPublicID, "status": status, "siteId": request.Snapshot.SiteID, "changeRequestId": created.ChangeRequestPublicID})
}

func normalizeAndValidateModpackRequest(request *createModpackRequest) error {
	err := normalizeAndValidateModpackRequestMode(request, false)
	request.ImportSelection = nil
	return err
}

func normalizeAndValidateModpackImportDraft(request *createModpackRequest) error {
	return normalizeAndValidateModpackRequestMode(request, true)
}

func normalizeAndValidateModpackRequestMode(request *createModpackRequest, importDraft bool) error {
	request.SiteID = normalizeModSiteID(request.SiteID)
	request.PrimaryName = strings.TrimSpace(request.PrimaryName)
	request.SecondaryName = strings.TrimSpace(request.SecondaryName)
	request.Abbreviation = strings.TrimSpace(request.Abbreviation)
	request.Summary = strings.TrimSpace(request.Summary)
	request.BodyMarkdown = strings.TrimSpace(request.BodyMarkdown)
	request.DefaultLocale = normalizeContentLocale(request.DefaultLocale)
	request.Environment = strings.TrimSpace(request.Environment)
	request.PrimaryCategory = strings.TrimSpace(request.PrimaryCategory)
	request.PackType = strings.ToLower(strings.TrimSpace(request.PackType))
	request.PackagingMethod = strings.ToLower(strings.TrimSpace(request.PackagingMethod))
	request.OfficialStatus = strings.TrimSpace(request.OfficialStatus)
	request.SourceStatus = strings.TrimSpace(request.SourceStatus)
	request.License = strings.TrimSpace(request.License)
	request.CurseForgeProjectID = strings.TrimSpace(request.CurseForgeProjectID)
	request.ModrinthProjectID = strings.TrimSpace(request.ModrinthProjectID)
	request.IconURL = strings.TrimSpace(request.IconURL)
	request.SubmissionMethod = strings.ToLower(strings.TrimSpace(request.SubmissionMethod))
	request.SearchKeywords = uniqueTrimmed(request.SearchKeywords, 80)
	request.Tags = uniqueTrimmed(request.Tags, 80)
	if request.PackType == "" {
		request.PackType = "native"
	}
	if request.PackagingMethod == "" {
		request.PackagingMethod = "other"
	}
	validLocale := isEditableContentLocale(request.DefaultLocale) || importDraft && request.DefaultLocale == "und"
	if request.PrimaryName == "" || !validLocale ||
		request.SiteID != "" && !validModSiteID(request.SiteID) || len([]rune(request.PrimaryName)) > 160 ||
		len([]rune(request.Summary)) > 500 || len(request.BodyMarkdown) > 1<<20 {
		return errors.New("invalid modpack content")
	}
	if !stringSet("clientOnly", "serverOnly", "bothRequired")[request.Environment] ||
		!stringSet("native", "customized")[request.PackType] ||
		!stringSet("curseforge", "ftb", "other_launcher", "manual", "atlauncher", "modrinth", "mcbbs", "other")[request.PackagingMethod] ||
		!allowedModStatuses[request.OfficialStatus] || !allowedModSourceStatuses[request.SourceStatus] ||
		!allowedModLicenses[request.License] || !stringSet("manual", "modrinth", "curseforge")[request.SubmissionMethod] {
		return errors.New("invalid modpack status")
	}
	if request.PrimaryCategory == "" {
		request.PrimaryCategory = "adventure"
	}
	if !catalogpolicy.IsModpackCategory(request.PrimaryCategory) {
		return errors.New("invalid modpack primary category")
	}
	for _, category := range request.Tags {
		if !allowedModpackCategories[category] {
			return errors.New("invalid modpack category")
		}
	}
	if request.IconURL != "" && !validHTTPURL(request.IconURL) {
		return errors.New("invalid modpack icon URL")
	}
	if importDraft && (request.ImportSelection == nil || request.ImportSelection.Provider == "" || request.ImportSelection.ProjectID == "" ||
		request.ImportSelection.VersionID == "" || request.ImportSelection.FileName == "" || request.ImportSelection.ReleaseType != "release" || request.ImportSelection.PublishedAt == "") {
		return errors.New("invalid modpack import selection")
	}
	compatibilities := make([]modLoaderCompatibilityPayload, 0, len(request.Compatibilities))
	seenCompatibility := map[string]bool{}
	for _, compatibility := range request.Compatibilities {
		compatibility.Loader = strings.TrimSpace(compatibility.Loader)
		compatibility.Versions = uniqueTrimmed(compatibility.Versions, 500)
		if compatibility.Loader == "" || len(compatibility.Versions) == 0 || seenCompatibility[compatibility.Loader] {
			continue
		}
		seenCompatibility[compatibility.Loader] = true
		compatibilities = append(compatibilities, compatibility)
	}
	if len(compatibilities) == 0 {
		return errors.New("a modpack requires at least one loader and Minecraft version")
	}
	request.Compatibilities = compatibilities
	links := make([]modLinkPayload, 0, len(request.Links))
	for _, link := range request.Links {
		link.Type = strings.TrimSpace(link.Type)
		link.URL = strings.TrimSpace(link.URL)
		link.Note = strings.TrimSpace(link.Note)
		if link.URL == "" {
			continue
		}
		if !allowedModLinkTypes[link.Type] || !validHTTPURL(link.URL) {
			return errors.New("invalid modpack link")
		}
		links = append(links, link)
	}
	request.Links = links
	authors, err := normalizeProjectAuthors(request.Authors)
	if err != nil {
		return err
	}
	request.Authors = authors
	if len(request.GalleryImages) > 32 || len(request.Mods) > 2000 {
		return errors.New("modpack contains too many gallery images or mods")
	}
	mods := make([]modpackModPayload, 0, len(request.Mods))
	seenMods := map[string]bool{}
	for _, item := range request.Mods {
		item.ModPublicID = strings.ToLower(strings.TrimSpace(item.ModPublicID))
		item.Provider = strings.ToLower(strings.TrimSpace(item.Provider))
		item.ProviderProjectID = strings.TrimSpace(item.ProviderProjectID)
		item.ProviderVersionID = strings.TrimSpace(item.ProviderVersionID)
		item.Identifier = strings.TrimSpace(item.Identifier)
		item.ModName = strings.TrimSpace(item.ModName)
		item.FileName = strings.TrimSpace(item.FileName)
		if item.Provider == "" {
			item.Provider = "manual"
		}
		if !stringSet("manual", "modrinth", "curseforge", "index")[item.Provider] ||
			item.ModPublicID == "" && item.ProviderProjectID == "" && item.Identifier == "" {
			return errors.New("invalid modpack mod reference")
		}
		key := item.ModPublicID + "\x00" + item.Provider + "\x00" + item.ProviderProjectID + "\x00" + item.ProviderVersionID + "\x00" + item.FileName
		if seenMods[key] {
			continue
		}
		seenMods[key] = true
		mods = append(mods, item)
	}
	request.Mods = mods
	return nil
}

func (s *Server) prepareImportedModpackAssets(ctx context.Context, snapshot *createModpackRequest, publicID string, actorID int64) error {
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
			log.Printf("store imported modpack author avatar: provider=%s author=%q: %v", snapshot.SubmissionMethod, author.Name, err)
			author.AvatarURL = ""
			continue
		}
		author.AvatarURL = avatar.URL
		author.AvatarFileID, author.AvatarInternalID = &avatar.FilePublicID, &avatar.FileInternalID
	}
	if snapshot.IconURL != "" {
		iconURL, err := s.mirrorExternalProjectIcon(ctx, snapshot.IconURL, "modpack", publicID, actorID)
		if err != nil {
			return fmt.Errorf("failed to store imported modpack icon: %w", err)
		}
		snapshot.IconURL = iconURL
	}
	return nil
}

func applyModpackSnapshotTx(ctx context.Context, tx pgx.Tx, modpackID, revisionID, actorID int64,
	canManageAuthors, canManageTeams bool, snapshot createModpackRequest) error {
	if _, err := tx.Exec(ctx, `update modpacks set slug=$2,primary_name=$3,secondary_name=$4,abbreviation=$5,
		summary=$6,default_locale=$7,environment=$8,primary_category=$9,pack_type=$10,packaging_method=$11,
		official_status=$12,source_status=$13,license=$14,curseforge_project_id=$15,modrinth_project_id=$16,
		icon_url=$17,body_markdown=$18,search_keywords=$19,submission_method=$20,review_status='approved',published_revision_id=$21,
		published_at=coalesce(published_at,now()),updated_at=now() where id=$1`, modpackID, snapshot.SiteID,
		snapshot.PrimaryName, snapshot.SecondaryName, snapshot.Abbreviation, snapshot.Summary, snapshot.DefaultLocale,
		snapshot.Environment, snapshot.PrimaryCategory, snapshot.PackType, snapshot.PackagingMethod,
		snapshot.OfficialStatus, snapshot.SourceStatus, snapshot.License,
		snapshot.CurseForgeProjectID, snapshot.ModrinthProjectID, snapshot.IconURL, snapshot.BodyMarkdown,
		snapshot.SearchKeywords, snapshot.SubmissionMethod, revisionID); err != nil {
		return err
	}
	return replaceModpackAssociationsTx(ctx, tx, modpackID, revisionID, actorID, true,
		canManageAuthors, canManageTeams, snapshot)
}

func replaceModpackAssociationsTx(ctx context.Context, tx pgx.Tx, modpackID, revisionID, actorID int64,
	projectApproved, canManageAuthors, canManageTeams bool, snapshot createModpackRequest) error {
	for _, query := range []string{
		`delete from modpack_loader_compatibilities where modpack_id=$1`,
		`delete from modpack_tags where modpack_id=$1`,
		`delete from modpack_links where modpack_id=$1`,
		`delete from modpack_mods where modpack_id=$1`,
	} {
		if _, err := tx.Exec(ctx, query, modpackID); err != nil {
			return err
		}
	}
	for _, compatibility := range snapshot.Compatibilities {
		for _, version := range compatibility.Versions {
			if _, err := tx.Exec(ctx, `insert into modpack_loader_compatibilities(modpack_id,loader,minecraft_version) values($1,$2,$3)`, modpackID, compatibility.Loader, version); err != nil {
				return err
			}
		}
	}
	for _, tag := range snapshot.Tags {
		if _, err := tx.Exec(ctx, `insert into modpack_tags(modpack_id,tag) values($1,$2)`, modpackID, tag); err != nil {
			return err
		}
	}
	for index, link := range snapshot.Links {
		if _, err := tx.Exec(ctx, `insert into modpack_links(modpack_id,link_type,url,note,display_order) values($1,$2,$3,$4,$5)`, modpackID, link.Type, link.URL, link.Note, index); err != nil {
			return err
		}
	}
	if err := syncProjectCreatorBindingsTx(ctx, tx, "modpack", modpackID, snapshot.Authors, actorID,
		projectApproved, canManageAuthors, canManageTeams, nil); err != nil {
		return err
	}
	for index, item := range snapshot.Mods {
		var modID *int64
		if item.ModPublicID != "" {
			var value int64
			if err := tx.QueryRow(ctx, `select id from mods where project_code=$1`, item.ModPublicID).Scan(&value); err != nil {
				return errors.New("selected mod does not exist")
			}
			modID = &value
		} else {
			var value int64
			err := tx.QueryRow(ctx, `select candidate.id from mods candidate
				where candidate.review_status='approved' and (
					$1='modrinth' and $2<>'' and candidate.modrinth_project_id=$2 or
					$1='curseforge' and $2<>'' and candidate.curseforge_project_id=$2 or
					$3<>'' and exists(select 1 from mod_identifiers identifier
						where identifier.mod_id=candidate.id and lower(identifier.identifier)=lower($3)))
				order by candidate.id limit 1`, item.Provider, item.ProviderProjectID, item.Identifier).Scan(&value)
			if err == nil {
				modID = &value
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		var entryID int64
		if err := tx.QueryRow(ctx, `insert into modpack_mods(modpack_id,mod_id,provider,provider_project_id,
			provider_version_id,identifier,mod_name,file_name,client_required,server_required,display_order)
			values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) returning id`, modpackID, modID, item.Provider,
			item.ProviderProjectID, item.ProviderVersionID, item.Identifier, item.ModName, item.FileName,
			item.ClientRequired, item.ServerRequired, index).Scan(&entryID); err != nil {
			return err
		}
		if modID == nil {
			rawIdentifier := item.Identifier
			if rawIdentifier == "" {
				rawIdentifier = item.Provider + ":" + item.ProviderProjectID
			}
			if _, err := tx.Exec(ctx, `insert into unresolved_references(
				source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
			) values('modpack_mod',$1,'mods','mod',$2,lower($2),jsonb_build_object(
				'provider',$3,'projectId',$4,'versionId',$5,'fileName',$6,'modpackId',$7))
			on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
			set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
				resolved_at=null,metadata=excluded.metadata,updated_at=now()`, entryID, rawIdentifier,
				item.Provider, item.ProviderProjectID, item.ProviderVersionID, item.FileName, modpackID); err != nil {
				return err
			}
		}
	}
	if revisionID > 0 {
		return replaceModpackGalleryImagesTx(ctx, tx, modpackID, revisionID, actorID, snapshot.GalleryImages)
	}
	return nil
}

func scanModpack(row scanner) (modpackResponse, error) {
	var item modpackResponse
	err := row.Scan(&item.ID, &item.PublicID, &item.SiteID, &item.PrimaryName, &item.SecondaryName,
		&item.Abbreviation, &item.Summary, &item.DefaultLocale, &item.Environment, &item.PrimaryCategory,
		&item.PackType, &item.PackagingMethod, &item.OfficialStatus, &item.SourceStatus, &item.License, &item.CurseForgeProjectID, &item.ModrinthProjectID,
		&item.IconURL, &item.BodyMarkdown, &item.SearchKeywords, &item.SubmissionMethod, &item.ReviewStatus,
		&item.SubmittedByInternal, &item.SubmittedBy, &item.PublishedRevisionID, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt)
	item.Compatibilities = []modLoaderCompatibilityPayload{}
	item.Tags = []string{}
	item.Authors = []modAuthorPayload{}
	item.Links = []modLinkPayload{}
	item.GalleryImages = []modGalleryImagePayload{}
	item.Mods = []modpackModPayload{}
	return item, err
}

func scanModpackCatalogCard(row scanner) (modpackCatalogCard, error) {
	var item modpackCatalogCard
	err := row.Scan(&item.ID, &item.PublicID, &item.SiteID, &item.PrimaryName, &item.SecondaryName,
		&item.Abbreviation, &item.Summary, &item.DefaultLocale, &item.Environment, &item.PrimaryCategory,
		&item.OfficialStatus, &item.SourceStatus, &item.License, &item.CurseForgeProjectID, &item.ModrinthProjectID,
		&item.IconURL, &item.SearchKeywords, &item.ReviewStatus, &item.CreatedAt, &item.UpdatedAt, &item.PublishedAt,
		&item.HasGallery)
	item.Compatibilities = []modLoaderCompatibilityPayload{}
	item.Tags = []string{}
	item.Authors = []catalogCardAuthorPayload{}
	return item, err
}

func (s *Server) loadModpackCatalogAssociations(ctx context.Context, items []modpackCatalogCard) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]*modpackCatalogCard, len(items))
	ids := make([]int64, 0, len(items))
	for index := range items {
		byID[items[index].ID] = &items[index]
		ids = append(ids, items[index].ID)
	}
	rows, err := s.db.Query(ctx, modpackCatalogCompatibilitiesSQL, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value modLoaderCompatibilityPayload
		if err = rows.Scan(&id, &value.Loader, &value.Versions); err != nil {
			rows.Close()
			return err
		}
		byID[id].Compatibilities = append(byID[id].Compatibilities, value)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select modpack_id,tag from (
		select modpack_id,tag,row_number() over(partition by modpack_id order by tag) position
		from modpack_tags where modpack_id=any($1)
	) ranked where position<=32 order by modpack_id,position`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var tag string
		if err = rows.Scan(&id, &tag); err != nil {
			rows.Close()
			return err
		}
		byID[id].Tags = append(byID[id].Tags, tag)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select subject_id,name,role from (
		select binding.subject_id,creator.name,coalesce(role.name,binding.role_snapshot) role,row_number() over(
			partition by binding.subject_id order by binding.display_order,binding.id) position
		from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
		left join creator_role_definitions role on role.id=binding.role_id
		where binding.subject_type='modpack' and binding.subject_id=any($1) and binding.status='approved'
	) ranked where position<=8 order by subject_id,position`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value catalogCardAuthorPayload
		if err = rows.Scan(&id, &value.Name, &value.Role); err != nil {
			rows.Close()
			return err
		}
		byID[id].Authors = append(byID[id].Authors, value)
	}
	rows.Close()
	return rows.Err()
}

func (s *Server) modpackBySiteID(ctx context.Context, siteID string, viewerID int64, editor bool) (modpackResponse, error) {
	row := s.db.QueryRow(ctx, `select pack.id,pack.public_id,pack.slug,pack.primary_name,pack.secondary_name,
		pack.abbreviation,pack.summary,pack.default_locale,pack.environment,pack.primary_category,pack.pack_type,
		pack.packaging_method,pack.official_status,
		pack.source_status,pack.license,pack.curseforge_project_id,pack.modrinth_project_id,pack.icon_url,
		pack.body_markdown,pack.search_keywords,pack.submission_method,pack.review_status,pack.submitted_by,
		coalesce(account.public_id,''),revision.public_id,pack.created_at,pack.updated_at,pack.published_at
		from modpacks pack left join users account on account.id=pack.submitted_by
		left join content_revisions revision on revision.id=pack.published_revision_id
		where pack.slug=$1 and ($2 or pack.review_status='approved' or pack.submitted_by=$3)`, siteID, editor, viewerID)
	item, err := scanModpack(row)
	if err != nil {
		return item, err
	}
	values := []modpackResponse{item}
	if err = s.loadModpackAssociations(ctx, values); err != nil {
		return item, err
	}
	return values[0], nil
}

func (s *Server) loadModpackAssociations(ctx context.Context, items []modpackResponse) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int64]*modpackResponse, len(items))
	ids := make([]int64, 0, len(items))
	for index := range items {
		byID[items[index].ID] = &items[index]
		ids = append(ids, items[index].ID)
	}
	rows, err := s.db.Query(ctx, `select modpack_id,loader,array_agg(minecraft_version order by minecraft_version desc)
		from modpack_loader_compatibilities where modpack_id=any($1) group by modpack_id,loader order by modpack_id,loader`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value modLoaderCompatibilityPayload
		if err = rows.Scan(&id, &value.Loader, &value.Versions); err != nil {
			rows.Close()
			return err
		}
		byID[id].Compatibilities = append(byID[id].Compatibilities, value)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	rows, err = s.db.Query(ctx, `select modpack_id,tag from modpack_tags where modpack_id=any($1) order by modpack_id,tag`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var tag string
		if err = rows.Scan(&id, &tag); err != nil {
			rows.Close()
			return err
		}
		byID[id].Tags = append(byID[id].Tags, tag)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	rows, err = s.db.Query(ctx, `select modpack_id,link_type,url,note from modpack_links where modpack_id=any($1) order by modpack_id,display_order,id`, ids)
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
	if err = finishRows(rows); err != nil {
		return err
	}
	rows, err = s.db.Query(ctx, `select binding.subject_id,creator.public_id,creator.kind,creator.name,creator.avatar_url,
		coalesce(role.public_id,''),coalesce(role.name,binding.role_snapshot),coalesce((select jsonb_agg(jsonb_build_object(
		'creatorId',member.public_id,'kind',member.kind,'name',member.name,'avatarUrl',member.avatar_url,
		'roleId',member_role.public_id,'role',member_role.name,'title',membership.title) order by membership.display_order,membership.created_at)
		from creator_team_members membership join creators member on member.id=membership.member_creator_id
		join creator_role_definitions member_role on member_role.id=membership.role_id
		where membership.team_id=creator.id and membership.status='approved'),'[]'::jsonb)
		from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
		left join creator_role_definitions role on role.id=binding.role_id
		where binding.subject_type='modpack' and binding.subject_id=any($1) and binding.status='approved'
		order by binding.subject_id,binding.display_order,binding.id`, ids)
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
	if err = finishRows(rows); err != nil {
		return err
	}
	rows, err = s.db.Query(ctx, `select gallery.modpack_id,gallery.public_id,file.public_id,file.original_name,file.content_type,file.size_bytes
		from modpack_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id and file.status='active'
		where gallery.modpack_id=any($1) order by gallery.modpack_id,gallery.display_order,gallery.id`, ids)
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
		value.URL = "/api/v1/modpacks/" + url.PathEscape(byID[id].SiteID) + "/gallery/" + value.PublicID
		byID[id].GalleryImages = append(byID[id].GalleryImages, value)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	rows, err = s.db.Query(ctx, `select entry.modpack_id,coalesce(resolved.project_code,''),coalesce(resolved.slug,''),
		coalesce(resolved.primary_name,nullif(entry.mod_name,''),entry.identifier,entry.provider_project_id),coalesce(resolved.icon_url,''),
		entry.provider,entry.provider_project_id,entry.provider_version_id,entry.identifier,entry.file_name,
		entry.client_required,entry.server_required,resolved.id is not null
		from modpack_mods entry left join lateral (select candidate.* from mods candidate
			where candidate.review_status='approved' and (candidate.id=entry.mod_id
				or entry.provider='modrinth' and entry.provider_project_id<>'' and candidate.modrinth_project_id=entry.provider_project_id
				or entry.provider='curseforge' and entry.provider_project_id<>'' and candidate.curseforge_project_id=entry.provider_project_id
				or entry.identifier<>'' and exists(select 1 from mod_identifiers identifier where identifier.mod_id=candidate.id and lower(identifier.identifier)=lower(entry.identifier)))
			order by (candidate.id=entry.mod_id) desc,candidate.id limit 1) resolved on true
		where entry.modpack_id=any($1) order by entry.modpack_id,entry.display_order,entry.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var value modpackModPayload
		if err = rows.Scan(&id, &value.ModPublicID, &value.ModSiteID, &value.ModName, &value.IconURL, &value.Provider, &value.ProviderProjectID, &value.ProviderVersionID, &value.Identifier, &value.FileName, &value.ClientRequired, &value.ServerRequired, &value.Resolved); err != nil {
			rows.Close()
			return err
		}
		byID[id].Mods = append(byID[id].Mods, value)
	}
	return finishRows(rows)
}

func canEditModpack(claims security.Claims, item modpackResponse) bool {
	return claims.Subject > 0 && (claimsAllow(claims, "project.edit."+item.PublicID) || claimsAllow(claims, "admin.*"))
}

func availableModpackSiteID(ctx context.Context, query databaseQuery, name string) (string, error) {
	base := modSiteIDBase(name)
	for suffix := 0; suffix < 1000; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s_%d", base, suffix+1)
		}
		if err := ensureModpackSiteIDAvailable(ctx, query, candidate, 0); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("unable to allocate modpack site ID")
}

func ensureModpackSiteIDAvailable(ctx context.Context, query databaseQuery, siteID string, excludeID int64) error {
	var exists bool
	if err := query.QueryRow(ctx, `select exists(select 1 from modpacks where slug=$1 and id<>$2)`, siteID, excludeID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errors.New("modpack site ID exists")
	}
	return nil
}

func validateModpackGalleryFilesTx(ctx context.Context, tx pgx.Tx, modpackID, actorID int64, images []modGalleryImagePayload) error {
	seen := map[string]bool{}
	for _, image := range images {
		if image.FileID == "" || seen[image.FileID] {
			return errors.New("modpack gallery contains an invalid or duplicate image")
		}
		seen[image.FileID] = true
		allowExisting := false
		if image.PublicID != "" {
			var existingModpackID int64
			var existingFileID string
			err := tx.QueryRow(ctx, `select gallery.modpack_id,file.public_id from modpack_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id where gallery.public_id=$1`, image.PublicID).Scan(&existingModpackID, &existingFileID)
			allowExisting = err == nil && existingModpackID == modpackID && existingFileID == image.FileID
			if err != nil && !errors.Is(err, pgx.ErrNoRows) || err == nil && !allowExisting {
				return errors.New("modpack gallery image reference is invalid")
			}
		}
		file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, image.FileID, ossRasterBindingScope{UploaderID: actorID, AllowAnyUploader: allowExisting})
		if err != nil {
			return errors.New("modpack gallery image is unavailable or not owned by the editor")
		}
		var used bool
		if err = tx.QueryRow(ctx, `select exists(select 1 from modpack_gallery_images where oss_file_id=$1 and modpack_id<>$2)`, file.ID, modpackID).Scan(&used); err != nil || used {
			return errors.New("modpack gallery image is already used")
		}
	}
	return nil
}

func replaceModpackGalleryImagesTx(ctx context.Context, tx pgx.Tx, modpackID, revisionID, actorID int64, images []modGalleryImagePayload) error {
	if _, err := tx.Exec(ctx, `delete from modpack_gallery_images where modpack_id=$1`, modpackID); err != nil {
		return err
	}
	for index, image := range images {
		publicID := strings.TrimSpace(image.PublicID)
		if publicID == "" {
			if _, err := tx.Exec(ctx, `insert into modpack_gallery_images(modpack_id,oss_file_id,display_order,created_by,published_revision_id)
				select $1,id,$3,$4,$5 from oss_files where public_id=$2`, modpackID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `insert into modpack_gallery_images(public_id,modpack_id,oss_file_id,display_order,created_by,published_revision_id)
			select $1,$2,id,$4,$5,$6 from oss_files where public_id=$3`, publicID, modpackID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) modpackGalleryImage(w http.ResponseWriter, r *http.Request) {
	var objectKey, contentType string
	err := s.db.QueryRow(r.Context(), `select file.object_key,file.content_type from modpack_gallery_images gallery
		join modpacks pack on pack.id=gallery.modpack_id and pack.slug=$1
		join oss_files file on file.id=gallery.oss_file_id and file.status='active'
		where gallery.public_id=$2 and (pack.review_status='approved' or pack.submitted_by=$3)`, normalizeModSiteID(r.PathValue("siteId")), strings.ToLower(r.PathValue("publicId")), currentClaims(r).Subject).Scan(&objectKey, &contentType)
	if err != nil {
		writeError(w, http.StatusNotFound, "modpack gallery image not found")
		return
	}
	s.redirectCatalogOSSAsset(w, r, objectKey, contentType)
}

func (s *Server) modpackHistory(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	pageRequest, err := parseContentHistoryPageRequest(r.URL.Query(), contentHistoryScope("modpack", siteID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	item, err := s.modpackBySiteID(r.Context(), siteID, claims.Subject, true)
	if err != nil {
		writeError(w, http.StatusNotFound, "modpack not found")
		return
	}
	canEdit := canEditModpack(claims, item)
	visibility := projectPendingReviewVisibility(claims, item.PublicID, canEdit)
	submittedBy := int64(0)
	if item.SubmittedByInternal != nil {
		submittedBy = *item.SubmittedByInternal
	}
	isSubmitter := submittedBy > 0 && submittedBy == claims.Subject
	if item.ReviewStatus != "approved" && !isSubmitter && !visibility.allows(item.ReviewStatus, submittedBy) {
		writeError(w, http.StatusNotFound, "modpack not found")
		return
	}
	var published *int64
	if item.PublishedRevisionID != nil {
		var id int64
		if s.db.QueryRow(r.Context(), `select id from content_revisions where public_id=$1`, *item.PublishedRevisionID).Scan(&id) == nil {
			published = &id
		}
	}
	items, err := contentRevisionHistoryPage(r.Context(), s.db, modpackAggregate, item.PublicID, published, visibility, pageRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load modpack history")
		return
	}
	writeJSON(w, http.StatusOK, mergeContentHistoryPage(items, nil, pageRequest))
}
