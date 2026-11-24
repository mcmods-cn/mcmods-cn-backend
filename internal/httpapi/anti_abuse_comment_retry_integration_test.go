package httpapi

import (
	"net/http"
	"testing"
)

func TestCommittedCommentRetryReturnsExistingResultWithoutNewTelemetryIntegration(t *testing.T) {
	f := newAuditFixGRFixture(t)
	if !f.server.antiAbuse.Enabled() {
		t.Fatal("requires the active anti-abuse middleware")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "comment.create")
	path := "/api/v1/comment-targets/mod/" + f.modCode + "/comments"
	submission := createCommentRequest{Body: "Ordinary comment about installing this mod", IdempotencyKey: "retry-body-key"}
	first := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPost, path, submission, 201))
	before := f.server.antiAbuse.AsyncMetrics().SuccessQueued
	retried := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPost, path, submission, 200))
	if first.ID == "" || retried.ID != first.ID {
		t.Fatalf("idempotent retry returned different comment: first=%q retry=%q", first.ID, retried.ID)
	}
	if after := f.server.antiAbuse.AsyncMetrics().SuccessQueued; after != before {
		t.Fatalf("replay queued new write telemetry: before=%d after=%d", before, after)
	}
	if count := f.number(t, "select count(*) from comments where author_id=$1 and idempotency_key=$2",
		f.userIDs[f.editor], submission.IdempotencyKey); count != 1 {
		t.Fatalf("persisted comment count=%d", count)
	}
}

func TestCommentSubmissionIdentities(t *testing.T) {
	for _, test := range []struct {
		name, bodyKey, header, parent, body, want string
		watch                                     bool
	}{
		{"body", " typed ", "header", "", "comment", "typed", false},
		{"header", " ", " legacy ", "parent", "reply", "legacy", false},
		{"generated", "", "", "", "comment", "", false},
		{"watch command", "", "", " parent ", " CY。 ", "", true},
		{"root text", "", "", "", " CY。 ", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := createCommentRequest{IdempotencyKey: test.bodyKey, ParentID: test.parent, Body: test.body}
			if key := r.key(test.header); key != test.want || r.watchOnly() != test.watch {
				t.Fatalf("key=%q watch=%v, want %q/%v", key, r.watchOnly(), test.want, test.watch)
			}
		})
	}
}
