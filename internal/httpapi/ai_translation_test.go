package httpapi

import (
	"strings"
	"testing"
)

func TestNormalizeAITaskModelsUsesRegisteredTasks(t *testing.T) {
	models := normalizeAITaskModels([]aiTaskModelConfig{
		{TaskType: aiTaskI18nTranslation, ModelKey: "openai/test", ConcurrencyLimit: 5, TimeoutSeconds: 90, Prompt: "Use concise wording"},
		{TaskType: "manually_added", ModelKey: "openai/ignored", ConcurrencyLimit: 9, TimeoutSeconds: 9},
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
	if models[1].ConcurrencyLimit != 5 || models[1].TimeoutSeconds != 90 {
		t.Fatalf("saved task limits were not preserved: %#v", models[1])
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
