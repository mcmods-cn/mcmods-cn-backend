package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	request, err := parseSeedCrawlerRunPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "爬虫任务分页参数不正确")
		return
	}
	query, args := seedCrawlerRunPageSQL(request)
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "读取爬虫任务失败")
		return
	}
	defer rows.Close()
	items := make([]seedCrawlerRunSummary, 0, request.Limit+1)
	for rows.Next() {
		var item seedCrawlerRunSummary
		if err = rows.Scan(&item.InternalID, &item.ID, &item.Status, &item.DryRun, &item.Attempts, &item.Stats, &item.LastError,
			&item.CreatedAt, &item.StartedAt, &item.FinishedAt); err != nil {
			writeError(w, 500, "解析爬虫任务失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, 500, "读取爬虫任务失败")
		return
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		last := items[len(items)-1]
		nextCursor = encodeSeedCrawlerRunPageCursor(seedCrawlerRunPageCursor{
			Version: seedCrawlerCursorVersion, Scope: request.Scope, CreatedAt: last.CreatedAt, ID: last.InternalID,
		})
	}
	writeBoundedCatalogJSON(w, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func (s *Server) seedCrawlerCandidateDetail(w http.ResponseWriter, r *http.Request) {
	externalProjectID := strings.TrimSpace(r.PathValue("id"))
	if externalProjectID == "" || len(externalProjectID) > 200 {
		writeError(w, http.StatusBadRequest, "爬虫候选 ID 不正确")
		return
	}
	var item seedCrawlerCandidateDetailResponse
	err := s.db.QueryRow(r.Context(), seedCrawlerCandidateDetailSQL, externalProjectID).Scan(
		&item.ExternalProjectID, &item.ProjectType, &item.Downloads, &item.Status, &item.FirstSeenRunID, &item.LastSeenRunID,
		&item.Payload, &item.LastError,
		&item.CreatedAt, &item.UpdatedAt, &item.DraftID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "爬虫候选不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取爬虫候选详情失败")
		return
	}
	writeBoundedCatalogJSON(w, item)
}

func (s *Server) adminSeedCrawlerCandidates(w http.ResponseWriter, r *http.Request) {
	request, err := parseSeedCrawlerCandidatePageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, "爬虫候选分页参数不正确")
		return
	}
	query, args := seedCrawlerCandidatePageSQL(request)
	rows, err := s.db.Query(r.Context(), query, args...)
	if err != nil {
		writeError(w, 500, "读取爬虫候选失败")
		return
	}
	defer rows.Close()
	items := make([]seedCrawlerCandidateSummary, 0, request.Limit+1)
	for rows.Next() {
		var item seedCrawlerCandidateSummary
		if err = rows.Scan(&item.InternalID, &item.ExternalProjectID, &item.ProjectType, &item.Downloads, &item.Status,
			&item.FirstSeenRunID, &item.LastSeenRunID, &item.LastError, &item.CreatedAt, &item.UpdatedAt, &item.DraftID); err != nil {
			writeError(w, 500, "解析爬虫候选失败")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, 500, "读取爬虫候选失败")
		return
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		last := items[len(items)-1]
		nextCursor = encodeSeedCrawlerCandidatePageCursor(seedCrawlerCandidatePageCursor{
			Version: seedCrawlerCursorVersion, Scope: request.Scope, Downloads: last.Downloads, ID: last.InternalID,
		})
	}
	writeBoundedCatalogJSON(w, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}
