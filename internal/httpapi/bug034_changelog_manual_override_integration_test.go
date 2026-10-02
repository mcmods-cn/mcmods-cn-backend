package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestBUG034ManualChangelogOverrideStartsOnlyAfterApprovalIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify changelog manual-override review timing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	catalog := `{"versions":[{"code":"1.21.1","type":"release"}],"commonVersions":["1.21.1"],"loaders":[{"code":"Fabric","name":"Fabric","versions":["1.21.1"]}]}`
	if _, err = pool.Exec(ctx, `insert into system_settings(key,value) values($1,$2::jsonb)
		on conflict(key) do update set value=excluded.value`, minecraftVersionsSettingKey, catalog); err != nil {
		t.Fatal(err)
	}
	userIDs := make([]int64, 2)
	for index := range userIDs {
		if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
			values($1,$2,'bug034',true) returning id`, fmt.Sprintf("bug034-%d-%d", time.Now().UnixNano(), index),
			fmt.Sprintf("bug034-%d-%d@example.invalid", time.Now().UnixNano(), index)).Scan(&userIDs[index]); err != nil {
			t.Fatal(err)
		}
	}
	var projectInternalID, routeID int64
	const projectID = "b034m0001"
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values($1,'bug034-project','BUG034 project','approved',$2) returning id`, projectID, userIDs[0]).Scan(&projectInternalID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, projectInternalID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool}
	worker := &ProjectAutomationWorker{server: server}
	job := projectAutomationJob{RouteID: routeID, InternalID: projectInternalID, ProjectType: "mod", ProjectPublicID: projectID,
		SourceType: "modrinth", ActorID: userIDs[0]}
	initial := projectAutomationRelease{ID: "managed-release", Version: "1.0.0", PublishedAt: time.Now().UTC(), GameVersions: []string{"1.21.1"},
		Body: "Initial upstream", URL: "https://example.invalid/managed-release"}
	if result, syncErr := worker.syncChangelogs(ctx, job, []projectAutomationRelease{initial}); syncErr != nil || result["created"] != 1 {
		t.Fatalf("initial synchronization result=%#v err=%v", result, syncErr)
	}
	var changelogPublicID string
	if err = pool.QueryRow(ctx, `select changelog_public_id from external_release_bindings where external_release_id=$1`, initial.ID).Scan(&changelogPublicID); err != nil {
		t.Fatal(err)
	}
	if _, invalidErr := pool.Exec(ctx, `update external_release_bindings set manual_override=true where changelog_public_id=$1`, changelogPublicID); invalidErr == nil {
		t.Fatal("database accepted a manual override without an approved revision origin")
	} else {
		var pgErr *pgconn.PgError
		if !errors.As(invalidErr, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "external_release_bindings_manual_override_origin_check" {
			t.Fatalf("invalid override error=%v, want named 23514 provenance check", invalidErr)
		}
	}
	rejectedRevisionID := invokeBUG034ManualChangelogEdit(t, ctx, server, userIDs[0], changelogPublicID, "Manual candidate to reject")
	assertBUG034ManualOverride(t, ctx, pool, changelogPublicID, false)
	invokeBUG034ChangelogReview(t, ctx, server, userIDs[1], rejectedRevisionID, "rejected")
	assertBUG034ManualOverride(t, ctx, pool, changelogPublicID, false)

	afterRejection := initial
	afterRejection.Version = "1.1.0"
	afterRejection.Body = "Upstream after rejection"
	if result, syncErr := worker.syncChangelogs(ctx, job, []projectAutomationRelease{afterRejection}); syncErr != nil || result["updated"] != 1 || result["manualOverrideConflicts"] != 0 {
		t.Fatalf("post-rejection synchronization result=%#v err=%v", result, syncErr)
	}
	assertBUG034ChangelogBody(t, ctx, pool, changelogPublicID, afterRejection.Body)

	approvedRevisionPublicID := invokeBUG034ManualChangelogEdit(t, ctx, server, userIDs[0], changelogPublicID, "Approved manual body")
	assertBUG034ManualOverride(t, ctx, pool, changelogPublicID, false)
	invokeBUG034ChangelogReview(t, ctx, server, userIDs[1], approvedRevisionPublicID, "approved")
	var approvedRevisionID int64
	if err = pool.QueryRow(ctx, `select id from content_revisions where public_id=$1`, approvedRevisionPublicID).Scan(&approvedRevisionID); err != nil {
		t.Fatal(err)
	}
	var override bool
	var overrideRevisionID *int64
	var overrideSource string
	if err = pool.QueryRow(ctx, `select manual_override,manual_override_revision_id,manual_override_source
		from external_release_bindings where changelog_public_id=$1`, changelogPublicID).Scan(&override, &overrideRevisionID, &overrideSource); err != nil {
		t.Fatal(err)
	}
	if !override || overrideRevisionID == nil || *overrideRevisionID != approvedRevisionID || overrideSource != "user" {
		t.Fatalf("approved override=(%t,%v,%q), want revision %d/user", override, overrideRevisionID, overrideSource, approvedRevisionID)
	}
	assertBUG034ChangelogBody(t, ctx, pool, changelogPublicID, "Approved manual body")

	afterApproval := afterRejection
	afterApproval.Version = "1.2.0"
	afterApproval.Body = "Upstream after approval"
	if result, syncErr := worker.syncChangelogs(ctx, job, []projectAutomationRelease{afterApproval}); syncErr != nil || result["updated"] != 0 || result["manualOverrideConflicts"] != 1 {
		t.Fatalf("post-approval synchronization result=%#v err=%v", result, syncErr)
	}
	assertBUG034ChangelogBody(t, ctx, pool, changelogPublicID, "Approved manual body")
}

func invokeBUG034ManualChangelogEdit(t *testing.T, ctx context.Context, server *Server, actorID int64, changelogPublicID, body string) string {
	t.Helper()
	payload, err := json.Marshal(projectChangelogSnapshot{EventAt: time.Now().UTC(), MinecraftVersions: []string{"1.21.1"},
		ProjectVersion: "manual", DefaultLocale: "en-US", Localizations: []projectChangelogLocalization{{Locale: "en-US", BodyMarkdown: body}}, Reason: "BUG034 review"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/changelogs/"+changelogPublicID, bytes.NewReader(payload))
	request.SetPathValue("id", changelogPublicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: actorID,
		PermissionRules: []security.PermissionRule{{Code: "project.edit.b034m0001", Allow: true, Priority: 100}}}))
	response := httptest.NewRecorder()
	server.projectChangelogItem(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("manual changelog edit status=%d body=%s", response.Code, response.Body.String())
	}
	var revisionPublicID string
	if err = server.db.QueryRow(ctx, `select revision.public_id from change_requests request
		join content_revisions revision on revision.id=request.proposed_revision_id
		where request.aggregate_type=$1 and request.aggregate_key=$2 and request.status='pending'`, projectChangelogAggregate, changelogPublicID).Scan(&revisionPublicID); err != nil {
		t.Fatal(err)
	}
	return revisionPublicID
}

func invokeBUG034ChangelogReview(t *testing.T, ctx context.Context, server *Server, reviewerID int64, revisionPublicID, status string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/content-revisions/"+revisionPublicID+"/review",
		bytes.NewBufferString(`{"status":"`+status+`","note":"BUG034 decision"}`))
	request.SetPathValue("revisionId", revisionPublicID)
	request = request.WithContext(context.WithValue(ctx, claimsContextKey, security.Claims{Subject: reviewerID,
		PermissionRules: []security.PermissionRule{{Code: "content.review", Allow: true, Priority: 100}}}))
	response := httptest.NewRecorder()
	server.reviewContentRevision(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s changelog review status=%d body=%s", status, response.Code, response.Body.String())
	}
}

func assertBUG034ManualOverride(t *testing.T, ctx context.Context, pool *pgxpool.Pool, changelogPublicID string, want bool) {
	t.Helper()
	var got bool
	if err := pool.QueryRow(ctx, `select manual_override from external_release_bindings where changelog_public_id=$1`, changelogPublicID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("manual_override=%t want %t", got, want)
	}
}

func assertBUG034ChangelogBody(t *testing.T, ctx context.Context, pool *pgxpool.Pool, changelogPublicID, want string) {
	t.Helper()
	var got string
	if err := pool.QueryRow(ctx, `select localization.body_markdown from project_changelogs changelog
		join project_changelog_localizations localization on localization.changelog_id=changelog.id and localization.locale='en-US'
		where changelog.public_id=$1`, changelogPublicID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("changelog body=%q want %q", got, want)
	}
}
