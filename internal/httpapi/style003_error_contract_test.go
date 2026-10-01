package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestSTYLE003LegacyErrorsHaveStableCodesAndOneEnvelope(t *testing.T) {
	for _, status := range []int{400, 401, 403, 404, 405, 409, 410, 413, 422, 429, 500, 502, 503, 504} {
		for _, message := range []string{"failed to load content language settings", "读取内容语言设置失败"} {
			w := httptest.NewRecorder()
			writeError(w, status, message)
			var response apiResponse
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != status || response.Code != fmt.Sprintf("HTTP_%d", status) || response.Error != message || response.Data != nil || response.Details != nil || response.RetryAfter != 0 {
				t.Fatalf("legacy error changed diagnostic/status or lacks stable code: status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get(backendResponseHeader) != "1" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Type") != "application/json; charset=utf-8" || w.Header().Get("Retry-After") != "" {
				t.Fatalf("error headers=%v", w.Header())
			}
		}
	}
}

func TestSTYLE003TypedErrorsKeepCodeRetryAndDetailsWithoutLanguageDecisions(t *testing.T) {
	for _, code := range []string{"challenge_required", "COMMENT_EDIT_CONFLICT", "COMMUNITY_POST_EDIT_CONFLICT"} {
		w := httptest.NewRecorder()
		writeAPIError(w, 409, code, "original diagnostic", 37, map[string]any{"currentRevisionId": "revision003", "challenge": map[string]any{"id": "proof003", "provider": "proof"}})
		var response apiResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		details, ok := response.Details.(map[string]any)
		if !ok || response.Code != code || response.Error != "original diagnostic" || response.RetryAfter != 37 || details["currentRevisionId"] != "revision003" || w.Header().Get("Retry-After") != "37" || w.Code != 409 {
			t.Fatalf("typed contract lost: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	writeAPIError(w, 503, "", "original diagnostic", 0, nil)
	var response apiResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Code != "HTTP_503" {
		t.Fatalf("empty explicit code must use canonical status fallback: %s", w.Body.String())
	}
}

func TestSTYLE003YggdrasilAndSuccessResponsesRemainSeparateProtocols(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, 200, map[string]string{"id": "success003"})
	if strings.Contains(w.Body.String(), `"code"`) || strings.Contains(w.Body.String(), `"error"`) || !strings.Contains(w.Body.String(), `"data"`) {
		t.Fatalf("success protocol changed: %s", w.Body.String())
	}
	w = httptest.NewRecorder()
	writeYggdrasilInternalError(w)
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 503 || response["error"] != "ServiceUnavailableException" || response["errorMessage"] == nil || response["code"] != nil || response["data"] != nil {
		t.Fatalf("launcher protocol changed: %d %s", w.Code, w.Body.String())
	}
}

func TestSTYLE003ContentLanguageErrorCodesAndAtomicFailureFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	const route = "/api/v1/users/me/content-languages"
	check := func(token, method string, body any, status int, code string) {
		t.Helper()
		raw := f.require(t, token, method, route, body, status)
		var response apiResponse
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatal(err)
		}
		if response.Code != code || response.Error == "" || response.Data != nil || strings.Contains(response.Error, "SQLSTATE") || strings.Contains(response.Error, "style003_reject_language") {
			t.Fatalf("error contract=%s", raw)
		}
	}
	check("", http.MethodGet, nil, 401, "HTTP_401")
	before := f.facts(t)
	check(f.editor, http.MethodPut, map[string]any{"primaryLocale": "not a language", "secondaryLocale": "en-US"}, 400, "CONTENT_LANGUAGE_TAG_INVALID")
	check(f.editor, http.MethodPut, map[string]any{"primaryLocale": "en-US", "secondaryLocale": "xx-YY"}, 400, "CONTENT_LANGUAGE_SECONDARY_UNSUPPORTED")
	check(f.editor, http.MethodPut, map[string]any{"unknown": true}, 400, "CONTENT_LANGUAGE_REQUEST_INVALID")
	if after := f.facts(t); !reflect.DeepEqual(after, before) {
		t.Fatal("invalid requests changed business facts")
	}
	var primary, secondary string
	if err := f.db.QueryRow(f.ctx, `select preferred_content_language,secondary_content_language from users where id=$1`, f.userIDs[f.editor]).Scan(&primary, &secondary); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table users add constraint style003_reject_language check(preferred_content_language <> 'de-DE') not valid`); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	check(f.editor, http.MethodPut, map[string]any{"primaryLocale": "de-DE", "secondaryLocale": "en-US"}, 500, "CONTENT_LANGUAGE_SETTINGS_UPDATE_FAILED")
	if after := f.facts(t); !reflect.DeepEqual(after, before) {
		t.Fatal("actual PostgreSQL failure changed business facts")
	}
	var afterPrimary, afterSecondary string
	if err := f.db.QueryRow(f.ctx, `select preferred_content_language,secondary_content_language from users where id=$1`, f.userIDs[f.editor]).Scan(&afterPrimary, &afterSecondary); err != nil {
		t.Fatal(err)
	}
	if afterPrimary != primary || afterSecondary != secondary {
		t.Fatal("failed settings update changed preferences")
	}
	if _, err := f.db.Exec(f.ctx, `alter table users drop constraint style003_reject_language`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table user_activity_events add constraint style003_reject_audit check(false) not valid`); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	check(f.editor, http.MethodPut, map[string]any{"primaryLocale": "de-DE", "secondaryLocale": "en-US"}, 500, "CONTENT_LANGUAGE_SETTINGS_AUDIT_FAILED")
	if after := f.facts(t); !reflect.DeepEqual(after, before) {
		t.Fatal("failed durable audit changed business facts")
	}
	if err := f.db.QueryRow(f.ctx, `select preferred_content_language,secondary_content_language from users where id=$1`, f.userIDs[f.editor]).Scan(&afterPrimary, &afterSecondary); err != nil {
		t.Fatal(err)
	}
	if afterPrimary != primary || afterSecondary != secondary {
		t.Fatal("failed durable audit committed language preferences")
	}
	if _, err := f.db.Exec(f.ctx, `alter table user_activity_events drop constraint style003_reject_audit`); err != nil {
		t.Fatal(err)
	}
	raw := f.require(t, f.editor, http.MethodPut, route, map[string]any{"primaryLocale": "de-DE", "secondaryLocale": "en-US"}, 200)
	var success struct {
		Data contentLanguageSettingsPayload `json:"data"`
	}
	if err := json.Unmarshal(raw, &success); err != nil {
		t.Fatal(err)
	}
	if success.Data.PrimaryLocale != "de-DE" || success.Data.SecondaryLocale != "en-US" {
		t.Fatalf("healthy recovery=%s", raw)
	}
	if _, err := f.db.Exec(f.ctx, `alter table users rename column preferred_content_language to style003_hidden_preference`); err != nil {
		t.Fatal(err)
	}
	check(f.editor, http.MethodGet, nil, 500, "CONTENT_LANGUAGE_SETTINGS_READ_FAILED")
	if _, err := f.db.Exec(f.ctx, `alter table users rename column style003_hidden_preference to preferred_content_language`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodGet, route, nil, 200)
}
