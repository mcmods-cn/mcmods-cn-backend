package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Use the ordinary review routes and the current installed schema. Observe the
// blocked reviewer in PostgreSQL before changing the durable authority facts.
func TestProjectReviewRechecksAuthorityAfterTargetLockIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	editorID, reviewerID := f.userIDs[f.editor], f.userIDs[f.reviewer]
	grantTEST044Permissions(t, f.ctx, f.db, editorID, "project.create", "modpack.create")

	t.Run("mod_reviewer_permission_revoked", func(t *testing.T) {
		input := createModRequest{
			SiteID: "review-authority-mod", PrimaryName: "Review authority mod", DefaultLocale: "en-US",
			Localizations: []catalogLocalizationEdit{{Locale: "en-US", Name: "Review authority mod", Summary: "Test fixture"}},
			Environment:   "bothRequired", PrimaryCategory: "utility", OfficialStatus: "active", SourceStatus: "open",
			License: "MIT", SubmissionMethod: "manual", ModIDs: []modIdentifierPayload{{Identifier: "review_authority_mod", Primary: true}},
			Compatibilities: []modLoaderCompatibilityPayload{{Loader: "neoforge", Versions: []string{"1.21.1"}}},
		}
		project := decodeTEST022Data[modResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/mods", input, http.StatusCreated))
		path := "/api/v1/mods/" + project.SiteID + "/revisions/" + project.SubmissionRevisionID
		query := `select published_revision_id from mods where slug=$1`
		assertLockedProjectReviewRejected(t, f, path, project.SubmissionRevisionID, query, project.SiteID, http.StatusForbidden, func(ctx context.Context) {
			deleted, err := f.db.Exec(ctx, `delete from user_permissions grant_row using permissions permission
				where grant_row.user_id=$1 and grant_row.permission_id=permission.id and permission.code='content.review'`, reviewerID)
			if err != nil || deleted.RowsAffected() != 1 {
				t.Fatalf("revoke fixture reviewer grant: affected=%d err=%v", deleted.RowsAffected(), err)
			}
		})
		grantTEST044Permissions(t, f.ctx, f.db, reviewerID, "content.review")
		f.require(t, f.reviewer, http.MethodPatch, path, map[string]string{"status": "approved"}, http.StatusOK)
	})

	t.Run("modpack_reviewer_session_revoked", func(t *testing.T) {
		grantTEST044Permissions(t, f.ctx, f.db, reviewerID, "content.review")
		project := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks",
			perf020ModpackSnapshot("review-authority-pack", 0), http.StatusCreated))
		path := "/api/v1/content-revisions/" + project.SubmissionRevisionID
		query := `select published_revision_id from modpacks where public_id=$1`
		assertLockedProjectReviewRejected(t, f, path, project.SubmissionRevisionID, query, project.PublicID, http.StatusUnauthorized, func(ctx context.Context) {
			revoked, err := f.db.Exec(ctx, `update auth_sessions set revoked_at=clock_timestamp()
				where user_id=$1 and revoked_at is null`, reviewerID)
			if err != nil || revoked.RowsAffected() != 1 {
				t.Fatalf("revoke fixture reviewer session: affected=%d err=%v", revoked.RowsAffected(), err)
			}
		})
	})
}

func TestProjectReviewDoesNotGrantGlobalContentDecisionIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "project.review")
	item := f.create(t, f.editor, f.snapshot("tutorial", "Independent community review"))
	if item.ReviewStatus != "pending" {
		t.Fatal("fixture tutorial must require independent review")
	}
	queue := decodeTEST022Data[struct {
		Items []modContentReviewItem `json:"items"`
	}](t, f.require(t, f.denied, http.MethodGet, "/api/v1/reviews/content?category=community_tutorial", nil, http.StatusOK))
	if len(queue.Items) != 0 {
		t.Fatal("project reviewer received a content-only task in their queue")
	}
	before := f.facts(t)
	f.require(t, f.denied, http.MethodPatch, "/api/v1/content-revisions/"+item.RevisionID,
		map[string]string{"status": "approved"}, http.StatusForbidden)
	f.unchanged(t, before)
	f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+item.ID, nil, http.StatusNotFound)
	f.review(t, item, "approved")
	if public := f.detail(t, "", item.ID); public.PublishedRevisionID != item.RevisionID {
		t.Fatal("global content reviewer did not publish the pending tutorial")
	}
}

func assertLockedProjectReviewRejected(t *testing.T, f test013Fixture, path, revision, projectQuery, project string, want int, change func(context.Context)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
	defer cancel()
	if revision == "" {
		t.Fatal("project submission returned no reviewable revision")
	}
	holder, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(ctx)
	var published *int64
	if err = holder.QueryRow(ctx, projectQuery+` for update`, project).Scan(&published); err != nil || published != nil {
		t.Fatalf("lock fixture publication target: published=%v err=%v", published, err)
	}
	var holderPID int32
	if err = holder.QueryRow(ctx, `select pg_backend_pid()`).Scan(&holderPID); err != nil {
		t.Fatal(err)
	}

	type result struct {
		status int
		body   string
		err    error
	}
	response := make(chan result, 1)
	request, err := http.NewRequestWithContext(ctx, http.MethodPatch, f.origin.URL+path,
		strings.NewReader(`{"status":"approved","note":"review authority regression"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+f.reviewer)
	go func() {
		review, err := f.origin.Client().Do(request)
		if err != nil {
			response <- result{err: err}
			return
		}
		defer review.Body.Close()
		body, err := io.ReadAll(io.LimitReader(review.Body, 64<<10))
		response <- result{status: review.StatusCode, body: string(body), err: err}
	}()

	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for {
		var waiting bool
		if err = f.db.QueryRow(ctx, `select exists(select 1 from pg_stat_activity
			where datname=current_database() and wait_event_type='Lock' and $1=any(pg_blocking_pids(pid)))`, holderPID).Scan(&waiting); err != nil {
			t.Fatalf("observe reviewer target lock: %v", err)
		}
		if waiting {
			break
		}
		select {
		case premature := <-response:
			t.Fatalf("review did not reach its target lock: status=%d err=%v body=%s", premature.status, premature.err, premature.body)
		case <-ctx.Done():
			t.Fatalf("review did not reach its target lock: %v", ctx.Err())
		case <-poll.C:
		}
	}
	change(ctx)
	if err = holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var reviewed result
	select {
	case reviewed = <-response:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var status string
	var decisions int
	if err = f.db.QueryRow(ctx, `select request.status,(select count(*)::int from review_events event
		where event.change_request_id=request.id and event.event_type in ('approved','rejected','conflicted','published'))
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where revision.public_id=$1`, revision).Scan(&status, &decisions); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(ctx, projectQuery, project).Scan(&published); err != nil {
		t.Fatal(err)
	}
	if reviewed.err != nil || reviewed.status != want || status != "pending" || published != nil || decisions != 0 {
		t.Fatalf("review after authority change status=%d want=%d request=%s published=%v decisions=%d err=%v body=%s",
			reviewed.status, want, status, published != nil, decisions, reviewed.err, reviewed.body)
	}
}
