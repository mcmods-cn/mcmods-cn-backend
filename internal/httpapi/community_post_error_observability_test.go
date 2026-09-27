package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestCommunityPostReadsAndUpdatesDoNotHideDataFailures(t *testing.T) {
	raw, err := os.ReadFile("community_post_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)

	directory := goFunctionBody(t, source, "communityPosts")
	for _, required := range []string{"rows.Scan", "rows.Err()", `logCommunityPostDataFailure("directory_query"`, `logCommunityPostDataFailure("directory_scan"`, `logCommunityPostDataFailure("directory_cursor"`} {
		if !strings.Contains(directory, required) {
			t.Errorf("community directory is missing strict boundary %q", required)
		}
	}
	if strings.Contains(strings.ToLower(directory), "count(") {
		t.Fatal("community directory restored the synchronous COUNT removed by PERF-052")
	}

	resourceReferences := goFunctionBody(t, source, "loadCommunityPostResourceReferencesWithQueryer")
	for _, required := range []string{"json.Unmarshal(namesRaw, &ref.Names)", "return nil, fmt.Errorf", "rows.Err()", "defer rows.Close()"} {
		if !strings.Contains(resourceReferences, required) {
			t.Errorf("community resource references are missing strict JSON/row handling %q", required)
		}
	}
	if strings.Contains(resourceReferences, "_ = json.Unmarshal") {
		t.Fatal("community resource reference names still ignore JSON corruption")
	}

	requestTranslation := goFunctionBody(t, source, "requestCommunityPostTranslation")
	for _, required := range []string{"errors.Is(err, pgx.ErrNoRows)", `logCommunityPostDataFailure("translation_source"`, `logCommunityPostDataFailure("translation_cache"`, "http.StatusInternalServerError"} {
		if !strings.Contains(requestTranslation, required) {
			t.Errorf("community translation request is missing database classification %q", required)
		}
	}

	translationResult := goFunctionBody(t, source, "communityPostTranslationResult")
	for _, required := range []string{"decodeCommunityPostTranslationTaskPayload", "errors.Is(err, pgx.ErrNoRows)", `logCommunityPostDataFailure("translation_task"`, `logCommunityPostDataFailure("translation_payload"`, `logCommunityPostDataFailure("translation_result"`, "http.StatusInternalServerError"} {
		if !strings.Contains(translationResult, required) {
			t.Errorf("community translation result is missing strict failure boundary %q", required)
		}
	}
	if strings.Contains(translationResult, "_ = json.Unmarshal") || strings.Contains(translationResult, ").Scan(&title, &body) == nil") {
		t.Fatal("completed community translation can still return without a validated payload/result")
	}

	update := goFunctionBody(t, source, "updateCommunityPost")
	if strings.Contains(update, "if err != nil || tx.Commit") {
		t.Fatal("community update still combines apply and commit failures")
	}
	for _, required := range []string{`logCommunityPostDataFailure("update_lookup"`, `logCommunityPostDataFailure("update_apply"`, `logCommunityPostDataFailure("update_commit"`, "if err = tx.Commit(r.Context()); err != nil"} {
		if !strings.Contains(update, required) {
			t.Errorf("community update is missing independently observable stage %q", required)
		}
	}

	for _, required := range []string{
		"func logCommunityPostDataFailure(",
		`slog.Error("community post data failure"`,
		`"module", "community_post"`,
		`"stage", stage`,
		`"identity", identity`,
		`"error", err`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("community structured failure log is missing %q", required)
		}
	}
}
