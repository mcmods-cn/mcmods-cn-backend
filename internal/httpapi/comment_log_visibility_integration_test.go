package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCommentLogAttachmentReadyShareRespectsCurrentVisibilityIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	worker := NewCommentLogAttachmentWorker(f.server)
	for _, name := range []string{"hidden", "deleted", "restricted-target", "banned-author"} {
		t.Run(name, func(t *testing.T) {
			target := f.modCode
			if name == "restricted-target" {
				target = f.otherModCode
			}
			comment, fileID, jobID := createTestCommentLogAttachment(t, f, target, "ready-"+name)
			if err := worker.processJob(f.ctx, jobID); err != nil {
				t.Fatal(err)
			}
			var code string
			if err := f.db.QueryRow(f.ctx, `select share.public_code from log_shares share
				join comment_log_bindings binding on binding.log_share_id=share.id
				where binding.comment_id=(select id from comments where public_id=$1)`, comment.ID).Scan(&code); err != nil {
				t.Fatal(err)
			}
			assertTestCommentLogRead(t, f, "", code, http.StatusOK)
			f.require(t, "", http.MethodGet, "/api/v1/comments/"+comment.ID+"/attachments/"+fileID+"/download", nil, http.StatusNotFound)
			changeTestCommentLogVisibility(t, f, name, comment.ID, target)
			assertTestCommentLogRead(t, f, "", code, http.StatusNotFound)
			if name == "restricted-target" {
				assertTestCommentLogRead(t, f, f.editor, code, http.StatusNotFound)
				assertTestCommentLogRead(t, f, f.otherEditor, code, http.StatusOK)
			}
		})
	}
}

func TestCommentLogAttachmentChecksVisibilityAfterClaimIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	worker := NewCommentLogAttachmentWorker(f.server)
	for _, name := range []string{"hidden", "deleted", "restricted-target", "banned-author"} {
		t.Run(name, func(t *testing.T) {
			target := f.modCode
			if name == "restricted-target" {
				target = f.otherModCode
			}
			comment, fileID, jobID := createTestCommentLogAttachment(t, f, target, "claimed-"+name)
			job, claimed, err := worker.claimJob(f.ctx, jobID)
			if err != nil || !claimed {
				t.Fatalf("claim = %v, %v", claimed, err)
			}
			prepared, err := prepareFileLogShare(f.ctx, f.db, worker.readSource, job.requestedBy, fileID, &job.commentID)
			if err != nil {
				t.Fatal(err)
			}
			changeTestCommentLogVisibility(t, f, name, comment.ID, target)
			completeErr := worker.completeJob(f.ctx, job, prepared)
			var shares, bindings int
			if err = f.db.QueryRow(f.ctx, `select (select count(*) from log_shares where source_file_id=$1),
				(select count(*) from comment_log_bindings where comment_id=$2 and attachment_file_id=$1)`, job.fileID, job.commentID).Scan(&shares, &bindings); err != nil {
				t.Fatal(err)
			}
			if completeErr == nil || shares != 0 || bindings != 0 {
				t.Errorf("visibility changed after claim: shares=%d bindings=%d, completion error=%v", shares, bindings, completeErr)
			}
		})
	}
}

func TestPendingCommentLogIsReadableOnlyAfterPublicationIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	comment, _, jobID := createTestCommentLogAttachment(t, f, f.modCode, "pending-comment")
	if _, err := f.db.Exec(f.ctx, `update comments set status='pending' where public_id=$1`, comment.ID); err != nil {
		t.Fatal(err)
	}
	if err := NewCommentLogAttachmentWorker(f.server).processJob(f.ctx, jobID); err != nil {
		t.Fatal(err)
	}
	var code string
	if err := f.db.QueryRow(f.ctx, `select public_code from log_shares where comment_scope_id=(select id from comments where public_id=$1)`, comment.ID).Scan(&code); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", f.editor} {
		assertTestCommentLogRead(t, f, token, code, http.StatusNotFound)
	}
	if _, err := f.db.Exec(f.ctx, `update comments set status='published' where public_id=$1`, comment.ID); err != nil {
		t.Fatal(err)
	}
	assertTestCommentLogRead(t, f, "", code, http.StatusOK)
}

func TestStandaloneAndCommentLogShareHaveSeparateVisibilityIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	comment, fileID, jobID := createTestCommentLogAttachment(t, f, f.modCode, "separate-scopes")
	standalone := decodeTEST022Data[struct {
		Items []struct {
			PublicCode string `json:"publicCode"`
		} `json:"items"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/log-shares/files", createFileLogSharesRequest{FileIDs: []string{fileID}, RetentionDays: 30}, http.StatusMultiStatus))
	if len(standalone.Items) != 1 || standalone.Items[0].PublicCode == "" {
		t.Fatal("owner's intentional standalone share is missing")
	}
	if err := NewCommentLogAttachmentWorker(f.server).processJob(f.ctx, jobID); err != nil {
		t.Fatal(err)
	}
	page := decodeTEST022Data[commentThreadPageResponse](t, f.require(t, "", http.MethodGet, "/api/v1/comments/"+comment.ID+"/thread", nil, http.StatusOK))
	if len(page.Items) != 1 || len(page.Items[0].Attachments) != 1 || page.Items[0].Attachments[0].URL == "" ||
		page.Items[0].Attachments[0].URL == "/log/s/"+standalone.Items[0].PublicCode {
		t.Fatal("comment must not share the intentionally public standalone capability")
	}
	code, found := strings.CutPrefix(page.Items[0].Attachments[0].URL, "/log/s/")
	if !found || code == "" {
		t.Fatal("comment did not get its scoped short link")
	}
	assertTestCommentLogRead(t, f, "", code, http.StatusOK)

	// A governance ban is an expiring role binding. The author remains active.
	authorID := f.userIDs[f.editor]
	var authorPublicID string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, authorID).Scan(&authorPublicID); err != nil {
		t.Fatal(err)
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "ban.create")
	endsAt := time.Now().Add(5 * time.Minute)
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/admin/bans", createBanRequest{
		UserID: authorPublicID, ReasonCode: "malicious_spam", EndsAt: &endsAt,
	}, http.StatusCreated)
	var status string
	if err := f.db.QueryRow(f.ctx, `select status from users where id=$1`, authorID).Scan(&status); err != nil || status != "active" {
		t.Fatalf("governance ban should leave the fixture author's status active: %q, %v", status, err)
	}
	assertTestCommentLogRead(t, f, "", code, http.StatusNotFound)
	assertTestCommentLogRead(t, f, "", standalone.Items[0].PublicCode, http.StatusOK)

	// Move just this fixture's enforcement window into the past without
	// running cleanup. Reads must observe expiry even with an active ban row.
	tag, err := f.db.Exec(f.ctx, `with elapsed as (
		update ban_records set starts_at=now()-interval '3 minutes',ends_at=now()-interval '1 minute'
		where user_id=$1 and status='active' returning id,user_id,ends_at
	) update user_role_bindings binding set expires_at=elapsed.ends_at from elapsed
		where binding.user_id=elapsed.user_id and binding.source=$2 and binding.source_key=elapsed.id::text`,
		authorID, authorizationSourceGovernanceBan)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("move the owned fixture ban window into the past: %v, %v", tag, err)
	}
	assertTestCommentLogRead(t, f, "", code, http.StatusOK)

	hideTestCommentLogTarget(t, f, "comment", comment.ID)
	assertTestCommentLogRead(t, f, "", code, http.StatusNotFound)
	assertTestCommentLogRead(t, f, "", standalone.Items[0].PublicCode, http.StatusOK)
}

func createTestCommentLogAttachment(t *testing.T, f test038Fixture, target, key string) (commentResponse, string, int64) {
	t.Helper()
	raw := []byte("ordinary log line " + key + "\n")
	fileID := uploadTestLogFile(t, f.test037Fixture, key+".log", raw)
	comment := f.comment(t, f.editor, target, key, "log attachment test", "", fileID)
	var jobID int64
	if err := f.db.QueryRow(f.ctx, `select id from comment_log_attachment_jobs
		where comment_id=(select id from comments where public_id=$1)`, comment.ID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	return comment, fileID, jobID
}

func uploadTestLogFile(t *testing.T, f test037Fixture, name string, raw []byte) string {
	t.Helper()
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", name, "text/plain", "comment", raw)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[struct{ ID string }](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), http.StatusCreated))
	if file.ID == "" {
		t.Fatal("completed test log upload did not return a file ID")
	}
	return file.ID
}

func changeTestCommentLogVisibility(t *testing.T, f test038Fixture, name, commentID, target string) {
	t.Helper()
	switch name {
	case "hidden":
		hideTestCommentLogTarget(t, f, "comment", commentID)
	case "deleted":
		f.require(t, f.editor, http.MethodDelete, "/api/v1/comments/"+commentID, nil, http.StatusOK)
	case "restricted-target":
		hideTestCommentLogTarget(t, f, "mod", target)
	case "banned-author":
		if _, err := f.db.Exec(f.ctx, "update users set status='banned' where id=$1", f.userIDs[f.editor]); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := f.db.Exec(f.ctx, "update users set status='active' where id=$1", f.userIDs[f.editor]); err != nil {
				t.Error(err)
			}
		})
	default:
		t.Fatal("unknown visibility scenario")
	}
}

func assertTestCommentLogRead(t *testing.T, f test038Fixture, token, code string, status int) {
	t.Helper()
	base := "/api/v1/log-shares/s/" + code
	for _, endpoint := range []string{"", "/entries/0/content", "/download"} {
		request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+base+endpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := f.origin.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		response.Body.Close()
		if err != nil || response.StatusCode != status {
			t.Errorf("log route suffix %q status=%d want=%d transport=%v", endpoint, response.StatusCode, status, err)
		}
		if response.Header.Get("Cache-Control") != "private, no-store" {
			t.Errorf("log route suffix %q can be cached across viewer changes", endpoint)
		}
		if status >= 400 && bytes.Contains(raw, []byte("ordinary log line")) {
			t.Error("inaccessible log returned its synthetic body")
		}
	}
}
