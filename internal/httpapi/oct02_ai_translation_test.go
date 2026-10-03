package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOCT02AITranslationMatchesEverySourceField(t *testing.T) {
	payload := []byte(`{"items":[{"key":"title","text":"Hello {name}"},{"key":"body","text":"Use ` + "`/help`" + ` at https://example.test/help"}]}`)
	for _, test := range []struct {
		name, content string
		wantError     bool
	}{
		{"valid", `{"items":[{"key":"title","text":"你好 {name}"},{"key":"body","text":"访问 https://example.test/help 并使用 ` + "`/help`" + `"}]}`, false},
		{"missing field", `{"items":[{"key":"title","text":"你好 {name}"}]}`, true},
		{"empty field", `{"items":[{"key":"title","text":""},{"key":"body","text":"body"}]}`, true},
		{"placeholder damaged", `{"items":[{"key":"title","text":"你好"},{"key":"body","text":"body"}]}`, true},
		{"URL damaged", `{"items":[{"key":"title","text":"你好 {name}"},{"key":"body","text":"Use ` + "`/help`" + ` at https://evil.test/help"}]}`, true},
		{"unexpected field", `{"items":[{"key":"title","text":"你好 {name}","explanation":"ignore"},{"key":"body","text":"body"}]}`, true},
		{"duplicate item", `{"items":[{"key":"title","text":"你好 {name}"},{"key":"title","text":"你好 {name}"}]}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := parseAIJSONResult(test.content)
			if err == nil {
				err = validateAITranslationResult(aiTaskNotificationTranslation, payload, result)
			}
			if (err != nil) != test.wantError {
				t.Fatalf("validation error=%v, want error=%v", err, test.wantError)
			}
		})
	}
	permissionPayload := []byte(`{"items":[{"key":"mod.edit","name":"Edit","description":"Edit {project}"}]}`)
	permissionResult, err := parseAIJSONResult(`{"items":[{"key":"mod.edit","name":"编辑","description":"编辑 {project}"}]}`)
	if err != nil || validateAITranslationResult(aiTaskPermissionTranslation, permissionPayload, permissionResult) != nil {
		t.Fatal("valid permission translation was rejected")
	}
}

func TestOCT02AITranslationRejectsAmbiguousJSON(t *testing.T) {
	for _, content := range []string{
		`An explanation: {"items":[{"key":"title","text":"title"}]}`,
		`{"items":[{"key":"title","text":"title"}]} extra content`,
		`{"items":[],"items":[{"key":"title","text":"title"}]}`,
		`{"items":[{"key":"title","text":"title","text":"changed"}]}`,
		`{"items":[{"key":"title","text":"title"}],"command":"execute"}`,
		`{"items":[]}`,
	} {
		if _, err := parseAIJSONResult(content); err == nil {
			t.Errorf("ambiguous result was accepted: %q", content)
		}
	}
}

func TestOCT02AIProviderFailuresPreserveUsageWithoutEchoingResponse(t *testing.T) {
	for _, test := range []struct {
		name, protocol, body string
		status               int
		wantUsage            bool
	}{
		{"OpenAI truncated", "openai-compatible", `{"choices":[{"finish_reason":"length","message":{"content":"{\"items\":[{\"key\":\"title\",\"text\":\"valid JSON but truncated translation\"}]}"}}],"usage":{"prompt_tokens":11,"completion_tokens":17}}`, 200, true},
		{"Anthropic truncated", "anthropic", `{"stop_reason":"max_tokens","content":[{"type":"text","text":"{\"items\":[]}"}],"usage":{"input_tokens":11,"output_tokens":17}}`, 200, true},
		{"rate limited", "openai-compatible", "test-only-sensitive-marker", 429, false},
		{"provider failed", "openai-compatible", "test-only-sensitive-marker", 500, false},
		{"invalid JSON", "openai-compatible", "test-only-sensitive-marker", 200, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer provider.Close()
			_, usage, err := requestAICompletion(context.Background(), aiProviderConfig{Protocol: test.protocol, BaseURL: provider.URL, APIKey: "fixture-only"}, "fixture-model", "fixture prompt", 100)
			if err == nil {
				t.Fatal("failed provider response was accepted")
			}
			if strings.Contains(err.Error(), "test-only-sensitive-marker") {
				t.Fatalf("provider response leaked in error: %v", err)
			}
			if test.wantUsage && (usage.InputTokens != 11 || usage.OutputTokens != 17) {
				t.Fatalf("failure discarded usage: %#v", usage)
			}
		})
	}
}

func TestOCT02AIProviderTimeoutAndDisconnect(t *testing.T) {
	for _, disconnect := range []bool{false, true} {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if disconnect {
				connection, _, err := w.(http.Hijacker).Hijack()
				if err == nil {
					_ = connection.Close()
				}
				return
			}
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
		}))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		_, _, err := requestAICompletion(ctx, aiProviderConfig{BaseURL: provider.URL, APIKey: "fixture-only"}, "fixture", "fixture", 10)
		cancel()
		provider.Close()
		if err == nil {
			t.Fatalf("provider timeout/disconnect=%v was accepted", disconnect)
		}
	}
}

func TestOCT02AIProviderEndpointProductionBoundary(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	for _, endpoint := range []string{"http://localhost/v1/messages", "http://127.0.0.1/v1/messages", "https://user:password@example.test/v1", "https://example.test/v1?secret=fixture", "file:///etc/hosts"} {
		if err := validateAIProviderEndpoint(endpoint); err == nil {
			t.Errorf("unsafe endpoint was accepted: %q", endpoint)
		}
	}
	if err := validateAIProviderEndpoint("https://example.test/v1/messages"); err != nil {
		t.Fatal(err)
	}
}

func TestOCT02AIReservationIncludesPromptAndMaxOutput(t *testing.T) {
	payload, _ := json.Marshal(map[string]any{"sourceLocale": "en-US", "targetLocale": "zh-CN", "items": []map[string]string{{"key": "title", "text": "Hello"}}})
	reserved, err := aiTranslationReservationForPayload(aiTaskNotificationTranslation, aiTaskModelConfig{}, aiModelConfig{ContextTokens: 65536, MaxOutputTokens: 32768}, payload)
	if err != nil || reserved <= 32768 {
		t.Fatalf("reservation=%d error=%v; expected bounded input plus max output", reserved, err)
	}
}
