package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

const (
	aiTaskPermissionTranslation   = "permission_translation_completion"
	aiTaskI18nTranslation         = "i18n_translation_completion"
	aiTaskNotificationTranslation = "notification_translation_completion"
	aiTaskContentTranslation      = "content_translation_completion"
)

type aiTaskDefinition struct {
	TaskType       string
	TimeoutSeconds int
}

var registeredAITaskDefinitions = []aiTaskDefinition{
	{TaskType: aiTaskPermissionTranslation, TimeoutSeconds: 120},
	{TaskType: aiTaskI18nTranslation, TimeoutSeconds: 120},
	{TaskType: aiTaskNotificationTranslation, TimeoutSeconds: 90},
	{TaskType: aiTaskContentTranslation, TimeoutSeconds: 120},
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
	TaskType       string `json:"taskType"`
	ModelKey       string `json:"modelKey"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
	Prompt         string `json:"prompt"`
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
	if worker == nil || worker.db == nil || worker.queue == nil {
		return queue.ErrUnavailable
	}
	// Registration happens before SubscribeTask reports an offline NATS
	// connection, so the PostgreSQL dispatcher can use the same local handler.
	_ = worker.queue.SubscribeTask("ai", worker.handleTask)
	recoveryErr := worker.recoverTasks(ctx)
	go worker.recoveryLoop(ctx)
	return recoveryErr
}

func (worker *AIWorker) recoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(aiTaskRecoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.recoverTasks(ctx); err != nil {
				slog.Warn("recover AI task outbox", "error", err)
			}
		}
	}
}

func (worker *AIWorker) recoverTasks(ctx context.Context) error {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = recoverAITaskOutboxTx(ctx, tx, aiTaskStaleAfter, aiTaskRecoveryBatch); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	run, err := claimAITaskExecution(ctx, worker.db, msg.TaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	ctx = context.WithValue(ctx, aiTaskExecutionKey{}, run)
	worker.writeTaskLog(ctx, msg.TaskID, "info", "task_started", "AI task claimed by reliable worker", msg)

	var taskType, provider, model string
	var createdBy int64
	var rawPayload []byte
	if err := worker.db.QueryRow(
		ctx,
		`select task_type, provider, model, payload, coalesce(created_by, 0) from ai_tasks where id = $1`,
		msg.TaskID,
	).Scan(&taskType, &provider, &model, &rawPayload, &createdBy); err != nil {
		failureErr := worker.failTask(ctx, msg.TaskID, err)
		return errors.Join(err, failureErr)
	}

	result, _, err := worker.executeTask(ctx, taskType, provider, model, rawPayload)
	if err != nil {
		failureErr := worker.failTask(ctx, msg.TaskID, err)
		return errors.Join(err, failureErr)
	}
	rawResult, err := json.Marshal(result)
	if err != nil {
		failureErr := worker.failTask(ctx, msg.TaskID, err)
		return errors.Join(err, failureErr)
	}
	if taskType == aiTaskNotificationTranslation {
		if err = worker.persistNotificationTranslation(ctx, createdBy, rawPayload, result); err != nil {
			failureErr := worker.failTask(ctx, msg.TaskID, err)
			return errors.Join(err, failureErr)
		}
	}
	if taskType == aiTaskContentTranslation {
		var scope string
		scope, err = decodeAITaskContentScope(rawPayload)
		if err == nil {
			if scope == "community_post" {
				err = worker.persistCommunityPostTranslation(ctx, msg.TaskID, rawPayload, result)
			} else {
				err = worker.persistCatalogContentTranslation(ctx, msg.TaskID, createdBy, rawPayload, result)
			}
		}
		if err != nil {
			failureErr := worker.failTask(ctx, msg.TaskID, err)
			return errors.Join(err, failureErr)
		}
	}
	tag, err := worker.db.Exec(
		ctx,
		`update ai_tasks
		 set status = 'completed',
		     result = $2::jsonb,
		     quota_reserved_tokens = case when input_tokens+output_tokens>0 then 0 else quota_reserved_tokens end,
		     finished_at = now(),
		     updated_at = now()
		 where id = $1 and status = 'running' and started_at=$3`,
		msg.TaskID,
		string(rawResult),
		run.Started,
	)
	if err != nil {
		failureErr := worker.failTask(ctx, msg.TaskID, err)
		return errors.Join(err, failureErr)
	}
	if tag.RowsAffected() != 1 {
		err = fmt.Errorf("complete AI task %d: expected one running task, updated %d", msg.TaskID, tag.RowsAffected())
		failureErr := worker.failTask(ctx, msg.TaskID, err)
		return errors.Join(err, failureErr)
	}
	worker.writeTaskLog(ctx, msg.TaskID, "info", "task_completed", "AI translation completed", map[string]any{
		"durationMs": time.Since(started).Milliseconds(),
	})
	worker.writeAICallLog(ctx, msg.TaskID, msg.TaskType, provider, model, time.Since(started))
	return nil
}

func decodeAITaskContentScope(rawPayload []byte) (string, error) {
	var payload struct {
		Scope string `json:"scope"`
	}
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return "", fmt.Errorf("decode AI content task scope: %w", err)
	}
	payload.Scope = strings.TrimSpace(payload.Scope)
	if payload.Scope != "" && payload.Scope != "community_post" {
		return "", fmt.Errorf("unsupported AI content task scope %q", payload.Scope)
	}
	return payload.Scope, nil
}

func logAITranslationFailure(stage, taskUID string, err error) {
	slog.Error("AI translation failure", "module", "ai_translation", "stage", stage, "task_uid", taskUID, "error", err)
}

func (worker *AIWorker) failTask(ctx context.Context, taskID int64, cause error) error {
	message := ""
	if cause != nil {
		message = cause.Error()
	}
	query := `update ai_tasks
		 set status = 'failed', error = $2, finished_at = now(), updated_at = now()
		 where id = $1 and status = 'running'`
	args := []any{taskID, message}
	if run, ok := ctx.Value(aiTaskExecutionKey{}).(aiTaskExecution); ok {
		query = `update ai_tasks set status='failed',error=$2,finished_at=now(),updated_at=now(),
			quota_reserved_tokens=case when input_tokens+output_tokens>0 then 0 else quota_reserved_tokens end
			where id=$1 and status='running'`
		query += " and started_at=$3"
		args = append(args, run.Started)
	}
	tag, err := worker.db.Exec(
		ctx,
		query,
		args...,
	)
	if err != nil {
		logAITranslationFailure("persist_failed_state", strconv.FormatInt(taskID, 10), err)
		return fmt.Errorf("persist failed AI task %d: %w", taskID, err)
	}
	if tag.RowsAffected() != 1 {
		err = fmt.Errorf("persist failed AI task %d: expected one running task, updated %d", taskID, tag.RowsAffected())
		logAITranslationFailure("persist_failed_state", strconv.FormatInt(taskID, 10), err)
		return err
	}
	logAITranslationFailure("task_failed", strconv.FormatInt(taskID, 10), cause)
	worker.writeTaskLog(ctx, taskID, "error", "task_failed", message, nil)
	return nil
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
	cfg, err := readAIConfigFromSettings(r.Context(), s.db, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
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
	payload = normalizeAIConfig(payload)
	if err := validateAIQuotas(payload.Quotas); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	providerCodes := make(map[string]bool, len(payload.Providers))
	for _, provider := range payload.Providers {
		if provider.Code == "" || providerCodes[provider.Code] {
			writeAPIError(w, http.StatusBadRequest, "AI_PROVIDER_CODE_INVALID", "provider codes must be nonempty and unique", 0, nil)
			return
		}
		providerCodes[provider.Code] = true
		if err := validateAIProviderEndpoint(provider.BaseURL); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	for _, binding := range payload.TaskModels {
		if binding.TimeoutSeconds > 600 {
			writeError(w, http.StatusBadRequest, "AI task timeout cannot exceed 600 seconds")
			return
		}
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext('ai-config-settings-write'))`); err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
		return
	}
	current, err := readAIConfigFromSettings(r.Context(), tx, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
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
	_, err = tx.Exec(
		r.Context(),
		`insert into system_settings (key, value, updated_by, updated_at)
		 values ('ai.config', $1::jsonb, $2, now())
		 on conflict (key) do update
		 set value = excluded.value, updated_by = excluded.updated_by, updated_at = now()`,
		raw,
		currentClaims(r).Subject,
	)
	if err != nil || tx.Commit(r.Context()) != nil {
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
		        t.created_at, t.queued_at, t.started_at, t.finished_at, t.updated_at, `+aiTaskDeliveryFailureProjectionSQL+`
		 from ai_tasks t
		 left join users u on u.id = t.created_by
		 where `+strings.Join(where, " and ")+`
		 order by t.created_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 任务失败")
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
		        cost_micros, result, error, created_at, started_at, finished_at, updated_at, `+aiTaskDeliveryFailureProjectionSQL+`
		 from ai_tasks t
		 where task_uid = $1
		 limit 1`,
		taskUID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 任务失败")
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
	cfg, err := readAIConfigFromSettings(r.Context(), s.db, s.cfg.SettingsEncryptionKey)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "AI_SETTINGS_UNAVAILABLE", "AI settings are temporarily unavailable", 0, nil)
		return
	}
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
	rawPayload, err := json.Marshal(req.Payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "任务 Payload 格式不正确")
		return
	}
	if _, err = aiTranslationReservationForPayload(req.TaskType, binding, model, rawPayload); err != nil {
		writeError(w, http.StatusBadRequest, "AI translation payload exceeds the model limits or has invalid source items")
		return
	}
	payloadHash := sha256.Sum256(rawPayload)
	req.ConcurrencyKey = strings.Join([]string{req.TaskType, provider.Code, model.Model, req.ConcurrencyKey, hex.EncodeToString(payloadHash[:]), strconv.FormatInt(currentClaims(r).Subject, 10)}, ":")
	taskUID := "ai_" + randomHex(16)
	var taskID int64
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, req.ConcurrencyKey); err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	var existingUID, existingStatus string
	err = tx.QueryRow(r.Context(), `select task_uid,status from ai_tasks where task_type=$1 and concurrency_key=$2
		and status in ('queued','running','retrying') order by created_at desc limit 1`, req.TaskType, req.ConcurrencyKey).Scan(&existingUID, &existingStatus)
	if err == nil {
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"id": existingUID, "taskUid": existingUID, "status": existingStatus})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	err = tx.QueryRow(
		r.Context(),
		`insert into ai_tasks (task_uid, task_type, provider, model, status, priority, concurrency_key, payload, created_by, queued_at)
		 values ($1, $2, $3, $4, 'queued', $5, $6, $7::jsonb, $8, now())
		 returning id`,
		taskUID,
		req.TaskType,
		req.Provider,
		req.Model,
		req.Priority,
		req.ConcurrencyKey,
		string(rawPayload),
		currentClaims(r).Subject,
	).Scan(&taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	message := aiTaskMessage{TaskID: taskID, TaskUID: taskUID, TaskType: req.TaskType}
	if err = enqueueAITaskTx(r.Context(), tx, "ai.task.requested", taskID, taskUID, req.TaskType, r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "AI 任务可靠入队失败")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建 AI 任务失败")
		return
	}
	s.writeAITaskLog(r.Context(), taskID, "info", "task_queued", "AI task committed to the reliable outbox", message)
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
		writeError(w, http.StatusInternalServerError, "读取 AI 统计失败")
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
		writeError(w, http.StatusInternalServerError, "读取 AI 统计失败")
		return
	}
	requestBudget, err := s.querySimpleRows(r, `select payload->>'provider' as provider,payload->>'model' as model,payload->>'state' as state,
		count(*) as requests,coalesce(sum((payload->>'inputTokens')::bigint),0) as input_tokens,
		coalesce(sum((payload->>'outputTokens')::bigint),0) as output_tokens,coalesce(sum((payload->>'costMicros')::bigint),0) as cost_micros,
		coalesce(sum((payload->>'reservedTokens')::bigint),0) as reserved_tokens,
		coalesce(sum((payload->>'reservedCostMicros')::bigint),0) as reserved_cost_micros
		from ai_task_logs where event=$1 and created_at>=now()-interval '30 days'
		group by payload->>'provider',payload->>'model',payload->>'state' order by provider,model,state`, aiRequestBudgetEvent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI 调用预算失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"byStatus":      stats,
		"byProvider":    byProvider,
		"requestBudget": requestBudget,
	})
}

type aiConfigQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readAIConfigFromSettings(ctx context.Context, queryer aiConfigQueryer, encryptionKey string) (aiConfigPayload, error) {
	payload := defaultAIConfig()
	var raw []byte
	err := queryer.QueryRow(ctx, `select value from system_settings where key = 'ai.config'`).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return payload, nil
	}
	if err != nil {
		return aiConfigPayload{}, err
	}
	raw, err = security.DecryptSetting(encryptionKey, raw)
	if err != nil {
		return aiConfigPayload{}, err
	}
	if _, err = decodeStoredJSONObject(raw, "AI settings"); err != nil {
		return aiConfigPayload{}, err
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return aiConfigPayload{}, err
	}
	return normalizeAIConfig(payload), nil
}

func (s *Server) aiConfigFromSettings(ctx context.Context) aiConfigPayload {
	payload, err := readAIConfigFromSettings(ctx, s.db, s.cfg.SettingsEncryptionKey)
	if err != nil {
		// Execution-oriented callers fail closed with disabled providers. Admin
		// reads/writes use the error-returning reader, never a successful fallback.
		return defaultAIConfig()
	}
	return payload
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
			TaskType:       definition.TaskType,
			TimeoutSeconds: definition.TimeoutSeconds,
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
			result[index].TimeoutSeconds = saved.TimeoutSeconds
			result[index].Prompt = strings.TrimSpace(saved.Prompt)
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
