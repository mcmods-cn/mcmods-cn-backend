package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAIResultRequiresCompleteUniqueSourceItems(t *testing.T) {
	source := []byte(`{"sourceLocale":"zh-CN","targetLocale":"en-US","items":[{"key":"name","text":"测试 {name}"},{"key":"summary","text":""}]}`)
	cases := []struct {
		name, body string
		valid      bool
	}{
		{"valid", `{"items":[{"key":"summary","text":""},{"key":"name","text":"Test {name}"}]}`, true},
		{"missing", `{"items":[{"key":"name","text":"Test {name}"}]}`, false},
		{"duplicate item", `{"items":[{"key":"name","text":"Test {name}"},{"key":"name","text":"Test {name}"}]}`, false},
		{"empty", `{"items":[{"key":"name","text":""},{"key":"summary","text":""}]}`, false},
		{"placeholder changed", `{"items":[{"key":"name","text":"Test {username}"},{"key":"summary","text":""}]}`, false},
		{"extra field", `{"items":[{"key":"name","text":"Test {name}","url":"https://invalid.example"},{"key":"summary","text":""}]}`, false},
		{"extra root", `{"items":[{"key":"name","text":"Test {name}"},{"key":"summary","text":""}],"action":"execute"}`, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var result map[string]any
			if err := json.Unmarshal([]byte(test.body), &result); err != nil {
				t.Fatal(err)
			}
			err := validateAITranslationResult(aiTaskContentTranslation, source, result)
			if (err == nil) != test.valid {
				t.Fatalf("validation error=%v valid=%v", err, test.valid)
			}
		})
	}
}

func TestAIJSONRejectsExplanationsDuplicateFieldsAndTrailingData(t *testing.T) {
	for _, body := range []string{
		`Here is your translation: {"items":[]}`,
		`{"items":[]} explanation`,
		`{"items":[],"items":[]}`,
		`{"items":[{"key":"x","text":"first","text":"second"}]}`,
		`{"items":[]} {"items":[]}`,
	} {
		if _, err := parseAIJSONResult(body); err == nil {
			t.Fatalf("invalid response accepted: %s", body)
		}
	}
}

func TestAITranslationProtectsCodeLinksAndRepeatedPlaceholders(t *testing.T) {
	source := "Use {{name}} {count} {count} [docs](https://docs.example/path) and `mod:id`\n```go\nrun()\n```"
	for _, translation := range []string{strings.Replace(source, "https://docs.example/path", "https://attacker.example", 1), strings.Replace(source, "{count} {count}", "{count}", 1), strings.Replace(source, "run()", "deleteAll()", 1), source + " https://attacker.example"} {
		if sameProtectedTranslationParts(source, translation) {
			t.Fatalf("damaged protected parts accepted")
		}
	}
	if !sameProtectedTranslationParts(source, strings.Replace(source, "Use", "使用", 1)) {
		t.Fatal("valid translation rejected")
	}
}

func TestAIProviderMockFailuresAndTruncation(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		delay   time.Duration
		wantErr bool
	}{
		{"normal", 200, `{"choices":[{"message":{"content":"{\"items\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`, 0, false},
		{"invalid json", 200, `{`, 0, true},
		{"no content", 200, `{"choices":[]}`, 0, true},
		{"truncation", 200, `{"choices":[{"message":{"content":"{\"items\":[]}"},"finish_reason":"length"}],"usage":{"prompt_tokens":11,"completion_tokens":7}}`, 0, true},
		{"rate limit", 429, `secret-provider-key`, 0, true},
		{"provider failure", 503, `secret-provider-key`, 0, true},
		{"timeout", 200, `{}`, 100 * time.Millisecond, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.delay > 0 {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(test.delay):
					}
				}
				var payload map[string]any
				if json.NewDecoder(r.Body).Decode(&payload) != nil {
					t.Error("invalid request")
				}
				if test.name == "normal" && payload["max_tokens"] != float64(32) {
					t.Errorf("output limit=%v", payload["max_tokens"])
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			_, usage, err := requestOpenAICompatibleCompletion(ctx, aiProviderConfig{BaseURL: server.URL, APIKey: "synthetic-test-key"}, aiModelConfig{Model: "mock", MaxOutputTokens: 32}, "fixture")
			if (err != nil) != test.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, test.wantErr)
			}
			if err != nil && strings.Contains(err.Error(), "secret-provider-key") {
				t.Fatal("provider body leaked")
			}
			if test.name == "normal" || test.name == "truncation" {
				if usage.InputTokens != 11 || usage.OutputTokens != 7 {
					t.Fatalf("billed usage lost: %+v", usage)
				}
			}
		})
	}
}

func TestAITranslationPayloadRejectsUnboundedOrInvalidInput(t *testing.T) {
	for _, payload := range []string{`{"sourceLocale":"zh-CN","targetLocale":"!invalid","items":[{"key":"x","text":"x"}]}`, `{"sourceLocale":"zh-CN","targetLocale":"en","items":[{"key":"x","text":"x"},{"key":"x","text":"y"}]}`, `{"sourceLocale":"zh-CN","targetLocale":"en","items":[{"key":"x"}]}`, strings.Repeat("x", 1<<20+1)} {
		if validateAITranslationPayload(aiTaskI18nTranslation, []byte(payload)) == nil {
			t.Fatal("invalid payload accepted")
		}
	}
}

func TestAIProviderURLRejectsCredentialBearingOrUnsupportedURLs(t *testing.T) {
	for _, endpoint := range []string{"file:///private", "https://user:secret@api.example/v1/chat", "https://api.example/v1/chat?key=secret", "https://api.example/v1/chat#secret"} {
		if err := sendAIJSONRequest(context.Background(), endpoint, nil, map[string]string{}, &aiCompletionResponse{}); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("unsafe URL accepted or credential leaked: %v", err)
		}
	}
}

func TestAIGlossarySnapshotIsBoundedAndSentAsDataWithoutTaskMetadata(t *testing.T) {
	cfg := defaultAIConfig()
	cfg.Translation.Glossary = "模组 = mod; {name} remains literal"
	payload := map[string]any{"sourceLocale": "zh-CN", "targetLocale": "en-US", "notificationId": 123, "quotaReservedCostMicros": 999, "items": []map[string]string{{"key": "name", "text": "模组"}}}
	if err := freezeAITranslationContext(payload, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(payload)
	prompt, err := buildTranslationPrompt(aiTaskNotificationTranslation, "", raw)
	if err != nil || !strings.Contains(prompt, cfg.Translation.Glossary) || strings.Contains(prompt, "notificationId") || strings.Contains(prompt, "quotaReservedCostMicros") {
		t.Fatalf("glossary or supplier data boundary failed: %v %s", err, prompt)
	}
	oldKey := aiTranslationContextKey("task", cfg)
	cfg.Translation.Glossary = "模组 = plugin"
	if aiTranslationContextKey("task", cfg) == oldKey {
		t.Fatal("different glossary reused task fingerprint")
	}
	payload["glossary"] = cfg.Translation.Glossary
	raw, _ = json.Marshal(payload)
	if validateAITranslationPayload(aiTaskNotificationTranslation, raw) == nil {
		t.Fatal("edited glossary snapshot accepted without matching fingerprint")
	}
	cfg.Translation.Glossary = strings.Repeat("x", 1025)
	if freezeAITranslationContext(payload, cfg) == nil {
		t.Fatal("unbounded glossary accepted")
	}
}

func TestAIConfigurationRejectsUnenforcedQuotaScopes(t *testing.T) {
	for _, quota := range []aiQuotaConfig{{Scope: "user", Subject: "123", Period: "day"}, {Scope: "role", Subject: "user", Period: "day"}, {Scope: "site", Subject: "other", Period: "day"}, {Scope: "site", Period: "year"}, {Scope: "site", Period: "day", TokenLimit: -1}} {
		if validateAIQuotaConfiguration(aiConfigPayload{Quotas: []aiQuotaConfig{quota}}) == nil {
			t.Fatalf("unenforced quota accepted: %+v", quota)
		}
	}
	if err := validateAIQuotaConfiguration(defaultAIConfig()); err != nil {
		t.Fatal(err)
	}
}
