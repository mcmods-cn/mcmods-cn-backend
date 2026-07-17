package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
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
}

type modAuthorPayload struct {
	CreatorID string `json:"creatorId,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Name      string `json:"name,omitempty"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	RoleID    *int64 `json:"roleId,omitempty"`
	Role      string `json:"role,omitempty"`
}

type modRelationshipPayload struct {
	Type           string `json:"type"`
	RelatedModID   *int64 `json:"relatedModId,omitempty"`
	RelatedModName string `json:"relatedModName"`
	Notes          string `json:"notes"`
}

type modRelationshipGroupPayload struct {
	Label             string                   `json:"label"`
	Loader            string                   `json:"loader"`
	MinecraftVersions []string                 `json:"minecraftVersions"`
	ModVersion        string                   `json:"modVersion"`
	Relationships     []modRelationshipPayload `json:"relationships"`
}

type modLoaderCompatibilityPayload struct {
	Loader   string   `json:"loader"`
	Versions []string `json:"versions"`
}

type createModRequest struct {
	SiteID              string                          `json:"siteId"`
	PrimaryName         string                          `json:"primaryName"`
	SecondaryName       string                          `json:"secondaryName"`
	Abbreviation        string                          `json:"abbreviation"`
	Summary             string                          `json:"summary"`
	ModID               string                          `json:"modId"`
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
	IconURL             string                          `json:"iconUrl"`
	BodyMarkdown        string                          `json:"bodyMarkdown"`
	SubmissionMethod    string                          `json:"submissionMethod"`
	Links               []modLinkPayload                `json:"links"`
	RelationshipGroups  []modRelationshipGroupPayload   `json:"relationshipGroups"`
}

type modResponse struct {
	ID                  int64                           `json:"id"`
	UniqueID            string                          `json:"uniqueId"`
	SiteID              string                          `json:"siteId"`
	PrimaryName         string                          `json:"primaryName"`
	SecondaryName       string                          `json:"secondaryName"`
	Abbreviation        string                          `json:"abbreviation"`
	Summary             string                          `json:"summary"`
	ModID               string                          `json:"modId"`
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
	IconURL             string                          `json:"iconUrl"`
	BodyMarkdown        string                          `json:"bodyMarkdown"`
	SearchKeywords      []string                        `json:"searchKeywords"`
	SubmissionMethod    string                          `json:"submissionMethod"`
	ReviewStatus        string                          `json:"reviewStatus"`
	CreatedBy           *int64                          `json:"createdBy,omitempty"`
	CreatedAt           time.Time                       `json:"createdAt"`
	UpdatedAt           time.Time                       `json:"updatedAt"`
	PublishedAt         *time.Time                      `json:"publishedAt,omitempty"`
	PublishedRevisionID *int64                          `json:"publishedRevisionId,omitempty"`
	Tags                []string                        `json:"tags"`
	Authors             []modAuthorPayload              `json:"authors"`
	Links               []modLinkPayload                `json:"links"`
	RelationshipGroups  []modRelationshipGroupPayload   `json:"relationshipGroups"`
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
			official_status, source_status, license, curseforge_project_id, modrinth_project_id, icon_url,
			body_markdown, search_keywords, submission_method, review_status, created_by, published_at, supported_versions, supported_loaders
		 ) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		 returning id`,
		uniqueID, req.SiteID, req.PrimaryName, req.SecondaryName, req.Abbreviation, req.Summary, req.ModID, req.Environment, req.PrimaryCategory,
		req.OfficialStatus, req.SourceStatus, req.License, req.CurseForgeProjectID, req.ModrinthProjectID, req.IconURL,
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

	for index, link := range req.Links {
		if _, err = tx.Exec(r.Context(), `insert into mod_links (mod_id, link_type, url, display_order) values ($1,$2,$3,$4)`, modID, link.Type, link.URL, index); err != nil {
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
		if _, err = tx.Exec(r.Context(), `insert into mod_authors(
			mod_id,creator_id,role_id,name_snapshot,role_snapshot,display_order
		) values ($1,$2,$3,$4,$5,$6)`, modID, creatorID, author.RoleID, nameSnapshot, roleSnapshot, index); err != nil {
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
		AggregateType: "mod",
		AggregateKey:  strconv.FormatInt(modID, 10),
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
		if _, err = tx.Exec(r.Context(), `update mods set published_revision_id=$2 where id=$1`, modID, createdRevision.RevisionID); err != nil {
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
	var total int
	if err := s.db.QueryRow(
		r.Context(),
		`select count(*)
		 from mods m
		 where (m.review_status = 'approved' or m.created_by = $1)
		   and ($2 = '' or m.slug ilike '%' || $2 || '%' or m.project_code ilike '%' || $2 || '%' or m.primary_name ilike '%' || $2 || '%' or m.secondary_name ilike '%' || $2 || '%'
		        or m.abbreviation ilike '%' || $2 || '%' or m.mod_id ilike '%' || $2 || '%' or $2 = any(m.search_keywords)
		        or exists (select 1 from mod_authors a join creators creator on creator.id=a.creator_id
		                   where a.mod_id = m.id and creator.name ilike '%' || $2 || '%'))`,
		claims.Subject, query,
	).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组总数失败")
		return
	}
	rows, err := s.db.Query(
		r.Context(),
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders, published_revision_id
		 from mods m
		 where (m.review_status = 'approved' or m.created_by = $1)
		   and ($2 = '' or m.slug ilike '%' || $2 || '%' or m.project_code ilike '%' || $2 || '%' or m.primary_name ilike '%' || $2 || '%' or m.secondary_name ilike '%' || $2 || '%'
		        or m.abbreviation ilike '%' || $2 || '%' or m.mod_id ilike '%' || $2 || '%' or $2 = any(m.search_keywords)
		        or exists (select 1 from mod_authors a join creators creator on creator.id=a.creator_id
		                   where a.mod_id = m.id and creator.name ilike '%' || $2 || '%'))
		 order by m.updated_at desc, m.id desc
		 limit $3`,
		claims.Subject, query, limit,
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
		if err = s.loadModAssociations(r.Context(), &mod); err != nil {
			writeError(w, http.StatusInternalServerError, "读取模组关联资料失败")
			return
		}
		items = append(items, mod)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组列表失败")
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
		&mod.CurseForgeProjectID, &mod.ModrinthProjectID, &mod.IconURL, &mod.BodyMarkdown, &mod.SearchKeywords,
		&mod.SubmissionMethod, &mod.ReviewStatus, &mod.CreatedBy, &mod.CreatedAt, &mod.UpdatedAt, &mod.PublishedAt, &mod.SupportedVersions, &mod.SupportedLoaders, &mod.PublishedRevisionID,
	)
	mod.Tags = []string{}
	mod.Authors = []modAuthorPayload{}
	mod.Links = []modLinkPayload{}
	mod.RelationshipGroups = []modRelationshipGroupPayload{}
	mod.Compatibilities = []modLoaderCompatibilityPayload{}
	return mod, err
}

func (s *Server) modByID(ctx context.Context, id int64, viewerID int64) (modResponse, error) {
	row := s.db.QueryRow(
		ctx,
		`select id, project_code, slug, primary_name, secondary_name, abbreviation, summary, mod_id, environment, primary_category,
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders, published_revision_id
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
		        official_status, source_status, license, curseforge_project_id, modrinth_project_id, icon_url,
		        body_markdown, search_keywords, submission_method, review_status, created_by, created_at, updated_at, published_at, supported_versions, supported_loaders, published_revision_id
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

func (s *Server) loadModAssociations(ctx context.Context, mod *modResponse) error {
	rows, err := s.db.Query(ctx, `select link_type, url from mod_links where mod_id = $1 order by display_order, id`, mod.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item modLinkPayload
		if err = rows.Scan(&item.Type, &item.URL); err != nil {
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
		author.role_id,coalesce(role.name,author.role_snapshot)
		from mod_authors author
		join creators creator on creator.id=author.creator_id
		left join creator_role_definitions role on role.id=author.role_id
		where author.mod_id=$1 order by author.display_order,author.id`, mod.ID)
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
		item.group.Relationships = []modRelationshipPayload{}
		groups = append(groups, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, group := range groups {
		rows, err = s.db.Query(ctx, `select relation_type, related_mod_id, related_mod_name, notes from mod_relationships where group_id = $1 order by display_order, id`, group.id)
		if err != nil {
			return err
		}
		for rows.Next() {
			var item modRelationshipPayload
			if err = rows.Scan(&item.Type, &item.RelatedModID, &item.RelatedModName, &item.Notes); err != nil {
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

func normalizeAndValidateModRequest(req *createModRequest) error {
	req.SiteID = normalizeModSiteID(req.SiteID)
	req.PrimaryName = strings.TrimSpace(req.PrimaryName)
	req.SecondaryName = strings.TrimSpace(req.SecondaryName)
	req.Abbreviation = strings.TrimSpace(req.Abbreviation)
	req.Summary = strings.TrimSpace(req.Summary)
	req.ModID = strings.TrimSpace(req.ModID)
	req.Environment = strings.TrimSpace(req.Environment)
	req.PrimaryCategory = strings.TrimSpace(req.PrimaryCategory)
	req.OfficialStatus = strings.TrimSpace(req.OfficialStatus)
	req.SourceStatus = strings.TrimSpace(req.SourceStatus)
	req.License = strings.TrimSpace(req.License)
	req.CurseForgeProjectID = strings.TrimSpace(req.CurseForgeProjectID)
	req.ModrinthProjectID = strings.TrimSpace(req.ModrinthProjectID)
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
	if len(req.ModID) > 128 || len(req.CurseForgeProjectID) > 128 || len(req.ModrinthProjectID) > 128 {
		return errors.New("项目标识不能超过 128 个字符")
	}
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
				return errors.New("Minecraft 版本名称不能超过 80 个字符")
			}
		}
		seenCompatibility[compatibility.Loader] = true
		cleanCompatibilities = append(cleanCompatibilities, compatibility)
	}
	req.Compatibilities = cleanCompatibilities
	req.SupportedLoaders, req.SupportedVersions = compatibilitySummary(req.Compatibilities)
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
		if item.Type == "" && item.URL == "" {
			continue
		}
		if !allowedModLinkTypes[item.Type] || !validHTTPURL(item.URL) {
			return errors.New("相关链接的类型或地址无效")
		}
		key := item.Type + "\x00" + item.URL
		if !seenLinks[key] {
			seenLinks[key] = true
			cleanLinks = append(cleanLinks, item)
		}
	}
	req.Links = cleanLinks

	cleanAuthors := make([]modAuthorPayload, 0, len(req.Authors))
	for _, item := range req.Authors {
		item.CreatorID = strings.ToLower(strings.TrimSpace(item.CreatorID))
		item.Kind = strings.ToLower(strings.TrimSpace(item.Kind))
		item.Name = strings.TrimSpace(item.Name)
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
		if len(item.Name) > 160 || len(item.Role) > 80 {
			return errors.New("作者或团队信息过长")
		}
		cleanAuthors = append(cleanAuthors, item)
	}
	req.Authors = cleanAuthors

	cleanGroups := make([]modRelationshipGroupPayload, 0, len(req.RelationshipGroups))
	relationshipCount := 0
	for _, group := range req.RelationshipGroups {
		group.Label = strings.TrimSpace(group.Label)
		group.Loader = strings.TrimSpace(group.Loader)
		group.MinecraftVersions = uniqueTrimmed(group.MinecraftVersions, 100)
		group.ModVersion = strings.TrimSpace(group.ModVersion)
		if len(group.Label) > 160 || len(group.Loader) > 80 || len(group.ModVersion) > 160 {
			return errors.New("模组关系条件字段过长")
		}
		for _, version := range group.MinecraftVersions {
			if len(version) > 80 {
				return errors.New("Minecraft 版本名称不能超过 80 个字符")
			}
		}
		cleanRelationships := make([]modRelationshipPayload, 0, len(group.Relationships))
		for _, item := range group.Relationships {
			item.Type = strings.TrimSpace(item.Type)
			item.RelatedModName = strings.TrimSpace(item.RelatedModName)
			item.Notes = strings.TrimSpace(item.Notes)
			if item.Type == "" && item.RelatedModName == "" && item.RelatedModID == nil {
				continue
			}
			if !allowedModRelationshipTypes[item.Type] || (item.RelatedModName == "" && item.RelatedModID == nil) {
				return errors.New("模组关系缺少有效的关系类型或关联模组")
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

func insertModRelationshipGroups(ctx context.Context, tx pgx.Tx, modID int64, groups []modRelationshipGroupPayload) error {
	for groupIndex, group := range groups {
		var groupID int64
		if err := tx.QueryRow(
			ctx,
			`insert into mod_relationship_groups (mod_id, label, loader, minecraft_versions, mod_version, display_order)
			 values ($1,$2,$3,$4,$5,$6) returning id`,
			modID, group.Label, group.Loader, group.MinecraftVersions, group.ModVersion, groupIndex,
		).Scan(&groupID); err != nil {
			return err
		}
		for relationshipIndex, relationship := range group.Relationships {
			if _, err := tx.Exec(
				ctx,
				`insert into mod_relationships (mod_id, group_id, relation_type, related_mod_id, related_mod_name, notes, display_order)
				 values ($1,$2,$3,$4,$5,$6,$7)`,
				modID, groupID, relationship.Type, relationship.RelatedModID, relationship.RelatedModName, relationship.Notes, relationshipIndex,
			); err != nil {
				return err
			}
		}
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
		creatorID, _, err = ensureNamedCreatorTx(ctx, tx, kind, author.Name, actorID, r)
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
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, "", errors.New("creator name is required")
	}
	if kind != "author" && kind != "team" {
		kind = "author"
	}
	var id int64
	var publicID string
	err := tx.QueryRow(ctx, `select id,public_id from creators
		where kind=$1 and normalized_name=$2
		order by review_status='approved' desc,id limit 1`, kind, normalizeCreatorName(name)).Scan(&id, &publicID)
	if err == nil {
		return id, publicID, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", err
	}
	id, publicID, _, err = (&Server{}).createCreatorTx(ctx, tx, creatorSnapshot{Kind: kind, Name: name}, actorID, "pending", r)
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
