package httpapi

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const markdownConfigSettingKey = "markdown.rendering"

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
		PlantUML:          true,
		CodeHighlight:     true,
		EnhancedTables:    true,
		CollapsibleBlocks: true,
		AlertBlocks:       true,
		TOC:               true,
		TabSize:           2,
		PlantUMLServer:    "https://www.plantuml.com/plantuml",
		TOCMinDepth:       2,
		TOCMaxDepth:       3,
	}
}

func (s *Server) markdownConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.markdownConfigFromSettings(r.Context()))
}

func (s *Server) updateMarkdownConfig(w http.ResponseWriter, r *http.Request) {
	var payload markdownConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	payload = normalizeMarkdownConfig(payload)
	raw, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Markdown 配置格式不正确")
		return
	}
	claims := currentClaims(r)
	_, err = s.db.Exec(
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
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_markdown_config", "markdown.rendering", claims.Subject, r, http.StatusOK, 0, nil)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) markdownConfigFromSettings(ctx context.Context) markdownConfigPayload {
	payload := defaultMarkdownConfig()
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = $1`, markdownConfigSettingKey).Scan(&raw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return payload
		}
		return payload
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return defaultMarkdownConfig()
	}
	return normalizeMarkdownConfig(payload)
}

func normalizeMarkdownConfig(payload markdownConfigPayload) markdownConfigPayload {
	if payload.TabSize < 1 {
		payload.TabSize = 1
	}
	if payload.TabSize > 8 {
		payload.TabSize = 8
	}
	if payload.PlantUMLServer == "" {
		payload.PlantUMLServer = "https://www.plantuml.com/plantuml"
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
