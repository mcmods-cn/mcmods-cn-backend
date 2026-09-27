package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSEC016AICompletionAppliesConfiguredOutputLimit(t *testing.T) {
	for _, protocol := range []string{"openai-compatible", "anthropic"} {
		protocol := protocol
		t.Run(protocol, func(t *testing.T) {
			var maxTokens int
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				var body struct {
					MaxTokens int `json:"max_tokens"`
				}
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Errorf("decode request: %v", err)
					return
				}
				maxTokens = body.MaxTokens
				response.Header().Set("Content-Type", "application/json")
				if protocol == "anthropic" {
					_, _ = response.Write([]byte(`{"content":[{"type":"text","text":"{\"items\":[]}"}],"usage":{"input_tokens":3,"output_tokens":4}}`))
					return
				}
				_, _ = response.Write([]byte(`{"choices":[{"message":{"content":"{\"items\":[]}"}}],"usage":{"prompt_tokens":3,"completion_tokens":4}}`))
			}))
			defer server.Close()

			_, _, err := requestAICompletion(context.Background(), aiProviderConfig{
				Protocol: protocol,
				BaseURL:  server.URL,
				APIKey:   "test",
			}, "bounded-model", "translate", 317)
			if err != nil {
				t.Fatal(err)
			}
			if maxTokens != 317 {
				t.Fatalf("provider max_tokens=%d, want 317", maxTokens)
			}
		})
	}
}

func TestSEC016AITranslationReservationBoundsInputAndOutput(t *testing.T) {
	model := aiModelConfig{ContextTokens: 65536, MaxOutputTokens: 50000}
	maxOutput, reservation, err := aiTranslationTokenReservation(model, "small prompt")
	if err != nil {
		t.Fatal(err)
	}
	if maxOutput != maxAITranslationOutputTokens {
		t.Fatalf("bounded max output=%d, want %d", maxOutput, maxAITranslationOutputTokens)
	}
	if reservation <= int64(maxOutput) {
		t.Fatalf("reservation=%d does not include bounded input", reservation)
	}

	if _, _, err = aiTranslationTokenReservation(
		aiModelConfig{ContextTokens: 256, MaxOutputTokens: 128},
		"prompt that cannot fit after conservative request overhead",
	); err == nil {
		t.Fatal("context-overflowing prompt was accepted")
	}
	if _, _, err = aiTranslationTokenReservation(
		aiModelConfig{ContextTokens: maxAITranslationPromptBytes * 2, MaxOutputTokens: 1},
		string(make([]byte, maxAITranslationPromptBytes+1)),
	); err == nil {
		t.Fatal("oversized prompt was accepted")
	}
}
