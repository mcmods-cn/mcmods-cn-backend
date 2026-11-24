package httpapi

import (
	"bytes"
	"net/http"
	"testing"
)

func TestOCT02CommunityRevisionFailuresDoNotExposeStoreDiagnosticsIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	authorID := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, authorID, "community.tutorial.create", "community.no-review")
	reject := func() {
		t.Helper()
		if _, err := f.db.Exec(f.ctx, `alter table content_revisions add constraint oct02_private_store_diagnostic check(entity_type<>'community_post') not valid`); err != nil {
			t.Fatal(err)
		}
	}
	checkFailure := func(method, path string, payload any) {
		t.Helper()
		raw := f.require(t, f.editor, method, path, payload, http.StatusInternalServerError)
		if bytes.Contains(raw, []byte("oct02_private_store_diagnostic")) || bytes.Contains(raw, []byte("SQLSTATE")) {
			t.Fatalf("%s %s returned private store diagnostics: %s", method, path, raw)
		}
	}

	reject()
	const path = "/api/v1/community/posts"
	snapshot := communityPostSnapshot{Kind: "tutorial", Category: "general", Title: "Synthetic original", SourceLocale: "en-US", BodyMarkdown: "Synthetic content"}
	checkFailure(http.MethodPost, path, snapshot)
	var posts int
	if err := f.db.QueryRow(f.ctx, `select count(*) from community_posts where author_id=$1`, authorID).Scan(&posts); err != nil || posts != 0 {
		t.Fatalf("failed create persisted posts=%d err=%v", posts, err)
	}
	if _, err := f.db.Exec(f.ctx, `alter table content_revisions drop constraint oct02_private_store_diagnostic`); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, path, snapshot, http.StatusCreated)
	var publicID, revisionID string
	if err := f.db.QueryRow(f.ctx, `select post.public_id,revision.public_id from community_posts post join content_revisions revision on revision.id=post.published_revision_id where post.author_id=$1`, authorID).Scan(&publicID, &revisionID); err != nil {
		t.Fatal(err)
	}

	reject()
	edit := communityPostMutationRequest{communityPostSnapshot: snapshot, BaseRevisionID: &revisionID}
	edit.Title = "Synthetic rejected edit"
	checkFailure(http.MethodPut, path+"/"+publicID, edit)
	var title, publishedRevision string
	if err := f.db.QueryRow(f.ctx, `select post.title,revision.public_id from community_posts post join content_revisions revision on revision.id=post.published_revision_id where post.public_id=$1`, publicID).Scan(&title, &publishedRevision); err != nil || title != snapshot.Title || publishedRevision != revisionID {
		t.Fatalf("failed update changed title/revision=%q/%q err=%v", title, publishedRevision, err)
	}
}
