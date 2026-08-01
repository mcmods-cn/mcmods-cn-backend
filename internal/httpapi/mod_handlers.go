package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type modLinkPayload struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Note string `json:"note"`
}

type modAuthorPayload struct {
	CreatorID        string  `json:"creatorId,omitempty"`
	Kind             string  `json:"kind,omitempty"`
	Name             string  `json:"name,omitempty"`
	AvatarURL        string  `json:"avatarUrl,omitempty"`
	AvatarFileID     *string `json:"-"`
	AvatarInternalID *int64  `json:"-"`
	RoleID           *string `json:"roleId,omitempty"`
	Role             string  `json:"role,omitempty"`
}

type modRelationshipPayload struct {
	Type                 string `json:"type"`
	RelatedModPublicID   string `json:"relatedModId,omitempty"`
	RelatedModSiteID     string `json:"relatedModSiteId,omitempty"`
	RelatedModName       string `json:"relatedModName"`
	RelatedModIdentifier string `json:"relatedModIdentifier,omitempty"`
	Notes                string `json:"notes"`
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
	ModID               string                          `json:"modId"`
	ModIDs              []modIdentifierPayload          `json:"modIds"`
	DefaultLocale       string                          `json:"defaultLocale"`
	Localizations       []catalogLocalizationEdit       `json:"localizations"`
	Environment         string                          `json:"environment"`
	PrimaryCategory     string                          `json:"primaryCategory"`
	SupportedVersions   []string                        `json:"supportedVersions"`
	SupportedLoaders    []string                        `json:"supportedLoaders"`
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
	ID                  int64                           `json:"-"`
	PublicID            string                          `json:"id"`
	UniqueID            string                          `json:"uniqueId"`
	SiteID              string                          `json:"siteId"`
	PrimaryName         string                          `json:"primaryName"`
	SecondaryName       string                          `json:"secondaryName"`
	Abbreviation        string                          `json:"abbreviation"`
	Summary             string                          `json:"summary"`
	ModID               string                          `json:"modId"`
	ModIDs              []modIdentifierPayload          `json:"modIds"`
	DefaultLocale       string                          `json:"defaultLocale"`
	Localizations       []catalogLocalizationEdit       `json:"localizations"`
	Environment         string                          `json:"environment"`
	PrimaryCategory     string                          `json:"primaryCategory"`
	SupportedVersions   []string                        `json:"supportedVersions"`
	SupportedLoaders    []string                        `json:"supportedLoaders"`
	Compatibilities     []modLoaderCompatibilityPayload `json:"compatibilities"`
	OfficialStatus      string                          `json:"officialStatus"`
	SourceStatus        string                          `json:"sourceStatus"`
	License             string                          `json:"license"`
	CurseForgeProjectID string                          `json:"curseforgeProjectId"`
	ModrinthProjectID   string                          `json:"modrinthProjectId"`
	GitHubProjectPath   string                          `json:"githubProjectPath"`
	IconURL             string                          `json:"iconUrl"`
	BodyMarkdown        string                          `json:"bodyMarkdown"`
	SearchKeywords      []string                        `json:"searchKeywords"`
	SubmissionMethod    string                          `json:"submissionMethod"`
	ReviewStatus        string                          `json:"reviewStatus"`
	CreatedByInternal   *int64                          `json:"-"`
	CreatedBy           string                          `json:"createdBy,omitempty"`
	CreatedAt           time.Time                       `json:"createdAt"`
	UpdatedAt           time.Time                       `json:"updatedAt"`
	PublishedAt         *time.Time                      `json:"publishedAt,omitempty"`
	PublishedRevisionID *string                         `json:"publishedRevisionId,omitempty"`
	Tags                []string                        `json:"tags"`
	Authors             []modAuthorPayload              `json:"authors"`
	Links               []modLinkPayload                `json:"links"`
	RelationshipGroups  []modRelationshipGroupPayload   `json:"relationshipGroups"`
	GalleryImages       []modGalleryImagePayload        `json:"galleryImages"`
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
var allowedModRelationshipTypes = stringSet("dependency", "extension", "integration")
var errModSiteIDTaken = errors.New("mod site ID already exists")
var allowedModLinkTypes = stringSet(
	"official", "curseforge", "modrinth", "klpbbs", "minebbs", "redstoneRelay", "mcbbsMemorial", "mcbbsArchive", "sourceforge", "minecraftForum", "planetMinecraft", "mcpedl", "spigotmc", "wiki",
	"github", "gitlab", "gitee", "gitea", "gitpod", "gitcode", "bitbucket", "maven", "crowdin", "mastodon",
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
	reviewRequired := loadReviewConfig(r.Context(), s.db).ModCreate && !hasPermission(claims.Permissions, "admin.*")
	reviewStatus := "approved"
	var publishedAt *time.Time
	if reviewRequired {
		reviewStatus = "pending"
	} else {
		now := time.Now().UTC()
		publishedAt = &now
	}
	permissionDefaults := s.permissionDefaultsFromSettings(r.Context())

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建模组失败")
		return
	}
	defer tx.Rollback(r.Context())

	if req.SiteID == "" {
		req.SiteID, err = availableModSiteID(r.Context(), tx, req.PrimaryName)
	} else {
		err = ensureModSiteIDAvailable(r.Context(), tx, req.SiteID, 0)
	}
	if errors.Is(err, errModSiteIDTaken) {
		writeError(w, http.StatusConflict, "模组站内 ID 已被占用")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成模组站内 ID 失败")
		return
	}
	uniqueID, err := availableModUniqueID(r.Context(), tx)
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
		mirroredIconURL, mirrorErr := s.mirrorExternalModIcon(r.Context(), req.IconURL, uniqueID, claims.Subject)
		if mirrorErr != nil {
			writeError(w, http.StatusBadGateway, "failed to store imported mod icon")
			return
		}
		req.IconURL = mirroredIconURL
	}
	var modID int64
	err = tx.QueryRow(
		r.Context(),
		`insert into mods (
			project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
			official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
			body_markdown, search_keywords, submission_method, review_status, created_by, published_at, supported_versions, supported_loaders
		 ) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		 returning id`,
		uniqueID, req.SiteID, req.PrimaryName, req.SecondaryName, req.Abbreviation, req.Summary, req.ModID, req.Environment, req.PrimaryCategory,
		req.OfficialStatus, req.SourceStatus, req.License, req.CurseForgeProjectID, req.ModrinthProjectID, req.GitHubProjectPath, req.IconURL,
		req.BodyMarkdown, req.SearchKeywords, req.SubmissionMethod, reviewStatus, claims.Subject, publishedAt, req.SupportedVersions, req.SupportedLoaders,
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
	for index, author := range req.Authors {
		creatorID, nameSnapshot, roleSnapshot, resolveErr := resolveModAuthorForCreateTx(r.Context(), tx, author, claims.Subject, r)
		if resolveErr != nil {
			writeError(w, http.StatusBadRequest, resolveErr.Error())
			return
		}
		roleID, resolveErr := creatorRoleInternalIDTx(r.Context(), tx, author.RoleID)
		if resolveErr != nil {
			writeError(w, http.StatusBadRequest, "selected creator role does not exist")
			return
		}
		if _, err = tx.Exec(r.Context(), `insert into content_creator_bindings(
			subject_id,subject_type,creator_id,role_id,name_snapshot,role_snapshot,display_order
		) values ($1,'mod',$2,$3,$4,$5,$6)`, modID, creatorID, roleID, nameSnapshot, roleSnapshot, index); err != nil {
			writeError(w, http.StatusInternalServerError, "保存作者资料失败")
			return
		}
	}
	if err = insertModCompatibilities(r.Context(), tx, modID, req.Compatibilities); err != nil {
		writeError(w, http.StatusInternalServerError, "保存模组兼容版本失败")
		return
	}
	if err = insertModRelationshipGroups(r.Context(), tx, modID, req.RelationshipGroups); err != nil {
		writeError(w, http.StatusInternalServerError, "保存模组关系失败")
		return
	}
	if permissionDefaults.DeveloperRole != "" {
		if err = s.bindProjectRoleTx(r.Context(), tx, claims.Subject, permissionDefaults.DeveloperRole, uniqueID); err != nil {
			writeError(w, http.StatusInternalServerError, "分配模组开发者权限组失败")
			return
		}
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
		if err = applyModSnapshot(r.Context(), tx, modID, createdRevision.RevisionID, req); err != nil {
			writeError(w, http.StatusInternalServerError, "发布模组版本失败")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, createdRevision.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "记录自动审核失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "提交模组资料失败")
		return
	}

	if !reviewRequired {
		s.scheduleModGalleryOSSRehome(modID)
	}
	mod, err := s.modByID(r.Context(), modID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取已创建模组失败")
		return
	}
	writeJSON(w, http.StatusCreated, mod)
}

func (s *Server) publicMods(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	limit := 100
	if parsed, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && parsed > 0 {
		limit = min(parsed, 100)
	}
	offset := boundedOffset(r.URL.Query().Get("offset"))
	var total int
	if err := s.db.QueryRow(
		r.Context(),
		`select count(*)
		 from mods m
		 where (m.review_status = 'approved' or m.created_by = $1)
		   and ($2 = '' or m.slug ilike '%' || $2 || '%' or m.project_code ilike '%' || $2 || '%' or m.primary_name ilike '%' || $2 || '%' or m.secondary_name ilike '%' || $2 || '%'
		        or m.abbreviation ilike '%' || $2 || '%' or m.mod_id ilike '%' || $2 || '%' or $2 = any(m.search_keywords)
		        or exists (select 1 from mod_identifiers identifier where identifier.mod_id=m.id and identifier.identifier ilike '%' || $2 || '%')
		        or exists (select 1 from content_creator_bindings a join creators creator on creator.id=a.creator_id
		                   where a.subject_id=m.id and a.subject_type='mod' and creator.name ilike '%' || $2 || '%'))`,
		claims.Subject, query,
	).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组总数失败")
		return
	}
	rows, err := s.db.Query(
		r.Context(),
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders,
		        (select revision.public_id from content_revisions revision where revision.id=m.published_revision_id)
		 from mods m
		 where (m.review_status = 'approved' or m.created_by = $1)
		   and ($2 = '' or m.slug ilike '%' || $2 || '%' or m.project_code ilike '%' || $2 || '%' or m.primary_name ilike '%' || $2 || '%' or m.secondary_name ilike '%' || $2 || '%'
		        or m.abbreviation ilike '%' || $2 || '%' or m.mod_id ilike '%' || $2 || '%' or $2 = any(m.search_keywords)
		        or exists (select 1 from mod_identifiers identifier where identifier.mod_id=m.id and identifier.identifier ilike '%' || $2 || '%')
		        or exists (select 1 from content_creator_bindings a join creators creator on creator.id=a.creator_id
		                   where a.subject_id=m.id and a.subject_type='mod' and creator.name ilike '%' || $2 || '%'))
		 order by m.updated_at desc, m.id desc
		 limit $3 offset $4`,
		claims.Subject, query, limit, offset,
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
	writeJSON(w, http.StatusOK, mod)
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
		&mod.ID, &mod.UniqueID, &mod.SiteID, &mod.PrimaryName, &mod.SecondaryName, &mod.Abbreviation, &mod.Summary, &mod.ModID,
		&mod.Environment, &mod.PrimaryCategory, &mod.OfficialStatus, &mod.SourceStatus, &mod.License,
		&mod.CurseForgeProjectID, &mod.ModrinthProjectID, &mod.GitHubProjectPath, &mod.IconURL, &mod.BodyMarkdown, &mod.SearchKeywords,
		&mod.SubmissionMethod, &mod.ReviewStatus, &mod.CreatedByInternal, &mod.CreatedAt, &mod.UpdatedAt, &mod.PublishedAt, &mod.SupportedVersions, &mod.SupportedLoaders, &mod.PublishedRevisionID,
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

	rows, err := s.db.Query(ctx, `select mod_id,tag from mod_tags where mod_id=any($1) order by mod_id,tag`, ids)
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
		where author.subject_id=any($1) and author.subject_type='mod'
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
		from mods mod join users user_account on user_account.id=mod.created_by where mod.id=any($1)`, ids)
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
		byID[modID].CreatedBy = publicID
	}
	return rows.Err()
}

func (s *Server) modByID(ctx context.Context, id int64, viewerID int64) (modResponse, error) {
	row := s.db.QueryRow(
		ctx,
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders,
		        (select revision.public_id from content_revisions revision where revision.id=mods.published_revision_id)
		 from mods where id = $1 and (review_status = 'approved' or created_by = $2)`,
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
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders,
		        (select revision.public_id from content_revisions revision where revision.id=mods.published_revision_id)
		 from mods where slug = $1 and (review_status = 'approved' or created_by = $2)`,
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
	row := s.db.QueryRow(ctx, `select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
		official_status, source_status, license, curseforge_project_id, modrinth_project_id, github_project_path, icon_url,
		body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders,
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
	if mod.CreatedByInternal != nil {
		if err := s.db.QueryRow(ctx, `select public_id from users where id=$1`, *mod.CreatedByInternal).Scan(&mod.CreatedBy); err != nil && !errors.Is(err, pgx.ErrNoRows) {
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
		role.public_id,coalesce(role.name,author.role_snapshot)
		from content_creator_bindings author
		join creators creator on creator.id=author.creator_id
		left join creator_role_definitions role on role.id=author.role_id
		where author.subject_id=$1 and author.subject_type='mod'
		order by author.display_order,author.id`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modAuthorPayload
		if err = rows.Scan(&item.CreatorID, &item.Kind, &item.Name, &item.AvatarURL, &item.RoleID, &item.Role); err != nil {
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

	rows, err = s.db.Query(ctx, `select id, label, loader, minecraft_versions, mod_version from mod_relationship_groups where mod_id = $1 order by display_order, id`, mod.ID)
	if err != nil {
		return err
	}
	groups := make([]struct {
		id    int64
		group modRelationshipGroupPayload
	}, 0)
	for rows.Next() {
		var item struct {
			id    int64
			group modRelationshipGroupPayload
		}
		if err = rows.Scan(&item.id, &item.group.Label, &item.group.Loader, &item.group.MinecraftVersions, &item.group.ModVersion); err != nil {
			rows.Close()
			return err
		}
		item.group.Direction = "outgoing"
		item.group.Relationships = []modRelationshipPayload{}
		groups = append(groups, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, group := range groups {
		rows, err = s.db.Query(ctx, `select relationship.relation_type,coalesce(related.project_code,''),
			coalesce(related.slug,''),relationship.related_mod_name,relationship.related_mod_identifier,relationship.notes
			from mod_relationships relationship
			left join mods related on related.id=relationship.related_mod_id
			where relationship.group_id=$1 order by relationship.display_order,relationship.id`, group.id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item modRelationshipPayload
			if err = rows.Scan(&item.Type, &item.RelatedModPublicID, &item.RelatedModSiteID, &item.RelatedModName, &item.RelatedModIdentifier, &item.Notes); err != nil {
				rows.Close()
				return err
			}
			group.group.Relationships = append(group.group.Relationships, item)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		mod.RelationshipGroups = append(mod.RelationshipGroups, group.group)
	}
	return nil
}

func (s *Server) loadIncomingModRelationships(ctx context.Context, mod *modResponse) error {
	rows, err := s.db.Query(ctx, `select relationship_group.id,relationship_group.label,relationship_group.loader,
		relationship_group.minecraft_versions,relationship_group.mod_version,relationship.relation_type,
		source_mod.project_code,source_mod.slug,source_mod.primary_name,relationship.notes
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
		var label, loader, modVersion, relationshipType, sourceModPublicID, sourceModSiteID, sourceModName, notes string
		var minecraftVersions []string
		if err = rows.Scan(&groupID, &label, &loader, &minecraftVersions, &modVersion, &relationshipType, &sourceModPublicID, &sourceModSiteID, &sourceModName, &notes); err != nil {
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
			Type: relationshipType, RelatedModPublicID: sourceModPublicID, RelatedModSiteID: sourceModSiteID, RelatedModName: sourceModName, Notes: notes,
		})
	}
	return rows.Err()
}

func normalizeAndValidateModRequest(req *createModRequest) error {
	req.SiteID = normalizeModSiteID(req.SiteID)
	req.PrimaryName = strings.TrimSpace(req.PrimaryName)
	req.SecondaryName = strings.TrimSpace(req.SecondaryName)
	req.Abbreviation = strings.TrimSpace(req.Abbreviation)
	req.Summary = strings.TrimSpace(req.Summary)
	req.ModID = strings.TrimSpace(req.ModID)
	defaultLocale, localizations, localizationErr := normalizeCatalogLocalizations(req.DefaultLocale, req.Localizations)
	if localizationErr != nil {
		return errors.New("localized mod content is invalid")
	}
	if len(localizations) == 0 {
		localizedName := req.SecondaryName
		if strings.TrimSpace(localizedName) == "" {
			localizedName = req.PrimaryName
		}
		localizations = []catalogLocalizationEdit{{Locale: defaultLocale, Name: localizedName, Summary: req.Summary, ContentMarkdown: req.BodyMarkdown}}
		defaultLocale, localizations, localizationErr = normalizeCatalogLocalizations(defaultLocale, localizations)
		if localizationErr != nil {
			return errors.New("localized mod content is invalid")
		}
	}
	if err := requireCatalogCreateDefaultLocalization(defaultLocale, localizations); err != nil {
		return errors.New("the default language must have a localized mod name")
	}
	req.DefaultLocale, req.Localizations = defaultLocale, localizations
	for _, localization := range localizations {
		if localization.Locale == defaultLocale {
			req.SecondaryName = localization.Name
			req.Summary = localization.Summary
			req.BodyMarkdown = localization.ContentMarkdown
			break
		}
	}
	req.Environment = strings.TrimSpace(req.Environment)
	req.PrimaryCategory = strings.TrimSpace(req.PrimaryCategory)
	req.OfficialStatus = strings.TrimSpace(req.OfficialStatus)
	req.SourceStatus = strings.TrimSpace(req.SourceStatus)
	req.License = strings.TrimSpace(req.License)
	req.CurseForgeProjectID = strings.TrimSpace(req.CurseForgeProjectID)
	req.ModrinthProjectID = strings.TrimSpace(req.ModrinthProjectID)
	req.GitHubProjectPath = normalizeGitHubProjectPath(req.GitHubProjectPath)
	req.IconURL = strings.TrimSpace(req.IconURL)
	req.BodyMarkdown = strings.TrimSpace(req.BodyMarkdown)
	req.SubmissionMethod = strings.TrimSpace(req.SubmissionMethod)
	if req.SubmissionMethod == "" {
		req.SubmissionMethod = "manual"
	}

	if req.PrimaryName == "" {
		return errors.New("主要名称不能为空")
	}
	if req.SiteID != "" && !validModSiteID(req.SiteID) {
		return errors.New("模组站内 ID 只能包含小写字母、数字、下划线或连字符，且必须以字母或数字开头和结尾")
	}
	if len(req.PrimaryName) > 160 || len(req.SecondaryName) > 160 {
		return errors.New("模组名称不能超过 160 个字符")
	}
	if !validModAbbreviation(req.Abbreviation) {
		return errors.New("简写名称只能包含 ASCII 字母、数字或符号，且不能超过 32 个字符")
	}
	if len(req.Summary) > 500 {
		return errors.New("简介不能超过 500 个字符")
	}
	if len(req.ModID) > 128 || len(req.CurseForgeProjectID) > 128 || len(req.ModrinthProjectID) > 128 || len(req.GitHubProjectPath) > 201 {
		return errors.New("项目标识不能超过 128 个字符")
	}
	if req.GitHubProjectPath != "" && !validGitHubProjectPath(req.GitHubProjectPath) {
		return errors.New("GitHub 项目地址必须使用 owner/repository 格式")
	}
	identifiers, err := normalizeModIdentifiers(req.ModID, req.ModIDs)
	if err != nil {
		return err
	}
	req.ModIDs = identifiers
	for _, identifier := range identifiers {
		if identifier.Primary {
			req.ModID = identifier.Identifier
			break
		}
	}
	if len(req.GalleryImages) > 32 {
		return errors.New("a mod gallery can contain at most 32 images")
	}
	seenGalleryFiles := map[string]bool{}
	seenGalleryIDs := map[string]bool{}
	cleanGallery := make([]modGalleryImagePayload, 0, len(req.GalleryImages))
	for _, image := range req.GalleryImages {
		image.PublicID = strings.ToLower(strings.TrimSpace(image.PublicID))
		image.FileID = strings.ToLower(strings.TrimSpace(image.FileID))
		if !validCatalogPublicID(image.FileID) || seenGalleryFiles[image.FileID] || (image.PublicID != "" && (!validCatalogPublicID(image.PublicID) || seenGalleryIDs[image.PublicID])) {
			return errors.New("mod gallery contains an invalid or duplicate image")
		}
		seenGalleryFiles[image.FileID] = true
		if image.PublicID != "" {
			seenGalleryIDs[image.PublicID] = true
		}
		cleanGallery = append(cleanGallery, modGalleryImagePayload{PublicID: image.PublicID, FileID: image.FileID})
	}
	req.GalleryImages = cleanGallery
	if len(req.BodyMarkdown) > 2*1024*1024 {
		return errors.New("正文内容过长")
	}
	if !allowedModEnvironments[req.Environment] || !allowedModCategories[req.PrimaryCategory] || !allowedModStatuses[req.OfficialStatus] || !allowedModSourceStatuses[req.SourceStatus] || !allowedModLicenses[req.License] || !allowedModSubmissionMethods[req.SubmissionMethod] {
		return errors.New("模组分类或状态字段无效")
	}
	if req.IconURL != "" && !validHTTPURL(req.IconURL) {
		return errors.New("模组图标链接无效")
	}

	req.Tags = uniqueTrimmed(req.Tags, 40)
	for _, tag := range req.Tags {
		if !allowedModTags[tag] {
			return fmt.Errorf("不支持的模组标签：%s", tag)
		}
	}
	req.SearchKeywords = uniqueTrimmed(req.SearchKeywords, 40)
	for _, keyword := range req.SearchKeywords {
		if len(keyword) > 80 {
			return errors.New("单个搜索关键词不能超过 80 个字符")
		}
	}
	req.SupportedVersions = uniqueTrimmed(req.SupportedVersions, 100)
	req.SupportedLoaders = uniqueTrimmed(req.SupportedLoaders, 30)
	if len(req.Compatibilities) == 0 && len(req.SupportedLoaders) > 0 {
		for _, loader := range req.SupportedLoaders {
			req.Compatibilities = append(req.Compatibilities, modLoaderCompatibilityPayload{Loader: loader, Versions: append([]string(nil), req.SupportedVersions...)})
		}
	}
	cleanCompatibilities := make([]modLoaderCompatibilityPayload, 0, len(req.Compatibilities))
	seenCompatibility := map[string]bool{}
	for _, compatibility := range req.Compatibilities {
		compatibility.Loader = strings.TrimSpace(compatibility.Loader)
		compatibility.Versions = uniqueTrimmed(compatibility.Versions, 500)
		if compatibility.Loader == "" || seenCompatibility[compatibility.Loader] {
			continue
		}
		if len(compatibility.Loader) > 80 {
			return errors.New("模组加载器名称不能超过 80 个字符")
		}
		for _, version := range compatibility.Versions {
			if len(version) > 80 {
				return errors.New("关系条件的 Minecraft 版本必须来自该模组支持的版本")
			}
		}
		seenCompatibility[compatibility.Loader] = true
		cleanCompatibilities = append(cleanCompatibilities, compatibility)
	}
	req.Compatibilities = cleanCompatibilities
	req.SupportedLoaders, req.SupportedVersions = compatibilitySummary(req.Compatibilities)
	supportedVersionSet := stringSet(req.SupportedVersions...)
	for index := range req.ModIDs {
		for _, version := range req.ModIDs[index].MinecraftVersions {
			if len(version) > 80 || (len(supportedVersionSet) > 0 && !supportedVersionSet[version]) {
				return errors.New("mod ID 的适用版本必须来自该模组支持的 Minecraft 版本")
			}
		}
	}
	for _, value := range append(append([]string{}, req.SupportedVersions...), req.SupportedLoaders...) {
		if len(value) > 80 {
			return errors.New("版本或加载器名称不能超过 80 个字符")
		}
	}
	if len(req.Links) > 64 || len(req.Authors) > 64 || len(req.RelationshipGroups) > 50 {
		return errors.New("关联资料数量过多")
	}

	cleanLinks := make([]modLinkPayload, 0, len(req.Links))
	seenLinks := map[string]bool{}
	for _, item := range req.Links {
		item.Type = strings.TrimSpace(item.Type)
		item.URL = strings.TrimSpace(item.URL)
		item.Note = strings.TrimSpace(item.Note)
		if item.Type == "" && item.URL == "" {
			continue
		}
		if !allowedModLinkTypes[item.Type] || !validHTTPURL(item.URL) || len(item.Note) > 240 {
			return errors.New("相关链接的类型或地址无效")
		}
		key := item.Type + "\x00" + item.URL
		if !seenLinks[key] {
			seenLinks[key] = true
			cleanLinks = append(cleanLinks, item)
		}
	}
	req.GitHubProjectPath, req.Links = synchronizeGitHubProjectLink(req.GitHubProjectPath, cleanLinks)

	cleanAuthors := make([]modAuthorPayload, 0, len(req.Authors))
	for _, item := range req.Authors {
		item.CreatorID = strings.ToLower(strings.TrimSpace(item.CreatorID))
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		item.Name = strings.TrimSpace(item.Name)
		item.AvatarURL = strings.TrimSpace(item.AvatarURL)
		item.Role = strings.TrimSpace(item.Role)
		if item.CreatorID == "" && item.Name == "" {
			continue
		}
		if item.CreatorID != "" && len(item.CreatorID) != 9 {
			return errors.New("作者或团队 ID 无效")
		}
		if item.Kind != "" && item.Kind != "author" && item.Kind != "team" {
			return errors.New("作者资料类型无效")
		}
		if item.AvatarURL != "" && !validHTTPURL(item.AvatarURL) {
			return errors.New("author avatar URL must use HTTP or HTTPS")
		}
		if len(item.Name) > 160 || len(item.Role) > 80 {
			return errors.New("作者或团队信息过长")
		}
		cleanAuthors = append(cleanAuthors, item)
	}
	req.Authors = cleanAuthors

	cleanGroups := make([]modRelationshipGroupPayload, 0, len(req.RelationshipGroups))
	relationshipCount := 0
	supportedLoaderSet := stringSet(req.SupportedLoaders...)
	for _, group := range req.RelationshipGroups {
		group.Label = strings.TrimSpace(group.Label)
		group.Loader = strings.TrimSpace(group.Loader)
		group.MinecraftVersions = uniqueTrimmed(group.MinecraftVersions, 100)
		group.ModVersion = strings.TrimSpace(group.ModVersion)
		if len(group.Label) > 160 || len(group.Loader) > 80 || len(group.ModVersion) > 160 {
			return errors.New("模组关系条件字段过长")
		}
		if group.Loader != "" && len(supportedLoaderSet) > 0 && !supportedLoaderSet[group.Loader] {
			return errors.New("关系条件的加载器必须来自该模组支持的加载器")
		}
		for _, version := range group.MinecraftVersions {
			if len(version) > 80 || (len(supportedVersionSet) > 0 && !supportedVersionSet[version]) {
				return errors.New("minecraft 版本名称不能超过 80 个字符")
			}
		}
		cleanRelationships := make([]modRelationshipPayload, 0, len(group.Relationships))
		for _, item := range group.Relationships {
			item.Type = strings.TrimSpace(item.Type)
			item.RelatedModPublicID = strings.ToLower(strings.TrimSpace(item.RelatedModPublicID))
			item.RelatedModName = strings.TrimSpace(item.RelatedModName)
			item.RelatedModIdentifier = strings.TrimSpace(item.RelatedModIdentifier)
			item.Notes = strings.TrimSpace(item.Notes)
			if item.Type == "" && item.RelatedModName == "" && item.RelatedModPublicID == "" && item.RelatedModIdentifier == "" {
				continue
			}
			if !allowedModRelationshipTypes[item.Type] ||
				(item.RelatedModName == "" && item.RelatedModPublicID == "" && item.RelatedModIdentifier == "") {
				return errors.New("模组关系缺少有效的关系类型或关联模组")
			}
			if item.RelatedModIdentifier != "" && !validModIdentifier(item.RelatedModIdentifier) {
				return errors.New("uncollected related Mod ID is invalid")
			}
			if len(item.RelatedModName) > 160 || len(item.Notes) > 500 {
				return errors.New("模组关系字段过长")
			}
			cleanRelationships = append(cleanRelationships, item)
			relationshipCount++
		}
		if relationshipCount > 200 {
			return errors.New("模组关系数量过多")
		}
		if len(cleanRelationships) == 0 {
			continue
		}
		group.Relationships = cleanRelationships
		cleanGroups = append(cleanGroups, group)
	}
	req.RelationshipGroups = cleanGroups
	return nil
}

func normalizeModIdentifiers(legacy string, values []modIdentifierPayload) ([]modIdentifierPayload, error) {
	if len(values) == 0 && strings.TrimSpace(legacy) != "" {
		values = []modIdentifierPayload{{Identifier: legacy, Primary: true}}
	}
	if len(values) > 32 {
		return nil, errors.New("a mod can have at most 32 Mod IDs")
	}
	result := make([]modIdentifierPayload, 0, len(values))
	seen := map[string]bool{}
	primaryCount := 0
	for _, value := range values {
		value.Identifier = strings.TrimSpace(value.Identifier)
		value.MinecraftVersionMin = strings.TrimSpace(value.MinecraftVersionMin)
		value.MinecraftVersionMax = strings.TrimSpace(value.MinecraftVersionMax)
		value.MinecraftVersions = uniqueTrimmed(value.MinecraftVersions, 500)
		if value.Identifier == "" {
			continue
		}
		if !validModIdentifier(value.Identifier) {
			return nil, errors.New("mod ID may only contain ASCII letters, numbers, underscores, dots and hyphens")
		}
		key := strings.ToLower(value.Identifier)
		if seen[key] {
			return nil, errors.New("duplicate Mod ID")
		}
		seen[key] = true
		if value.Primary {
			primaryCount++
		}
		result = append(result, value)
	}
	if len(result) > 0 && primaryCount == 0 {
		result[0].Primary = true
		primaryCount = 1
	}
	if primaryCount > 1 {
		return nil, errors.New("only one Mod ID can be primary")
	}
	return result, nil
}

func validModIdentifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || (index > 0 && (character == '_' || character == '.' || character == '-')) {
			continue
		}
		return false
	}
	return true
}

func normalizeGitHubProjectPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := url.Parse(value); err == nil && strings.EqualFold(parsed.Hostname(), "github.com") {
		value = parsed.Path
	}
	value = strings.Trim(strings.TrimSpace(value), "/")
	value = strings.TrimSuffix(value, ".git")
	parts := strings.Split(value, "/")
	if len(parts) >= 2 {
		return parts[0] + "/" + parts[1]
	}
	return value
}

func validGitHubProjectPath(value string) bool {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(parts[0]) > 100 || len(parts[1]) > 100 {
		return false
	}
	for partIndex, part := range parts {
		for index, character := range part {
			valid := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '-'
			if partIndex == 1 {
				valid = valid || character == '_' || character == '.'
			}
			if !valid || (partIndex == 0 && (index == 0 || index == len(part)-1) && character == '-') {
				return false
			}
		}
	}
	return true
}

func synchronizeGitHubProjectLink(projectPath string, links []modLinkPayload) (string, []modLinkPayload) {
	if projectPath == "" {
		for _, link := range links {
			if link.Type == "github" {
				candidate := normalizeGitHubProjectPath(link.URL)
				if validGitHubProjectPath(candidate) {
					projectPath = candidate
					break
				}
			}
		}
	}
	if projectPath == "" {
		return "", links
	}
	canonicalURL := "https://github.com/" + projectPath
	for index := range links {
		if links[index].Type == "github" {
			links[index].URL = canonicalURL
			return projectPath, links
		}
	}
	return projectPath, append(links, modLinkPayload{Type: "github", URL: canonicalURL})
}

func insertModIdentifiersTx(ctx context.Context, tx pgx.Tx, modID int64, values []modIdentifierPayload) error {
	for index, item := range values {
		if _, err := tx.Exec(ctx, `insert into mod_identifiers(
			mod_id,identifier,is_primary,minecraft_version_min,minecraft_version_max,minecraft_versions,display_order
		) values($1,$2,$3,$4,$5,$6,$7)`, modID, item.Identifier, item.Primary,
			item.MinecraftVersionMin, item.MinecraftVersionMax, item.MinecraftVersions, index); err != nil {
			return err
		}
	}
	return resolvePendingModReferencesTx(ctx, tx, modID)
}

func replaceModIdentifiersTx(ctx context.Context, tx pgx.Tx, modID int64, values []modIdentifierPayload) error {
	if _, err := tx.Exec(ctx, `delete from mod_identifiers where mod_id=$1`, modID); err != nil {
		return err
	}
	return insertModIdentifiersTx(ctx, tx, modID, values)
}

func validateModGalleryFilesTx(ctx context.Context, tx pgx.Tx, modID, actorID int64, images []modGalleryImagePayload) error {
	seenFileIDs := make(map[string]struct{}, len(images))
	for _, image := range images {
		if _, duplicate := seenFileIDs[image.FileID]; duplicate {
			return errors.New("mod gallery cannot contain the same uploaded file more than once")
		}
		seenFileIDs[image.FileID] = struct{}{}
		if image.PublicID != "" {
			var existingModID int64
			var existingFileID string
			err := tx.QueryRow(ctx, `select gallery.mod_id,file.public_id from mod_gallery_images gallery join oss_files file on file.id=gallery.oss_file_id where gallery.public_id=$1`, image.PublicID).
				Scan(&existingModID, &existingFileID)
			if err == nil && existingModID == modID && existingFileID == image.FileID {
				continue
			}
			if err == nil || !errors.Is(err, pgx.ErrNoRows) {
				return errors.New("mod gallery contains an invalid image reference")
			}
		}
		var valid bool
		if err := tx.QueryRow(ctx, `select exists(select 1 from oss_files file
			where file.public_id=$1 and file.uploader_id=$2 and file.status='active' and file.content_type ilike 'image/%'
			and not exists(select 1 from mod_gallery_images gallery where gallery.oss_file_id=file.id and gallery.mod_id<>$3))`,
			image.FileID, actorID, modID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return errors.New("a gallery image does not exist, is not an image, or is not owned by the editor")
		}
	}
	return nil
}

func replaceModGalleryImagesTx(ctx context.Context, tx pgx.Tx, modID, revisionID, actorID int64, images []modGalleryImagePayload) error {
	if _, err := tx.Exec(ctx, `delete from mod_gallery_images where mod_id=$1`, modID); err != nil {
		return err
	}
	for index, image := range images {
		if image.PublicID == "" {
			if _, err := tx.Exec(ctx, `insert into mod_gallery_images(mod_id,oss_file_id,display_order,created_by,published_revision_id)
				select $1,file.id,$3,$4,$5 from oss_files file where file.public_id=$2`,
				modID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(ctx, `insert into mod_gallery_images(public_id,mod_id,oss_file_id,display_order,created_by,published_revision_id)
			select $1,$2,file.id,$4,$5,$6 from oss_files file where file.public_id=$3`,
			image.PublicID, modID, image.FileID, index, nullableActorID(actorID), revisionID); err != nil {
			return err
		}
	}
	return nil
}

func insertModRelationshipGroups(ctx context.Context, tx pgx.Tx, modID int64, groups []modRelationshipGroupPayload) error {
	for groupIndex, group := range groups {
		var groupID int64
		if err := tx.QueryRow(ctx, `insert into mod_relationship_groups(
			mod_id,label,loader,minecraft_versions,mod_version,display_order
		) values($1,$2,$3,$4,$5,$6) returning id`,
			modID, group.Label, group.Loader, group.MinecraftVersions, group.ModVersion, groupIndex).Scan(&groupID); err != nil {
			return err
		}
		for relationshipIndex, relationship := range group.Relationships {
			var relatedModID *int64
			if relationship.RelatedModPublicID != "" {
				var resolvedID int64
				if err := tx.QueryRow(ctx, `select id,primary_name from mods where project_code=$1`,
					relationship.RelatedModPublicID).Scan(&resolvedID, &relationship.RelatedModName); err != nil || resolvedID == modID {
					return errors.New("selected related mod does not exist")
				}
				relatedModID = &resolvedID
			} else if relationship.RelatedModIdentifier != "" {
				var resolvedID int64
				err := tx.QueryRow(ctx, `select mod.id,mod.primary_name
					from mod_identifiers identifier join mods mod on mod.id=identifier.mod_id
					where lower(identifier.identifier)=lower($1) and mod.id<>$2 and mod.review_status='approved'
					order by identifier.is_primary desc,mod.id limit 1`,
					relationship.RelatedModIdentifier, modID).Scan(&resolvedID, &relationship.RelatedModName)
				if err == nil {
					relatedModID = &resolvedID
				} else if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
			if relatedModID == nil && relationship.RelatedModIdentifier == "" {
				return errors.New("a related mod or an uncollected Mod ID is required")
			}
			if relationship.Type == "integration" && relatedModID != nil {
				if _, err := tx.Exec(ctx, `delete from mod_relationships
					where relation_type='integration' and mod_id=$1 and related_mod_id=$2`,
					*relatedModID, modID); err != nil {
					return err
				}
			}
			var relationshipID int64
			if err := tx.QueryRow(ctx, `insert into mod_relationships(
				mod_id,group_id,relation_type,related_mod_id,related_mod_name,related_mod_identifier,notes,display_order
			) values($1,$2,$3,$4,$5,$6,$7,$8) returning id`,
				modID, groupID, relationship.Type, relatedModID, relationship.RelatedModName,
				relationship.RelatedModIdentifier, relationship.Notes, relationshipIndex).Scan(&relationshipID); err != nil {
				return err
			}
			if relatedModID == nil {
				if _, err := tx.Exec(ctx, `insert into unresolved_references(
					source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
				) values('mod_relationship',$1,'relatedMod','mod',$2,lower($2),jsonb_build_object('sourceModId',$3))
				on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
				set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
					resolved_at=null,metadata=excluded.metadata,updated_at=now()`,
					relationshipID, relationship.RelatedModIdentifier, modID); err != nil {
					return err
				}
			}
		}
	}
	if _, err := tx.Exec(ctx, `delete from mod_relationship_groups relationship_group
		where not exists(select 1 from mod_relationships relationship where relationship.group_id=relationship_group.id)`); err != nil {
		return err
	}
	return nil
}

func resolvePendingModReferencesTx(ctx context.Context, tx pgx.Tx, modID int64) error {
	var name, reviewStatus string
	if err := tx.QueryRow(ctx, `select primary_name,review_status from mods where id=$1`, modID).Scan(&name, &reviewStatus); err != nil {
		return err
	}
	if reviewStatus != "approved" {
		return nil
	}
	rows, err := tx.Query(ctx, `select unresolved.id,unresolved.source_id
		from unresolved_references unresolved
		join mod_relationships relationship
		  on unresolved.source_type='mod_relationship' and unresolved.source_id=relationship.id
		where unresolved.status='pending' and unresolved.reference_type='mod'
		  and relationship.mod_id<>$1
		  and exists(select 1 from mod_identifiers identifier
			where identifier.mod_id=$1 and lower(identifier.identifier)=unresolved.normalized_identifier)
		order by unresolved.id for update of unresolved`, modID)
	if err != nil {
		return err
	}
	type pendingReference struct {
		id             int64
		relationshipID int64
	}
	pending := make([]pendingReference, 0)
	for rows.Next() {
		var item pendingReference
		if err = rows.Scan(&item.id, &item.relationshipID); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, item := range pending {
		if _, err = tx.Exec(ctx, `update mod_relationships
			set related_mod_id=$2,related_mod_name=$3 where id=$1 and related_mod_id is null`,
			item.relationshipID, modID, name); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `update unresolved_references
			set status='resolved',resolved_type='mod',resolved_id=$2,resolved_at=now(),updated_at=now()
			where id=$1 and status='pending'`, item.id, modID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `update minecraft_server_mods server_mod
		set mod_id=$1
		from unresolved_references unresolved
		where unresolved.source_type='minecraft_server_mod'
		  and unresolved.source_id=server_mod.id
		  and unresolved.status='pending'
		  and unresolved.reference_type='mod'
		  and server_mod.mod_id is null
		  and exists(select 1 from mod_identifiers identifier
			where identifier.mod_id=$1
			  and lower(identifier.identifier)=unresolved.normalized_identifier)`, modID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update unresolved_references unresolved
		set status='resolved',resolved_type='mod',resolved_id=$1,resolved_at=now(),updated_at=now()
		where unresolved.source_type='minecraft_server_mod'
		  and unresolved.status='pending'
		  and unresolved.reference_type='mod'
		  and exists(select 1 from minecraft_server_mods server_mod
			join mod_identifiers identifier on identifier.mod_id=$1
			where server_mod.id=unresolved.source_id and server_mod.mod_id=$1
			  and lower(identifier.identifier)=unresolved.normalized_identifier)`, modID); err != nil {
		return err
	}
	return nil
}

func insertModCompatibilities(ctx context.Context, tx pgx.Tx, modID int64, compatibilities []modLoaderCompatibilityPayload) error {
	for _, compatibility := range compatibilities {
		for _, version := range compatibility.Versions {
			if _, err := tx.Exec(ctx, `insert into mod_loader_compatibilities (mod_id, loader, minecraft_version) values ($1,$2,$3) on conflict do nothing`, modID, compatibility.Loader, version); err != nil {
				return err
			}
		}
	}
	return nil
}

func validModAbbreviation(value string) bool {
	if len(value) > 32 {
		return false
	}
	for _, character := range value {
		if character < 33 || character > 126 {
			return false
		}
	}
	return true
}

func validHTTPURL(value string) bool {
	if len(value) > 2048 {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func uniqueTrimmed(values []string, maximum int) []string {
	result := make([]string, 0, min(len(values), maximum))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] || len(result) >= maximum {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func stringSet(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func resolveModAuthorForCreateTx(ctx context.Context, tx pgx.Tx, author modAuthorPayload, actorID int64, r *http.Request) (int64, string, string, error) {
	var creatorID int64
	var creatorName string
	if author.CreatorID != "" {
		err := tx.QueryRow(ctx, `select id,name from creators where public_id=$1`, author.CreatorID).Scan(&creatorID, &creatorName)
		if err != nil {
			return 0, "", "", errors.New("selected author or team does not exist")
		}
	} else {
		kind := author.Kind
		if kind == "" {
			kind = "author"
		}
		var err error
		creatorID, _, err = ensureNamedCreatorSnapshotTx(ctx, tx, creatorSnapshot{
			Kind: kind, Name: author.Name, AvatarURL: author.AvatarURL,
			AvatarFileID: author.AvatarFileID, AvatarInternalID: author.AvatarInternalID,
		}, actorID, r)
		if err != nil {
			return 0, "", "", err
		}
		creatorName, err = creatorNameTx(ctx, tx, creatorID)
		if err != nil {
			return 0, "", "", err
		}
	}
	roleName, err := creatorRoleNameTx(ctx, tx, author.RoleID)
	if err != nil {
		return 0, "", "", errors.New("selected creator role does not exist")
	}
	if roleName == "" {
		roleName = author.Role
	}
	return creatorID, creatorName, roleName, nil
}

func ensureNamedCreatorTx(ctx context.Context, tx pgx.Tx, kind, name string, actorID int64, r *http.Request) (int64, string, error) {
	return ensureNamedCreatorSnapshotTx(ctx, tx, creatorSnapshot{Kind: kind, Name: name}, actorID, r)
}

func ensureNamedCreatorSnapshotTx(ctx context.Context, tx pgx.Tx, snapshot creatorSnapshot, actorID int64, r *http.Request) (int64, string, error) {
	snapshot.Name = strings.TrimSpace(snapshot.Name)
	if snapshot.Name == "" {
		return 0, "", errors.New("creator name is required")
	}
	if snapshot.Kind != "author" && snapshot.Kind != "team" {
		snapshot.Kind = "author"
	}
	var id int64
	var publicID string
	err := tx.QueryRow(ctx, `select id,public_id from creators
		where kind=$1 and normalized_name=$2
		order by review_status='approved' desc,id limit 1`, snapshot.Kind, normalizeCreatorName(snapshot.Name)).Scan(&id, &publicID)
	if err == nil {
		return id, publicID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", err
	}
	id, publicID, _, err = (&Server{}).createCreatorTx(ctx, tx, snapshot, actorID, "pending", r)
	if err != nil {
		return 0, "", fmt.Errorf("create imported creator: %w", err)
	}
	return id, publicID, nil
}

type databaseQuery interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func availableModUniqueID(ctx context.Context, query databaseQuery) (string, error) {
	for attempt := 0; attempt < 100; attempt++ {
		code, err := newModUniqueID()
		if err != nil {
			return "", err
		}
		var exists bool
		if err := query.QueryRow(ctx, `select exists(select 1 from public_routes where public_id = $1)`, code).Scan(&exists); err != nil {
			return "", err
		}
		if !exists {
			return code, nil
		}
	}
	return "", errors.New("unable to allocate mod unique ID")
}

func newModUniqueID() (string, error) {
	const letters = "abcdefghjkmnpqrstuvwxyz"
	const digits = "23456789"
	const alphabet = letters + digits
	candidate := make([]byte, 9)
	for index, characters := range []string{letters, digits, alphabet, alphabet, alphabet, alphabet, alphabet, alphabet, alphabet} {
		value, err := rand.Int(rand.Reader, big.NewInt(int64(len(characters))))
		if err != nil {
			return "", err
		}
		candidate[index] = characters[value.Int64()]
	}
	return string(candidate), nil
}

func availableModSiteID(ctx context.Context, query databaseQuery, name string) (string, error) {
	base := modSiteIDBase(name)
	for suffix := 0; suffix < 1000; suffix++ {
		candidate := base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s_%d", base, suffix+1)
		}
		if err := ensureModSiteIDAvailable(ctx, query, candidate, 0); err == nil {
			return candidate, nil
		} else if !errors.Is(err, errModSiteIDTaken) {
			return "", err
		}
	}
	return "", errors.New("unable to allocate mod site ID")
}

func ensureModSiteIDAvailable(ctx context.Context, query databaseQuery, siteID string, excludeModID int64) error {
	var exists bool
	if err := query.QueryRow(ctx, `select exists(select 1 from mods where slug = $1 and id <> $2)`, siteID, excludeModID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return errModSiteIDTaken
	}
	return nil
}

func normalizeModSiteID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validModSiteID(value string) bool {
	if len(value) == 0 || len(value) > 100 || !isASCIIAlphaNumeric(value[0]) || !isASCIIAlphaNumeric(value[len(value)-1]) {
		return false
	}
	for index := 1; index < len(value)-1; index++ {
		character := value[index]
		if !isASCIIAlphaNumeric(character) && character != '_' && character != '-' {
			return false
		}
	}
	return true
}

func isASCIIAlphaNumeric(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
}

func modSiteIDBase(value string) string {
	var builder strings.Builder
	previousSeparator := false
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		isASCIIAlphanumeric := (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9')
		if isASCIIAlphanumeric {
			builder.WriteRune(character)
			previousSeparator = false
			continue
		}
		if !previousSeparator && builder.Len() > 0 {
			builder.WriteByte('_')
			previousSeparator = true
		}
	}
	siteID := strings.Trim(builder.String(), "_")
	if siteID == "" {
		return "mod"
	}
	if len(siteID) > 100 {
		siteID = strings.Trim(siteID[:100], "_")
	}
	return siteID
}
