package httpapi

import (
	"net/http"
	"testing"
)

func TestHiddenParentCommentPreviewStaysPrivateIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	root := f.comment(t, f.editor, f.modCode, "preview-root", "ordinary root", "")
	parent := f.comment(t, f.otherEditor, f.modCode, "preview-parent", "ordinary parent for review", root.ID)
	reply := f.comment(t, f.editor, f.modCode, "preview-reply", "ordinary reply", parent.ID)
	threadPath := "/api/v1/comments/" + reply.ID + "/thread"
	before := decodeTEST022Data[commentThreadPageResponse](t, f.require(t, "", http.MethodGet, threadPath, nil, http.StatusOK))
	var baselinePreview bool
	for _, item := range before.Items {
		if item.ID == reply.ID && item.Parent != nil && item.Parent.BodySummary == parent.Body {
			baselinePreview = true
		}
	}
	if !baselinePreview {
		t.Fatal("published parent preview is missing from the visible child")
	}
	hideTestCommentLogTarget(t, f, "comment", parent.ID)
	f.require(t, "", http.MethodGet, "/api/v1/comments/"+parent.ID+"/thread", nil, http.StatusNotFound)
	after := decodeTEST022Data[commentThreadPageResponse](t, f.require(t, "", http.MethodGet, threadPath, nil, http.StatusOK))
	var sawReply bool
	for _, item := range after.Items {
		if item.ID == parent.ID {
			t.Fatal("hidden parent was returned as a thread item")
		}
		if item.ID != reply.ID {
			continue
		}
		sawReply = true
		if item.Parent == nil || item.Parent.ID != parent.ID || !item.Parent.Deleted || item.Parent.BodySummary != "" || item.Parent.AuthorName != "" {
			t.Error("hidden parent preview should keep only the thread placeholder")
		}
	}
	if !sawReply {
		t.Fatal("hiding a parent must preserve its still-published replies")
	}
}

func hideTestCommentLogTarget(t *testing.T, f test038Fixture, kind, publicID string) {
	t.Helper()
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if err = f.server.moderationHideTarget(f.ctx, tx, kind, publicID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
}
