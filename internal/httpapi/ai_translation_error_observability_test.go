package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestAITranslationsPersistValidatedBusinessResultsBeforeCompletion(t *testing.T) {
	handlerRaw, err := os.ReadFile("ai_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	translationRaw, err := os.ReadFile("ai_translation.go")
	if err != nil {
		t.Fatal(err)
	}
	contentRaw, err := os.ReadFile("content_localization_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	notificationRaw, err := os.ReadFile("notification_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, translation, content, notification := string(handlerRaw), string(translationRaw), string(contentRaw), string(notificationRaw)

	handle := goFunctionBody(t, handlers, "handleTask")
	persistAt := strings.Index(handle, "persistNotificationTranslation")
	completeAt := strings.Index(handle, "set status = 'completed'")
	if persistAt < 0 || completeAt < 0 || persistAt > completeAt {
		t.Fatal("AI worker still marks a task completed before persisting its business result")
	}
	for _, required := range []string{
		"decodeAITaskContentScope(rawPayload)",
		"if err = worker.persistNotificationTranslation",
		"if err != nil",
		"worker.failTask(ctx, msg.TaskID, err)",
		"RowsAffected() != 1",
	} {
		if !strings.Contains(handle, required) {
			t.Errorf("AI worker completion boundary is missing %q", required)
		}
	}
	if strings.Contains(handle, "_ = json.Unmarshal") {
		t.Fatal("AI worker still ignores content task scope JSON errors")
	}

	persistNotification := goFunctionBody(t, translation, "persistNotificationTranslation") +
		goFunctionBody(t, translation, "decodeNotificationTranslation")
	for _, required := range []string{
		") error",
		"decodeNotificationTranslation",
		"strictTranslationItemsToMap",
		"return fmt.Errorf",
		"return err",
	} {
		if !strings.Contains(persistNotification, required) {
			t.Errorf("notification translation persistence is missing %q", required)
		}
	}
	if strings.Contains(persistNotification, "_, _ =") || strings.Contains(persistNotification, "json.Unmarshal(rawPayload, &payload) != nil") {
		t.Fatal("notification translation persistence can still silently return")
	}

	resultHandler := goFunctionBody(t, content, "catalogContentTranslationResult")
	for _, required := range []string{
		"decodeCatalogTranslationResult(payloadRaw, resultRaw)",
		`logAITranslationFailure("result_decode"`,
		"http.StatusInternalServerError",
	} {
		if !strings.Contains(resultHandler, required) {
			t.Errorf("catalog translation result boundary is missing %q", required)
		}
	}
	if strings.Contains(resultHandler, "_ = json.Unmarshal") {
		t.Fatal("catalog translation result still ignores JSON corruption")
	}

	notificationResult := goFunctionBody(t, notification, "notificationTranslationResult")
	for _, required := range []string{"decodeStoredNotificationTranslation", `logAITranslationFailure("notification_result_decode"`, "http.StatusInternalServerError"} {
		if !strings.Contains(notificationResult, required) {
			t.Errorf("notification translation result boundary is missing %q", required)
		}
	}
	for _, forbidden := range []string{"_ = json.Unmarshal", "insert into notification_translations", "_, _ = s.db.Exec"} {
		if strings.Contains(notificationResult, forbidden) {
			t.Errorf("notification translation result GET still mutates or hides errors through %q", forbidden)
		}
	}

	failTask := goFunctionBody(t, handlers, "failTask")
	for _, required := range []string{"error", "RowsAffected()", "logAITranslationFailure", "return nil"} {
		if !strings.Contains(failTask, required) {
			t.Errorf("AI failed-state persistence is missing %q", required)
		}
	}
}
