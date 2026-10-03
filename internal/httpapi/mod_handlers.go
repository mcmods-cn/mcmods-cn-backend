package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type modLinkPayload struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Note string `json:"note"`
}

type modAuthorPayload struct {
	CreatorID        string                   `json:"creatorId,omitempty"`
	Kind             string                   `json:"kind,omitempty"`
	Name             string                   `json:"name,omitempty"`
	AvatarURL        string                   `json:"avatarUrl,omitempty"`
	AvatarFileID     *string                  `json:"-"`
	AvatarInternalID *int64                   `json:"-"`
	RoleID           *string                  `json:"roleId,omitempty"`
	Role             string                   `json:"role,omitempty"`
	Members          []modAuthorMemberPayload `json:"members,omitempty"`
}

type modAuthorMemberPayload struct {
	CreatorID string  `json:"creatorId"`
	Kind      string  `json:"kind"`
	Name      string  `json:"name"`
	AvatarURL string  `json:"avatarUrl"`
	RoleID    *string `json:"roleId,omitempty"`
	Role      string  `json:"role"`
	Title     string  `json:"title,omitempty"`
}

type modRelationshipPayload struct {
	Type                 string `json:"type"`
	RelatedModPublicID   string `json:"relatedModId,omitempty"`
	RelatedModSiteID     string `json:"relatedModSiteId,omitempty"`
	RelatedModName       string `json:"relatedModName"`
	RelatedModIdentifier string `json:"relatedModIdentifier,omitempty"`
}

type modRelationshipGroupPayload struct {
	Label             string                   `json:"label"`
	Loader            string                   `json:"loader"`
	MinecraftVersions []string                 `json:"minecraftVersions"`
	ModVersion        string                   `json:"modVersion"`
	Direction         string                   `json:"direction,omitempty"`
	Relationships     []modRelationshipPayload `json:"relationships"`
}

type modLoaderCompatibilityPayload struct {
	Loader   string   `json:"loader"`
	Versions []string `json:"versions"`
}

type modIdentifierPayload struct {
	ID                  int64    `json:"-"`
	Identifier          string   `json:"identifier"`
	Primary             bool     `json:"primary"`
	MinecraftVersionMin string   `json:"minecraftVersionMin,omitempty"`
	MinecraftVersionMax string   `json:"minecraftVersionMax,omitempty"`
	MinecraftVersions   []string `json:"minecraftVersions"`
}

type modGalleryImagePayload struct {
	PublicID    string `json:"publicId,omitempty"`
	FileID      string `json:"fileId"`
	Name        string `json:"name,omitempty"`
	ContentType string `json:"contentType,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	URL         string `json:"url,omitempty"`
}

type createModRequest struct {
	SiteID              string                          `json:"siteId"`
	PrimaryName         string                          `json:"primaryName"`
	SecondaryName       string                          `json:"secondaryName"`
	Abbreviation        string                          `json:"abbreviation"`
	Summary             string                          `json:"summary"`
	ModIDs              []modIdentifierPayload          `json:"modIds"`
	DefaultLocale       string                          `json:"defaultLocale"`
	Localizations       []catalogLocalizationEdit       `json:"localizations"`
	Environment         string                          `json:"environment"`
	PrimaryCategory     string                          `json:"primaryCategory"`
	Compatibilities     []modLoaderCompatibilityPayload `json:"compatibilities"`
	Tags                []string                        `json:"tags"`
	SearchKeywords      []string                        `json:"searchKeywords"`
	Authors             []modAuthorPayload              `json:"authors"`
	OfficialStatus      string                          `json:"officialStatus"`
	SourceStatus        string                          `json:"sourceStatus"`
	License             string                          `json:"license"`
	CurseForgeProjectID string                          `json:"curseforgeProjectId"`
	ModrinthProjectID   string                          `json:"modrinthProjectId"`
	GitHubProjectPath   string                          `json:"githubProjectPath"`
	IconURL             string                          `json:"iconUrl"`
	BodyMarkdown        string                          `json:"bodyMarkdown"`
	SubmissionMethod    string                          `json:"submissionMethod"`
	Links               []modLinkPayload                `json:"links"`
	RelationshipGroups  []modRelationshipGroupPayload   `json:"relationshipGroups"`
	GalleryImages       []modGalleryImagePayload        `json:"galleryImages"`
}

type modResponse struct {
	ID                   int64                           `json:"-"`
	PublicID             string                          `json:"id"`
	UniqueID             string                          `json:"uniqueId"`
	SiteID               string                          `json:"siteId"`
	PrimaryName          string                          `json:"primaryName"`
	SecondaryName        string                          `json:"secondaryName"`
	Abbreviation         string                          `json:"abbreviation"`
	Summary              string                          `json:"summary"`
	ModIDs               []modIdentifierPayload          `json:"modIds"`
	DefaultLocale        string                          `json:"defaultLocale"`
	Localizations        []catalogLocalizationEdit       `json:"localizations"`
	Environment          string                          `json:"environment"`
	PrimaryCategory      string                          `json:"primaryCategory"`
	Compatibilities      []modLoaderCompatibilityPayload `json:"compatibilities"`
	OfficialStatus       string                          `json:"officialStatus"`
	SourceStatus         string                          `json:"sourceStatus"`
	License              string                          `json:"license"`
	CurseForgeProjectID  string                          `json:"curseforgeProjectId"`
	ModrinthProjectID    string                          `json:"modrinthProjectId"`
	GitHubProjectPath    string                          `json:"githubProjectPath"`
	IconURL              string                          `json:"iconUrl"`
	BodyMarkdown         string                          `json:"bodyMarkdown"`
	SearchKeywords       []string                        `json:"searchKeywords"`
	SubmissionMethod     string                          `json:"submissionMethod"`
	ReviewStatus         string                          `json:"reviewStatus"`
	SubmittedByInternal  *int64                          `json:"-"`
	SubmittedBy          string                          `json:"submittedBy,omitempty"`
	CreatedAt            time.Time                       `json:"createdAt"`
	UpdatedAt            time.Time                       `json:"updatedAt"`
	PublishedAt          *time.Time                      `json:"publishedAt,omitempty"`
	PublishedRevisionID  *string                         `json:"publishedRevisionId,omitempty"`
	SubmissionRevisionID string                          `json:"submissionRevisionId,omitempty"`
	ChangeRequestID      string                          `json:"changeRequestId,omitempty"`
	Tags                 []string                        `json:"tags"`
	Authors              []modAuthorPayload              `json:"authors"`
	Links                []modLinkPayload                `json:"links"`
	RelationshipGroups   []modRelationshipGroupPayload   `json:"relationshipGroups"`
	GalleryImages        []modGalleryImagePayload        `json:"galleryImages"`
}

type modListResponse struct {
	Items []modResponse `json:"items"`
	Total int           `json:"total"`
}

var allowedModEnvironments = stringSet("clientOnly", "serverOnly", "bothRequired", "clientOptional", "serverOptional")
var allowedModStatuses = stringSet("active", "lowFrequency", "discontinued", "archived", "development")
var allowedModSourceStatuses = stringSet("open", "partial", "closed", "unknown")
var allowedModLicenses = stringSet("MIT", "GPL-3.0", "LGPL-3.0", "Apache-2.0", "ARR", "Custom")
var allowedModSubmissionMethods = stringSet("manual", "modrinth", "curseforge", "github")
var allowedModCategories = stringSet("technology", "magic", "adventure", "agriculture", "decoration", "utility", "assistance", "customization", "library")
var allowedModTags = stringSet(
	"building", "creatures", "worldGeneration", "biomes", "structures", "weapons", "tools", "storage", "logistics", "energy", "redstone", "automation",
	"optimization", "assistance", "modpackSupport", "adventure", "economy", "equipment", "gameMechanics", "management", "minigames", "social", "transportation",
)
var allowedModRelationshipTypes = stringSet("dependency", "integration", "conflict")
var errModSiteIDTaken = errors.New("mod site ID already exists")
var allowedModLinkTypes = stringSet(
	"official", "curseforge", "modrinth", "mcmod", "klpbbs", "minebbs", "redstoneRelay", "mcbbsMemorial", "mcbbsArchive", "sourceforge", "minecraftForum", "planetMinecraft", "mcpedl", "spigotmc", "wiki",
	"github", "gitlab", "gitee", "gitea", "gitpod", "gitcode", "bitbucket", "maven", "crowdin", "mastodon", "issue",
	"baiduPan", "aliyunDrive", "quarkDrive", "weiyun", "lanzou", "chinaMobileCloud", "tianyiCloud", "cowTransfer", "googleDrive", "oneDrive", "dropbox", "mediaFire",
	"bilibili", "weibo", "tieba", "zhihu", "bcy", "ftb", "patreon", "buyMeACoffee", "kofi", "aifadian", "kook", "discord", "twitter", "youtube", "reddit", "other",
)

func (s *Server) createMod(w http.ResponseWriter, r *http.Request) {
	var req createModRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if err := normalizeAndValidateModRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	claims := currentClaims(r)
	reviewRequired := loadReviewConfig(r.Context(), s.db).ModCreate && !projectMutationBypassesReview(claims)
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	reviewStatus := "approved"
	var publishedAt *time.Time
	if reviewRequired {
		reviewStatus = "pending"
	} else {
		now := time.Now().UTC()
		publishedAt = &now
	}
	ossCfg := s.ossConfigFromSettings(r.Context())
	var err error
	if req.SiteID == "" {
		req.SiteID, err = availableModSiteID(r.Context(), s.db, req.PrimaryName)
	} else {
		err = ensureModSiteIDAvailable(r.Context(), s.db, req.SiteID, 0)
	}
	if errors.Is(err, errModSiteIDTaken) {
		writeError(w, http.StatusConflict, "模组站内 ID 已被占用")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成模组站内 ID 失败")
		return
	}
	uniqueID, err := availableModUniqueID(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成模组唯一 ID 失败")
		return
	}
	if req.SubmissionMethod != "manual" {
		for index := range req.Authors {
			author := &req.Authors[index]
			if author.CreatorID != "" || author.AvatarURL == "" {
				continue
			}
			kind := author.Kind
			if kind == "" {
				kind = "author"
			}
			mirroredAvatar, mirrorErr := s.mirrorExternalCreatorAvatar(r.Context(), author.AvatarURL, kind, author.Name, claims.Subject)
			if mirrorErr != nil {
				log.Printf("store imported mod author avatar: provider=%s author=%q: %v", req.SubmissionMethod, author.Name, mirrorErr)
				author.AvatarURL = ""
				continue
			}
			author.AvatarURL = mirroredAvatar.URL
			filePublicID := mirroredAvatar.FilePublicID
			fileInternalID := mirroredAvatar.FileInternalID
			author.AvatarFileID = &filePublicID
			author.AvatarInternalID = &fileInternalID
		}
	}
	if req.IconURL != "" && req.SubmissionMethod != "manual" {
		mirroredIconURL, mirrorErr := s.mirrorExternalProjectIcon(r.Context(), req.IconURL, "mod", uniqueID, claims.Subject)
		if mirrorErr != nil {
			writeError(w, http.StatusBadGateway, "failed to store imported mod icon")
			return
		}
		req.IconURL = mirroredIconURL
	}
	// Mirroring reads settings and registers its owned OSS file through the pool.
	// Prepare it before acquiring the business connection; final file ownership
	// and live scan state are still checked and locked inside this transaction.
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建模组失败")
		return
	}
	defer tx.Rollback(r.Context())
	req.IconURL, err = validateStoredProjectIconURL(r.Context(), tx, ossCfg, req.IconURL, "", claims.Subject)
	if errors.Is(err, errInvalidStoredProjectIcon) {
		writeError(w, http.StatusBadRequest, errInvalidStoredProjectIcon.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to validate project icon")
		return
	}
	var modID int64
	err = tx.QueryRow(
		r.Context(),
		`insert into mods (
			project_code, slug, primary_name, secondary_name, abbreviation, summary, environment, primary_category,
			official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
			body_markdown, search_keywords, submission_method, review_status, submitted_by, published_at
		 ) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)
		 returning id`,
		uniqueID, req.SiteID, req.PrimaryName, req.SecondaryName, req.Abbreviation, req.Summary, req.Environment, req.PrimaryCategory,
		req.OfficialStatus, req.SourceStatus, req.License, req.CurseForgeProjectID, req.ModrinthProjectID, req.GitHubProjectPath, req.IconURL,
		req.BodyMarkdown, req.SearchKeywords, req.SubmissionMethod, reviewStatus, claims.Subject, publishedAt,
	).Scan(&modID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "模组站内 ID 已被占用")
			return
		}
		writeError(w, http.StatusInternalServerError, "保存模组资料失败")
		return
	}
	if err = insertModIdentifiersTx(r.Context(), tx, modID, req.ModIDs); err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a mod ID is already assigned to another mod")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save mod IDs")
		return
	}
	if err = validateModGalleryFilesTx(r.Context(), tx, 0, claims.Subject, req.GalleryImages); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	for index, link := range req.Links {
		if _, err = tx.Exec(r.Context(), `insert into mod_links (mod_id, link_type, url, note, display_order) values ($1,$2,$3,$4,$5)`, modID, link.Type, link.URL, link.Note, index); err != nil {
			writeError(w, http.StatusInternalServerError, "保存相关链接失败")
			return
		}
	}
	for _, tag := range req.Tags {
		if _, err = tx.Exec(r.Context(), `insert into mod_tags (mod_id, tag) values ($1,$2)`, modID, tag); err != nil {
			writeError(w, http.StatusInternalServerError, "保存模组标签失败")
			return
		}
	}
	canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
	if err = syncProjectCreatorBindingsTx(r.Context(), tx, "mod", modID, req.Authors, claims.Subject,
		reviewStatus == "approved", canManageAuthors, canManageTeams, r); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err = insertModCompatibilities(r.Context(), tx, modID, req.Compatibilities); err != nil {
		writeError(w, http.StatusInternalServerError, "保存模组兼容版本失败")
		return
	}
	if err = insertModRelationshipGroups(r.Context(), tx, modID, req.RelationshipGroups); err != nil {
		writeError(w, http.StatusInternalServerError, "保存模组关系失败")
		return
	}
	snapshot, err := json.Marshal(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成审核快照失败")
		return
	}
	createdRevision, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType:    "mod",
		EntityID:      modID,
		AggregateType: "mod",
		AggregateKey:  uniqueID,
		Snapshot:      snapshot,
		ActorID:       claims.Subject,
		Source:        req.SubmissionMethod,
		Status:        reviewStatus,
		Metadata:      map[string]any{"modId": modID, "siteId": req.SiteID, "initial": true},
		Request:       r,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存审核版本失败")
		return
	}
	if !reviewRequired {
		if err = applyModSnapshot(r.Context(), tx, modID, createdRevision.RevisionID, claims.Subject,
			canManageAuthors, canManageTeams, req); err != nil {
			writeError(w, http.StatusInternalServerError, "发布模组版本失败")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, createdRevision.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			log.Printf("record automatic mod approval: mod=%s change_request=%d: %v", uniqueID, createdRevision.ChangeRequestID, err)
			writeError(w, http.StatusInternalServerError, "记录自动审核失败")
			return
		}
		if err = enqueueOSSRehomeJobTx(r.Context(), tx, modID); err != nil {
			writeError(w, http.StatusInternalServerError, "安排资料图片迁址失败")
			return
		}
	}
	if err = runProjectCreationTransactionHook(r.Context(), tx, projectCreationTransactionResult{
		ProjectType: "mod", ProjectID: modID, ProjectPublicID: uniqueID, SiteID: req.SiteID,
		ProjectTitle: req.PrimaryName, TargetURL: "/mods/" + url.PathEscape(req.SiteID), ReviewStatus: reviewStatus,
		ChangeRequestID: createdRevision.ChangeRequestID, ChangeRequestUID: createdRevision.ChangeRequestPublicID,
	}); err != nil {
		log.Printf("finalize transactional mod creation: mod=%s: %v", uniqueID, err)
		writeError(w, http.StatusInternalServerError, "提交模组关联事实失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交模组资料失败")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "create_mod", 0, s.refreshProjectACLVersion(r.Context())) {
		return
	}

	mod, err := s.modByID(r.Context(), modID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取已创建模组失败")
		return
	}
	mod.SubmissionRevisionID = createdRevision.RevisionPublicID
	mod.ChangeRequestID = createdRevision.ChangeRequestPublicID
	writeJSON(w, http.StatusCreated, mod)
}

const publicModCatalogFilter = `where (m.review_status='approved' or m.submitted_by=$1)
	and (($3 and m.id=any($4::bigint[])) or (not $3 and ($2='' or m.slug ilike '%%'||$2||'%%'
		or m.project_code ilike '%%'||$2||'%%' or m.primary_name ilike '%%'||$2||'%%'
		or m.secondary_name ilike '%%'||$2||'%%' or m.abbreviation ilike '%%'||$2||'%%'
		or $2=any(m.search_keywords)
		or exists(select 1 from mod_identifiers identifier where identifier.mod_id=m.id and identifier.identifier ilike '%%'||$2||'%%')
		or exists(select 1 from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
			where binding.subject_id=m.id and binding.subject_type='mod' and binding.status='approved'
			  and creator.name ilike '%%'||$2||'%%'))))
	and (cardinality($5::text[])=0 or (
		$9='all' and not exists(select 1 from unnest($5::text[]) requested(value) where not exists(
			select 1 from mod_loader_compatibilities compatibility where compatibility.mod_id=m.id and compatibility.minecraft_version=requested.value))
		or $9<>'all' and exists(select 1 from mod_loader_compatibilities compatibility
			where compatibility.mod_id=m.id and compatibility.minecraft_version=any($5::text[]))))
	and (cardinality($6::text[])=0 or exists(select 1 from mod_loader_compatibilities compatibility
		where compatibility.mod_id=m.id and compatibility.loader=any($6::text[])))
	and (cardinality($7::text[])=0 or m.primary_category=any($7::text[]))
	and (cardinality($8::text[])=0 or not exists(select 1 from unnest($8::text[]) requested(value) where not exists(
		select 1 from mod_tags tag where tag.mod_id=m.id and tag.tag=requested.value)))
	and (cardinality($10::text[])=0 or m.environment=any($10::text[]))
	and (cardinality($11::text[])=0 or m.official_status=any($11::text[]))
	and (cardinality($12::text[])=0 or m.source_status=any($12::text[]))
	and (cardinality($13::text[])=0 or m.license=any($13::text[]))
	and (cardinality($14::text[])=0 or not exists(select 1 from unnest($14::text[]) requested(value) where not case requested.value
		when 'gallery' then exists(select 1 from mod_gallery_images gallery where gallery.mod_id=m.id)
		when 'downloads' then m.modrinth_project_id<>'' or m.curseforge_project_id<>''
		when 'reviewed' then m.review_status='approved'
		else false end))
	and ($15::integer=0 or $15>0 and m.updated_at>=now()-make_interval(days=>$15)
		or $15<0 and m.updated_at<now()-make_interval(days=>abs($15)))`

func (s *Server) publicMods(w http.ResponseWriter, r *http.Request) {
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
	updatedDays, validUpdated := parseCatalogUpdatedRange(r.URL.Query().Get("updated"))
	if !validQuery || exclusionErr != nil || !validVersions || !validLoaders || !validPrimary || !validTags || !validEnvironments ||
		!validStatuses || !validSources || !validLicenses || !validFeatures || !validCatalogFeatures(features) || !validUpdated ||
		!everyCatalogValueAllowed(primaryCategories, allowedModCategories) || !everyCatalogValueAllowed(tags, allowedModTags) ||
		!everyCatalogValueAllowed(environments, allowedModEnvironments) || !everyCatalogValueAllowed(statuses, allowedModStatuses) ||
		!everyCatalogValueAllowed(sources, allowedModSourceStatuses) || !everyCatalogValueAllowed(licenses, allowedModLicenses) {
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
	limit := 100
	if parsed, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && parsed > 0 {
		limit = min(parsed, 100)
	}
	offset := boundedOffset(r.URL.Query().Get("offset"))
	indexed := indexedSearchPage{}
	filtered := len(versions)+len(loaders)+len(primaryCategories)+len(tags)+len(environments)+len(statuses)+len(sources)+len(licenses)+len(features) > 0 || updatedDays != 0
	if catalogSortUsesSearchIndex(sort) && !filtered && excludeSiteID == "" {
		indexed = s.searchProjectPage(r.Context(), query, "mod", "", "", "", claims, limit, offset)
	}
	databaseOffset := offset
	if indexed.Used {
		databaseOffset = 0
	}
	var total int
	if indexed.Used {
		total = indexed.Total
	} else if err := s.db.QueryRow(r.Context(), `select count(*) from mods m `+publicModCatalogFilter+` and ($16='' or m.slug<>$16)`,
		claims.Subject, query, false, []int64{}, versions, loaders, primaryCategories, tags, versionMode,
		environments, statuses, sources, licenses, features, updatedDays, excludeSiteID).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组总数失败")
		return
	}
	orderSQL := catalogOrderSQL(sort, direction, indexed.Used, 4,
		"coalesce(m.published_at,m.created_at)", "m.updated_at", "m.id", "m.primary_name")
	rows, err := s.db.Query(
		r.Context(),
		`select m.id, project_code, slug, primary_name, secondary_name, abbreviation, summary, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, submitted_by, m.created_at, m.updated_at, published_at,
		        (select revision.public_id from content_revisions revision where revision.id=m.published_revision_id)
		 from mods m
		 left join public_routes popularity_route on popularity_route.entity_type='mod' and popularity_route.internal_id=m.id
		 left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		 `+publicModCatalogFilter+` and ($16='' or m.slug<>$16)
		 order by `+orderSQL+`
		 limit $17 offset $18`,
		claims.Subject, query, indexed.Used, indexed.IDs, versions, loaders, primaryCategories, tags, versionMode,
		environments, statuses, sources, licenses, features, updatedDays, excludeSiteID, limit, databaseOffset,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组列表失败")
		return
	}
	defer rows.Close()

	items := make([]modResponse, 0)
	for rows.Next() {
		mod, err := scanMod(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "解析模组列表失败")
			return
		}
		items = append(items, mod)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组列表失败")
		return
	}
	rows.Close()
	if err = s.loadModListAssociations(r.Context(), items); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组关联资料失败")
		return
	}
	ossCfg := s.ossConfigFromSettings(r.Context())
	iconURLs := make([]string, len(items))
	for index := range items {
		iconURLs[index] = items[index].IconURL
	}
	iconURLs, err = s.resolveStoredOSSImageURLsWithConfig(r.Context(), ossCfg, iconURLs)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate mod icon URL")
		return
	}
	for index := range items {
		items[index].IconURL = iconURLs[index]
		if err = s.resolveModAuthorOSSURLsWithConfig(r.Context(), ossCfg, items[index].Authors); err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate mod author avatar URL")
			return
		}
	}
	writeJSON(w, http.StatusOK, modListResponse{Items: items, Total: total})
}

func (s *Server) publicModDetail(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	if siteID == "" {
		writeError(w, http.StatusBadRequest, "模组地址无效")
		return
	}
	claims := currentClaims(r)
	mod, err := s.modBySiteID(r.Context(), siteID, claims.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组详情失败")
		return
	}
	if err = s.loadIncomingModRelationships(r.Context(), &mod); err != nil {
		writeError(w, http.StatusInternalServerError, "读取反向模组关系失败")
		return
	}
	ossCfg := s.ossConfigFromSettings(r.Context())
	if err = s.resolveModAuthorOSSURLsWithConfig(r.Context(), ossCfg, mod.Authors); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate mod author avatar URL")
		return
	}
	mod.IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, mod.IconURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate mod icon URL")
		return
	}
	writeJSON(w, http.StatusOK, mod)
}

func (s *Server) resolveModAuthorOSSURLsWithConfig(ctx context.Context, ossCfg ossConfigPayload, authors []modAuthorPayload) error {
	for authorIndex := range authors {
		var err error
		authors[authorIndex].AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, authors[authorIndex].AvatarURL)
		if err != nil {
			return err
		}
		for memberIndex := range authors[authorIndex].Members {
			authors[authorIndex].Members[memberIndex].AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(
				ctx, ossCfg, authors[authorIndex].Members[memberIndex].AvatarURL,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func normalizeProjectAuthors(authors []modAuthorPayload) ([]modAuthorPayload, error) {
	if len(authors) > 64 {
		return nil, errors.New("project contains too many authors or teams")
	}
	result := make([]modAuthorPayload, 0, len(authors))
	for _, author := range authors {
		author.CreatorID = strings.ToLower(strings.TrimSpace(author.CreatorID))
		author.Kind = strings.ToLower(strings.TrimSpace(author.Kind))
		author.Name = strings.TrimSpace(author.Name)
		author.Role = strings.TrimSpace(author.Role)
		author.AvatarURL = strings.TrimSpace(author.AvatarURL)
		if author.CreatorID == "" && author.Name == "" {
			continue
		}
		if author.CreatorID != "" && !validCatalogPublicID(author.CreatorID) ||
			author.Kind != "" && author.Kind != "author" && author.Kind != "team" ||
			author.AvatarURL != "" && !validHTTPURL(author.AvatarURL) {
			return nil, errors.New("invalid project author")
		}
		result = append(result, author)
	}
	return result, nil
}

func (s *Server) publicModIcon(w http.ResponseWriter, r *http.Request) {
	s.publicProjectIcon(w, r, "mod")
}

func (s *Server) publicProjectIcon(w http.ResponseWriter, r *http.Request, projectKind string) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	if siteID == "" {
		writeError(w, http.StatusBadRequest, projectKind+" site ID is invalid")
		return
	}
	query := `select icon_url from mods where slug=$1 and (review_status='approved' or submitted_by=$2)`
	if projectKind == "modpack" {
		query = `select icon_url from modpacks where slug=$1 and (review_status='approved' or submitted_by=$2)`
	}
	var iconURL string
	err := s.db.QueryRow(r.Context(), query, siteID, currentClaims(r).Subject).Scan(&iconURL)
	if errors.Is(err, pgx.ErrNoRows) || strings.TrimSpace(iconURL) == "" {
		writeError(w, http.StatusNotFound, projectKind+" icon does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load "+projectKind+" icon")
		return
	}
	s.redirectStoredRasterURL(w, r, iconURL)
}

func (s *Server) modEditorDetail(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod does not exist")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load mod")
		return
	}
	if !canEditMod(currentClaims(r), identity) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	mod, err := s.modBySiteIDForEditor(r.Context(), siteID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load mod editor")
		return
	}
	writeJSON(w, http.StatusOK, mod)
}

type scanner interface {
	Scan(dest ...any) error
}

func scanMod(row scanner) (modResponse, error) {
	var mod modResponse
	err := row.Scan(
		&mod.ID, &mod.UniqueID, &mod.SiteID, &mod.PrimaryName, &mod.SecondaryName, &mod.Abbreviation, &mod.Summary,
		&mod.Environment, &mod.PrimaryCategory, &mod.OfficialStatus, &mod.SourceStatus, &mod.License,
		&mod.CurseForgeProjectID, &mod.ModrinthProjectID, &mod.GitHubProjectPath, &mod.IconURL, &mod.BodyMarkdown, &mod.SearchKeywords,
		&mod.SubmissionMethod, &mod.ReviewStatus, &mod.SubmittedByInternal, &mod.CreatedAt, &mod.UpdatedAt, &mod.PublishedAt, &mod.PublishedRevisionID,
	)
	mod.PublicID = mod.UniqueID
	mod.Tags = []string{}
	mod.ModIDs = []modIdentifierPayload{}
	mod.Localizations = []catalogLocalizationEdit{}
	mod.GalleryImages = []modGalleryImagePayload{}
	mod.Authors = []modAuthorPayload{}
	mod.Links = []modLinkPayload{}
	mod.RelationshipGroups = []modRelationshipGroupPayload{}
	mod.Compatibilities = []modLoaderCompatibilityPayload{}
	return mod, err
}

func (s *Server) loadModListAssociations(ctx context.Context, mods []modResponse) error {
	if len(mods) == 0 {
		return nil
	}
	byID := make(map[int64]*modResponse, len(mods))
	ids := make([]int64, 0, len(mods))
	for index := range mods {
		byID[mods[index].ID] = &mods[index]
		ids = append(ids, mods[index].ID)
	}

	rows, err := s.db.Query(ctx, `select mod_id,identifier,is_primary,minecraft_version_min,minecraft_version_max,minecraft_versions
		from mod_identifiers where mod_id=any($1) order by mod_id,is_primary desc,display_order,id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var modID int64
		var item modIdentifierPayload
		if err = rows.Scan(&modID, &item.Identifier, &item.Primary, &item.MinecraftVersionMin, &item.MinecraftVersionMax, &item.MinecraftVersions); err != nil {
			rows.Close()
			return err
		}
		byID[modID].ModIDs = append(byID[modID].ModIDs, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select mod_id,tag from mod_tags where mod_id=any($1) order by mod_id,tag`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var modID int64
		var tag string
		if err = rows.Scan(&modID, &tag); err != nil {
			rows.Close()
			return err
		}
		byID[modID].Tags = append(byID[modID].Tags, tag)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select link.mod_id,link.link_type,link.url,link.note
		from mod_links link where link.mod_id=any($1) order by link.mod_id,link.display_order,link.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var modID int64
		var item modLinkPayload
		if err = rows.Scan(&modID, &item.Type, &item.URL, &item.Note); err != nil {
			rows.Close()
			return err
		}
		byID[modID].Links = append(byID[modID].Links, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select compatibility.mod_id,compatibility.loader,
		array_agg(compatibility.minecraft_version order by compatibility.minecraft_version desc)
		from mod_loader_compatibilities compatibility where compatibility.mod_id=any($1)
		group by compatibility.mod_id,compatibility.loader order by compatibility.mod_id,compatibility.loader`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var modID int64
		var item modLoaderCompatibilityPayload
		if err = rows.Scan(&modID, &item.Loader, &item.Versions); err != nil {
			rows.Close()
			return err
		}
		byID[modID].Compatibilities = append(byID[modID].Compatibilities, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select author.subject_id,creator.public_id,creator.kind,creator.name,creator.avatar_url,
		coalesce(role.public_id,''),coalesce(role.name,author.role_snapshot)
		from content_creator_bindings author
		join creators creator on creator.id=author.creator_id
		left join creator_role_definitions role on role.id=author.role_id
		where author.subject_id=any($1) and author.subject_type='mod' and author.status='approved'
		order by author.subject_id,author.display_order,author.id`, ids)
	if err != nil {
		return err
	}
	for rows.Next() {
		var modID int64
		var item modAuthorPayload
		if err = rows.Scan(&modID, &item.CreatorID, &item.Kind, &item.Name, &item.AvatarURL, &item.RoleID, &item.Role); err != nil {
			rows.Close()
			return err
		}
		byID[modID].Authors = append(byID[modID].Authors, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select mod.id,user_account.public_id
		from mods mod join users user_account on user_account.id=mod.submitted_by where mod.id=any($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var modID int64
		var publicID string
		if err = rows.Scan(&modID, &publicID); err != nil {
			return err
		}
		byID[modID].SubmittedBy = publicID
	}
	return rows.Err()
}

func (s *Server) modByID(ctx context.Context, id int64, viewerID int64) (modResponse, error) {
	row := s.db.QueryRow(
		ctx,
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, submitted_by, created_at, updated_at, published_at,
		        (select revision.public_id from content_revisions revision where revision.id=mods.published_revision_id)
		 from mods where id = $1 and (review_status = 'approved' or submitted_by = $2)`,
		id, viewerID,
	)
	mod, err := scanMod(row)
	if err != nil {
		return mod, err
	}
	err = s.loadModAssociations(ctx, &mod)
	return mod, err
}

func (s *Server) modBySiteID(ctx context.Context, siteID string, viewerID int64) (modResponse, error) {
	row := s.db.QueryRow(
		ctx,
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, submitted_by, created_at, updated_at, published_at,
		        (select revision.public_id from content_revisions revision where revision.id=mods.published_revision_id)
		 from mods where slug = $1 and (review_status = 'approved' or submitted_by = $2)`,
		siteID, viewerID,
	)
	mod, err := scanMod(row)
	if err != nil {
		return mod, err
	}
	err = s.loadModAssociations(ctx, &mod)
	return mod, err
}

func (s *Server) modBySiteIDForEditor(ctx context.Context, siteID string) (modResponse, error) {
	row := s.db.QueryRow(ctx, `select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, environment, primary_category,
		official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		body_markdown, search_keywords, submission_method, review_status, submitted_by, created_at, updated_at, published_at,
		(select revision.public_id from content_revisions revision where revision.id=mods.published_revision_id)
		from mods where slug=$1`, siteID)
	mod, err := scanMod(row)
	if err != nil {
		return mod, err
	}
	err = s.loadModAssociations(ctx, &mod)
	return mod, err
}

func (s *Server) loadModAssociations(ctx context.Context, mod *modResponse) error {
	if mod.SubmittedByInternal != nil {
		if err := s.db.QueryRow(ctx, `select public_id from users where id=$1`, *mod.SubmittedByInternal).Scan(&mod.SubmittedBy); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if err := s.db.QueryRow(ctx, `select default_locale from content_subjects where subject_id=$1 and subject_type='mod'`, mod.ID).Scan(&mod.DefaultLocale); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if mod.DefaultLocale == "" {
		mod.DefaultLocale = "en-US"
	}
	rows, err := s.db.Query(ctx, `select id,identifier,is_primary,minecraft_version_min,minecraft_version_max,minecraft_versions
		from mod_identifiers where mod_id=$1 order by is_primary desc,display_order,id`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modIdentifierPayload
		if err = rows.Scan(&item.ID, &item.Identifier, &item.Primary, &item.MinecraftVersionMin, &item.MinecraftVersionMax, &item.MinecraftVersions); err != nil {
			rows.Close()
			return err
		}
		mod.ModIDs = append(mod.ModIDs, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select locale,name,summary,content_markdown from content_localizations
		where subject_id=$1 and subject_type='mod' and review_status='approved' order by locale`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item catalogLocalizationEdit
		if err = rows.Scan(&item.Locale, &item.Name, &item.Summary, &item.ContentMarkdown); err != nil {
			rows.Close()
			return err
		}
		mod.Localizations = append(mod.Localizations, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select gallery.public_id,file.public_id,file.original_name,file.content_type,file.size_bytes
		from mod_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id
		where gallery.mod_id=$1 and file.status='active' order by gallery.display_order,gallery.id`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modGalleryImagePayload
		if err = rows.Scan(&item.PublicID, &item.FileID, &item.Name, &item.ContentType, &item.SizeBytes); err != nil {
			rows.Close()
			return err
		}
		item.URL = fmt.Sprintf("/api/v1/mods/%s/gallery/%s", url.PathEscape(mod.SiteID), item.PublicID)
		mod.GalleryImages = append(mod.GalleryImages, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select link_type, url, note from mod_links where mod_id = $1 order by display_order, id`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modLinkPayload
		if err = rows.Scan(&item.Type, &item.URL, &item.Note); err != nil {
			rows.Close()
			return err
		}
		mod.Links = append(mod.Links, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select loader, array_agg(minecraft_version order by minecraft_version desc) from mod_loader_compatibilities where mod_id=$1 group by loader order by loader`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modLoaderCompatibilityPayload
		if err = rows.Scan(&item.Loader, &item.Versions); err != nil {
			rows.Close()
			return err
		}
		mod.Compatibilities = append(mod.Compatibilities, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select tag from mod_tags where mod_id = $1 order by tag`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tag string
		if err = rows.Scan(&tag); err != nil {
			rows.Close()
			return err
		}
		mod.Tags = append(mod.Tags, tag)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select creator.public_id,creator.kind,creator.name,creator.avatar_url,
		role.public_id,coalesce(role.name,author.role_snapshot),
		coalesce((select jsonb_agg(jsonb_build_object(
			'creatorId',member.public_id,'kind',member.kind,'name',member.name,'avatarUrl',member.avatar_url,
			'roleId',member_role.public_id,'role',member_role.name,'title',membership.title
		) order by membership.display_order,membership.created_at)
		from creator_team_members membership
		join creators member on member.id=membership.member_creator_id
		join creator_role_definitions member_role on member_role.id=membership.role_id
		where membership.team_id=creator.id and membership.status='approved'),'[]'::jsonb)
		from content_creator_bindings author
		join creators creator on creator.id=author.creator_id
		left join creator_role_definitions role on role.id=author.role_id
		where author.subject_id=$1 and author.subject_type='mod' and author.status='approved'
		order by author.display_order,author.id`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modAuthorPayload
		var membersRaw []byte
		if err = rows.Scan(&item.CreatorID, &item.Kind, &item.Name, &item.AvatarURL, &item.RoleID, &item.Role, &membersRaw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(membersRaw, &item.Members); err != nil {
			rows.Close()
			return err
		}
		mod.Authors = append(mod.Authors, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	rows, err = s.db.Query(ctx, `select relationship_group.id,relationship_group.label,relationship_group.loader,
		relationship_group.minecraft_versions,relationship_group.mod_version,
		relationship.id,coalesce(relationship.relation_type,''),coalesce(relationship.related_mod_public_id,''),
		coalesce(relationship.related_mod_site_id,''),coalesce(relationship.related_mod_name,''),
		coalesce(relationship.related_mod_identifier,'')
		from mod_relationship_groups relationship_group
		left join lateral (
			select item.id,item.relation_type,coalesce(related.project_code,'') related_mod_public_id,
				coalesce(related.slug,'') related_mod_site_id,item.related_mod_name,item.related_mod_identifier,
				item.display_order
			from mod_relationships item
			left join mods related on related.id=item.related_mod_id and related.review_status='approved'
			where item.group_id=relationship_group.id
			  and (item.related_mod_id is null or related.id is not null)
			order by item.display_order,item.id
		) relationship on true
		where relationship_group.mod_id=$1
		order by relationship_group.display_order,relationship_group.id,relationship.display_order,relationship.id`, mod.ID)
	if err != nil {
		return err
	}
	groupIndexes := map[int64]int{}
	for rows.Next() {
		var groupID int64
		var label, loader, modVersion string
		var minecraftVersions []string
		var relationshipID *int64
		var relationship modRelationshipPayload
		if err = rows.Scan(
			&groupID,
			&label,
			&loader,
			&minecraftVersions,
			&modVersion,
			&relationshipID,
			&relationship.Type,
			&relationship.RelatedModPublicID,
			&relationship.RelatedModSiteID,
			&relationship.RelatedModName,
			&relationship.RelatedModIdentifier,
		); err != nil {
			rows.Close()
			return err
		}
		index, exists := groupIndexes[groupID]
		if !exists {
			index = len(mod.RelationshipGroups)
			groupIndexes[groupID] = index
			mod.RelationshipGroups = append(mod.RelationshipGroups, modRelationshipGroupPayload{
				Label: label, Loader: loader, MinecraftVersions: minecraftVersions, ModVersion: modVersion,
				Direction: "outgoing", Relationships: []modRelationshipPayload{},
			})
		}
		if relationshipID != nil {
			mod.RelationshipGroups[index].Relationships = append(mod.RelationshipGroups[index].Relationships, relationship)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	return nil
}

func (s *Server) loadIncomingModRelationships(ctx context.Context, mod *modResponse) error {
	rows, err := s.db.Query(ctx, `select relationship_group.id,relationship_group.label,relationship_group.loader,
		relationship_group.minecraft_versions,relationship_group.mod_version,relationship.relation_type,
		source_mod.project_code,source_mod.slug,source_mod.primary_name
		from mod_relationships relationship
		join mod_relationship_groups relationship_group on relationship_group.id=relationship.group_id
		join mods source_mod on source_mod.id=relationship.mod_id
		where relationship.related_mod_id=$1 and source_mod.review_status='approved'
		order by relationship_group.display_order,relationship_group.id,relationship.display_order,relationship.id`, mod.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	groupIndexes := map[int64]int{}
	for rows.Next() {
		var groupID int64
		var label, loader, modVersion, relationshipType, sourceModPublicID, sourceModSiteID, sourceModName string
		var minecraftVersions []string
		if err = rows.Scan(&groupID, &label, &loader, &minecraftVersions, &modVersion, &relationshipType, &sourceModPublicID, &sourceModSiteID, &sourceModName); err != nil {
			return err
		}
		index, exists := groupIndexes[groupID]
		if !exists {
			index = len(mod.RelationshipGroups)
			groupIndexes[groupID] = index
			mod.RelationshipGroups = append(mod.RelationshipGroups, modRelationshipGroupPayload{
				Label: label, Loader: loader, MinecraftVersions: minecraftVersions, ModVersion: modVersion,
				Direction: "incoming", Relationships: []modRelationshipPayload{},
			})
		}
		mod.RelationshipGroups[index].Relationships = append(mod.RelationshipGroups[index].Relationships, modRelationshipPayload{
			Type: relationshipType, RelatedModPublicID: sourceModPublicID, RelatedModSiteID: sourceModSiteID, RelatedModName: sourceModName,
		})
	}
	return rows.Err()
}
