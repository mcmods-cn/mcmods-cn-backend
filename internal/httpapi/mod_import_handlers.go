package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mcmods-cn-backend/internal/queue"
)

const modMetadataImportTaskCode = "mod_metadata_import"

type modMetadataImportRequest struct {
	Provider string `json:"provider"`
	URL      string `json:"url"`
}

type modMetadataImportMessage struct {
	JobID string `json:"jobId"`
}

type modMetadataImportJobResponse struct {
	ID          string          `json:"id"`
	ProjectType string          `json:"projectType"`
	Provider    string          `json:"provider"`
	SourceURL   string          `json:"sourceUrl"`
	Status      string          `json:"status"`
	Progress    int             `json:"progress"`
	Result      json.RawMessage `json:"result,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

func (s *Server) createModMetadataImport(w http.ResponseWriter, r *http.Request) {
	s.createProjectMetadataImport(w, r, "mod")
}

func (s *Server) createModpackMetadataImport(w http.ResponseWriter, r *http.Request) {
	s.createProjectMetadataImport(w, r, "modpack")
}

func (s *Server) createSimpleProjectMetadataImport(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	if projectType == "" {
		writeError(w, http.StatusNotFound, "project type not found")
		return
	}
	claims := currentClaims(r)
	if !claimsAllow(claims, "project.create."+projectType) && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "project creation permission is required")
		return
	}
	s.createProjectMetadataImport(w, r, projectType)
}

func (s *Server) getSimpleProjectMetadataImport(w http.ResponseWriter, r *http.Request) {
	projectType := normalizeSimpleProjectType(r.PathValue("projectType"))
	if projectType == "" {
		writeError(w, http.StatusNotFound, "project type not found")
		return
	}
	job, err := s.modMetadataImportJob(r.Context(), strings.TrimSpace(r.PathValue("jobId")), currentClaims(r).Subject)
	if err != nil || job.ProjectType != projectType {
		writeError(w, http.StatusNotFound, "project import job not found")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) createProjectMetadataImport(w http.ResponseWriter, r *http.Request, projectType string) {
	var request modMetadataImportRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	provider, sourceURL, _, err := parseProjectImportSource(projectType, request.Provider, request.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := s.modImportConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组数据源配置失败")
		return
	}
	if err = ensureModImportProviderAvailable(cfg, provider); err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	userID := currentClaims(r).Subject
	var jobID string
	tx, txErr := s.db.Begin(r.Context())
	if txErr != nil {
		writeError(w, http.StatusInternalServerError, "创建模组导入任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	if err = tx.QueryRow(r.Context(),
		`insert into mod_metadata_import_jobs(user_id,project_type,provider,source_url,status,progress)
		 values($1,$2,$3,$4,'queued',0) returning public_id`,
		userID, projectType, provider, sourceURL,
	).Scan(&jobID); err != nil {
		writeError(w, http.StatusInternalServerError, "创建模组导入任务失败")
		return
	}
	message := modMetadataImportMessage{JobID: jobID}
	if s.cfg.NATS.OutboxEnabled {
		if _, err = queue.EnqueueTx(r.Context(), tx, modMetadataImportTaskCode, "mod.metadata.import.requested", "mod_metadata_import_job", jobID, r.Header.Get("X-Request-ID"), message); err != nil {
			writeError(w, http.StatusInternalServerError, "模组导入任务可靠入队失败")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建模组导入任务失败")
		return
	}
	if !s.cfg.NATS.OutboxEnabled && (s.queue == nil || s.queue.PublishTask(r.Context(), modMetadataImportTaskCode, message) != nil) {
		_, _ = s.db.Exec(r.Context(), `update mod_metadata_import_jobs set status='failed',error='NATS queue unavailable',
			finished_at=now(),updated_at=now() where public_id=$1`, jobID)
		writeError(w, http.StatusServiceUnavailable, "NATS 模组导入任务队列不可用")
		return
	}
	s.writeAppLog(r.Context(), "user_interaction", "info", "create_mod_metadata_import", provider, userID, r, http.StatusAccepted, 0, map[string]any{"jobId": jobID})
	job, err := s.modMetadataImportJob(r.Context(), jobID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取模组导入任务失败")
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (s *Server) getModMetadataImport(w http.ResponseWriter, r *http.Request) {
	job, err := s.modMetadataImportJob(r.Context(), strings.TrimSpace(r.PathValue("jobId")), currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusNotFound, "模组导入任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) modMetadataImportJob(ctx context.Context, jobID string, userID int64) (modMetadataImportJobResponse, error) {
	var job modMetadataImportJobResponse
	var result []byte
	err := s.db.QueryRow(ctx,
		`select public_id,project_type,provider,source_url,status,progress,coalesce(result,'null'::jsonb),error,created_at,updated_at
		 from mod_metadata_import_jobs where public_id=$1 and user_id=$2`,
		jobID, userID,
	).Scan(&job.ID, &job.ProjectType, &job.Provider, &job.SourceURL, &job.Status, &job.Progress, &result, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return modMetadataImportJobResponse{}, err
	}
	if string(result) != "null" {
		job.Result = result
	}
	return job, nil
}

func ensureModImportProviderAvailable(cfg modImportConfig, provider string) error {
	switch provider {
	case "modrinth":
		if !cfg.Modrinth.Enabled {
			return errors.New("modrinth 自动导入已在后台关闭")
		}
	case "curseforge":
		if !cfg.CurseForge.Enabled {
			return errors.New("CurseForge 自动导入已在后台关闭")
		}
		if cfg.CurseForge.APIKey == "" {
			return errors.New("CurseForge API Key 尚未配置")
		}
	case "github":
		if !cfg.GitHub.Enabled {
			return errors.New("GitHub 自动导入已在后台关闭")
		}
	default:
		return errors.New("不支持的模组导入来源")
	}
	return nil
}

func parseProjectImportSource(projectType, provider, rawURL string) (string, string, string, error) {
	if _, supported := projectImportSourceSpecs[projectType]; !supported {
		return "", "", "", errors.New("不支持的项目类型")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return "", "", "", errors.New("请输入有效的 HTTPS 项目链接")
	}
	host := strings.ToLower(parsed.Hostname())
	segments := splitURLPath(parsed.Path)
	switch provider {
	case "modrinth":
		spec := projectImportSourceSpecs[projectType]
		if len(spec.ModrinthSections) == 0 {
			return "", "", "", fmt.Errorf("%s does not support Modrinth imports", projectImportDisplayName(projectType))
		}
		expectedSection := spec.ModrinthSections[0]
		if len(segments) > 0 && containsImportSection(spec.ModrinthSections, segments[0]) {
			expectedSection = segments[0]
		}
		if (host != "modrinth.com" && host != "www.modrinth.com") || len(segments) < 2 || segments[0] != expectedSection || !isSafeImportReferenceSegment(segments[1]) {
			return "", "", "", fmt.Errorf("请输入有效的 Modrinth %s项目链接", projectImportDisplayName(projectType))
		}
		return provider, "https://modrinth.com/" + expectedSection + "/" + segments[1], segments[1], nil
	case "curseforge":
		spec := projectImportSourceSpecs[projectType]
		expectedSection := spec.CurseForgeSections[0]
		if len(segments) > 1 && containsImportSection(spec.CurseForgeSections, segments[1]) {
			expectedSection = segments[1]
		}
		if (host != "curseforge.com" && host != "www.curseforge.com") || len(segments) < 3 || segments[0] != "minecraft" || segments[1] != expectedSection || !isSafeImportReferenceSegment(segments[2]) {
			return "", "", "", fmt.Errorf("请输入有效的 CurseForge Minecraft %s项目链接", projectImportDisplayName(projectType))
		}
		return provider, "https://www.curseforge.com/minecraft/" + expectedSection + "/" + segments[2], segments[2], nil
	case "github":
		if projectType != "mod" {
			return "", "", "", errors.New("GitHub 自动导入仅支持模组")
		}
		if (host != "github.com" && host != "www.github.com") || len(segments) < 2 {
			return "", "", "", errors.New("请输入 GitHub 仓库链接")
		}
		repository := strings.TrimSuffix(segments[1], ".git")
		if !isSafeImportReferenceSegment(segments[0]) || !isSafeImportReferenceSegment(repository) {
			return "", "", "", errors.New("请输入 GitHub 仓库链接")
		}
		ref := segments[0] + "/" + repository
		return provider, "https://github.com/" + ref, ref, nil
	default:
		return "", "", "", errors.New("不支持的模组导入来源")
	}
}

type projectImportSourceSpec struct {
	ModrinthSections   []string
	CurseForgeSections []string
}

var projectImportSourceSpecs = map[string]projectImportSourceSpec{
	"mod":           {ModrinthSections: []string{"mod"}, CurseForgeSections: []string{"mc-mods"}},
	"modpack":       {ModrinthSections: []string{"modpack"}, CurseForgeSections: []string{"modpacks"}},
	"plugin":        {ModrinthSections: []string{"plugin"}, CurseForgeSections: []string{"bukkit-plugins"}},
	"map":           {CurseForgeSections: []string{"worlds"}},
	"resource_pack": {ModrinthSections: []string{"resourcepack"}, CurseForgeSections: []string{"texture-packs"}},
	"shader_pack":   {ModrinthSections: []string{"shader"}, CurseForgeSections: []string{"shaders"}},
	"datapack":      {ModrinthSections: []string{"datapack"}, CurseForgeSections: []string{"data-packs"}},
	"addon": {
		ModrinthSections:   []string{"mod", "plugin", "datapack", "resourcepack", "shader"},
		CurseForgeSections: []string{"mc-addons", "mc-mods", "bukkit-plugins", "data-packs", "texture-packs", "shaders", "worlds"},
	},
}

func containsImportSection(sections []string, value string) bool {
	for _, section := range sections {
		if section == value {
			return true
		}
	}
	return false
}

func projectImportDisplayName(projectType string) string {
	if name := map[string]string{
		"mod": "mod", "modpack": "modpack", "plugin": "plugin", "map": "map",
		"resource_pack": "resource pack", "shader_pack": "shader pack", "datapack": "data pack", "addon": "add-on",
	}[projectType]; name != "" {
		return name
	}
	return "project"
}

func splitURLPath(value string) []string {
	parts := strings.Split(strings.Trim(value, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" && part != "." && part != ".." {
			result = append(result, part)
		}
	}
	return result
}

func isSafeImportReferenceSegment(value string) bool {
	if value == "" || len(value) > 128 || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' && character != '.' {
			return false
		}
	}
	return true
}
