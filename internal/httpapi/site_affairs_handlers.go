package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func logSiteChangelogReadFailure(stage, identity string, err error) {
	slog.Error("site changelog read failed",
		"module", "site_affairs",
		"stage", stage,
		"identity", identity,
		"error", err,
	)
}

func normalizedSiteAffairsLocale(value string) string {
	value = normalizeContentLocale(value)
	if isEditableContentLocale(value) {
		return value
	}
	return "zh-CN"
}

func normalizedSiteAffairsAdminLocale(value string) (string, bool) {
	value = normalizeContentLocale(value)
	return value, isEditableContentLocale(value)
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
	BaseRevision *int64 `json:"baseRevision"`
}

func (s *Server) adminAboutPage(w http.ResponseWriter, r *http.Request) {
	locale, validLocale := normalizedSiteAffairsAdminLocale(r.PathValue("locale"))
	if !validLocale {
		writeError(w, http.StatusBadRequest, "站务页面语言不正确")
		return
	}
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
	if request.BaseRevision == nil || *request.BaseRevision < 0 {
		writeError(w, http.StatusBadRequest, "站务页面版本不正确")
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
	err = tx.QueryRow(r.Context(), `insert into site_pages(code,status,updated_by,published_revision) values('about','published',$1,case when $2::boolean then 1 else 0 end)
		on conflict(code) do update set status='published',updated_by=excluded.updated_by,updated_at=now(),published_revision=site_pages.published_revision+case when $2::boolean then 1 else 0 end returning id`, currentClaims(r).Subject, request.Publish).Scan(&pageID)
	var revision int64
	if err == nil && *request.BaseRevision == 0 {
		err = tx.QueryRow(r.Context(), `insert into site_page_translations(page_id,locale,title,body_markdown,status,revision,updated_by)
			values($1,$2,$3,$4,$5,1,$6) on conflict(page_id,locale) do nothing returning revision`,
			pageID, locale, request.Title, request.BodyMarkdown, status, currentClaims(r).Subject).Scan(&revision)
	} else if err == nil {
		err = tx.QueryRow(r.Context(), `update site_page_translations
			set title=$3,body_markdown=$4,status=$5,revision=revision+1,updated_by=$6,
				updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond')
			where page_id=$1 and locale=$2 and revision=$7 returning revision`,
			pageID, locale, request.Title, request.BodyMarkdown, status, currentClaims(r).Subject, *request.BaseRevision).Scan(&revision)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		var currentTitle, currentBody, currentStatus string
		var currentRevision int64
		currentErr := tx.QueryRow(r.Context(), `select title,body_markdown,status,revision
			from site_page_translations where page_id=$1 and locale=$2`, pageID, locale).
			Scan(&currentTitle, &currentBody, &currentStatus, &currentRevision)
		if errors.Is(currentErr, pgx.ErrNoRows) {
			currentTitle, currentBody, currentStatus, currentRevision = "", "", "draft", 0
		} else if currentErr != nil {
			writeError(w, http.StatusInternalServerError, "读取站务页面当前版本失败")
			return
		}
		if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "回滚站务页面事务失败")
			return
		}
		writeAPIError(w, http.StatusConflict, "SITE_PAGE_EDIT_CONFLICT", "站务页面已被其他管理员更新", 0, map[string]any{
			"locale": locale, "title": currentTitle, "bodyMarkdown": currentBody, "status": currentStatus, "revision": currentRevision,
		})
		return
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, 500, "保存站务页面失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "site_affairs.about.update", locale, currentClaims(r).Subject, r, 200, 0, map[string]any{
		"published": request.Publish, "baseRevision": *request.BaseRevision, "revision": revision,
	})
	writeJSON(w, http.StatusOK, map[string]any{"locale": locale, "status": status, "revision": revision})
}

func (s *Server) publicSiteChangelogs(w http.ResponseWriter, r *http.Request) {
	request, err := parseSiteChangelogPageRequest(r.URL.Query(), false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryPublicSiteChangelogPage(r.Context(), request)
	if err != nil {
		logSiteChangelogReadFailure("public_page", request.Scope, err)
		writeError(w, 500, "读取站点更新记录失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) publicSiteChangelogDetail(w http.ResponseWriter, r *http.Request) {
	locale := normalizedSiteAffairsLocale(r.URL.Query().Get("locale"))
	var id, resolvedLocale, title, body string
	var changeDate time.Time
	err := s.db.QueryRow(r.Context(), `select changelog.public_id,changelog.change_date,translation.locale,translation.title,translation.body_markdown
		from site_changelogs changelog join lateral (
			select locale,title,body_markdown from site_changelog_translations where changelog_id=changelog.id
			and status='published'
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
	ChangeDate    string     `json:"changeDate"`
	Locale        string     `json:"locale"`
	Title         string     `json:"title"`
	BodyMarkdown  string     `json:"bodyMarkdown"`
	Publish       bool       `json:"publish"`
	BaseUpdatedAt *time.Time `json:"baseUpdatedAt"`
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
	locale, validLocale := normalizedSiteAffairsAdminLocale(request.Locale)
	if !validLocale {
		writeError(w, http.StatusBadRequest, "更新记录语言不正确")
		return
	}
	request.Locale = locale
	date, err := time.Parse("2006-01-02", request.ChangeDate)
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
	err = tx.QueryRow(r.Context(), `insert into site_changelogs(change_date,status,created_by) values($1,'published',$2) returning id,public_id`, date, currentClaims(r).Subject).Scan(&id, &publicID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,status,updated_by) values($1,$2,$3,$4,$5,$6)`, id, request.Locale, request.Title, request.BodyMarkdown, status, currentClaims(r).Subject)
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
	request, err := parseSiteChangelogPageRequest(r.URL.Query(), true)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := s.queryAdminSiteChangelogPage(r.Context(), request)
	if err != nil {
		logSiteChangelogReadFailure("admin_page", request.Scope, err)
		writeError(w, 500, "读取更新记录失败")
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) adminSiteChangelogDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if r.Method == http.MethodGet {
		detail, err := loadAdminSiteChangelogDetail(r.Context(), s.db, id)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "更新记录不存在")
			return
		}
		if err != nil {
			logSiteChangelogReadFailure("admin_detail", id, err)
			writeError(w, http.StatusInternalServerError, "读取更新记录失败")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "changeDate": detail.ChangeDate, "status": detail.Status, "translations": detail.Translations, "updatedAt": detail.UpdatedAt,
		})
		return
	}
	var request saveSiteChangelogRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "更新记录格式不正确")
		return
	}
	locale, validLocale := normalizedSiteAffairsAdminLocale(request.Locale)
	if !validLocale {
		writeError(w, http.StatusBadRequest, "更新记录语言不正确")
		return
	}
	request.Locale = locale
	date, err := time.Parse("2006-01-02", request.ChangeDate)
	request.Title = strings.TrimSpace(request.Title)
	request.BodyMarkdown = strings.TrimSpace(request.BodyMarkdown)
	if err != nil || request.Title == "" || request.BodyMarkdown == "" || len(request.BodyMarkdown) > 200000 {
		writeError(w, http.StatusBadRequest, "日期、标题或正文不正确")
		return
	}
	if request.BaseUpdatedAt == nil || request.BaseUpdatedAt.IsZero() {
		writeError(w, http.StatusBadRequest, "更新记录版本不正确")
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
	var updatedAt time.Time
	err = tx.QueryRow(r.Context(), `update site_changelogs
		set change_date=$2,updated_at=greatest(clock_timestamp(),updated_at+interval '1 microsecond')
		where public_id=$1 and updated_at=$3 returning id,updated_at`, id, date, *request.BaseUpdatedAt).Scan(&internalID, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		current, currentErr := loadAdminSiteChangelogDetail(r.Context(), tx, id)
		if errors.Is(currentErr, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "更新记录不存在")
			return
		}
		if currentErr != nil {
			writeError(w, http.StatusInternalServerError, "读取更新记录当前版本失败")
			return
		}
		if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil {
			writeError(w, http.StatusInternalServerError, "回滚更新记录事务失败")
			return
		}
		writeAPIError(w, http.StatusConflict, "SITE_CHANGELOG_EDIT_CONFLICT", "更新记录已被其他管理员更新", 0, map[string]any{
			"changeDate": current.ChangeDate, "status": current.Status, "translations": current.Translations, "updatedAt": current.UpdatedAt,
		})
		return
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `insert into site_changelog_translations(changelog_id,locale,title,body_markdown,status,updated_by)
			values($1,$2,$3,$4,$5,$6) on conflict(changelog_id,locale) do update set title=excluded.title,body_markdown=excluded.body_markdown,status=excluded.status,updated_by=excluded.updated_by,updated_at=now()`,
			internalID, request.Locale, request.Title, request.BodyMarkdown, status, currentClaims(r).Subject)
	}
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存更新记录失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "site_affairs.changelog.update", id, currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"locale": request.Locale, "published": request.Publish, "baseUpdatedAt": *request.BaseUpdatedAt, "updatedAt": updatedAt,
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": status, "updatedAt": updatedAt})
}
