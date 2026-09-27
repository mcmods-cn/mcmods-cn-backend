package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAITaskModelConfigurationDoesNotExposeUnenforcedConcurrencyLimit(t *testing.T) {
	if _, exposed := reflect.TypeOf(aiTaskModelConfig{}).FieldByName("ConcurrencyLimit"); exposed {
		t.Fatal("per-task AI concurrency is exposed even though only the NATS ai worker limit is enforced")
	}
	raw, err := json.Marshal(defaultAIConfig())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"concurrencyLimit"`)) {
		t.Fatalf("AI configuration still serializes an unenforced concurrency limit: %s", raw)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/admin/ai/config", strings.NewReader(`{"taskModels":[{"taskType":"i18n_translation_completion","concurrencyLimit":2}]}`))
	var payload aiConfigPayload
	if err = decodeJSON(request, &payload); err == nil {
		t.Fatal("the removed per-task concurrency field must not remain as a hidden write contract")
	}
}

func TestNormalizeAITaskModelsUsesRegisteredTasks(t *testing.T) {
	models := normalizeAITaskModels([]aiTaskModelConfig{
		{TaskType: aiTaskI18nTranslation, ModelKey: "openai/test", TimeoutSeconds: 90, Prompt: "Use concise wording"},
		{TaskType: "manually_added", ModelKey: "openai/ignored", TimeoutSeconds: 9},
	})
	if len(models) != len(registeredAITaskDefinitions) {
		t.Fatalf("task model count = %d, want %d", len(models), len(registeredAITaskDefinitions))
	}
	if models[0].TaskType != aiTaskPermissionTranslation {
		t.Fatalf("first task type = %q", models[0].TaskType)
	}
	if models[1].TaskType != aiTaskI18nTranslation || models[1].ModelKey != "openai/test" {
		t.Fatalf("saved task binding was not preserved: %#v", models[1])
	}
	if models[1].TimeoutSeconds != 90 {
		t.Fatalf("saved task timeout was not preserved: %#v", models[1])
	}
	if models[1].Prompt != "Use concise wording" {
		t.Fatalf("saved task prompt was not preserved: %#v", models[1])
	}
}

func TestParseAIJSONResultAcceptsCodeFence(t *testing.T) {
	result, err := parseAIJSONResult("```json\n{\"items\":[{\"key\":\"home.title\",\"text\":\"Home\"}]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	items, ok := result["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", result["items"])
	}
}

func TestBuildTranslationPromptIncludesAdministratorInstructions(t *testing.T) {
	prompt, err := buildTranslationPrompt(
		aiTaskI18nTranslation,
		"Use concise Minecraft terminology",
		[]byte(`{"sourceLocale":"zh-CN","targetLocale":"en","items":[{"key":"test","text":"测试"}]}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "Use concise Minecraft terminology") {
		t.Fatalf("custom prompt was not included: %s", prompt)
	}
}

func TestCompletionEndpoint(t *testing.T) {
	tests := map[string]string{
		"https://api.openai.com/v1": "https://api.openai.com/v1/chat/completions",
		"https://example.com":       "https://example.com/v1/chat/completions",
	}
	for baseURL, want := range tests {
		if got := completionEndpoint(baseURL, "chat/completions"); got != want {
			t.Fatalf("completionEndpoint(%q) = %q, want %q", baseURL, got, want)
		}
	}
}
