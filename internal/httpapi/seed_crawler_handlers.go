package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

var seedCrawlerProjectTypes = stringSet("mod", "plugin", "shader_pack", "resource_pack")

type seedCrawlerConfigRequest struct {
	Enabled            bool       `json:"enabled"`
	ProjectTypes       []string   `json:"projectTypes"`
	BatchSize          int        `json:"batchSize"`
	DailyLimit         int        `json:"dailyLimit"`
	MinimumDownloads   int64      `json:"minimumDownloads"`
	IntervalSeconds    int        `json:"intervalSeconds"`
	MaxConcurrency     int        `json:"maxConcurrency"`
	AIDailyTokenBudget int64      `json:"aiDailyTokenBudget"`
	AutoSubmitReview   bool       `json:"autoSubmitReview"`
	LastRunAt          *time.Time `json:"lastRunAt"`
	NextRunAt          *time.Time `json:"nextRunAt"`
}

func normalizeSeedCrawlerTypes(values []string) ([]string, bool) {
	values = uniqueTrimmed(values, 4)
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, raw := range values {
		projectType := strings.ToLower(strings.TrimSpace(raw))
		if projectType != "mod" {
			projectType = normalizeSimpleProjectType(projectType)
		}
		if !seedCrawlerProjectTypes[projectType] {
			return nil, false
		}
		if !seen[projectType] {
			seen[projectType] = true
			result = append(result, projectType)
		}
	}
	return result, len(result) > 0
}

func (s *Server) adminSeedCrawler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var request seedCrawlerConfigRequest
		if decodeJSON(r, &request) != nil {
			writeError(w, 400, "爬虫配置格式不正确")
			return
		}
		projectTypes, valid := normalizeSeedCrawlerTypes(request.ProjectTypes)
		if !valid || request.BatchSize < 1 || request.BatchSize > 50 || request.DailyLimit < 1 || request.DailyLimit > 1000 ||
			request.MinimumDownloads < 0 || request.IntervalSeconds < 60 || request.MaxConcurrency < 1 || request.MaxConcurrency > 16 || request.AIDailyTokenBudget < 0 {
			writeError(w, 400, "爬虫配置超出安全范围")
			return
		}
		_, err := s.db.Exec(r.Context(), `update seed_crawler_configs set enabled=$1,project_types=$2,batch_size=$3,daily_limit=$4,
			minimum_downloads=$5,interval_seconds=$6,max_concurrency=$7,ai_daily_token_budget=$8,auto_submit_review=$9,updated_by=$10,
			next_run_at=case when $1 then coalesce(next_run_at,now()) else null end,updated_at=now() where id`,
			request.Enabled, projectTypes, request.BatchSize, request.DailyLimit, request.MinimumDownloads, request.IntervalSeconds,
			request.MaxConcurrency, request.AIDailyTokenBudget, request.AutoSubmitReview, currentClaims(r).Subject)
		if err != nil {
			writeError(w, 500, "保存爬虫配置失败")
			return
		}
		s.writeAppLog(r.Context(), "admin_operation", "info", "seed_crawler.config.update", "seed_crawler", currentClaims(r).Subject, r, 200, 0, request)
	}
	var config seedCrawlerConfigRequest
	var updatedBy *int64
	var updatedAt any
	err := s.db.QueryRow(r.Context(), `select enabled,project_types,batch_size,daily_limit,minimum_downloads,interval_seconds,
		max_concurrency,ai_daily_token_budget,auto_submit_review,last_run_at,next_run_at,updated_by,updated_at from seed_crawler_configs where id`).Scan(
		&config.Enabled, &config.ProjectTypes, &config.BatchSize, &config.DailyLimit, &config.MinimumDownloads, &config.IntervalSeconds,
		&config.MaxConcurrency, &config.AIDailyTokenBudget, &config.AutoSubmitReview, &config.LastRunAt, &config.NextRunAt, &updatedBy, &updatedAt)
	if err != nil {
		writeError(w, 500, "读取爬虫配置失败")
		return
	}
	writeJSON(w, 200, map[string]any{"config": config, "updatedBy": updatedBy, "updatedAt": updatedAt})
}

func (s *Server) adminSeedCrawlerRuns(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var request struct {
			DryRun bool `json:"dryRun"`
		}
		if decodeJSON(r, &request) != nil {
			writeError(w, 400, "运行参数不正确")
			return
		}
		var id string
		err := s.db.QueryRow(r.Context(), `insert into seed_crawler_runs(dry_run,requested_by) values($1,$2) returning public_id`, request.DryRun, currentClaims(r).Subject).Scan(&id)
		if err != nil {
			writeError(w, 500, "创建爬虫任务失败")
			return
		}
		s.writeAppLog(r.Context(), "admin_operation", "info", "seed_crawler.run", id, currentClaims(r).Subject, r, 202, 0, map[string]bool{"dryRun": request.DryRun})
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "status": "pending", "dryRun": request.DryRun})
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	rows, err := s.db.Query(r.Context(), `select public_id,status,dry_run,attempts,stats,last_error,created_at,started_at,finished_at
		from seed_crawler_runs order by created_at desc,id desc limit $1`, limit)
	if err != nil {
		writeError(w, 500, "读取爬虫任务失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, status, lastError string
		var dryRun bool
		var attempts int
		var stats []byte
		var created any
		var started, finished any
		if err = rows.Scan(&id, &status, &dryRun, &attempts, &stats, &lastError, &created, &started, &finished); err != nil {
			writeError(w, 500, "解析爬虫任务失败")
			return
		}
		items = append(items, map[string]any{"id": id, "status": status, "dryRun": dryRun, "attempts": attempts, "stats": json.RawMessage(stats), "lastError": lastError, "createdAt": created, "startedAt": started, "finishedAt": finished})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "读取爬虫任务失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (s *Server) adminSeedCrawlerCandidates(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	rows, err := s.db.Query(r.Context(), `select candidate.external_project_id,candidate.project_type,candidate.downloads,candidate.status,
		candidate.payload,candidate.last_error,candidate.created_at,candidate.updated_at,draft.public_id
		from seed_crawler_candidates candidate left join user_drafts draft
			on draft.draft_key='seed-crawler:'||candidate.external_project_id and draft.submitted_at is null
		where ($1='' or candidate.status=$1) order by candidate.downloads desc,candidate.id desc limit 100`, status)
	if err != nil {
		writeError(w, 500, "读取爬虫候选失败")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, projectType, status, lastError string
		var downloads int64
		var payload []byte
		var created, updated any
		var draftID *string
		if err = rows.Scan(&id, &projectType, &downloads, &status, &payload, &lastError, &created, &updated, &draftID); err != nil {
			writeError(w, 500, "解析爬虫候选失败")
			return
		}
		items = append(items, map[string]any{"externalProjectId": id, "projectType": projectType, "downloads": downloads, "status": status, "payload": json.RawMessage(payload), "lastError": lastError, "createdAt": created, "updatedAt": updated, "draftId": draftID})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusServiceUnavailable, "读取爬虫候选失败")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
