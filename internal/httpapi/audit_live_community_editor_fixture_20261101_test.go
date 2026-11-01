package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/security"
)

// Ordinary HTTP authoring and independent moderation are data prerequisites.
// A fixture's successful exit does not certify an external editor/browser or NATS.
func TestAuditLiveCommunityEditorFixture20261101(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_COMMUNITY_EDITOR") != "1" {
		t.Skip("opt-in task-owned actual community editor fixture")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	top, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	frontend := os.Getenv("MCMODS_AUDIT_FRONTEND_REPO")
	if frontend == "" {
		t.Fatal("declare exact clean frontend checkout")
	}
	revisions := map[string]any{}
	for repo, path := range map[string]string{"mcmods-cn-backend": strings.TrimSpace(string(top)), "mcmods-cn-frontend": frontend, "mcmods_exporter": "/workspace/mcmods_exporter"} {
		revision := auditJourneyRevision(path)
		if revision["error"] != nil || revision["dirty"] != false {
			t.Fatalf("clean checkout required: %s", repo)
		}
		revisions[repo] = revision
	}
	directory := os.Getenv("MCMODS_AUDIT_COMMUNITY_DIR")
	if !filepath.IsAbs(directory) {
		t.Fatal("new absolute evidence directory required")
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenFile(filepath.Join(directory, "OWNER"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintf(owner, "mcmods-live-community-editor pid=%d\n", os.Getpid())
	owner.Close()
	if err != nil {
		t.Fatal(err)
	}
	frontendOrigin := "http://127.0.0.1:3341"
	t.Setenv("FRONTEND_ORIGIN", frontendOrigin)
	unlockDDL := auditGlobalCatalogPGLock(t)
	t.Cleanup(func() { unlockDDL() })
	// Waiting for a shared schema-setup window consumes no browser/fixture TTL.
	f := newAuditFixGRFixture(t, 45*time.Minute)
	unlockDDL()
	t.Cleanup(func() { unlockDDL = auditGlobalCatalogPGLock(t) })
	f.origin.Close()
	dbConfig := f.db.Config()
	dbConfig.MaxConns = 6
	dbConfig.MinConns = 1
	db, err := pgxpool.NewWithConfig(f.ctx, dbConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	caps := []string{"community.tutorial.create", "user.draft.retention_seconds.2592000"}
	grantTEST044Permissions(t, f.ctx, db, f.userIDs[f.editor], caps...)
	// Existing reviewer has only content.review; other editor keeps its unrelated
	// project.edit capability. No community.edit, no-review or administrator grant.
	cfg := f.server.cfg
	cfg.FrontendOrigin = frontendOrigin
	cfg.NATS.Enabled = false
	private := map[string]any{"creatorToken": f.editor, "otherToken": f.otherEditor, "reviewerToken": f.reviewer, "ordinaryToken": f.denied}
	actors := map[string]any{}
	for role, token := range map[string]string{"creator": f.editor, "other": f.otherEditor, "reviewer": f.reviewer, "ordinary": f.denied} {
		var publicID, username string
		if err = db.QueryRow(f.ctx, `select public_id,username from users where id=$1`, f.userIDs[token]).Scan(&publicID, &username); err != nil {
			t.Fatal(err)
		}
		actors[role] = map[string]any{"publicId": publicID, "username": username}
		// Synthetic credentials support a later ordinary native login. Never log or
		// publish the password/token; these accounts belong to this owned schema.
		if role == "creator" || role == "other" {
			password := "Audit-private-" + randomCatalogPublicID() + "-2026!"
			hash, e := security.HashPassword(password)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec(f.ctx, `update users set password_hash=$2 where id=$1`, f.userIDs[token], hash); e != nil {
				t.Fatal(e)
			}
			private[role+"Account"] = username
			private[role+"Password"] = password
		}
	}
	// Four predefined authors are the four kind-specific authorization cases.
	// The previous same-author fourth submit correctly hit ObjectLimit3/60min;
	// do not change that policy or claim continuous same-author creation here.
	authorTokens := map[string]string{"tutorial": f.editor}
	authorActors := map[string]any{"tutorial": actors["creator"]}
	for _, kind := range []string{"issue", "news", "discussion"} {
		id, publicID, token := createTEST044User(t, f.ctx, db, cfg, "community-"+kind+"-"+randomCatalogPublicID(), "UTC")
		f.userIDs[token] = id
		if _, err := db.Exec(f.ctx, `update users set created_at=now()-interval '30 days',email_verified=true where id=$1`, id); err != nil {
			t.Fatal(err)
		}
		grantTEST044Permissions(t, f.ctx, db, id, "community."+kind+".create", "user.draft.retention_seconds.2592000")
		var username string
		if err := db.QueryRow(f.ctx, `select username from users where id=$1`, id).Scan(&username); err != nil {
			t.Fatal(err)
		}
		authorTokens[kind] = token
		authorActors[kind] = map[string]any{"publicId": publicID, "username": username, "createPermission": "community." + kind + ".create"}
	}
	for _, actor := range []string{f.editor, f.otherEditor, f.denied, f.reviewer} {
		grantTEST044Permissions(t, f.ctx, db, f.userIDs[actor], "user.message.receive")
	}
	for _, actor := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, db, f.userIDs[actor], "user.message.send")
	}
	api := NewServer(f.ctx, cfg, db, nil, nil, nil, nil)
	origin := httptest.NewServer(api)
	t.Cleanup(func() {
		origin.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := api.Shutdown(ctx); e != nil {
			t.Error(e)
		}
		api.cache.Close()
	})
	f.server, f.origin, f.db = api, origin, db
	// Setting a password deliberately advances auth_version and revokes the
	// helper session. Obtain its replacement through the normal login endpoint;
	// reusing the helper token would make this fixture fail before authoring.
	for _, role := range []string{"creator", "other"} {
		payload, err := json.Marshal(loginRequest{Account: private[role+"Account"].(string), Password: private[role+"Password"].(string)})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, origin.URL+"/api/v1/auth/login", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", frontendOrigin)
		response, err := origin.Client().Do(request)
		if err != nil {
			t.Fatal("owned normal login failed")
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("owned %s normal login status=%d", role, response.StatusCode)
		}
		var token string
		for _, cookie := range response.Cookies() {
			if cookie.Name == authSessionCookieName {
				token = cookie.Value
			}
		}
		if token == "" {
			t.Fatal("normal login did not set its session cookie")
		}
		old := f.editor
		if role == "other" {
			old = f.otherEditor
		}
		f.userIDs[token] = f.userIDs[old]
		if role == "creator" {
			f.editor = token
		} else {
			f.otherEditor = token
		}
		private[role+"Token"] = token
	}
	authorTokens["tutorial"] = f.editor // normal login replaced its revoked helper token
	private["authorTokens"] = authorTokens
	actors["communityAuthors"] = authorActors
	rows := []map[string]any{}
	purposes := map[string]string{"tutorial": "Configure a supported client safely using a published community guide.", "issue": "Describe a reproducible menu rendering issue for the declared project.", "news": "Announce the next documented maintenance release with source references.", "discussion": "Discuss resource selection behavior and preserve author editing access."}
	collections := map[string]string{"tutorial": "tutorials", "issue": "issues", "news": "news", "discussion": "discussions"}
	for index, kind := range []string{"tutorial", "issue", "news", "discussion", "tutorial"} {
		authorToken := authorTokens[kind]
		pending := index == 4
		title := "Community editor " + kind
		body := purposes[kind]
		if pending {
			title = "Queued author guide"
			body = "A separate guide awaiting independent moderation must keep its editor locked."
		}
		snapshot := communityPostSnapshot{Kind: kind, Title: title, SourceLocale: "en-US", BodyMarkdown: body, MinecraftVersions: []string{}, Projects: []communityPostReference{}, Resources: []communityPostReference{}}
		if kind == "issue" {
			snapshot.Severity = "minor"
			snapshot.ModVersionMin = "1.0.0"
			snapshot.ModVersionMax = "1.0.0"
			snapshot.MinecraftVersions = []string{"1.20.1"}
			snapshot.Projects = []communityPostReference{{PublicID: f.modCode, Type: "mod"}}
		}
		payload, err := json.Marshal(communityPostMutationRequest{communityPostSnapshot: snapshot})
		if err != nil {
			t.Fatal(err)
		}
		request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, origin.URL+"/api/v1/community/posts", bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+authorToken)
		response, err := origin.Client().Do(request)
		if err != nil {
			t.Fatal("owned community author HTTP failed")
		}
		bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		var envelope map[string]any
		if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
			t.Fatal(err)
		}
		auditJourneyWriteJSON(t, filepath.Join(directory, fmt.Sprintf("community-create-%02d.json", index)), map[string]any{"kind": kind, "author": authorActors[kind], "status": response.StatusCode, "retryAfter": response.Header.Get("Retry-After"), "bodySHA256": fmt.Sprintf("%x", sha256.Sum256(bodyBytes)), "envelope": envelope})
		if response.StatusCode != 201 {
			t.Fatalf("ordinary %s create status=%d retryAfter=%s; exact safe response evidence retained, no automatic write replay", kind, response.StatusCode, response.Header.Get("Retry-After"))
		}
		item := decodeTEST022Data[test042Mutation](t, bodyBytes)
		if item.ReviewStatus != "pending" || item.RevisionID == "" || item.ChangeRequestID == "" {
			t.Fatal("ordinary author bypassed review or lost revision identity")
		}
		lockPath := "/api/v1/review-locks/community_post/" + item.ID
		initial := decodeTEST022Data[reviewLockResponse](t, f.require(t, authorToken, http.MethodGet, lockPath, nil, 200))
		if !initial.Locked || initial.RequestID != item.ChangeRequestID {
			t.Fatal("pending source is not correctly locked")
		}
		for _, actor := range []string{f.otherEditor, f.reviewer, f.denied} {
			f.require(t, actor, http.MethodGet, lockPath, nil, 403)
		}
		f.require(t, "", http.MethodGet, lockPath, nil, 401)
		if !pending {
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+item.RevisionID, map[string]string{"status": "approved", "note": "Independent ordinary community editor fixture precondition"}, 200)
			unlocked := decodeTEST022Data[reviewLockResponse](t, f.require(t, authorToken, http.MethodGet, lockPath, nil, 200))
			if unlocked.Locked {
				t.Fatal("approved author remains locked")
			}
			detail := decodeTEST022Data[communityPostResponse](t, f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+item.ID, nil, 200))
			if detail.Title != title || detail.ReviewStatus != "approved" {
				t.Fatal("independently approved source is not public")
			}
		}
		rows = append(rows, map[string]any{"kind": kind, "author": authorActors[kind], "id": item.ID, "revisionId": item.RevisionID, "changeRequestId": item.ChangeRequestID, "pending": pending, "title": title, "body": body, "editURL": "/" + collections[kind] + "/" + item.ID + "/edit", "reviewLockPath": lockPath})
	}
	// Normal messaging prerequisites for the separate identity-race risk. No
	// SQL message/conversation insertion, administrator or contact-rule bypass.
	messageCases := map[string]any{}
	for _, row := range []struct{ key, owner, peer, body string }{
		{"A", f.editor, f.denied, "Private alpha notebook: only author A and its own recipient may read this synthetic text."},
		{"B", f.otherEditor, f.reviewer, "Private beta journal: the newly logged-in B identity has a different isolated synthetic conversation."},
	} {
		var ownerPublicID, peerPublicID string
		if err := db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[row.owner]).Scan(&ownerPublicID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[row.peer]).Scan(&peerPublicID); err != nil {
			t.Fatal(err)
		}
		conversation := decodeTEST022Data[map[string]string](t, f.require(t, row.owner, http.MethodPost, "/api/v1/messages/conversations", startConversationRequest{UserID: peerPublicID}, 200))
		path := "/api/v1/messages/conversations/" + conversation["id"]
		message := decodeTEST022Data[directMessageItem](t, f.require(t, row.owner, http.MethodPost, path, sendDirectMessageRequest{Body: row.body}, 201))
		foreign := f.otherEditor
		if row.key == "B" {
			foreign = f.editor
		}
		f.require(t, foreign, http.MethodGet, path, nil, 403)
		messageCases[row.key] = map[string]any{"conversationId": conversation["id"], "messageId": message.ID, "body": row.body, "ownerPublicId": ownerPublicID, "peerPublicId": peerPublicID, "createdVia": "normal actual HTTP with user.message.send/receive; no block or contact-policy exception"}
	}
	snapshotFacts := func(name string) {
		var facts []byte
		err := db.QueryRow(f.ctx, `select jsonb_agg(jsonb_build_object('id',post.public_id,'kind',post.kind,'status',post.status,'reviewStatus',post.review_status,'authorId',actor.public_id,'publishedRevisionId',revision.public_id,'title',post.title) order by post.public_id) from community_posts post join users actor on actor.id=post.author_id left join content_revisions revision on revision.id=post.published_revision_id`).Scan(&facts)
		if err != nil {
			t.Fatal(err)
		}
		var decoded any
		if err = json.Unmarshal(facts, &decoded); err != nil {
			t.Fatal(err)
		}
		auditJourneyWriteJSON(t, filepath.Join(directory, name), map[string]any{"posts": decoded, "messageCases": messageCases, "sourceRevisions": revisions, "capturedAt": time.Now().UTC().Format(time.RFC3339Nano)})
	}
	snapshotFacts("community-before-browser.json")
	authFile := filepath.Join(directory, "auth.private.json")
	auditJourneyWriteJSON(t, authFile, private)
	var schema string
	if err = db.QueryRow(f.ctx, "select current_schema()").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	runtime := map[string]any{"owner": "mcmods-live-community-editor", "pid": os.Getpid(), "apiOrigin": origin.URL, "frontendOrigin": frontendOrigin, "repoRevisions": revisions, "authFile": authFile, "ownedSchema": schema, "actors": actors, "posts": rows, "messageCases": messageCases, "stopPath": filepath.Join(directory, "STOP"), "createdAt": time.Now().UTC().Format(time.RFC3339Nano), "scope": "Four predeclared kind-specific ordinary authors, normal HTTP creation and independent review prerequisites, actual PostgreSQL; same-author fourth-submit429 remains its separate historical quota result. Two normal HTTP private message conversations for A/B identity-race testing. External native browser still required. No queue, administrator or no-review claim."}
	auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
	t.Logf("COMMUNITY_EDITOR_READY runtime=%s; private credentials omitted", filepath.Join(directory, "runtime.json"))
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-f.ctx.Done():
			runtime["state"] = "EXPIRED"
			auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
			t.Fatal("owned community fixture expired before normal STOP")
		case <-ticker.C:
			if _, e := os.Stat(filepath.Join(directory, "STOP")); e == nil {
				snapshotFacts("community-after-browser.json")
				runtime["state"] = "STOPPED"
				runtime["stoppedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
				auditJourneyWriteJSON(t, filepath.Join(directory, "runtime.json"), runtime)
				return
			} else if !os.IsNotExist(e) {
				t.Fatal(e)
			}
		}
	}
}
