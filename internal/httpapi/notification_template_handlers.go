package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	notificationTemplatesSettingKey = "notifications.templates"
	reviewConfigSettingKey          = "review.config"
)

type localizedNotificationTemplate struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type notificationTemplateDefinition struct {
	Code         string                                   `json:"code"`
	Translations map[string]localizedNotificationTemplate `json:"translations"`
}

type notificationTemplateConfig struct {
	Templates []notificationTemplateDefinition `json:"templates"`
}

type reviewConfig struct {
	BlueprintCreate         bool `json:"blueprintCreate"`
	BlueprintEdit           bool `json:"blueprintEdit"`
	ServerCreate            bool `json:"serverCreate"`
	ModCreate               bool `json:"modCreate"`
	ModEdit                 bool `json:"modEdit"`
	AuthorCreate            bool `json:"authorCreate"`
	AuthorEdit              bool `json:"authorEdit"`
	AuthorClaim             bool `json:"authorClaim"`
	TeamCreate              bool `json:"teamCreate"`
	TeamEdit                bool `json:"teamEdit"`
	TeamClaim               bool `json:"teamClaim"`
	CatalogCreate           bool `json:"catalogCreate"`
	CatalogEdit             bool `json:"catalogEdit"`
	CatalogDelete           bool `json:"catalogDelete"`
	ModContentSectionCreate bool `json:"modContentSectionCreate"`
	AITranslation           bool `json:"aiTranslation"`
}

func defaultNotificationTemplateConfig() notificationTemplateConfig {
	definitions := []struct {
		code    string
		zhTitle string
		zhBody  string
		enTitle string
		enBody  string
	}{
		{"mod_import_success", "模组资料导入完成", "{name} 的资料已经导入完成。", "Mod import completed", "The data for {name} has been imported."},
		{"mod_import_partial", "模组资料部分导入完成", "{name} 的资料已经导入，但存在警告；共跳过 {skipped} 条非字符串翻译。", "Mod import partially completed", "The data for {name} was imported with warnings; {skipped} non-string translation values were skipped."},
		{"mod_import_failure", "模组资料导入失败", "{name} 的资料导入失败：{error}", "Mod import failed", "The data import for {name} failed: {error}"},
		{"blueprint_conversion_success", "蓝图处理完成", "{name} 已处理完成，可以在蓝图库中查看。", "Blueprint processing completed", "{name} is ready in the blueprint library."},
		{"blueprint_conversion_failure", "蓝图处理失败", "{name} 处理失败：{error}", "Blueprint processing failed", "{name} could not be processed: {error}"},
		{"blueprint_format_success", "蓝图格式转换完成", "{name} 已转换为 {format} 格式。", "Blueprint format converted", "{name} has been converted to {format}."},
		{"review_approved", "内容审核通过", "您提交的 {name} 已通过审核。", "Content approved", "Your submission for {name} was approved."},
		{"review_rejected", "内容审核未通过", "您提交的 {name} 未通过审核。原因：{reason}", "Content rejected", "Your submission for {name} was rejected. Reason: {reason}"},
		{"creator_claim_approved", "作者或团队认领已通过", "您对 {name} 的认领已通过审核。", "Creator claim approved", "Your claim for {name} was approved."},
		{"creator_claim_rejected", "作者或团队认领未通过", "您对 {name} 的认领未通过审核。原因：{reason}", "Creator claim rejected", "Your claim for {name} was rejected. Reason: {reason}"},
	}
	result := notificationTemplateConfig{Templates: make([]notificationTemplateDefinition, 0, len(definitions))}
	for _, value := range definitions {
		result.Templates = append(result.Templates, notificationTemplateDefinition{
			Code: value.code,
			Translations: map[string]localizedNotificationTemplate{
				"zh-CN": {Title: value.zhTitle, Body: value.zhBody},
				"en-US": {Title: value.enTitle, Body: value.enBody},
			},
		})
	}
	return result
}

func mergeNotificationTemplates(config notificationTemplateConfig) notificationTemplateConfig {
	defaults := defaultNotificationTemplateConfig()
	byCode := make(map[string]notificationTemplateDefinition, len(config.Templates))
	for _, item := range config.Templates {
		if strings.TrimSpace(item.Code) != "" {
			byCode[item.Code] = item
		}
	}
	for index, item := range defaults.Templates {
		custom, ok := byCode[item.Code]
		if !ok {
			continue
		}
		if custom.Translations == nil {
			custom.Translations = map[string]localizedNotificationTemplate{}
		}
		for locale, value := range item.Translations {
			if _, exists := custom.Translations[locale]; !exists {
				custom.Translations[locale] = value
			}
		}
		defaults.Templates[index] = custom
	}
	return defaults
}

func loadNotificationTemplateConfig(ctx context.Context, db *pgxpool.Pool) notificationTemplateConfig {
	var raw []byte
	var config notificationTemplateConfig
	if err := db.QueryRow(ctx, `select value from system_settings where key=$1`, notificationTemplatesSettingKey).Scan(&raw); err == nil {
		_ = json.Unmarshal(raw, &config)
	}
	return mergeNotificationTemplates(config)
}

func renderNotificationTemplate(ctx context.Context, db *pgxpool.Pool, userID int64, code string, values map[string]string) (string, string, string) {
	locale := "zh-CN"
	_ = db.QueryRow(ctx, `select preferred_ui_language from users where id=$1`, userID).Scan(&locale)
	config := loadNotificationTemplateConfig(ctx, db)
	var selected localizedNotificationTemplate
	for _, item := range config.Templates {
		if item.Code != code {
			continue
		}
		selected = item.Translations[locale]
		if selected.Title == "" && selected.Body == "" {
			selected = item.Translations[languageBase(locale)]
		}
		if selected.Title == "" && selected.Body == "" {
			selected = item.Translations["zh-CN"]
		}
		if selected.Title == "" && selected.Body == "" {
			selected = item.Translations["en-US"]
		}
		break
	}
	for key, value := range values {
		selected.Title = strings.ReplaceAll(selected.Title, "{"+key+"}", value)
		selected.Body = strings.ReplaceAll(selected.Body, "{"+key+"}", value)
	}
	return selected.Title, selected.Body, locale
}

func (s *Server) sendTemplatedNotification(ctx context.Context, userID int64, code string, values map[string]string, data map[string]any) {
	if userID <= 0 {
		return
	}
	title, body, locale := renderNotificationTemplate(ctx, s.db, userID, code, values)
	if data == nil {
		data = map[string]any{}
	}
	if s.queue != nil && s.queue.PublishTask(ctx, notificationTaskCode, notificationEvent{
		Action: "direct", RecipientID: userID, Kind: "system", Title: title, Body: body, SourceLocale: locale, Data: data,
	}) == nil {
		return
	}
	raw, _ := json.Marshal(data)
	_, _ = s.db.Exec(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,data)
		values($1,'system',$2,$3,$4,$5::jsonb)`, userID, title, body, locale, string(raw))
}

func languageBase(locale string) string {
	if index := strings.IndexAny(locale, "-_"); index > 0 {
		return locale[:index]
	}
	return locale
}

func (s *Server) getNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, loadNotificationTemplateConfig(r.Context(), s.db))
}

func (s *Server) updateNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	var request notificationTemplateConfig
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request = mergeNotificationTemplates(request)
	raw, _ := json.Marshal(request)
	_, err := s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now())
		on conflict(key) do update
		set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		notificationTemplatesSettingKey, string(raw), currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存通知模板失败")
		return
	}
	writeJSON(w, http.StatusOK, request)
}

func (s *Server) getReviewConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, loadReviewConfig(r.Context(), s.db))
}

func (s *Server) updateReviewConfig(w http.ResponseWriter, r *http.Request) {
	var request reviewConfig
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	raw, _ := json.Marshal(request)
	_, err := s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
		values($1,$2::jsonb,$3,now())
		on conflict(key) do update
		set value=excluded.value,updated_by=excluded.updated_by,updated_at=now()`,
		reviewConfigSettingKey, string(raw), currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存审核设置失败")
		return
	}
	writeJSON(w, http.StatusOK, request)
}

func loadReviewConfig(ctx context.Context, db *pgxpool.Pool) reviewConfig {
	var raw []byte
	config := defaultReviewConfig()
	err := db.QueryRow(ctx, `select value from system_settings where key=$1`, reviewConfigSettingKey).Scan(&raw)
	if err == nil {
		_ = json.Unmarshal(raw, &config)
	}
	return config
}

func defaultReviewConfig() reviewConfig {
	return reviewConfig{
		ServerCreate:            true,
		ModCreate:               true,
		ModEdit:                 true,
		AuthorCreate:            true,
		AuthorEdit:              true,
		AuthorClaim:             true,
		TeamCreate:              true,
		TeamEdit:                true,
		TeamClaim:               true,
		CatalogCreate:           true,
		CatalogEdit:             true,
		CatalogDelete:           true,
		ModContentSectionCreate: false,
		AITranslation:           false,
	}
}

func creatorReviewRequired(config reviewConfig, kind, action string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "team":
		switch action {
		case "create":
			return config.TeamCreate
		case "edit":
			return config.TeamEdit
		case "claim":
			return config.TeamClaim
		}
	default:
		switch action {
		case "create":
			return config.AuthorCreate
		case "edit":
			return config.AuthorEdit
		case "claim":
			return config.AuthorClaim
		}
	}
	return true
}
