package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/queue"
)

const (
	aiTaskPermissionTranslation   = "permission_translation_completion"
	aiTaskI18nTranslation         = "i18n_translation_completion"
	aiTaskNotificationTranslation = "notification_translation_completion"
	aiTaskContentTranslation      = "content_translation_completion"
)

type aiTaskDefinition struct {
	TaskType         string
	ConcurrencyLimit int
	TimeoutSeconds   int
}

var registeredAITaskDefinitions = []aiTaskDefinition{
	{TaskType: aiTaskPermissionTranslation, ConcurrencyLimit: 2, TimeoutSeconds: 120},
	{TaskType: aiTaskI18nTranslation, ConcurrencyLimit: 2, TimeoutSeconds: 120},
	{TaskType: aiTaskNotificationTranslation, ConcurrencyLimit: 4, TimeoutSeconds: 90},
	{TaskType: aiTaskContentTranslation, ConcurrencyLimit: 4, TimeoutSeconds: 120},
}

type aiConfigPayload struct {
	Providers   []aiProviderConfig  `json:"providers"`
	Models      []aiModelConfig     `json:"models"`
	TaskModels  []aiTaskModelConfig `json:"taskModels"`
	Quotas      []aiQuotaConfig     `json:"quotas"`
	Translation aiTranslationConfig `json:"translation"`
}

type aiProviderConfig struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	BaseURL   string `json:"baseUrl"`
	APIKey    string `json:"apiKey,omitempty"`
	HasAPIKey bool   `json:"hasApiKey,omitempty"`
	Protocol  string `json:"protocol"`
	Notes     string `json:"notes"`
}

type aiModelConfig struct {
	Provider              string  `json:"provider"`
	Model                 string  `json:"model"`
	DisplayName           string  `json:"displayName"`
	Enabled               bool    `json:"enabled"`
	ContextTokens         int     `json:"contextTokens"`
	MaxOutputTokens       int     `json:"maxOutputTokens"`
	InputPricePerMillion  float64 `json:"inputPricePerMillion"`
	OutputPricePerMillion float64 `json:"outputPricePerMillion"`
}

type aiTaskModelConfig struct {
	TaskType         string `json:"taskType"`
	ModelKey         string `json:"modelKey"`
	ConcurrencyLimit int    `json:"concurrencyLimit"`
	TimeoutSeconds   int    `json:"timeoutSeconds"`
	Prompt           string `json:"prompt"`
}

type aiQuotaConfig struct {
	Scope        string `json:"scope"`
	Subject      string `json:"subject"`
	Period       string `json:"period"`
	RequestLimit int64  `json:"requestLimit"`
	TokenLimit   int64  `json:"tokenLimit"`
	CostLimitCNY int64  `json:"costLimitCny"`
}

type aiTranslationConfig struct {
	Enabled       bool     `json:"enabled"`
	SourceLocale  string   `json:"sourceLocale"`
	TargetLocales []string `json:"targetLocales"`
	TaskType      string   `json:"taskType"`
	AutoSubmit    bool     `json:"autoSubmit"`
	Glossary      string   `json:"glossary"`
}

type createAITaskRequest struct {
	TaskType       string         `json:"taskType"`
	Provider       string         `json:"provider"`
	Model          string         `json:"model"`
	Priority       int            `json:"priority"`
	ConcurrencyKey string         `json:"concurrencyKey"`
	Payload        map[string]any `json:"payload"`
}

type aiTaskMessage struct {
	TaskID   int64  `json:"taskId"`
	TaskUID  string `json:"taskUid"`
	TaskType string `json:"taskType"`
}

type AIWorker struct {
	db                    *pgxpool.Pool
	queue                 *queue.Client
	settingsEncryptionKey string
}

func NewAIWorker(db *pgxpool.Pool, queueClient *queue.Client, settingsEncryptionKey string) *AIWorker {
	return &AIWorker{db: db, queue: queueClient, settingsEncryptionKey: settingsEncryptionKey}
}

func (worker *AIWorker) Start(ctx context.Context) error {
	if worker == nil || worker.db == nil {
		return queue.ErrUnavailable
	}
	subscribeErr := queue.ErrUnavailable
	if worker.queue != nil {
		subscribeErr = worker.queue.SubscribeTask("ai", worker.handleTask)
	}
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		worker.recoverTasks(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				worker.recoverTasks(ctx)
			}
		}
	}()
	return subscribeErr
}

func (worker *AIWorker) handleTask(ctx context.Context, raw []byte) error {
	var msg aiTaskMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return err
	}
	if msg.TaskID <= 0 {
		return errors.New("ai task message missing task id")
	}
	started := time.Now()
	claimed, err := worker.claimTask(ctx, msg.TaskID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	worker.writeTaskLog(ctx, msg.TaskID, "info", "task_started", "AI task picked by NATS worker", msg)

	var taskType, provider, model string
	var createdBy int64
	var rawPayload []byte
	if err := worker.db.QueryRow(
		ctx,
		`select task_type, provider, model, payload, coalesce(created_by, 0) from ai_tasks where id = $1`,
		msg.TaskID,
	).Scan(&taskType, &provider, &model, &rawPayload, &createdBy); err != nil {
		worker.failTask(ctx, msg.TaskID, err)
		return err
	}

	var recovery seedRecoveryPayload
	if json.Unmarshal(rawPayload, &recovery) == nil && recovery.Scope == seedRecoveryScope {
		leased, stop := maintainSeedCrawlerLease(ctx, worker.db, recovery.RunID, recovery.RunToken)
		defer stop()
		defer worker.finishSeedRecoveryRun(ctx, msg.TaskID)
		ctx = leased
	}
	if err = worker.preflightTranslation(ctx, taskType, rawPayload); err != nil {
		worker.failTask(ctx, msg.TaskID, err)
		return err
	}
	result, usage, err := worker.executeTask(ctx, taskType, provider, model, rawPayload)
	// Record returned usage even when the provider produced an invalid result:
	// an unsuccessful translation can still be a billed provider call.
	rawResult, _ := json.Marshal(result)
	if _, accountingErr := worker.db.Exec(ctx, `update ai_tasks
		set input_tokens=$2,output_tokens=$3,cost_micros=$4,result=$5::jsonb,updated_at=now()
		where id=$1 and status in ('running','cancelled','failed')`, msg.TaskID, max(usage.InputTokens, 0), max(usage.OutputTokens, 0), max(usage.CostMicros, 0), string(rawResult)); accountingErr != nil {
		worker.failTask(ctx, msg.TaskID, accountingErr)
		return accountingErr
	}
	if err != nil {
		worker.failTask(ctx, msg.TaskID, err)
		return err
	}
	if taskType == aiTaskNotificationTranslation && createdBy > 0 {
		if err = worker.persistNotificationTranslation(ctx, msg.TaskID, createdBy, rawPayload, result); err != nil {
			worker.failTask(ctx, msg.TaskID, err)
			return err
		}
	}
	if taskType == aiTaskContentTranslation {
		var payloadScope struct {
			Scope string `json:"scope"`
		}
		_ = json.Unmarshal(rawPayload, &payloadScope)
		if payloadScope.Scope == seedRecoveryScope {
			err = worker.persistSeedRecovery(ctx, msg.TaskID, rawPayload, result)
		} else if payloadScope.Scope == "seed_crawler" {
			err = worker.persistSeedTranslation(ctx, msg.TaskID, rawPayload)
		} else if payloadScope.Scope == "community_post" {
			err = worker.persistCommunityPostTranslation(ctx, msg.TaskID, rawPayload, result)
		} else {
			err = worker.persistCatalogContentTranslation(ctx, msg.TaskID, createdBy, rawPayload, result)
		}
		if err != nil {
			worker.failTask(ctx, msg.TaskID, err)
			return err
		}
	}
	// Completion is visible only after publication/persistence succeeds.
	tag, err := worker.db.Exec(ctx, `update ai_tasks set status='completed',
		finished_at=now(),updated_at=now() where id=$1 and status='running'`, msg.TaskID)
	if err != nil {
		worker.failTask(ctx, msg.TaskID, err)
		return err
	}
	if tag.RowsAffected() == 0 {
		var status string
		if err = worker.db.QueryRow(ctx, `select status from ai_tasks where id=$1`, msg.TaskID).Scan(&status); err != nil {
			return err
		}
		if status != "completed" {
			return errors.New("AI task was cancelled before completion")
		}
	}
	worker.writeTaskLog(ctx, msg.TaskID, "info", "task_completed", "AI translation persisted", map[string]any{
		"durationMs": time.Since(started).Milliseconds(),
	})
	worker.writeAICallLog(ctx, msg.TaskID, msg.TaskType, provider, model, time.Since(started))
	return nil
}

func (worker *AIWorker) failTask(ctx context.Context, taskID int64, cause error) {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	_, _ = worker.db.Exec(
		ctx,
		`update ai_tasks
		 set status = 'failed', error = $2, finished_at = now(), updated_at = now()
		 where id = $1 and status='running'`,
		taskID,
		message,
	)
	var raw []byte
	if worker.db.QueryRow(ctx, `select payload from ai_tasks where id=$1 and status='failed'`, taskID).Scan(&raw) == nil {
		var payload seedTranslationPayload
		if json.Unmarshal(raw, &payload) == nil && payload.Scope == "seed_crawler" {
			seedCtx := withSeedCrawlerLease(ctx, payload.RunID, payload.RunToken)
			crawler := &SeedCrawlerWorker{server: &Server{db: worker.db}}
			_, _ = crawler.execSeedWrite(seedCtx, `update seed_crawler_translation_tasks set status='failed',input_tokens=task.input_tokens,output_tokens=task.output_tokens,last_error=$4,updated_at=now() from ai_tasks task where task.id=$1 and candidate_id=$2 and locale=$3`, taskID, payload.CandidateID, payload.TargetLocale, truncateRunes(message, 1000))
		}
	}
	worker.writeTaskLog(ctx, taskID, "error", "task_failed", message, nil)
}

func (worker *AIWorker) writeTaskLog(ctx context.Context, taskID int64, level string, event string, message string, payload any) {
	raw, _ := json.Marshal(payload)
	_, _ = worker.db.Exec(
		ctx,
		`insert into ai_task_logs (task_id, level, event, message, payload)
		 values ($1, $2, $3, $4, $5::jsonb)`,
		taskID,
		level,
		event,
		message,
		string(raw),
	)
}

func (worker *AIWorker) writeAICallLog(ctx context.Context, taskID int64, taskType string, provider string, model string, duration time.Duration) {
	payload, _ := json.Marshal(map[string]any{
		"taskId":   taskID,
		"taskType": taskType,
		"provider": provider,
		"model":    model,
	})
	_, _ = worker.db.Exec(
		ctx,
		`insert into app_logs (category, level, action, target, latency_ms, payload)
		 values ('ai_call', 'info', $1, $2, $3, $4::jsonb)`,
		taskType,
		provider+"/"+model,
		duration.Milliseconds(),
		string(payload),
	)
}

func (s *Server) getAIConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.aiConfigFromSettingsChecked(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_CONFIG_UNAVAILABLE", "AI 配置暂时不可用", 0, nil)
		return
	}
	writeJSON(w, http.StatusOK, redactAIConfig(cfg))
}

func (s *Server) updateAIConfig(w http.ResponseWriter, r *http.Request) {
	var payload aiConfigPayload
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	current, err := s.aiConfigFromSettingsChecked(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_CONFIG_UNAVAILABLE", "无法读取现有 AI 配置，未保存修改", 0, nil)
		return
	}
	payload = normalizeAIConfig(payload)
	if err := validateAIQuotaConfiguration(payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "AI_CONFIG_INVALID", err.Error(), 0, nil)
		return
	}
	for index := range payload.Providers {
		if payload.Providers[index].APIKey == "" {
			payload.Providers[index].APIKey = providerAPIKey(current.Providers, payload.Providers[index].Code)
		}
	}
	raw, err := s.sealSystemSetting(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "AI 配置格式不正确")
		return
	}
	_, err = s.db.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('ai.config', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		raw,
		currentClaims(r).Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 AI 配置失败")
		return
	}
	s.writeAppLog(r.Context(), "admin_operation", "info", "update_ai_config", "ai.config", currentClaims(r).Subject, r, http.StatusOK, 0, map[string]any{
		"providers": len(payload.Providers),
		"models":    len(payload.Models),
	})
	writeJSON(w, http.StatusOK, redactAIConfig(payload))
}

func (s *Server) adminAITasks(w http.ResponseWriter, r *http.Request) {
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	taskType := strings.TrimSpace(r.URL.Query().Get("taskType"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	args := []any{}
	where := []string{"1 = 1"}
	if status != "" {
		args = append(args, status)
		where = append(where, fmt.Sprintf("t.status = $%d", len(args)))
	}
	if taskType != "" {
		args = append(args, taskType)
		where = append(where, fmt.Sprintf("t.task_type = $%d", len(args)))
	}
	if query != "" {
		args = append(args, "%"+strings.ToLower(query)+"%")
		placeholder := fmt.Sprintf("$%d", len(args))
		where = append(where, "(lower(t.task_uid) like "+placeholder+" or lower(t.provider) like "+placeholder+" or lower(t.model) like "+placeholder+" or lower(t.payload::text) like "+placeholder+")")
	}
	if from, ok := parseLogTime(r.URL.Query().Get("from"), false); ok {
		args = append(args, from)
		where = append(where, fmt.Sprintf("t.created_at >= $%d", len(args)))
	}
	if to, ok := parseLogTime(r.URL.Query().Get("to"), true); ok {
		args = append(args, to)
		where = append(where, fmt.Sprintf("t.created_at <= $%d", len(args)))
	}
	args = append(args, boundedLimit(r.URL.Query().Get("limit"), 100, 500))
	rows, err := s.querySimpleRows(
		r,
		`select t.task_uid as id, t.task_uid, t.task_type, t.provider, t.model, t.status, t.priority,
		        t.concurrency_key, t.input_tokens, t.output_tokens, t.cost_micros, t.payload,
		        t.result, t.error, u.public_id as created_by, u.username as created_by_username,
		        t.created_at, t.queued_at, t.started_at, t.finished_at, t.updated_at
		 from ai_tasks t
		 left join users u on u.id = t.created_by
		 where `+strings.Join(where, " and ")+`
		 order by t.created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 管理数据失败")
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) adminAITask(w http.ResponseWriter, r *http.Request) {
	taskUID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if taskUID == "" || len(taskUID) > 80 {
		writeError(w, http.StatusBadRequest, "AI 任务 ID 不正确")
		return
	}
	rows, err := s.querySimpleRows(
		r,
		`select task_uid as id, task_uid, task_type, provider, model, status, input_tokens, output_tokens,
		        cost_micros, result, error, created_at, started_at, finished_at, updated_at
		 from ai_tasks
		 where task_uid = $1
		 limit 1`,
		taskUID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 管理数据失败")
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "AI 任务不存在")
		return
	}
	writeJSON(w, http.StatusOK, rows[0])
}

func (s *Server) createAITask(w http.ResponseWriter, r *http.Request) {
	var req createAITaskRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.TaskType = normalizeAIToken(req.TaskType)
	req.Provider = normalizeAIToken(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.ConcurrencyKey = strings.TrimSpace(req.ConcurrencyKey)
	if req.TaskType == "" {
		writeError(w, http.StatusBadRequest, "任务类型不能为空")
		return
	}
	cfg := s.aiConfigFromSettings(r.Context())
	binding, ok := findAITaskModel(cfg.TaskModels, req.TaskType)
	if !ok {
		writeError(w, http.StatusBadRequest, "该 AI 任务尚未配置任务模型")
		return
	}
	provider, model, ok := resolveAIModel(cfg, binding.ModelKey)
	if !ok {
		writeError(w, http.StatusBadRequest, "任务绑定的模型不存在、未启用或供应商未启用")
		return
	}
	req.Provider = provider.Code
	req.Model = model.Model
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	if scope, _ := req.Payload["scope"].(string); isSeedTaskScope(scope) {
		writeError(w, http.StatusBadRequest, "seed crawler tasks must be created by the fenced crawler")
		return
	}
	if err := freezeAITranslationContext(req.Payload, cfg); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.ConcurrencyKey = aiTranslationContextKey(req.ConcurrencyKey, cfg)
	rawPayload, err := json.Marshal(req.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "任务 Payload 格式不正确")
		return
	}
	if err = validateAITranslationPayload(req.TaskType, rawPayload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reserved := estimatedAIReservation(rawPayload, model)
	taskUID := "ai_" + randomHex(16)
	var taskID int64
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	if err = reserveAISiteQuotaTx(r.Context(), tx, cfg, reserved); err != nil {
		writeError(w, http.StatusTooManyRequests, "AI site quota is insufficient")
		return
	}
	err = tx.QueryRow(
		r.Context(),
		`insert into ai_tasks (task_uid, task_type, provider, model, status, priority, concurrency_key, payload, created_by, queued_at,quota_reserved_tokens)
		 values ($1, $2, $3, $4, 'queued', $5, $6, $7::jsonb, $8, now(),$9)
		 returning id`,
		taskUID,
		req.TaskType,
		req.Provider,
		req.Model,
		req.Priority,
		req.ConcurrencyKey,
		string(rawPayload),
		currentClaims(r).Subject,
		reserved,
	).Scan(&taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	message := aiTaskMessage{TaskID: taskID, TaskUID: taskUID, TaskType: req.TaskType}
	if err = s.enqueueAIOutboxTx(r.Context(), tx, taskID, taskUID, req.TaskType); err != nil {
		writeError(w, http.StatusInternalServerError, "AI 任务可靠入队失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	if !s.cfg.NATS.OutboxEnabled && (s.queue == nil || s.queue.PublishTask(r.Context(), "ai", message) != nil) {
		_, _ = s.db.Exec(
			r.Context(),
			`update ai_tasks set status = 'failed', error = $2, finished_at = now(), updated_at = now() where id = $1`,
			taskID,
			queue.ErrUnavailable.Error(),
		)
		writeError(w, http.StatusServiceUnavailable, "NATS 任务队列不可用，请检查 NATS 服务是否运行")
		return
	}
	s.writeAITaskLog(r.Context(), taskID, "info", "task_queued", "AI task published to NATS", message)
	writeJSON(w, http.StatusCreated, map[string]any{"id": taskUID, "taskUid": taskUID, "status": "queued"})
}

func (s *Server) adminAIStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.querySimpleRows(
		r,
		`select status, count(*) as tasks, coalesce(sum(input_tokens), 0) as input_tokens,
		        coalesce(sum(output_tokens), 0) as output_tokens,
		        coalesce(sum(cost_micros), 0) as cost_micros
		 from ai_tasks
		 where created_at >= now() - interval '30 days'
		 group by status
		 order by status`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 管理数据失败")
		return
	}
	byProvider, err := s.querySimpleRows(
		r,
		`select provider, model, count(*) as tasks, coalesce(sum(cost_micros), 0) as cost_micros
		 from ai_tasks
		 where created_at >= now() - interval '30 days'
		 group by provider, model
		 order by cost_micros desc, tasks desc
		 limit 50`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 管理数据失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"byStatus":   stats,
		"byProvider": byProvider,
	})
}

func (s *Server) aiConfigFromSettings(ctx context.Context) aiConfigPayload {
	payload, err := s.aiConfigFromSettingsChecked(ctx)
	if err != nil {
		// Execution paths fail closed through the existing disabled defaults.
		return defaultAIConfig()
	}
	return payload
}

func (s *Server) aiConfigFromSettingsChecked(ctx context.Context) (aiConfigPayload, error) {
	payload := defaultAIConfig()
	var raw []byte
	err := s.db.QueryRow(ctx, `select value from system_settings where key = 'ai.config'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return payload, nil
	}
	if err != nil {
		return aiConfigPayload{}, err
	}
	if err := s.openSystemSetting(raw, &payload); err != nil {
		return aiConfigPayload{}, err
	}
	return normalizeAIConfig(payload), nil
}

func (s *Server) writeAITaskLog(ctx context.Context, taskID int64, level string, event string, message string, payload any) {
	raw, _ := json.Marshal(payload)
	_, _ = s.db.Exec(
		ctx,
		`insert into ai_task_logs (task_id, level, event, message, payload)
		 values ($1, $2, $3, $4, $5::jsonb)`,
		taskID,
		level,
		event,
		message,
		string(raw),
	)
}

func defaultAIConfig() aiConfigPayload {
	return aiConfigPayload{
		Providers: []aiProviderConfig{
			{Code: "openai", Name: "OpenAI", Enabled: false, BaseURL: "https://api.openai.com/v1", Protocol: "openai-compatible"},
			{Code: "aliyun", Name: "阿里云百炼", Enabled: false, BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Protocol: "openai-compatible"},
			{Code: "anthropic", Name: "Anthropic", Enabled: false, BaseURL: "https://api.anthropic.com", Protocol: "anthropic"},
		},
		Models: []aiModelConfig{
			{Provider: "openai", Model: "gpt-4.1-mini", DisplayName: "GPT-4.1 mini", Enabled: false, ContextTokens: 1047576, MaxOutputTokens: 32768},
		},
		TaskModels: defaultAITaskModels(),
		Quotas: []aiQuotaConfig{
			{Scope: "site", Subject: "default", Period: "day", RequestLimit: 1000, TokenLimit: 1000000, CostLimitCNY: 10000},
		},
		Translation: aiTranslationConfig{
			Enabled:       false,
			SourceLocale:  "zh-CN",
			TargetLocales: []string{"en-US"},
			TaskType:      "translation",
			AutoSubmit:    false,
		},
	}
}

func normalizeAIConfig(payload aiConfigPayload) aiConfigPayload {
	for index := range payload.Providers {
		payload.Providers[index].Code = normalizeAIToken(payload.Providers[index].Code)
		payload.Providers[index].Name = strings.TrimSpace(payload.Providers[index].Name)
		payload.Providers[index].BaseURL = strings.TrimSpace(payload.Providers[index].BaseURL)
		payload.Providers[index].APIKey = strings.TrimSpace(payload.Providers[index].APIKey)
		payload.Providers[index].Protocol = normalizeAIProtocol(payload.Providers[index].Protocol)
		payload.Providers[index].Notes = strings.TrimSpace(payload.Providers[index].Notes)
	}
	for index := range payload.Models {
		payload.Models[index].Provider = normalizeAIToken(payload.Models[index].Provider)
		payload.Models[index].Model = strings.TrimSpace(payload.Models[index].Model)
		payload.Models[index].DisplayName = strings.TrimSpace(payload.Models[index].DisplayName)
		if payload.Models[index].ContextTokens < 0 {
			payload.Models[index].ContextTokens = 0
		}
		if payload.Models[index].MaxOutputTokens < 0 {
			payload.Models[index].MaxOutputTokens = 0
		}
	}
	payload.TaskModels = normalizeAITaskModels(payload.TaskModels)
	for index := range payload.Quotas {
		payload.Quotas[index].Scope = normalizeAIToken(payload.Quotas[index].Scope)
		payload.Quotas[index].Subject = strings.TrimSpace(payload.Quotas[index].Subject)
		payload.Quotas[index].Period = normalizeAIToken(payload.Quotas[index].Period)
	}
	payload.Translation.SourceLocale = strings.TrimSpace(payload.Translation.SourceLocale)
	payload.Translation.TaskType = normalizeAIToken(payload.Translation.TaskType)
	payload.Translation.Glossary = strings.TrimSpace(payload.Translation.Glossary)
	if payload.Translation.SourceLocale == "" {
		payload.Translation.SourceLocale = "zh-CN"
	}
	if payload.Translation.TaskType == "" {
		payload.Translation.TaskType = "translation"
	}
	return payload
}

func defaultAITaskModels() []aiTaskModelConfig {
	models := make([]aiTaskModelConfig, 0, len(registeredAITaskDefinitions))
	for _, definition := range registeredAITaskDefinitions {
		models = append(models, aiTaskModelConfig{
			TaskType:         definition.TaskType,
			ConcurrencyLimit: definition.ConcurrencyLimit,
			TimeoutSeconds:   definition.TimeoutSeconds,
		})
	}
	return models
}

func normalizeAITaskModels(current []aiTaskModelConfig) []aiTaskModelConfig {
	byType := make(map[string]aiTaskModelConfig, len(current))
	for _, item := range current {
		item.TaskType = normalizeAIToken(item.TaskType)
		if item.TaskType != "" {
			byType[item.TaskType] = item
		}
	}
	result := defaultAITaskModels()
	for index := range result {
		if saved, ok := byType[result[index].TaskType]; ok {
			result[index].ModelKey = strings.TrimSpace(saved.ModelKey)
			result[index].ConcurrencyLimit = saved.ConcurrencyLimit
			result[index].TimeoutSeconds = saved.TimeoutSeconds
			result[index].Prompt = strings.TrimSpace(saved.Prompt)
		}
		result[index].ConcurrencyLimit = min(result[index].ConcurrencyLimit, 32)
		result[index].TimeoutSeconds = min(result[index].TimeoutSeconds, 600)
		if result[index].ConcurrencyLimit <= 0 {
			result[index].ConcurrencyLimit = 1
		}
		if result[index].TimeoutSeconds <= 0 {
			result[index].TimeoutSeconds = 60
		}
	}
	return result
}

func findAITaskModel(models []aiTaskModelConfig, taskType string) (aiTaskModelConfig, bool) {
	taskType = normalizeAIToken(taskType)
	for _, model := range models {
		if model.TaskType == taskType && strings.TrimSpace(model.ModelKey) != "" {
			return model, true
		}
	}
	return aiTaskModelConfig{}, false
}

func resolveAIModel(cfg aiConfigPayload, modelKey string) (aiProviderConfig, aiModelConfig, bool) {
	providerCode, modelID, ok := strings.Cut(strings.TrimSpace(modelKey), "/")
	if !ok || providerCode == "" || modelID == "" {
		return aiProviderConfig{}, aiModelConfig{}, false
	}
	var selectedModel aiModelConfig
	foundModel := false
	for _, model := range cfg.Models {
		if model.Enabled && model.Provider == providerCode && model.Model == modelID {
			selectedModel = model
			foundModel = true
			break
		}
	}
	if !foundModel {
		return aiProviderConfig{}, aiModelConfig{}, false
	}
	for _, provider := range cfg.Providers {
		if provider.Enabled && provider.Code == providerCode && provider.APIKey != "" {
			return provider, selectedModel, true
		}
	}
	return aiProviderConfig{}, aiModelConfig{}, false
}

func redactAIConfig(payload aiConfigPayload) aiConfigPayload {
	for index := range payload.Providers {
		payload.Providers[index].HasAPIKey = payload.Providers[index].APIKey != ""
		payload.Providers[index].APIKey = ""
	}
	return payload
}

func providerAPIKey(providers []aiProviderConfig, code string) string {
	code = normalizeAIToken(code)
	for _, provider := range providers {
		if provider.Code == code {
			return provider.APIKey
		}
	}
	return ""
}

func normalizeAIToken(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.Trim(value, ".")
	return value
}

func normalizeAIProtocol(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "anthropic") {
		return "anthropic"
	}
	return "openai-compatible"
}

func randomHex(bytesCount int) string {
	raw := make([]byte, bytesCount)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(raw)
}
