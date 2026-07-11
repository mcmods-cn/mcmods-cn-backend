package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type createModDataPageRequest struct {
	MinecraftVersion string `json:"minecraftVersion"`
	Category         string `json:"category"`
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	ContentMarkdown  string `json:"contentMarkdown"`
}

type reviewModDataPageRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type createModDataVersionRequest struct {
	MinecraftVersion string `json:"minecraftVersion"`
}

type modDataVersionResponse struct {
	ID               int64     `json:"id"`
	MinecraftVersion string    `json:"minecraftVersion"`
	ItemCount        int       `json:"itemCount"`
	CreatedAt        time.Time `json:"createdAt"`
}

type modDataPageResponse struct {
	ID               int64      `json:"id"`
	ModID            int64      `json:"modId"`
	MinecraftVersion string     `json:"minecraftVersion"`
	Category         string     `json:"category"`
	Title            string     `json:"title"`
	Summary          string     `json:"summary"`
	ContentMarkdown  string     `json:"contentMarkdown"`
	Status           string     `json:"status"`
	CreatedBy        *int64     `json:"createdBy,omitempty"`
	ReviewedBy       *int64     `json:"reviewedBy,omitempty"`
	ReviewNote       string     `json:"reviewNote"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	ReviewedAt       *time.Time `json:"reviewedAt,omitempty"`
}

var allowedModDataCategories = stringSet(
	"itemsBlocks", "biomes", "entities", "enchantments", "buffs", "multiblocks", "worldgen", "keybinds",
	"gameSettings", "commands", "skills", "elements", "achievements", "lootTables", "customPages",
)

func (s *Server) modDataPages(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	canSeePending := canEditMod(claims, identity) || hasPermission(claims.Permissions, "project.review")
	version := strings.TrimSpace(r.URL.Query().Get("version"))
	rows, err := s.db.Query(
		r.Context(),
		`select id, mod_id, minecraft_version, category, title, summary, content_markdown, status,
		        created_by, reviewed_by, review_note, created_at, updated_at, reviewed_at
		 from mod_data_pages
		 where mod_id = $1 and ($2 = '' or minecraft_version = $2) and (status = 'approved' or $3)
		 order by minecraft_version desc, category, title, id`,
		identity.ID, version, canSeePending,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组资料失败")
		return
	}
	defer rows.Close()
	items := make([]modDataPageResponse, 0)
	for rows.Next() {
		item, scanErr := scanModDataPage(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "解析模组资料失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组资料失败")
		return
	}
	versions, err := s.modDataVersions(r, identity.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取资料版本失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"versions": versions, "items": items})
}

func (s *Server) createModDataVersion(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	if !canEditMod(claims, identity) {
		writeError(w, http.StatusForbidden, "没有新增资料版本的权限")
		return
	}
	var req createModDataVersionRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.MinecraftVersion = strings.TrimSpace(req.MinecraftVersion)
	if req.MinecraftVersion == "" || len(req.MinecraftVersion) > 80 {
		writeError(w, http.StatusBadRequest, "Minecraft 版本不正确")
		return
	}
	var item modDataVersionResponse
	err = s.db.QueryRow(
		r.Context(),
		`insert into mod_data_versions (mod_id, minecraft_version, display_order, created_by)
		 values ($1,$2,(select count(*) from mod_data_versions where mod_id=$1),$3)
		 on conflict (mod_id, minecraft_version) do update set minecraft_version=excluded.minecraft_version
		 returning id, minecraft_version, created_at`,
		identity.ID, req.MinecraftVersion, claims.Subject,
	).Scan(&item.ID, &item.MinecraftVersion, &item.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建资料版本失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) createModDataPage(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	if !canEditMod(claims, identity) {
		writeError(w, http.StatusForbidden, "没有新增模组资料的权限")
		return
	}
	var req createModDataPageRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.MinecraftVersion = strings.TrimSpace(req.MinecraftVersion)
	req.Category = strings.TrimSpace(req.Category)
	req.Title = strings.TrimSpace(req.Title)
	req.Summary = strings.TrimSpace(req.Summary)
	req.ContentMarkdown = strings.TrimSpace(req.ContentMarkdown)
	if req.MinecraftVersion == "" || req.Title == "" || !allowedModDataCategories[req.Category] {
		writeError(w, http.StatusBadRequest, "Minecraft 版本、资料类型和标题不能为空")
		return
	}
	if len(req.MinecraftVersion) > 80 || len(req.Title) > 160 || len(req.Summary) > 500 || len(req.ContentMarkdown) > 2*1024*1024 {
		writeError(w, http.StatusBadRequest, "模组资料内容过长")
		return
	}
	var versionExists bool
	if err = s.db.QueryRow(r.Context(), `select exists(select 1 from mod_data_versions where mod_id=$1 and minecraft_version=$2)`, identity.ID, req.MinecraftVersion).Scan(&versionExists); err != nil || !versionExists {
		writeError(w, http.StatusBadRequest, "请先创建对应的资料版本")
		return
	}
	var id int64
	status := "pending"
	if canSkipProjectReview(claims, identity) {
		status = "approved"
	}
	err = s.db.QueryRow(
		r.Context(),
		`insert into mod_data_pages (mod_id, minecraft_version, category, title, summary, content_markdown, status, created_by, reviewed_by, reviewed_at)
		 values ($1,$2,$3,$4,$5,$6,$7,$8,case when $7='approved' then $8 else null end,case when $7='approved' then now() else null end) returning id`,
		identity.ID, req.MinecraftVersion, req.Category, req.Title, req.Summary, req.ContentMarkdown, status, claims.Subject,
	).Scan(&id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "新增模组资料失败")
		return
	}
	item, err := s.modDataPageByID(r, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取新资料失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) reviewModDataPage(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "模组不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组失败")
		return
	}
	id, err := strconv.ParseInt(r.PathValue("dataId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "资料编号无效")
		return
	}
	var req reviewModDataPageRequest
	if err = decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	req.Note = strings.TrimSpace(req.Note)
	if req.Status != "approved" && req.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "审核结果无效")
		return
	}
	claims := currentClaims(r)
	result, err := s.db.Exec(
		r.Context(),
		`update mod_data_pages set status=$2, reviewed_by=$3, review_note=$4, reviewed_at=now(), updated_at=now()
		 where id=$1 and status='pending' and mod_id=$5`,
		id, req.Status, claims.Subject, req.Note, identity.ID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存资料审核结果失败")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusConflict, "该资料不存在或已经审核")
		return
	}
	item, _ := s.modDataPageByID(r, id)
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) modDataPageByID(r *http.Request, id int64) (modDataPageResponse, error) {
	return scanModDataPage(s.db.QueryRow(
		r.Context(),
		`select id, mod_id, minecraft_version, category, title, summary, content_markdown, status,
		        created_by, reviewed_by, review_note, created_at, updated_at, reviewed_at from mod_data_pages where id=$1`,
		id,
	))
}

func scanModDataPage(row scanner) (modDataPageResponse, error) {
	var item modDataPageResponse
	err := row.Scan(&item.ID, &item.ModID, &item.MinecraftVersion, &item.Category, &item.Title, &item.Summary,
		&item.ContentMarkdown, &item.Status, &item.CreatedBy, &item.ReviewedBy, &item.ReviewNote,
		&item.CreatedAt, &item.UpdatedAt, &item.ReviewedAt)
	return item, err
}

func (s *Server) modDataVersions(r *http.Request, modID int64) ([]modDataVersionResponse, error) {
	rows, err := s.db.Query(
		r.Context(),
		`select v.id, v.minecraft_version, count(p.id), v.created_at
		 from mod_data_versions v
		 left join mod_data_pages p on p.mod_id=v.mod_id and p.minecraft_version=v.minecraft_version and p.status='approved'
		 where v.mod_id=$1
		 group by v.id
		 order by v.display_order, v.id`,
		modID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]modDataVersionResponse, 0)
	for rows.Next() {
		var item modDataVersionResponse
		if err = rows.Scan(&item.ID, &item.MinecraftVersion, &item.ItemCount, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
