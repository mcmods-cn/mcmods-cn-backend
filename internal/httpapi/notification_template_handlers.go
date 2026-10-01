package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
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
	Version      int                                      `json:"version"`
	Variables    []string                                 `json:"variables"`
	Translations map[string]localizedNotificationTemplate `json:"translations"`
}

type notificationTemplateConfig struct {
	Templates []notificationTemplateDefinition `json:"templates"`
}

type renderedNotificationTemplate struct {
	Key     string
	Version int
	Locale  string
	Title   string
	Body    string
	Values  map[string]string
}

var notificationTemplateVariablePattern = regexp.MustCompile(`\{([a-z][a-z0-9_]*)\}`)

type reviewConfig struct {
	BlueprintCreate         bool `json:"blueprintCreate"`
	BlueprintEdit           bool `json:"blueprintEdit"`
	ServerCreate            bool `json:"serverCreate"`
	ModCreate               bool `json:"modCreate"`
	ModEdit                 bool `json:"modEdit"`
	ModpackCreate           bool `json:"modpackCreate"`
	ModpackEdit             bool `json:"modpackEdit"`
	PluginCreate            bool `json:"pluginCreate"`
	PluginEdit              bool `json:"pluginEdit"`
	MapCreate               bool `json:"mapCreate"`
	MapEdit                 bool `json:"mapEdit"`
	ResourcePackCreate      bool `json:"resourcePackCreate"`
	ResourcePackEdit        bool `json:"resourcePackEdit"`
	ShaderPackCreate        bool `json:"shaderPackCreate"`
	ShaderPackEdit          bool `json:"shaderPackEdit"`
	DatapackCreate          bool `json:"datapackCreate"`
	DatapackEdit            bool `json:"datapackEdit"`
	AddonCreate             bool `json:"addonCreate"`
	AddonEdit               bool `json:"addonEdit"`
	AuthorCreate            bool `json:"authorCreate"`
	AuthorEdit              bool `json:"authorEdit"`
	TeamCreate              bool `json:"teamCreate"`
	TeamEdit                bool `json:"teamEdit"`
	CatalogCreate           bool `json:"catalogCreate"`
	CatalogEdit             bool `json:"catalogEdit"`
	CatalogDelete           bool `json:"catalogDelete"`
	ModContentSectionCreate bool `json:"modContentSectionCreate"`
	AITranslation           bool `json:"aiTranslation"`
	TutorialCreate          bool `json:"tutorialCreate"`
	TutorialEdit            bool `json:"tutorialEdit"`
	IssueCreate             bool `json:"issueCreate"`
	IssueEdit               bool `json:"issueEdit"`
	NewsCreate              bool `json:"newsCreate"`
	NewsEdit                bool `json:"newsEdit"`
	DiscussionCreate        bool `json:"discussionCreate"`
	DiscussionEdit          bool `json:"discussionEdit"`
	ChangelogCreate         bool `json:"changelogCreate"`
	ChangelogEdit           bool `json:"changelogEdit"`
}

func defaultNotificationTemplateConfig() notificationTemplateConfig {
	definitions := []struct {
		code      string
		variables []string
		zhTitle   string
		zhBody    string
		enTitle   string
		enBody    string
	}{
		{"mod_import_success", []string{"name"}, "模组资料导入完成", "{name} 的资料已经导入完成。", "Mod import completed", "The data for {name} has been imported."},
		{"mod_import_partial", []string{"name", "skipped"}, "模组资料部分导入完成", "{name} 的资料已经导入，但存在警告；共跳过 {skipped} 条非字符串翻译。", "Mod import partially completed", "The data for {name} was imported with warnings; {skipped} non-string translation values were skipped."},
		{"mod_import_failure", []string{"name", "error"}, "模组资料导入失败", "{name} 的资料导入失败：{error}", "Mod import failed", "The data import for {name} failed: {error}"},
		{"blueprint_conversion_success", []string{"name"}, "蓝图处理完成", "{name} 已处理完成，可以在蓝图库中查看。", "Blueprint processing completed", "{name} is ready in the blueprint library."},
		{"blueprint_conversion_failure", []string{"name", "error"}, "蓝图处理失败", "{name} 处理失败：{error}", "Blueprint processing failed", "{name} could not be processed: {error}"},
		{"blueprint_format_success", []string{"name", "format"}, "蓝图格式转换完成", "{name} 已转换为 {format} 格式。", "Blueprint format converted", "{name} has been converted to {format}."},
		{"review_approved", []string{"name"}, "内容审核通过", "您提交的 {name} 已通过审核。", "Content approved", "Your submission for {name} was approved."},
		{"review_rejected", []string{"name", "reason"}, "内容审核未通过", "您提交的 {name} 未通过审核。原因：{reason}", "Content rejected", "Your submission for {name} was rejected. Reason: {reason}"},
		{"question_answer_accepted", []string{"name"}, "回答已被采纳", "您在问题「{name}」下的回答已被采纳，悬赏已按转账税率结算。", "Answer accepted", "Your answer to {name} was accepted and its bounty was settled after transfer tax."},
		{"creator_claim_approved", []string{"name"}, "个人作者认领已通过", "您对个人作者 {name} 的认领已通过审核。", "Personal author claim approved", "Your claim for the personal author {name} was approved."},
		{"creator_claim_rejected", []string{"name", "reason"}, "个人作者认领未通过", "您对个人作者 {name} 的认领未通过审核。原因：{reason}", "Personal author claim rejected", "Your claim for the personal author {name} was rejected. Reason: {reason}"},
		{"project_updated", []string{"project_name", "changed_sections"}, "关注的项目有新更新", "{project_name} 更新了{changed_sections}。", "A followed project was updated", "{project_name} updated {changed_sections}."},
		{"modpack_export_completed", []string{"pack_name", "minecraft_version", "loader", "exported", "dependencies", "skipped"}, "收藏夹整合包导出完成", "{pack_name} 已导出完成。Minecraft {minecraft_version} / {loader}；成功 {exported} 个，自动依赖 {dependencies} 个，未导出 {skipped} 个。", "Collection modpack export completed", "{pack_name} is ready for Minecraft {minecraft_version} / {loader}: {exported} exported, {dependencies} dependencies, {skipped} skipped."},
		{"modpack_export_completed_with_skips", []string{"pack_name", "minecraft_version", "loader", "exported", "dependencies", "skipped"}, "收藏夹整合包导出完成（存在未导出项目）", "{pack_name} 已导出完成。Minecraft {minecraft_version} / {loader}；成功 {exported} 个，自动依赖 {dependencies} 个，未导出 {skipped} 个。请查看完整报告。", "Collection modpack export completed with skipped items", "{pack_name} is ready for Minecraft {minecraft_version} / {loader}: {exported} exported, {dependencies} dependencies, {skipped} skipped. Review the complete report."},
		{"modpack_export_failed", []string{"pack_name", "stage", "reason"}, "收藏夹整合包导出失败", "{pack_name} 在 {stage} 阶段导出失败：{reason}", "Collection modpack export failed", "{pack_name} failed during {stage}: {reason}"},
		{"modpack_export_expired", []string{"pack_name"}, "收藏夹整合包下载已过期", "{pack_name} 的临时下载文件已过期，导出报告仍可查看。", "Collection modpack download expired", "The temporary download for {pack_name} expired. The export report remains available."},
	}
	result := notificationTemplateConfig{Templates: make([]notificationTemplateDefinition, 0, len(definitions))}
	for _, value := range definitions {
		result.Templates = append(result.Templates, notificationTemplateDefinition{
			Code: value.code, Version: 1, Variables: value.variables,
			Translations: defaultNotificationTranslations(value.zhTitle, value.zhBody, value.enTitle, value.enBody),
		})
	}
	return result
}

// Only genuinely authored default languages are stored. Other UI languages
// use the normal fallback chain and report the language actually rendered;
// English under a French/Arabic/etc. key is not a French/Arabic translation.
func defaultNotificationTranslations(zhTitle, zhBody, enTitle, enBody string) map[string]localizedNotificationTemplate {
	return map[string]localizedNotificationTemplate{
		"zh-CN": {Title: zhTitle, Body: zhBody},
		"en-US": {Title: enTitle, Body: enBody},
	}
}

func mergeNotificationTemplates(config notificationTemplateConfig) notificationTemplateConfig {
	defaults := defaultNotificationTemplateConfig()
	byCode := make(map[string]notificationTemplateDefinition, len(defaults.Templates)+len(config.Templates))
	order := make([]string, 0, len(defaults.Templates)+len(config.Templates))
	for _, item := range defaults.Templates {
		byCode[item.Code] = item
		order = append(order, item.Code)
	}
	for _, item := range config.Templates {
		item.Code = strings.TrimSpace(item.Code)
		if item.Code == "" {
			continue
		}
		base, exists := byCode[item.Code]
		if !exists {
			order = append(order, item.Code)
			base = notificationTemplateDefinition{Code: item.Code, Version: 1}
		}
		if item.Version <= 0 {
			item.Version = base.Version
		}
		if len(item.Variables) == 0 {
			item.Variables = append([]string(nil), base.Variables...)
		}
		if item.Translations == nil {
			item.Translations = map[string]localizedNotificationTemplate{}
		}
		for locale, value := range base.Translations {
			if current, ok := item.Translations[locale]; !ok || strings.TrimSpace(current.Title) == "" || strings.TrimSpace(current.Body) == "" {
				item.Translations[locale] = value
			}
		}
		byCode[item.Code] = item
	}
	result := notificationTemplateConfig{Templates: make([]notificationTemplateDefinition, 0, len(order))}
	for _, code := range order {
		result.Templates = append(result.Templates, byCode[code])
	}
	return result
}

func loadNotificationTemplateConfig(ctx context.Context, db revisionQuery) notificationTemplateConfig {
	config, err := loadNotificationTemplateConfigChecked(ctx, db)
	if err != nil {
		return mergeNotificationTemplates(notificationTemplateConfig{})
	}
	return config
}

func loadNotificationTemplateConfigChecked(ctx context.Context, db revisionQuery) (notificationTemplateConfig, error) {
	var raw []byte
	var config notificationTemplateConfig
	err := db.QueryRow(ctx, `select value from system_settings where key=$1`, notificationTemplatesSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return mergeNotificationTemplates(config), nil
	}
	if err != nil {
		return notificationTemplateConfig{}, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return notificationTemplateConfig{}, errors.New("notification templates must be an object")
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return notificationTemplateConfig{}, err
	}
	return mergeNotificationTemplates(config), nil
}

func renderNotificationTemplate(ctx context.Context, db *pgxpool.Pool, userID int64, code string, values map[string]string) (renderedNotificationTemplate, error) {
	requestedLocale := "zh-CN"
	_ = db.QueryRow(ctx, `select preferred_ui_language from users where id=$1`, userID).Scan(&requestedLocale)
	return renderNotificationTemplateForLocale(loadNotificationTemplateConfig(ctx, db), requestedLocale, code, values)
}

func renderNotificationTemplateForLocale(config notificationTemplateConfig, requestedLocale, code string, values map[string]string) (renderedNotificationTemplate, error) {
	requestedLocale = normalizeContentLocale(requestedLocale)
	if requestedLocale == "" {
		requestedLocale = "zh-CN"
	}
	var definition notificationTemplateDefinition
	for _, item := range config.Templates {
		if item.Code == code {
			definition = item
			break
		}
	}
	if definition.Code == "" {
		return renderedNotificationTemplate{}, fmt.Errorf("notification template %q not found", code)
	}
	selectedLocale := requestedLocale
	selected := definition.Translations[selectedLocale]
	for _, fallback := range []string{"zh-CN", "en-US"} {
		if strings.TrimSpace(selected.Title) != "" && strings.TrimSpace(selected.Body) != "" {
			break
		}
		selectedLocale = fallback
		selected = definition.Translations[fallback]
	}
	if strings.TrimSpace(selected.Title) == "" || strings.TrimSpace(selected.Body) == "" {
		return renderedNotificationTemplate{}, fmt.Errorf("notification template %q has no usable translation", code)
	}
	allowed := make(map[string]bool, len(definition.Variables))
	for _, variable := range definition.Variables {
		allowed[variable] = true
	}
	valuesCopy := make(map[string]string, len(definition.Variables))
	for _, variable := range unresolvedNotificationVariables(selected.Title + "\n" + selected.Body) {
		if !allowed[variable] {
			return renderedNotificationTemplate{}, fmt.Errorf("notification template %q uses an undeclared variable", code)
		}
		value, exists := values[variable]
		if !exists {
			return renderedNotificationTemplate{}, fmt.Errorf("notification template %q missing value: %s", code, variable)
		}
		valuesCopy[variable] = value
	}
	// Replace only tokens from the template, exactly once. User values are
	// literal content: braces in a project name must not become instructions
	// or be interpreted as another variable depending on map iteration order.
	replace := func(token string) string { return valuesCopy[token[1:len(token)-1]] }
	selected.Title = notificationTemplateVariablePattern.ReplaceAllStringFunc(selected.Title, replace)
	selected.Body = notificationTemplateVariablePattern.ReplaceAllStringFunc(selected.Body, replace)
	return renderedNotificationTemplate{Key: code, Version: max(1, definition.Version), Locale: selectedLocale, Title: selected.Title, Body: selected.Body, Values: valuesCopy}, nil
}

func unresolvedNotificationVariables(value string) []string {
	found := notificationTemplateVariablePattern.FindAllStringSubmatch(value, -1)
	set := make(map[string]struct{}, len(found))
	for _, match := range found {
		set[match[1]] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for key := range set {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func (s *Server) sendTemplatedNotification(ctx context.Context, userID int64, code string, values map[string]string, data map[string]any) {
	if userID <= 0 {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	if (s.queue != nil || s.cfg.NATS.OutboxEnabled) && s.enqueueNotificationTask(ctx, notificationEvent{
		Action: "direct", RecipientID: userID, Kind: "system", TemplateKey: code, TemplateValues: values, Data: data,
	}) == nil {
		return
	}
	rendered, err := renderNotificationTemplate(ctx, s.db, userID, code, values)
	if err != nil {
		return
	}
	raw, _ := json.Marshal(data)
	params, _ := json.Marshal(rendered.Values)
	_, _ = s.db.Exec(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,data,template_key,template_version,template_params)
		values($1,'system',$2,$3,$4,$5::jsonb,$6,$7,$8::jsonb)`, userID, rendered.Title, rendered.Body, rendered.Locale, string(raw), rendered.Key, rendered.Version, string(params))
}

func (s *Server) getNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	config, err := loadNotificationTemplateConfigChecked(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取通知模板失败")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) updateNotificationTemplates(w http.ResponseWriter, r *http.Request) {
	var request notificationTemplateConfig
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	current, err := loadNotificationTemplateConfigChecked(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取通知模板失败")
		return
	}
	request = versionNotificationTemplateChanges(current, mergeNotificationTemplates(request))
	if err := validateNotificationTemplateConfig(request); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	raw, _ := json.Marshal(request)
	_, err = s.db.Exec(r.Context(), `insert into system_settings(key,value,updated_by,updated_at)
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

func versionNotificationTemplateChanges(current, next notificationTemplateConfig) notificationTemplateConfig {
	versions := make(map[string]notificationTemplateDefinition, len(current.Templates))
	for _, item := range current.Templates {
		versions[item.Code] = item
	}
	for index := range next.Templates {
		previous, exists := versions[next.Templates[index].Code]
		if !exists {
			next.Templates[index].Version = 1
			continue
		}
		if reflect.DeepEqual(previous.Variables, next.Templates[index].Variables) && reflect.DeepEqual(previous.Translations, next.Templates[index].Translations) {
			next.Templates[index].Version = max(1, previous.Version)
		} else {
			next.Templates[index].Version = max(1, previous.Version) + 1
		}
	}
	return next
}

func validateNotificationTemplateConfig(config notificationTemplateConfig) error {
	seen := map[string]struct{}{}
	locales := supportedContentLocaleList()
	for _, item := range config.Templates {
		if item.Code == "" || len(item.Code) > 80 {
			return errors.New("通知模板 Key 不正确")
		}
		if _, ok := seen[item.Code]; ok {
			return fmt.Errorf("通知模板 Key 重复：%s", item.Code)
		}
		seen[item.Code] = struct{}{}
		allowed := map[string]struct{}{}
		for _, variable := range item.Variables {
			allowed[variable] = struct{}{}
		}
		for locale := range item.Translations {
			if !slices.Contains(locales, locale) {
				return fmt.Errorf("模板 %s 语言 %s 未受支持", item.Code, locale)
			}
		}
		for _, locale := range locales {
			value, ok := item.Translations[locale]
			if !ok && locale != "zh-CN" && locale != "en-US" {
				continue
			}
			if !ok || strings.TrimSpace(value.Title) == "" || strings.TrimSpace(value.Body) == "" {
				return fmt.Errorf("模板 %s 缺少 %s 文案", item.Code, locale)
			}
			for _, variable := range unresolvedNotificationVariables(value.Title + "\n" + value.Body) {
				if _, ok := allowed[variable]; !ok {
					return fmt.Errorf("模板 %s 使用了未声明变量 %s", item.Code, variable)
				}
			}
		}
	}
	return nil
}

func (s *Server) getReviewConfig(w http.ResponseWriter, r *http.Request) {
	config, err := loadReviewConfigChecked(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "读取审核设置失败")
		return
	}
	writeJSON(w, http.StatusOK, config)
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

func loadReviewConfig(ctx context.Context, db revisionQuery) reviewConfig {
	config, err := loadReviewConfigChecked(ctx, db)
	if err == nil {
		return config
	}
	// An unavailable or malformed configuration must not disable moderation.
	// Missing settings retain the documented bootstrap defaults below.
	config = defaultReviewConfig()
	config.BlueprintCreate, config.BlueprintEdit = true, true
	config.ModContentSectionCreate, config.AITranslation = true, true
	return config
}

func loadReviewConfigChecked(ctx context.Context, db revisionQuery) (reviewConfig, error) {
	var raw []byte
	config := defaultReviewConfig()
	err := db.QueryRow(ctx, `select value from system_settings where key=$1`, reviewConfigSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return reviewConfig{}, err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return reviewConfig{}, errors.New("review configuration must be an object")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return reviewConfig{}, err
	}
	configType := reflect.TypeOf(config)
	for index := 0; index < configType.NumField(); index++ {
		if value, exists := fields[configType.Field(index).Tag.Get("json")]; exists && strings.TrimSpace(string(value)) == "null" {
			return reviewConfig{}, errors.New("review switches cannot be null")
		}
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		return reviewConfig{}, err
	}
	return config, nil
}

func defaultReviewConfig() reviewConfig {
	return reviewConfig{
		ServerCreate:            true,
		ModCreate:               true,
		ModEdit:                 true,
		ModpackCreate:           true,
		ModpackEdit:             true,
		PluginCreate:            true,
		PluginEdit:              true,
		MapCreate:               true,
		MapEdit:                 true,
		ResourcePackCreate:      true,
		ResourcePackEdit:        true,
		ShaderPackCreate:        true,
		ShaderPackEdit:          true,
		DatapackCreate:          true,
		DatapackEdit:            true,
		AddonCreate:             true,
		AddonEdit:               true,
		AuthorCreate:            true,
		AuthorEdit:              true,
		TeamCreate:              true,
		TeamEdit:                true,
		CatalogCreate:           true,
		CatalogEdit:             true,
		CatalogDelete:           true,
		ModContentSectionCreate: false,
		AITranslation:           false,
		TutorialCreate:          true,
		TutorialEdit:            true,
		IssueCreate:             true,
		IssueEdit:               true,
		NewsCreate:              true,
		NewsEdit:                true,
		DiscussionCreate:        true,
		DiscussionEdit:          true,
		ChangelogCreate:         true,
		ChangelogEdit:           true,
	}
}

func simpleProjectReviewRequired(config reviewConfig, projectType, action string) bool {
	switch normalizeSimpleProjectType(projectType) + ":" + strings.ToLower(strings.TrimSpace(action)) {
	case "plugin:create":
		return config.PluginCreate
	case "plugin:edit":
		return config.PluginEdit
	case "map:create":
		return config.MapCreate
	case "map:edit":
		return config.MapEdit
	case "resource_pack:create":
		return config.ResourcePackCreate
	case "resource_pack:edit":
		return config.ResourcePackEdit
	case "shader_pack:create":
		return config.ShaderPackCreate
	case "shader_pack:edit":
		return config.ShaderPackEdit
	case "datapack:create":
		return config.DatapackCreate
	case "datapack:edit":
		return config.DatapackEdit
	case "addon:create":
		return config.AddonCreate
	case "addon:edit":
		return config.AddonEdit
	default:
		return true
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
		}
	default:
		switch action {
		case "create":
			return config.AuthorCreate
		case "edit":
			return config.AuthorEdit
		}
	}
	return true
}
