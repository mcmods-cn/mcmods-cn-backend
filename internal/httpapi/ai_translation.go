package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

type anthropicCompletionResponse struct {
	Content []struct {
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
	return preparedAITask{
		provider: provider, model: model, prompt: prompt, timeout: timeout,
		maxOutputTokens: maxOutputTokens, reservationTokens: reservationTokens,
	}, nil
}

func (worker *AIWorker) executePreparedTask(ctx context.Context, prepared preparedAITask) (map[string]any, aiTaskUsage, error) {
	requestContext, cancel := context.WithTimeout(ctx, prepared.timeout)
	defer cancel()
	content, usage, err := requestAICompletion(
		requestContext, prepared.provider, prepared.model.Model, prepared.prompt, prepared.maxOutputTokens,
	)
	usage.CostMicros = calculateAICostMicros(prepared.model, usage)
	if err != nil {
		return nil, usage, err
	}
	result, err := parseAIJSONResult(content)
	if err != nil {
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
	if len(parts) == 0 {
		return "", usage, errors.New("AI provider returned no translated content")
	}
	return strings.Join(parts, "\n"), usage, nil
}

func sendAIJSONRequest(ctx context.Context, endpoint string, headers map[string]string, payload any, target any) error {
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
	client, err := newProviderHTTPClient(45*time.Second, endpoint)
	if err != nil {
		return fmt.Errorf("AI provider endpoint is invalid: %w", err)
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("AI provider request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("AI provider returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
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
	start := strings.IndexByte(content, '{')
	end := strings.LastIndexByte(content, '}')
	if start < 0 || end < start {
		return nil, errors.New("AI provider did not return a JSON object")
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(content[start:end+1]), &result); err != nil {
		return nil, fmt.Errorf("AI translation JSON is invalid: %w", err)
	}
	if _, ok := result["items"].([]any); !ok {
		return nil, errors.New("AI translation result has no items array")
	}
	return result, nil
}

func calculateAICostMicros(model aiModelConfig, usage aiTaskUsage) int64 {
	inputCost := float64(usage.InputTokens) * model.InputPricePerMillion
	outputCost := float64(usage.OutputTokens) * model.OutputPricePerMillion
	return int64(inputCost + outputCost)
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
	_, err = worker.db.Exec(
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
	return err
}
