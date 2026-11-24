package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestImportedCreatorRevisionUsesPublicRouteTypeIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the creator revision integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano()
	username := fmt.Sprintf("creatorrev%d", suffix)
	var actorID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, username, username+"@example.invalid").Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/mods", nil)
	for _, kind := range []string{"author", "team"} {
		creatorID, publicID, _, createErr := ensureNamedCreatorSnapshotTx(ctx, tx, creatorSnapshot{
			Kind: kind,
			Name: fmt.Sprintf("Imported %s %d", kind, suffix),
		}, actorID, "pending", request)
		if createErr != nil {
			t.Fatalf("create imported %s: %v", kind, createErr)
		}
		var routeType, revisionType, aggregateType string
		var routeID, revisionEntityID int64
		if err = tx.QueryRow(ctx, `select route.entity_type,route.internal_id,
			revision.entity_type,revision.entity_id,revision.aggregate_type
			from public_routes route
			join content_revisions revision on revision.aggregate_key=route.public_id
			where route.public_id=$1 order by revision.id desc limit 1`, publicID).
			Scan(&routeType, &routeID, &revisionType, &revisionEntityID, &aggregateType); err != nil {
			t.Fatal(err)
		}
		if routeType != kind || revisionType != kind || routeID != creatorID ||
			revisionEntityID != creatorID || aggregateType != "creator" {
			t.Fatalf("unexpected %s route/revision: route=(%s,%d) revision=(%s,%d,%s)",
				kind, routeType, routeID, revisionType, revisionEntityID, aggregateType)
		}
	}
}

func TestCreatorProfileEditUsesTheRevisionReadByTheEditorIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	id := auditOSS001CreateReviewedCreator(t, f, "author")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "creator.edit."+id)
	path := "/api/v1/creators/" + id
	read := func(token string) string {
		return decodeTEST022Data[struct {
			Revision string `json:"publishedRevisionId"`
		}](t, f.require(t, token, http.MethodGet, path, nil, http.StatusOK)).Revision
	}
	startA, startB := read(f.editor), read(f.otherEditor)
	if startA == "" || startB != startA {
		t.Fatal("both editors must read the same published profile")
	}
	update := func(name, base string) map[string]any {
		return map[string]any{"kind": "author", "name": name, "defaultLocale": "en-US", "baseRevisionId": base, "links": []any{}}
	}
	a := decodeTEST022Data[struct {
		Revision string `json:"revisionId"`
	}](t, f.require(t, f.editor, http.MethodPut, path, update("First confirmed editor", startA), http.StatusOK))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+a.Revision, map[string]any{"status": "approved"}, http.StatusOK)
	before := auditOSS001CreatorWriteFacts(t, f, id)
	missing := update("Other draft", startB)
	delete(missing, "baseRevisionId")
	f.require(t, f.otherEditor, http.MethodPut, path, missing, http.StatusPreconditionRequired)
	f.require(t, f.otherEditor, http.MethodPut, path, update("Other draft", startB), http.StatusConflict)
	if string(auditOSS001CreatorWriteFacts(t, f, id)) != string(before) || read(f.otherEditor) != a.Revision {
		t.Fatal("a missing or stale baseline changed the published profile or revision log")
	}
	f.require(t, f.otherEditor, http.MethodPut, path, update("Other draft", a.Revision), http.StatusOK)
}

func TestCreatorProfileEditWaitsForFirstPublicationIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create")
	created := decodeTEST022Data[struct {
		PublicID   string `json:"publicId"`
		RevisionID string `json:"revisionId"`
		Status     string `json:"reviewStatus"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/creators", map[string]any{
		"kind": "author", "name": "Unpublished author", "defaultLocale": "en-US",
	}, http.StatusCreated))
	if created.Status != "pending" {
		t.Fatal("expected an unpublished profile")
	}
	// The initial proposal closed without a publication. The next publishing
	// transaction can now create a new revision after the waiting edit starts.
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+created.RevisionID,
		map[string]string{"status": "rejected", "note": "Needs additional evidence before first publication"}, http.StatusOK)
	path := "/api/v1/creators/" + created.PublicID
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.edit."+created.PublicID)
	auditOSS001PublishFixturePermissions(t, f, f.editor)

	// The ordinary import fixture has one application connection. Use its same
	// schema on independent connections for the publisher and the lock observer.
	cfg := f.db.Config()
	cfg.MaxConns = 2
	control, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	publication, err := control.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer publication.Rollback(f.ctx)
	var creatorID int64
	if err := publication.QueryRow(f.ctx, "select id from creators where public_id=$1 and published_revision_id is null for update", created.PublicID).Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	waiter := auditCompletionAsyncJSON(f.test013Fixture, f.editor, http.MethodPut, path, map[string]any{
		"kind": "author", "name": "Draft from before publication", "defaultLocale": "en-US", "baseRevisionId": "",
	})
	waitCtx, cancel := context.WithTimeout(f.ctx, 5*time.Second)
	defer cancel()
	for {
		var blocked bool
		err := control.QueryRow(waitCtx, `select exists(select 1 from pg_stat_activity where datname=current_database()
			and wait_event_type='Lock' and $1::int=any(pg_blocking_pids(pid)))`, publication.Conn().PgConn().PID()).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		select {
		case result := <-waiter:
			t.Fatalf("edit returned before the publication lock was observed: %d %v", result.status, result.err)
		case <-waitCtx.Done():
			t.Fatal(waitCtx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	revision, err := createContentRevisionTx(f.ctx, publication, createContentRevisionParams{
		EntityType: "author", EntityID: creatorID, AggregateType: "creator", AggregateKey: created.PublicID,
		Snapshot: []byte(`{"kind":"author","name":"First publication"}`), ActorID: f.userIDs[f.reviewer],
		Source: "user", Status: "approved", Request: httptest.NewRequest(http.MethodPut, path, nil),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := publication.Exec(f.ctx, `update creators set published_revision_id=$2,review_status='approved' where id=$1`, creatorID, revision.RevisionID); err != nil {
		t.Fatal(err)
	}
	if err := publication.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	result := <-waiter
	if result.err != nil || result.status != http.StatusConflict {
		t.Fatalf("stale unpublished edit after concurrent publication: status=%d err=%v body=%s", result.status, result.err, result.raw)
	}
	if f.number(t, "select count(*) from content_revisions where aggregate_type='creator' and aggregate_key=$1", created.PublicID) != 2 {
		t.Fatal("a waiting edit added a revision after its baseline changed")
	}
}

func TestCreatorCollaborationsFollowPublishedProjectsIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	author := auditOSS001CreateReviewedCreator(t, f, "author")
	team := auditOSS001CreateReviewedCreator(t, f, "team")
	var packID, pluginID int64
	var pack, plugin string
	if err := f.db.QueryRow(f.ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by)
		values('fixture-pack','Fixture pack','approved',$1) returning id,public_id`, f.userIDs[f.editor]).Scan(&packID, &pack); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into simple_projects(project_type,slug,primary_name,review_status,submitted_by)
		values('plugin','fixture-plugin','Fixture plugin','approved',$1) returning id,public_id`, f.userIDs[f.editor]).Scan(&pluginID, &plugin); err != nil {
		t.Fatal(err)
	}
	for kind, projectID := range map[string]int64{"modpack": packID, "plugin": pluginID} {
		if _, err := f.db.Exec(f.ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status)
			select $1,$2,id,'approved' from creators where public_id=any($3::text[])`, kind, projectID, []string{author, team}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(want bool) {
		t.Helper()
		detail := decodeTEST022Data[struct {
			Collaborators []creatorSummary `json:"collaborators"`
		}](t, f.require(t, "", http.MethodGet, "/api/v1/creators/"+author, nil, http.StatusOK))
		var found bool
		for _, peer := range detail.Collaborators {
			if peer.PublicID == team {
				found = true
			}
		}
		if found != want {
			t.Fatalf("shared published project: collaborator present=%v, want %v", found, want)
		}
	}
	check(true)
	for _, project := range []struct {
		kind, id           string
		stillCollaborating bool
	}{{"modpack", pack, true}, {"plugin", plugin, false}} {
		tx, err := f.db.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = f.server.moderationHideTarget(f.ctx, tx, project.kind, project.id); err != nil {
			_ = tx.Rollback(f.ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		check(project.stillCollaborating)
	}
}
