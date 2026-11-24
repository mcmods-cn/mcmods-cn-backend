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
	f.require(t, f.otherEditor, http.MethodPut, path, update("Explicitly compared draft", a.Revision), http.StatusOK)
}
