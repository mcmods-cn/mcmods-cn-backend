package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02CommunityRevisionFailuresDoNotExposeStoreDiagnosticsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	s := &Server{db: f.db}
	claims := security.Claims{Subject: f.userIDs[f.editor], PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}}
	reject := func() {
		t.Helper()
		if _, err := f.db.Exec(f.ctx, `alter table content_revisions add constraint oct02_private_store_diagnostic check(entity_type<>'community_post') not valid`); err != nil {
			t.Fatal(err)
		}
	}
	reject()
	snapshot := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "Synthetic original", SourceLocale: "en-US", BodyMarkdown: "Synthetic content"}
	invokeCreate := func() *httptest.ResponseRecorder {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/community/posts", bytes.NewReader(raw))
		r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, claims))
		w := httptest.NewRecorder()
		s.createCommunityPost(w, r)
		return w
	}
	w := invokeCreate()
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "oct02_private_store_diagnostic") || strings.Contains(w.Body.String(), "SQLSTATE") {
		t.Errorf("create leaked store diagnostic status=%d body=%s", w.Code, w.Body.String())
	}
	var posts int
	if err := f.db.QueryRow(f.ctx, `select count(*) from community_posts where author_id=$1`, claims.Subject).Scan(&posts); err != nil || posts != 0 {
		t.Fatalf("failed create persisted posts=%d err=%v", posts, err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table content_revisions drop constraint oct02_private_store_diagnostic`); err != nil {
		t.Fatal(err)
	}
	w = invokeCreate()
	if w.Code != http.StatusCreated {
		t.Fatalf("healthy create status=%d body=%s", w.Code, w.Body.String())
	}
	var publicID, revisionID string
	if err := f.db.QueryRow(f.ctx, `select post.public_id,revision.public_id from community_posts post join content_revisions revision on revision.id=post.published_revision_id where post.author_id=$1`, claims.Subject).Scan(&publicID, &revisionID); err != nil {
		t.Fatal(err)
	}
	reject()
	w = invokeBUG090CommunityPostUpdate(t, f.ctx, s, claims, publicID, revisionID, "Synthetic rejected edit")
	if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "oct02_private_store_diagnostic") || strings.Contains(w.Body.String(), "SQLSTATE") {
		t.Errorf("update leaked store diagnostic status=%d body=%s", w.Code, w.Body.String())
	}
	var title string
	if err := f.db.QueryRow(f.ctx, `select title from community_posts where public_id=$1`, publicID).Scan(&title); err != nil || title != snapshot.Title {
		t.Fatalf("failed update changed title=%q err=%v", title, err)
	}
}
