package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func normalizedSiteAffairsLocale(value string) string {
	value = normalizeContentLocale(value)
	if isEditableContentLocale(value) {
		return value
	}
	return "zh-CN"
}

func (s *Server) publicAboutPage(w http.ResponseWriter, r *http.Request) {
	locale := normalizedSiteAffairsLocale(r.URL.Query().Get("locale"))
	var title, body, resolvedLocale string
	err := s.db.QueryRow(r.Context(), `select translation.title,translation.body_markdown,translation.locale
		from site_pages page join site_page_translations translation on translation.page_id=page.id
		where page.code='about' and page.status='published' and translation.status='published'
		order by case translation.locale when $1 then 0 when 'zh-CN' then 1 when 'en-US' then 2 else 3 end limit 1`, locale).
		Scan(&title, &body, &resolvedLocale)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "关于本站页面尚未发布")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取关于本站失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": "about", "locale": resolvedLocale, "title": title, "bodyMarkdown": body})
}

type updateSitePageRequest struct {
	Title        string `json:"title"`
	BodyMarkdown string `json:"bodyMarkdown"`
	Publish      bool   `json:"publish"`
}

func (s *Server) adminAboutPage(w http.ResponseWriter, r *http.Request) {
	locale := normalizedSiteAffairsLocale(r.PathValue("locale"))
	if r.Method == http.MethodGet {
		var title, body, pageStatus, translationStatus string
		var revision int64
		err := s.db.QueryRow(r.Context(), `select translation.title,translation.body_markdown,page.status,translation.status,translation.revision
			from site_pages page join site_page_translations translation on translation.page_id=page.id
			where page.code='about' and translation.locale=$1`, locale).Scan(&title, &body, &pageStatus, &translationStatus, &revision)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "title": "", "bodyMarkdown": "", "status": "draft", "revision": 0})
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取站务页面失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "title": title, "bodyMarkdown": body, "status": pageStatus, "translationStatus": translationStatus, "revision": revision})
		return
	}
	var request updateSitePageRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "页面内容格式不正确")
		return
	}
	request.Title = strings.TrimSpace(request.Title)
	request.BodyMarkdown = strings.TrimSpace(request.BodyMarkdown)
	if request.Title == "" || request.BodyMarkdown == "" || len(request.BodyMarkdown) > 200000 {
		writeError(w, http.StatusBadRequest, "标题和正文不能为空")
		return
	}
	status := "draft"
	if request.Publish {
		status = "published"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "创建站务页面事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var pageID int64
	err = tx.QueryRow(r.Context(), `insert into site_pages(code,status,updated_by) values('about',$1,$2)
		on conflict(code) do update set status=excluded.status,updated_by=excluded.updated_by,updated_at=now(),published_revision=site_pages.published_revision+case when excluded.status='published' then 1 else 0 end returning id`, status, currentClaims(r).Subject).Scan(&pageID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `insert into site_page_translations(page_id,locale,title,body_markdown,status,updated_by)
			values($1,$2,$3,$4,$5,$6) on conflict(page_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown,status=excluded.status,revision=site_page_translations.revision+1,updated_by=excluded.updated_by,updated_at=now()`, pageID, locale, request.Title, request.BodyMarkdown, status, currentClaims(r).Subject)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "保存站务页面失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "site_affairs.about.update", locale, currentClaims(r).Subject, r, 200, 0, map[string]any{"published": request.Publish})
	writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "status": status})
}

func (s *Server) publicSiteChangelogs(w http.ResponseWriter, r *http.Request) {
	locale := normalizedSiteAffairsLocale(r.URL.Query().Get("locale"))
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	rows, err := s.db.Query(r.Context(), `select changelog.public_id,changelog.change_date,translation.locale,translation.title,translation.body_markdown
		from site_changelogs changelog join lateral (
			select locale,title,body_markdown from site_changelog_translations where changelog_id=changelog.id
			order by case locale when $1 then 0 when 'zh-CN' then 1 when 'en-US' then 2 else 3 end limit 1
		) translation on true where changelog.status='published'
		order by changelog.change_date desc,changelog.id desc limit $2 offset $3`, locale, limit, offset)
	if err != nil {
		writeError(w, 500, "读取站点更新记录失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, resolvedLocale, title, body string
		var changeDate time.Time
		if err = rows.Scan(&id, &changeDate, &resolvedLocale, &title, &body); err != nil {
			writeError(w, 500, "解析站点更新记录失败")
			return
		}
		items = append(items, map[string]any{"id": id, "changeDate": changeDate, "locale": resolvedLocale, "title": title, "bodyMarkdown": body})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
}

func (s *Server) publicSiteChangelogDetail(w http.ResponseWriter, r *http.Request) {
	locale := normalizedSiteAffairsLocale(r.URL.Query().Get("locale"))
	var id, resolvedLocale, title, body string
	var changeDate time.Time
	err := s.db.QueryRow(r.Context(), `select changelog.public_id,changelog.change_date,translation.locale,translation.title,translation.body_markdown
		from site_changelogs changelog join lateral (
			select locale,title,body_markdown from site_changelog_translations where changelog_id=changelog.id
			order by case locale when $2 then 0 when 'zh-CN' then 1 when 'en-US' then 2 else 3 end limit 1
		) translation on true where changelog.public_id=$1 and changelog.status='published'`, r.PathValue("id"), locale).
		Scan(&id, &changeDate, &resolvedLocale, &title, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "站点更新记录不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取站点更新记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "changeDate": changeDate, "locale": resolvedLocale, "title": title, "bodyMarkdown": body})
}

type saveSiteChangelogRequest struct {
	ChangeDate   string `json:"changeDate"`
	Locale       string `json:"locale"`
	Title        string `json:"title"`
	BodyMarkdown string `json:"bodyMarkdown"`
	Publish      bool   `json:"publish"`
}

func (s *Server) adminSiteChangelogs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.adminSiteChangelogList(w, r)
		return
	}
	var request saveSiteChangelogRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, 400, "更新记录格式不正确")
		return
	}
	date, err := time.Parse("2006-01-02", request.ChangeDate)
	request.Locale = normalizedSiteAffairsLocale(request.Locale)
	request.Title = strings.TrimSpace(request.Title)
	request.BodyMarkdown = strings.TrimSpace(request.BodyMarkdown)
	if err != nil || request.Title == "" || request.BodyMarkdown == "" || len(request.BodyMarkdown) > 200000 {
		writeError(w, 400, "日期、标题或正文不正确")
		return
	}
	status := "draft"
	if request.Publish {
		status = "published"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "创建更新记录事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var id int64
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into site_changelogs(change_date,status,created_by) values($1,$2,$3) returning id,public_id`, date, status, currentClaims(r).Subject).Scan(&id, &publicID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,updated_by) values($1,$2,$3,$4,$5)`, id, request.Locale, request.Title, request.BodyMarkdown, currentClaims(r).Subject)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "保存站点更新记录失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "site_affairs.changelog.create", publicID, currentClaims(r).Subject, r, 201, 0, nil)
	writeJSON(w, http.StatusCreated, map[string]any{"id": publicID, "status": status})
}

func (s *Server) adminSiteChangelogList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `select changelog.public_id,changelog.change_date,changelog.status,
		coalesce(jsonb_object_agg(translation.locale,jsonb_build_object('title',translation.title,'bodyMarkdown',translation.body_markdown)) filter(where translation.locale is not null),'{}'::jsonb),changelog.updated_at
		from site_changelogs changelog left join site_changelog_translations translation on translation.changelog_id=changelog.id
		group by changelog.id order by changelog.change_date desc,changelog.id desc limit 100`)
	if err != nil {
		writeError(w, 500, "读取更新记录失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, date, status string
		var translations []byte
		var updated time.Time
		if err = rows.Scan(&id, &date, &status, &translations, &updated); err != nil {
			writeError(w, 500, "解析更新记录失败")
			return
		}
		items = append(items, map[string]any{"id": id, "changeDate": date, "status": status, "translations": json.RawMessage(translations), "updatedAt": updated})
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) adminSiteChangelogDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if r.Method == http.MethodGet {
		var changeDate, status string
		var translations []byte
		err := s.db.QueryRow(r.Context(), `select changelog.change_date,changelog.status,
			coalesce(jsonb_object_agg(translation.locale,jsonb_build_object('title',translation.title,'bodyMarkdown',translation.body_markdown)) filter(where translation.locale is not null),'{}'::jsonb)
			from site_changelogs changelog left join site_changelog_translations translation on translation.changelog_id=changelog.id
			where changelog.public_id=$1 group by changelog.id`, id).Scan(&changeDate, &status, &translations)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "更新记录不存在")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "读取更新记录失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "changeDate": changeDate, "status": status, "translations": json.RawMessage(translations)})
		return
	}
	var request saveSiteChangelogRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "更新记录格式不正确")
		return
	}
	date, err := time.Parse("2006-01-02", request.ChangeDate)
	request.Locale = normalizedSiteAffairsLocale(request.Locale)
	request.Title = strings.TrimSpace(request.Title)
	request.BodyMarkdown = strings.TrimSpace(request.BodyMarkdown)
	if err != nil || request.Title == "" || request.BodyMarkdown == "" || len(request.BodyMarkdown) > 200000 {
		writeError(w, http.StatusBadRequest, "日期、标题或正文不正确")
		return
	}
	status := "draft"
	if request.Publish {
		status = "published"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建更新记录事务失败")
		return
	}
	defer tx.Rollback(r.Context())
	var internalID int64
	err = tx.QueryRow(r.Context(), `update site_changelogs set change_date=$2,status=$3,updated_at=now() where public_id=$1 returning id`, id, date, status).Scan(&internalID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "更新记录不存在")
		return
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,updated_by)
			values($1,$2,$3,$4,$5) on conflict(changelog_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown,updated_by=excluded.updated_by,updated_at=now()`,
			internalID, request.Locale, request.Title, request.BodyMarkdown, currentClaims(r).Subject)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存更新记录失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "site_affairs.changelog.update", id, currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{"locale": request.Locale, "published": request.Publish})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status})
}
