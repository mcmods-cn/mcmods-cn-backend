package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02ChangelogPreviewShowsPendingBodyWithExactTargetPermissionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pool := newOCT02IsolatedDatabase(t, ctx)
	var author, reviewer, modID, changelogID, revisionID int64
	var projectID, changelogPublicID, revisionPublicID string
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('preview-author','author@example.invalid','test') returning id`).Scan(&author); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash) values('preview-reviewer','reviewer@example.invalid','test') returning id`).Scan(&reviewer); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by) values('clogprev1','changelog-preview','Preview target','approved',$1) returning id,project_code`, author).Scan(&modID, &projectID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `insert into project_changelogs(object_route_id,event_at,project_version,default_locale,created_by) select id,now(),'1.0','en-US',$2 from public_routes where entity_type='mod' and internal_id=$1 returning id,public_id`, modID, author).Scan(&changelogID, &changelogPublicID); err != nil {
		t.Fatal(err)
	}
	snapshot := `{"eventAt":"2026-10-02T12:00:00Z","minecraftVersions":["1.21"],"projectVersion":"1.1","defaultLocale":"en-US","localizations":[{"locale":"en-US","bodyMarkdown":"Pending **new** body"}],"privateInternalMetadata":"hidden"}`
	if err := pool.QueryRow(ctx, `insert into content_revisions(entity_type,entity_id,aggregate_type,aggregate_key,revision_no,snapshot,snapshot_hash,created_by) values('project_changelog',$1,'project_changelog',$2,1,$3::jsonb,'synthetic-hash',$4) returning id,public_id`, changelogID, changelogPublicID, snapshot, author).Scan(&revisionID, &revisionPublicID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into change_requests(entity_type,entity_id,aggregate_type,aggregate_key,proposed_revision_id,status,submitted_by) values('project_changelog',$1,'project_changelog',$2,$3,'pending',$4)`, changelogID, changelogPublicID, revisionID, author); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	invoke := func(claims security.Claims) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/content-revisions/"+revisionPublicID, nil)
		req.SetPathValue("revisionId", revisionPublicID)
		req = req.WithContext(context.WithValue(ctx, claimsContextKey, claims))
		response := httptest.NewRecorder()
		server.projectRevisionPreview(response, req)
		return response
	}
	scoped := func(actor int64, target string) security.Claims {
		return security.Claims{Subject: actor, PermissionRules: []security.PermissionRule{{Code: "project.review." + target, Allow: true, Priority: 100}}}
	}
	response := invoke(scoped(reviewer, projectID))
	if response.Code != http.StatusOK {
		t.Fatalf("review preview %d: %s", response.Code, response.Body.String())
	}
	var result struct {
		Data struct {
			EntityType string
			ProjectID  string
			Status     string
			Snapshot   map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Data.EntityType != "project_changelog" || result.Data.ProjectID != projectID || result.Data.Status != "pending" {
		t.Fatal("incorrect changelog target identity")
	}
	if _, found := result.Data.Snapshot["privateInternalMetadata"]; found {
		t.Fatal("non-whitelist field leaked")
	}
	var localizations []projectChangelogLocalization
	if err := json.Unmarshal(result.Data.Snapshot["localizations"], &localizations); err != nil || len(localizations) != 1 || localizations[0].BodyMarkdown != "Pending **new** body" {
		t.Fatalf("pending body not visible: %#v %v", localizations, err)
	}
	for _, claims := range []security.Claims{scoped(reviewer, "other0001"), scoped(author, projectID), {Subject: reviewer}} {
		if response := invoke(claims); response.Code != http.StatusForbidden {
			t.Fatalf("unauthorized preview %d", response.Code)
		}
	}
	globalSelf := security.Claims{Subject: author, PermissionRules: []security.PermissionRule{{Code: "project.review", Allow: true, Priority: 100}}}
	if response := invoke(globalSelf); response.Code != http.StatusOK {
		t.Fatal("existing global self-review policy changed")
	}
	var publishedBodies int
	if err := pool.QueryRow(ctx, `select count(*) from project_changelog_localizations where changelog_id=$1`, changelogID).Scan(&publishedBodies); err != nil || publishedBodies != 0 {
		t.Fatal("preview published pending content")
	}
	if _, err := pool.Exec(ctx, `update project_changelogs set status='deleted' where id=$1`, changelogID); err != nil {
		t.Fatal(err)
	}
	if response := invoke(scoped(reviewer, projectID)); response.Code != http.StatusNotFound {
		t.Fatal("deleted changelog remained previewable")
	}
}
