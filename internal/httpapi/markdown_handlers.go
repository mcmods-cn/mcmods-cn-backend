package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

const markdownConfigSettingKey = "markdown.rendering"

const plantUMLProxyPath = "/plantuml"

type markdownConfigPayload struct {
	Core              bool   `json:"core"`
	Abbreviations     bool   `json:"abbreviations"`
	Emoji             bool   `json:"emoji"`
	Footnotes         bool   `json:"footnotes"`
	Subscript         bool   `json:"subscript"`
	Superscript       bool   `json:"superscript"`
	TaskLists         bool   `json:"taskLists"`
	Katex             bool   `json:"katex"`
	ExpandTabs        bool   `json:"expandTabs"`
	ImageSize         bool   `json:"imageSize"`
	PlantUML          bool   `json:"plantUML"`
	CodeHighlight     bool   `json:"codeHighlight"`
	EnhancedTables    bool   `json:"enhancedTables"`
	CollapsibleBlocks bool   `json:"collapsibleBlocks"`
	AlertBlocks       bool   `json:"alertBlocks"`
	TOC               bool   `json:"toc"`
	TabSize           int    `json:"tabSize"`
	PlantUMLServer    string `json:"plantUMLServer"`
	TOCMinDepth       int    `json:"tocMinDepth"`
	TOCMaxDepth       int    `json:"tocMaxDepth"`
}

func defaultMarkdownConfig() markdownConfigPayload {
	return markdownConfigPayload{
		Core:              true,
		Abbreviations:     true,
		Emoji:             true,
		Footnotes:         true,
		Subscript:         true,
		Superscript:       true,
		TaskLists:         true,
		Katex:             true,
		ExpandTabs:        true,
		ImageSize:         true,
		PlantUML:          false,
		CodeHighlight:     true,
		EnhancedTables:    true,
		CollapsibleBlocks: true,
		AlertBlocks:       true,
		TOC:               true,
		TabSize:           2,
		PlantUMLServer:    plantUMLProxyPath,
		TOCMinDepth:       2,
		TOCMaxDepth:       3,
	}
}

func (s *Server) markdownConfig(w http.ResponseWriter, r *http.Request) {
	payload := defaultMarkdownConfig()
	raw, err := s.loadCachedPublicSetting(r.Context(), markdownConfigSettingKey)
	if decodeOptionalSetting(raw, err, markdownConfigSettingKey, &payload) != nil {
		payload = defaultMarkdownConfig()
	}
	writeJSON(w, http.StatusOK, normalizeMarkdownConfig(payload))
}

func (s *Server) adminMarkdownConfig(w http.ResponseWriter, r *http.Request) {
	payload := defaultMarkdownConfig()
	if err := s.readEditableSetting(r.Context(), markdownConfigSettingKey, &payload); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MARKDOWN_SETTINGS_UNAVAILABLE", "Markdown settings are temporarily unavailable", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, normalizeMarkdownConfig(payload))
}

func (s *Server) updateMarkdownConfig(w http.ResponseWriter, r *http.Request) {
	var payload markdownConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	if err := validatePlantUMLProxyPath(payload.PlantUMLServer); err != nil {
		writeError(w, http.StatusBadRequest, "PlantUML 仅支持本站 /plantuml 自建代理")
		return
	}
	payload = normalizeMarkdownConfig(payload)
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Markdown 配置格式不正确")
		return
	}
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "Markdown settings are temporarily unavailable")
		return
	}
	defer tx.Rollback(r.Context())
	current := defaultMarkdownConfig()
	if err = checkEditableSettingTx(r.Context(), tx, markdownConfigSettingKey, &current); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "MARKDOWN_SETTINGS_UNAVAILABLE", "Markdown settings are temporarily unavailable", 0, nil)
		return
	}
	_, err = tx.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ($1, $2::jsonb, $3, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		markdownConfigSettingKey,
		string(raw),
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Markdown 配置失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Markdown 配置失败")
		return
	}
	s.invalidateSettingsCache(r.Context())
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_markdown_config", "markdown.rendering", claims.Subject, r, http.StatusOK, 0, nil)
	writeJSON(w, http.StatusOK, payload)
}

func normalizeMarkdownConfig(payload markdownConfigPayload) markdownConfigPayload {
	if payload.TabSize < 1 {
		payload.TabSize = 1
	}
	if payload.TabSize > 8 {
		payload.TabSize = 8
	}
	plantUMLServerIsTrusted := validatePlantUMLProxyPath(payload.PlantUMLServer) == nil
	payload.PlantUMLServer = plantUMLProxyPath
	if !plantUMLServerIsTrusted {
		payload.PlantUML = false
	}
	if payload.TOCMinDepth < 1 {
		payload.TOCMinDepth = 1
	}
	if payload.TOCMinDepth > 6 {
		payload.TOCMinDepth = 6
	}
	if payload.TOCMaxDepth < payload.TOCMinDepth {
		payload.TOCMaxDepth = payload.TOCMinDepth
	}
	if payload.TOCMaxDepth > 6 {
		payload.TOCMaxDepth = 6
	}
	return payload
}

func validatePlantUMLProxyPath(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.TrimRight(trimmed, "/") == plantUMLProxyPath {
		return nil
	}
	return errors.New("PlantUML server must use the fixed same-origin /plantuml proxy")
}
