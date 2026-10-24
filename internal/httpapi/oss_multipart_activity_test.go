package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestOSSMultipartAbortIntentRetainsOnlyBoundedTypedOriginalActivity(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/users/me/oss/uploads/complete", nil)
	request = request.WithContext(context.WithValue(request.Context(), requestActivityContextKey, &requestActivity{userID: 42, actionID: activity.ActionUpload, objectTypeID: activity.ObjectFile}))
	request.Header.Set("Authorization", "Bearer NEVER_PERSIST_THIS")
	request.Header.Set("Cookie", "auth_token=NEVER_PERSIST_COOKIE")
	raw, err := newOSSMultipartAbortIntent(request, 42)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "NEVER_PERSIST") || strings.Contains(string(raw), "uploads/complete") {
		t.Fatal("intent retained request credentials or URL")
	}
	intent, err := decodeOSSMultipartAbortIntent(raw, 42)
	if err != nil || intent == nil || intent.Queued || intent.Event.UserID != 42 || intent.Event.ActionID != activity.ActionUpload || intent.Event.ObjectTypeID != activity.ObjectFile || intent.ID == "" {
		t.Fatalf("intent=%+v err=%v", intent, err)
	}
	if _, err = decodeOSSMultipartAbortIntent(raw, 43); err == nil {
		t.Fatal("cross-owner activity accepted")
	}
	for _, invalid := range [][]byte{[]byte(strings.Repeat("a", 4097)), []byte(`{"url":"https://example.invalid/private"}`), append(raw, []byte(` {}`)...), []byte(strings.Replace(string(raw), `"version":1`, `"version":2`, 1)), []byte(strings.Replace(string(raw), `"userId":42`, `"userId":0`, 1)), []byte(strings.Replace(string(raw), `"event":{`, `"event":{"token":"private",`, 1))} {
		if _, err = decodeOSSMultipartAbortIntent(invalid, 42); err == nil {
			t.Fatal("invalid/unknown deferred activity fields accepted")
		}
	}
	unannotated := httptest.NewRequest("POST", "/api/v1/users/me/oss/uploads/complete", nil)
	empty, err := newOSSMultipartAbortIntent(unannotated, 42)
	if err != nil || string(empty) != "{}" {
		t.Fatal("unannotated direct caller acquired a new activity")
	}
}
