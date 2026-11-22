package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

const aiConfigTestPath = "/api/v1/admin/ai/config"

type aiConfigTestForm struct {
	aiConfigPayload
	Revision     string   `json:"revision,omitempty"`
	ClearAPIKeys []string `json:"clearApiKeys,omitempty"`
}

func TestAIConfigurationConcurrentAdminsKeepTheFirstCommitIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	for _, token := range []string{f.editor, f.reviewer} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "ai.read", "ai.write")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "ai.read")
	readForm := func(token string) aiConfigTestForm {
		return decodeTEST022Data[aiConfigTestForm](t, f.require(t, token, http.MethodGet, aiConfigTestPath, nil, http.StatusOK))
	}
	adminA, adminB := readForm(f.editor), readForm(f.reviewer)
	adminA.Quotas[0].CostLimitCNY += 17
	saved := decodeTEST022Data[aiConfigTestForm](t, f.require(t, f.editor, http.MethodPut, aiConfigTestPath, adminA, http.StatusOK))
	if saved.Quotas[0].CostLimitCNY != adminA.Quotas[0].CostLimitCNY {
		t.Fatal("the first administrator's budget update did not persist")
	}
	adminB.Providers[1].Notes = "second administrator's provider note"
	assertAIConfigErrorCode(t,
		f.require(t, f.reviewer, http.MethodPut, aiConfigTestPath, adminB, http.StatusConflict), "AI_CONFIG_CONFLICT")
	persisted := readForm(f.denied)
	if !isAIConfigRevision(saved.Revision) || saved.Revision == adminA.Revision || persisted.Revision != saved.Revision ||
		persisted.Quotas[0].CostLimitCNY != adminA.Quotas[0].CostLimitCNY || persisted.Providers[1].Notes == adminB.Providers[1].Notes {
		t.Fatal("a stale form changed the accepted budget, provider, or revision")
	}
	f.require(t, f.denied, http.MethodPut, aiConfigTestPath, persisted, http.StatusForbidden)
	persisted.Revision = ""
	assertAIConfigErrorCode(t,
		f.require(t, f.editor, http.MethodPut, aiConfigTestPath, persisted, http.StatusPreconditionRequired), "AI_CONFIG_REVISION_REQUIRED")
	persisted = readForm(f.reviewer)
	persisted.Providers[1].Notes = adminB.Providers[1].Notes
	saved = decodeTEST022Data[aiConfigTestForm](t, f.require(t, f.reviewer, http.MethodPut, aiConfigTestPath, persisted, http.StatusOK))
	if saved.Providers[1].Notes != adminB.Providers[1].Notes || saved.Quotas[0].CostLimitCNY != adminA.Quotas[0].CostLimitCNY {
		t.Fatal("the explicitly refreshed provider update replaced the accepted budget")
	}
}

func TestAIConfigurationPreservesReplacesAndClearsProviderKeysIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "ai.read", "ai.write")
	form := decodeTEST022Data[aiConfigTestForm](t, f.require(t, f.editor, http.MethodGet, aiConfigTestPath, nil, http.StatusOK))
	const firstKey, replacementKey = "fixture-only-first-provider-key", "fixture-only-replacement-provider-key"
	getKey := func() string {
		server := f.origin.Config.Handler.(*Server)
		cfg, err := readAIConfigFromSettings(f.ctx, f.db, server.cfg.SettingsEncryptionKey)
		if err != nil {
			t.Fatal(err)
		}
		return cfg.Providers[0].APIKey
	}
	save := func(payload aiConfigTestForm) aiConfigTestForm {
		raw := f.require(t, f.editor, http.MethodPut, aiConfigTestPath, payload, http.StatusOK)
		if bytes.Contains(raw, []byte(`"apiKey"`)) || bytes.Contains(raw, []byte(firstKey)) || bytes.Contains(raw, []byte(replacementKey)) {
			t.Fatal("a provider secret was included in the settings response")
		}
		next := decodeTEST022Data[aiConfigTestForm](t, raw)
		if next.Revision == payload.Revision || !isAIConfigRevision(next.Revision) || len(next.ClearAPIKeys) != 0 {
			t.Fatal("the write failed to advance the revision or exposed a request-only key action")
		}
		return next
	}
	form.Providers[0].APIKey = firstKey
	form.Providers[0].BaseURL = "https://model-fixture.example/v1"
	saved := save(form)
	saved.Providers[0].Notes = "retain the configured key"
	saved.Providers[0].BaseURL = "https://MODEL-FIXTURE.EXAMPLE:443/v1"
	retained := save(saved)
	if !retained.Providers[0].HasAPIKey || getKey() != firstKey {
		t.Fatal("an equivalent provider origin and a redacted empty key must preserve the credential")
	}
	retained.Providers[0].BaseURL = "https://alternate-model-fixture.example/v1"
	assertAIConfigErrorCode(t,
		f.require(t, f.editor, http.MethodPut, aiConfigTestPath, retained, http.StatusBadRequest), "AI_PROVIDER_KEY_ACTION_REQUIRED")
	if getKey() != firstKey {
		t.Fatal("changing origins without a key action changed storage")
	}
	retained.Providers[0].APIKey = replacementKey
	retained.ClearAPIKeys = []string{"openai"}
	assertAIConfigErrorCode(t,
		f.require(t, f.editor, http.MethodPut, aiConfigTestPath, retained, http.StatusBadRequest), "AI_PROVIDER_KEY_UPDATE_INVALID")
	retained.ClearAPIKeys = nil
	replaced := save(retained)
	assertAIConfigErrorCode(t,
		f.require(t, f.editor, http.MethodPut, aiConfigTestPath, saved, http.StatusConflict), "AI_CONFIG_CONFLICT")
	if !replaced.Providers[0].HasAPIKey || getKey() != replacementKey {
		t.Fatal("a stale save interfered with the accepted key replacement")
	}
	replaced.ClearAPIKeys = []string{"openai"}
	cleared := save(replaced)
	if cleared.Providers[0].HasAPIKey || getKey() != "" || cleared.Providers[0].Notes != saved.Providers[0].Notes {
		t.Fatal("explicit key removal changed unrelated settings or retained the credential")
	}
	var tasks int
	if err := f.db.QueryRow(f.ctx, `select count(*) from ai_tasks`).Scan(&tasks); err != nil || tasks != 0 {
		t.Fatalf("configuration saves or retries queued AI requests: tasks=%d error=%v", tasks, err)
	}
}

func isAIConfigRevision(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, digit := range value {
		if !('0' <= digit && digit <= '9') && !('a' <= digit && digit <= 'f') {
			return false
		}
	}
	return true
}

func assertAIConfigErrorCode(t *testing.T, raw []byte, want string) {
	t.Helper()
	var result apiResponse
	if err := json.Unmarshal(raw, &result); err != nil || result.Code != want {
		t.Fatalf("AI configuration response code=%q want=%q, decode error=%v", result.Code, want, err)
	}
}
