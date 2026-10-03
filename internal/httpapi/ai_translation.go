package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
)

const (
	maxAIProviderResponseBytes         = int64(8 << 20)
	maxAITranslationPromptBytes        = 1 << 20
	maxAITranslationOutputTokens       = 32768
	aiTranslationRequestOverheadTokens = int64(1024)
	aiTranslationSystemPrompt          = "You are a precise localization translator. Respond with JSON only."
)

type aiTaskUsage struct {
	InputTokens  int64
	OutputTokens int64
	CostMicros   int64
}

type aiCompletionResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

type anthropicCompletionResponse struct {
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

type preparedAITask struct {
	provider          aiProviderConfig
	model             aiModelConfig
	prompt            string
	timeout           time.Duration
	maxOutputTokens   int
	reservationTokens int64
	taskType          string
	rawPayload        []byte
	quotas            []aiQuotaConfig
}

func (worker *AIWorker) executeTask(
	ctx context.Context,
	taskType string,
	providerCode string,
	modelID string,
	rawPayload []byte,
) (map[string]any, aiTaskUsage, error) {
	prepared, err := worker.prepareTask(ctx, taskType, providerCode, modelID, rawPayload)
	if err != nil {
		return nil, aiTaskUsage{}, err
	}
	return worker.executePreparedTask(ctx, prepared)
}

func (worker *AIWorker) prepareTask(
	ctx context.Context,
	taskType string,
	providerCode string,
	modelID string,
	rawPayload []byte,
) (preparedAITask, error) {
	if taskType != aiTaskPermissionTranslation && taskType != aiTaskI18nTranslation &&
		taskType != aiTaskNotificationTranslation && taskType != aiTaskContentTranslation {
		return preparedAITask{}, fmt.Errorf("unsupported AI task type: %s", taskType)
	}
	cfg := aiConfigFromDatabase(ctx, worker.db, worker.settingsEncryptionKey)
	provider, model, ok := resolveAIModel(cfg, providerCode+"/"+modelID)
	if !ok {
		return preparedAITask{}, errors.New("AI task provider or model is unavailable")
	}
	binding, ok := findAITaskModel(cfg.TaskModels, taskType)
	if !ok {
		return preparedAITask{}, errors.New("AI task model binding is missing")
	}
	timeout := time.Duration(binding.TimeoutSeconds) * time.Second
	if binding.TimeoutSeconds > 600 {
		return preparedAITask{}, errors.New("AI task timeout cannot exceed 600 seconds")
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	prompt, err := buildTranslationPrompt(taskType, binding.Prompt, rawPayload)
	if err != nil {
		return preparedAITask{}, err
	}
	maxOutputTokens, reservationTokens, err := aiTranslationTokenReservation(model, prompt)
	if err != nil {
		return preparedAITask{}, err
	}
	if run, claimed := ctx.Value(aiTaskExecutionKey{}).(aiTaskExecution); claimed {
		var queuedReservation int64
		if err = worker.db.QueryRow(ctx, `select quota_reserved_tokens from ai_tasks where id=$1 and status='running' and started_at=$2`, run.ID, run.Started).Scan(&queuedReservation); err != nil {
			return preparedAITask{}, err
		}
		if queuedReservation > 0 && reservationTokens > queuedReservation {
			inputReservation := reservationTokens - int64(maxOutputTokens)
			if queuedReservation <= inputReservation {
				return preparedAITask{}, errors.New("AI prompt no longer fits its admitted token reservation")
			}
			maxOutputTokens = int(queuedReservation - inputReservation)
			reservationTokens = queuedReservation
		}
	}
	return preparedAITask{
		provider: provider, model: model, prompt: prompt, timeout: timeout,
		maxOutputTokens: maxOutputTokens, reservationTokens: reservationTokens,
		taskType: taskType, rawPayload: rawPayload,
		quotas: cfg.Quotas,
	}, nil
}

func (worker *AIWorker) executePreparedTask(ctx context.Context, prepared preparedAITask) (map[string]any, aiTaskUsage, error) {
	ledgerID, budget, err := worker.reserveProviderRequestBudget(ctx, prepared)
	if err != nil {
		return nil, aiTaskUsage{}, err
	}
	requestContext, cancel := context.WithTimeout(ctx, prepared.timeout)
	defer cancel()
	content, usage, err := requestAICompletion(
		requestContext, prepared.provider, prepared.model.Model, prepared.prompt, prepared.maxOutputTokens,
	)
	if usage.InputTokens > prepared.reservationTokens-int64(prepared.maxOutputTokens) || usage.OutputTokens > int64(prepared.maxOutputTokens) {
		usage = aiTaskUsage{}
		err = errors.New("AI provider reported token usage beyond the admitted request limits")
	}
	usage.CostMicros = calculateAICostMicros(prepared.model, usage)
	if settleErr := worker.settleProviderRequestBudget(ctx, ledgerID, budget, usage); settleErr != nil {
		return nil, usage, errors.Join(err, fmt.Errorf("settle AI provider request budget: %w", settleErr))
	}
	if run, ok := ctx.Value(aiTaskExecutionKey{}).(aiTaskExecution); ok {
		if _, writeErr := worker.db.Exec(ctx, `update ai_tasks set input_tokens=input_tokens+$3,output_tokens=output_tokens+$4,cost_micros=cost_micros+$5
			where id=$1 and status='running' and started_at=$2`, run.ID, run.Started, usage.InputTokens, usage.OutputTokens, usage.CostMicros); writeErr != nil {
			return nil, usage, errors.Join(err, fmt.Errorf("persist AI provider usage: %w", writeErr))
		}
	}
	if err != nil {
		return nil, usage, err
	}
	result, err := parseAIJSONResult(content)
	if err != nil {
		return nil, usage, err
	}
	if err = validateAITranslationResult(prepared.taskType, prepared.rawPayload, result); err != nil {
		return nil, usage, err
	}
	return result, usage, nil
}

func aiTranslationTokenReservation(model aiModelConfig, prompt string) (int, int64, error) {
	promptBytes := len([]byte(prompt))
	if promptBytes > maxAITranslationPromptBytes {
		return 0, 0, fmt.Errorf("AI translation prompt exceeds %d bytes", maxAITranslationPromptBytes)
	}
	if model.ContextTokens <= 0 || model.MaxOutputTokens <= 0 {
		return 0, 0, errors.New("AI model token limits are unavailable")
	}
	inputReservation := int64(promptBytes) + aiTranslationRequestOverheadTokens
	availableOutput := int64(model.ContextTokens) - inputReservation
	if availableOutput <= 0 {
		return 0, 0, errors.New("AI translation prompt exceeds the model context limit")
	}
	maxOutputTokens := model.MaxOutputTokens
	if maxOutputTokens > maxAITranslationOutputTokens {
		maxOutputTokens = maxAITranslationOutputTokens
	}
	if int64(maxOutputTokens) > availableOutput {
		maxOutputTokens = int(availableOutput)
	}
	if maxOutputTokens <= 0 {
		return 0, 0, errors.New("AI model has no output capacity for the translation prompt")
	}
	return maxOutputTokens, inputReservation + int64(maxOutputTokens), nil
}

func aiConfigFromDatabase(ctx context.Context, db *pgxpool.Pool, settingsEncryptionKey string) aiConfigPayload {
	payload := defaultAIConfig()
	var raw []byte
	if err := db.QueryRow(ctx, `select value from system_settings where key = 'ai.config'`).Scan(&raw); err != nil {
		return payload
	}
	raw, err := security.DecryptSetting(settingsEncryptionKey, raw)
	if err != nil {
		return defaultAIConfig()
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return defaultAIConfig()
	}
	return normalizeAIConfig(payload)
}

func buildTranslationPrompt(taskType string, customPrompt string, rawPayload []byte) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return "", errors.New("AI translation payload is invalid")
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) == 0 {
		return "", errors.New("AI translation payload has no items")
	}
	if len(items) > 100 {
		return "", errors.New("AI translation batch cannot exceed 100 items")
	}
	validationItems := make([]any, len(items))
	fields := []string{"key", "text"}
	if taskType == aiTaskPermissionTranslation {
		fields = []string{"key", "name", "description"}
	}
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return "", errors.New("AI translation source item is not an object")
		}
		projected := make(map[string]any, len(fields))
		for _, field := range fields {
			projected[field] = item[field]
		}
		validationItems[index] = projected
	}
	if err := validateAITranslationResult(taskType, rawPayload, map[string]any{"items": validationItems}); err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	shape := `{"items":[{"key":"original key","text":"translated text"}]}`
	if taskType == aiTaskPermissionTranslation {
		shape = `{"items":[{"key":"permission code","name":"translated name","description":"translated description"}]}`
	}
	prompt := "Translate the supplied mcmods.cn interface content from sourceLocale to targetLocale. " +
		"Preserve placeholders such as {name}, Markdown, permission codes, product names, URLs, punctuation intent, and Minecraft terminology. " +
		"Do not translate object keys. Return valid JSON only, with exactly this shape: " + shape + "."
	if customPrompt = strings.TrimSpace(customPrompt); customPrompt != "" {
		prompt += " Additional administrator instructions: " + customPrompt
	}
	return prompt + " Input JSON: " + string(payloadJSON), nil
}

func requestAICompletion(
	ctx context.Context,
	provider aiProviderConfig,
	model string,
	prompt string,
	maxOutputTokens int,
) (string, aiTaskUsage, error) {
	if provider.Protocol == "anthropic" {
		return requestAnthropicCompletion(ctx, provider, model, prompt, maxOutputTokens)
	}
	return requestOpenAICompatibleCompletion(ctx, provider, model, prompt, maxOutputTokens)
}

func requestOpenAICompatibleCompletion(
	ctx context.Context,
	provider aiProviderConfig,
	model string,
	prompt string,
	maxOutputTokens int,
) (string, aiTaskUsage, error) {
	body := map[string]any{
		"model":      model,
		"max_tokens": maxOutputTokens,
		"messages": []map[string]string{
			{"role": "system", "content": aiTranslationSystemPrompt},
			{"role": "user", "content": prompt},
		},
		"temperature": 0.1,
	}
	var response aiCompletionResponse
	if err := sendAIJSONRequest(
		ctx,
		completionEndpoint(provider.BaseURL, "chat/completions"),
		map[string]string{"Authorization": "Bearer " + provider.APIKey},
		body,
		&response,
	); err != nil {
		return "", aiTaskUsage{}, err
	}
	usage := aiTaskUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}
	if response.Usage.PromptTokens < 0 || response.Usage.CompletionTokens < 0 {
		return "", aiTaskUsage{}, errors.New("AI provider returned invalid token usage")
	}
	if len(response.Choices) > 0 && response.Choices[0].FinishReason != "" && response.Choices[0].FinishReason != "stop" {
		return "", usage, errors.New("AI provider did not finish the translation normally")
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", usage, errors.New("AI provider returned no translated content")
	}
	return response.Choices[0].Message.Content, usage, nil
}

func requestAnthropicCompletion(
	ctx context.Context,
	provider aiProviderConfig,
	model string,
	prompt string,
	maxOutputTokens int,
) (string, aiTaskUsage, error) {
	body := map[string]any{
		"model":       model,
		"max_tokens":  maxOutputTokens,
		"system":      aiTranslationSystemPrompt,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0.1,
	}
	var response anthropicCompletionResponse
	if err := sendAIJSONRequest(
		ctx,
		completionEndpoint(provider.BaseURL, "messages"),
		map[string]string{
			"x-api-key":         provider.APIKey,
			"anthropic-version": "2023-06-01",
		},
		body,
		&response,
	); err != nil {
		return "", aiTaskUsage{}, err
	}
	parts := make([]string, 0, len(response.Content))
	for _, item := range response.Content {
		if item.Type == "text" && strings.TrimSpace(item.Text) != "" {
			parts = append(parts, item.Text)
		}
	}
	usage := aiTaskUsage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}
	if response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 {
		return "", aiTaskUsage{}, errors.New("AI provider returned invalid token usage")
	}
	if response.StopReason != "" && response.StopReason != "end_turn" && response.StopReason != "stop_sequence" {
		return "", usage, errors.New("AI provider did not finish the translation normally")
	}
	if len(parts) == 0 {
		return "", usage, errors.New("AI provider returned no translated content")
	}
	return strings.Join(parts, "\n"), usage, nil
}

func sendAIJSONRequest(ctx context.Context, endpoint string, headers map[string]string, payload any, target any) error {
	if err := validateAIProviderEndpoint(endpoint); err != nil {
		return err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	timeout := 45 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout <= 0 {
			return ctx.Err()
		}
	}
	client, err := newProviderHTTPClient(timeout, endpoint)
	if err != nil {
		return fmt.Errorf("AI provider endpoint is invalid: %w", err)
	}
	response, err := client.Do(req)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return fmt.Errorf("AI provider request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// Provider error bodies can echo credentials or source content. Persist only
		// the status, never untrusted response text in task errors and application logs.
		return fmt.Errorf("AI provider returned HTTP %d", response.StatusCode)
	}
	rawResponse, err := io.ReadAll(io.LimitReader(response.Body, maxAIProviderResponseBytes+1))
	if err != nil {
		return fmt.Errorf("AI provider response could not be read: %w", err)
	}
	if int64(len(rawResponse)) > maxAIProviderResponseBytes {
		return errors.New("AI provider response is too large")
	}
	if err := json.Unmarshal(rawResponse, target); err != nil {
		return fmt.Errorf("AI provider response is invalid: %w", err)
	}
	return nil
}

func validateAIProviderEndpoint(endpoint string) error {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("AI provider endpoint must be an HTTP(S) URL without credentials, query, or fragment")
	}
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		address := net.ParseIP(parsed.Hostname())
		if strings.EqualFold(parsed.Hostname(), "localhost") || (address != nil && address.IsLoopback()) {
			return errors.New("AI provider loopback endpoints are unavailable in production")
		}
	}
	if address := net.ParseIP(parsed.Hostname()); address != nil && !address.IsLoopback() &&
		(address.IsPrivate() || address.IsUnspecified() || address.IsLinkLocalUnicast() || address.IsMulticast()) {
		return errors.New("AI provider private network endpoints are unavailable")
	}
	return nil
}

func completionEndpoint(baseURL string, operation string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL + "/" + operation
	}
	return baseURL + "/v1/" + operation
}

func parseAIJSONResult(content string) (map[string]any, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)
	decoded, err := decodeUniqueAIJSON(content)
	if err != nil {
		return nil, fmt.Errorf("AI translation JSON is invalid: %w", err)
	}
	result, ok := decoded.(map[string]any)
	if !ok || len(result) != 1 {
		return nil, errors.New("AI translation result must contain only an items array")
	}
	if items, ok := result["items"].([]any); !ok || len(items) == 0 || len(items) > 100 {
		return nil, errors.New("AI translation result has no items array")
	}
	return result, nil
}

func calculateAICostMicros(model aiModelConfig, usage aiTaskUsage) int64 {
	inputCost := float64(usage.InputTokens) * model.InputPricePerMillion
	outputCost := float64(usage.OutputTokens) * model.OutputPricePerMillion
	return int64(math.Ceil(inputCost + outputCost))
}

type notificationTranslationPayload struct {
	NotificationID int64  `json:"notificationId"`
	TargetLocale   string `json:"targetLocale"`
}

func decodeNotificationTranslation(rawPayload []byte, result map[string]any) (notificationTranslationPayload, map[string]string, error) {
	var payload notificationTranslationPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return notificationTranslationPayload{}, nil, fmt.Errorf("decode notification translation payload: %w", err)
	}
	var supported bool
	payload.TargetLocale, supported = normalizeNotificationTranslationLocale(payload.TargetLocale)
	if payload.NotificationID <= 0 || !supported {
		return notificationTranslationPayload{}, nil, errors.New("notification translation payload is invalid")
	}
	translated, err := strictTranslationItemsToMap(result, stringSet("title", "body"))
	if err != nil {
		return notificationTranslationPayload{}, nil, fmt.Errorf("decode notification translation result: %w", err)
	}
	if translated["title"] == "" && translated["body"] == "" {
		return notificationTranslationPayload{}, nil, errors.New("notification translation result is empty")
	}
	return payload, translated, nil
}

func (worker *AIWorker) persistNotificationTranslation(ctx context.Context, userID int64, rawPayload []byte, result map[string]any) error {
	if userID <= 0 {
		return errors.New("notification translation user is invalid")
	}
	payload, translated, err := decodeNotificationTranslation(rawPayload, result)
	if err != nil {
		return fmt.Errorf("validate notification translation: %w", err)
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = lockAITaskExecutionTx(ctx, tx); err != nil {
		return err
	}
	if _, claimed := ctx.Value(aiTaskExecutionKey{}).(aiTaskExecution); claimed {
		var source struct {
			Items []struct {
				Key  string `json:"key"`
				Text string `json:"text"`
			} `json:"items"`
		}
		if err = json.Unmarshal(rawPayload, &source); err != nil {
			return err
		}
		fields := make(map[string]string, len(source.Items))
		for _, item := range source.Items {
			fields[item.Key] = item.Text
		}
		var title, body string
		if err = tx.QueryRow(ctx, `select title,body from notifications
			where id=$1 and (recipient_id is null or recipient_id=$2) and kind<>'system' for share`, payload.NotificationID, userID).Scan(&title, &body); err != nil {
			return fmt.Errorf("load notification translation source: %w", err)
		}
		if fields["title"] != title || fields["body"] != body {
			return errors.New("notification source changed while translation was running")
		}
	}
	_, err = tx.Exec(
		ctx,
		`insert into notification_translations (notification_id, user_id, locale, title, body)
		 values ($1, $2, $3, $4, $5)
		 on conflict (notification_id, user_id, locale) do update
		 set title = excluded.title, body = excluded.body, created_at = now()`,
		payload.NotificationID,
		userID,
		payload.TargetLocale,
		translated["title"],
		translated["body"],
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
