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

func (worker *AIWorker) executeTask(
	ctx context.Context,
	taskType string,
	providerCode string,
	modelID string,
	rawPayload []byte,
) (map[string]any, aiTaskUsage, error) {
	if taskType != aiTaskPermissionTranslation && taskType != aiTaskI18nTranslation &&
		taskType != aiTaskNotificationTranslation && taskType != aiTaskContentTranslation {
		return nil, aiTaskUsage{}, fmt.Errorf("unsupported AI task type: %s", taskType)
	}
	cfg := aiConfigFromDatabase(ctx, worker.db)
	provider, model, ok := resolveAIModel(cfg, providerCode+"/"+modelID)
	if !ok {
		return nil, aiTaskUsage{}, errors.New("AI task provider or model is unavailable")
	}
	binding, ok := findAITaskModel(cfg.TaskModels, taskType)
	if !ok {
		return nil, aiTaskUsage{}, errors.New("AI task model binding is missing")
	}
	timeout := time.Duration(binding.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt, err := buildTranslationPrompt(taskType, binding.Prompt, rawPayload)
	if err != nil {
		return nil, aiTaskUsage{}, err
	}
	content, usage, err := requestAICompletion(requestContext, provider, model.Model, prompt)
	if err != nil {
		return nil, aiTaskUsage{}, err
	}
	result, err := parseAIJSONResult(content)
	if err != nil {
		return nil, aiTaskUsage{}, err
	}
	usage.CostMicros = calculateAICostMicros(model, usage)
	return result, usage, nil
}

func aiConfigFromDatabase(ctx context.Context, db *pgxpool.Pool) aiConfigPayload {
	payload := defaultAIConfig()
	var raw []byte
	if err := db.QueryRow(ctx, `select value from system_settings where key = 'ai.config'`).Scan(&raw); err != nil {
		return payload
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
) (string, aiTaskUsage, error) {
	if provider.Protocol == "anthropic" {
		return requestAnthropicCompletion(ctx, provider, model, prompt)
	}
	return requestOpenAICompatibleCompletion(ctx, provider, model, prompt)
}

func requestOpenAICompatibleCompletion(
	ctx context.Context,
	provider aiProviderConfig,
	model string,
	prompt string,
) (string, aiTaskUsage, error) {
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are a precise localization translator. Respond with JSON only."},
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
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", aiTaskUsage{}, errors.New("AI provider returned no translated content")
	}
	return response.Choices[0].Message.Content, aiTaskUsage{
		InputTokens:  response.Usage.PromptTokens,
		OutputTokens: response.Usage.CompletionTokens,
	}, nil
}

func requestAnthropicCompletion(
	ctx context.Context,
	provider aiProviderConfig,
	model string,
	prompt string,
) (string, aiTaskUsage, error) {
	body := map[string]any{
		"model":       model,
		"max_tokens":  8192,
		"system":      "You are a precise localization translator. Respond with JSON only.",
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
	if len(parts) == 0 {
		return "", aiTaskUsage{}, errors.New("AI provider returned no translated content")
	}
	return strings.Join(parts, "\n"), aiTaskUsage{
		InputTokens:  response.Usage.InputTokens,
		OutputTokens: response.Usage.OutputTokens,
	}, nil
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
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("AI provider request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("AI provider returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
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

func (worker *AIWorker) persistNotificationTranslation(ctx context.Context, userID int64, rawPayload []byte, result map[string]any) {
	var payload struct {
		NotificationID int64  `json:"notificationId"`
		TargetLocale   string `json:"targetLocale"`
	}
	if json.Unmarshal(rawPayload, &payload) != nil || payload.NotificationID <= 0 || payload.TargetLocale == "" {
		return
	}
	translated := map[string]string{}
	items, _ := result["items"].([]any)
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		key, _ := item["key"].(string)
		text, _ := item["text"].(string)
		if key != "" && text != "" {
			translated[key] = text
		}
	}
	if translated["title"] == "" && translated["body"] == "" {
		return
	}
	_, _ = worker.db.Exec(
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
}
