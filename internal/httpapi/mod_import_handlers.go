package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
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
	ID        string          `json:"id"`
	Provider  string          `json:"provider"`
	SourceURL string          `json:"sourceUrl"`
	Status    string          `json:"status"`
	Progress  int             `json:"progress"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     string          `json:"error,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Server) createModMetadataImport(w http.ResponseWriter, r *http.Request) {
	var request modMetadataImportRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	provider, sourceURL, _, err := parseModImportSource(request.Provider, request.URL)
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
	jobID := newExportID()
	userID := currentClaims(r).Subject
	if _, err = s.db.Exec(r.Context(),
		`insert into mod_metadata_import_jobs(id,user_id,provider,source_url,status,progress) values($1,$2,$3,$4,'queued',0)`,
		jobID, userID, provider, sourceURL,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "创建模组导入任务失败")
		return
	}
	message := modMetadataImportMessage{JobID: jobID}
	if s.queue == nil || s.queue.PublishTask(r.Context(), modMetadataImportTaskCode, message) != nil {
		_, _ = s.db.Exec(r.Context(), `update mod_metadata_import_jobs set status='failed',error='NATS queue unavailable',finished_at=now(),updated_at=now() where id=$1`, jobID)
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
		`select id,provider,source_url,status,progress,coalesce(result,'null'::jsonb),error,created_at,updated_at
		 from mod_metadata_import_jobs where id=$1 and user_id=$2`,
		jobID, userID,
	).Scan(&job.ID, &job.Provider, &job.SourceURL, &job.Status, &job.Progress, &result, &job.Error, &job.CreatedAt, &job.UpdatedAt)
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
			return errors.New("Modrinth 自动导入已在后台关闭")
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

func parseModImportSource(provider, rawURL string) (string, string, string, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return "", "", "", errors.New("请输入有效的 HTTPS 项目链接")
	}
	host := strings.ToLower(parsed.Hostname())
	segments := splitURLPath(parsed.Path)
	switch provider {
	case "modrinth":
		if (host != "modrinth.com" && host != "www.modrinth.com") || len(segments) < 2 || segments[0] != "mod" || !isSafeImportReferenceSegment(segments[1]) {
			return "", "", "", errors.New("请输入 Modrinth 模组项目链接")
		}
		return provider, "https://modrinth.com/mod/" + segments[1], segments[1], nil
	case "curseforge":
		if (host != "curseforge.com" && host != "www.curseforge.com") || len(segments) < 3 || segments[0] != "minecraft" || segments[1] != "mc-mods" || !isSafeImportReferenceSegment(segments[2]) {
			return "", "", "", errors.New("请输入 CurseForge Minecraft 模组链接")
		}
		return provider, "https://www.curseforge.com/minecraft/mc-mods/" + segments[2], segments[2], nil
	case "github":
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
