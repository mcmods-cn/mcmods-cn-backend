package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/security"
)

// The editor stays authorized to edit. An administrator grants automatic
// publication while its ordinary request waits for the revision's row lock.
func TestProjectEditUsesNewApprovalGrantAfterLockIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create")
	input := perf020ModpackSnapshot("locked-edit-pack", 0)
	project := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", input, http.StatusCreated))
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+project.SubmissionRevisionID, map[string]string{"status": "approved"}, http.StatusOK)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+project.PublicID)
	input.Summary = "Approved by the current editor grant"
	status, body := runLockedEdit(t, f, f.otherEditor, http.MethodPut, "/api/v1/modpacks/"+project.SiteID,
		map[string]any{"snapshot": input, "baseRevisionId": project.SubmissionRevisionID},
		"select id from modpacks where public_id=$1 for update", []any{project.PublicID}, func() {
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "content.no-review")
		})
	if status != http.StatusCreated {
		t.Fatalf("edit response %d: %s", status, body)
	}
	edit := decodeTEST022Data[struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}](t, body)
	if edit.Status != "approved" {
		t.Fatalf("edit decision after granting publication permission = %q", edit.Status)
	}
	public := decodeTEST022Data[modpackResponse](t, f.require(t, "", http.MethodGet, "/api/v1/modpacks/"+project.SiteID, nil, http.StatusOK))
	if public.PublishedRevisionID == nil || *public.PublishedRevisionID != edit.ID || public.Summary != input.Summary {
		t.Fatalf("public projection did not use the approved edit: id=%v summary=%q", public.PublishedRevisionID, public.Summary)
	}
}

func runLockedEdit(t *testing.T, f test013Fixture, token, method, path string, body any, lockSQL string, args []any, change func()) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 15*time.Second)
	defer cancel()
	holder, err := f.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err = holder.Exec(ctx, lockSQL, args...); err != nil {
		t.Fatal(err)
	}
	pid := holder.Conn().PgConn().PID()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(ctx, method, f.origin.URL+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	type reply struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan reply, 1)
	go func() {
		res, err := f.origin.Client().Do(request)
		if err != nil {
			done <- reply{err: err}
			return
		}
		defer res.Body.Close()
		data, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		done <- reply{res.StatusCode, data, err}
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err = f.db.QueryRow(ctx, `select exists(select from pg_stat_activity where datname=current_database()
			and $1::int=any(pg_blocking_pids(pid)))`, pid).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case early := <-done:
			t.Fatalf("edit did not reach its target lock: %d %s %v", early.status, early.body, early.err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	change()
	if err = holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		return result.status, result.body
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return 0, nil
	}
}

func newCurrentWriteTestActor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, suffix string, grants ...string) security.Claims {
	t.Helper()
	cfg := config.Load()
	id, _, token := createTEST044User(t, ctx, pool, cfg, suffix, "UTC")
	grantTEST044Permissions(t, ctx, pool, id, grants...)
	claims, err := security.ParseToken(cfg.JWTSecret, token)
	if err != nil {
		t.Fatal(err)
	}
	claims.Subject = id
	permissions, err := resolveUsersRootPermissionsQuery(ctx, pool, []int64{id})
	if err != nil {
		t.Fatal(err)
	}
	claims.PermissionRules = permissions[id].Permissions
	return claims
}

// Opening a policy during a lock wait changes no post content. A stale
// repeatable-read edit can retry against the identical content baseline.
func TestCommunityEditRetriesAfterReviewPolicyChangesDuringLockIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	f.reviewPolicy(t, false)
	snapshot := f.snapshot("tutorial", "Policy baseline")
	post := f.create(t, f.editor, snapshot)
	if post.ReviewStatus != "approved" {
		t.Fatal("initial policy did not publish fixture")
	}
	if _, err := f.db.Exec(f.ctx, `update system_settings set value=jsonb_set(value,'{tutorialEdit}','true') where key=$1`, reviewConfigSettingKey); err != nil {
		t.Fatal(err)
	}
	snapshot.Title = "Edit after policy change"
	update := communityPostMutationRequest{communityPostSnapshot: snapshot, BaseRevisionID: &post.RevisionID}
	path := "/api/v1/community/posts/" + post.ID
	status, raw := runLockedEdit(t, f.test013Fixture, f.editor, http.MethodPut, path, update,
		"select pg_advisory_xact_lock(hashtext($1))", []any{communityPostAggregate + ":" + post.ID}, func() { f.reviewPolicy(t, false) })
	var conflict struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &conflict); err != nil || status != http.StatusConflict || conflict.Code != "COMMUNITY_POST_AUTHORITY_CHANGED" {
		t.Fatalf("changing policy while the edit waited: status=%d response=%s err=%v", status, raw, err)
	}
	detail := f.detail(t, "", post.ID)
	if detail.PublishedRevisionID != post.RevisionID || detail.Title == snapshot.Title {
		t.Fatal("policy conflict changed the public post")
	}
	saved := decodeTEST022Data[test042Mutation](t, f.require(t, f.editor, http.MethodPut, path, update, http.StatusOK))
	if saved.ReviewStatus != "approved" || saved.RevisionID == post.RevisionID {
		t.Fatalf("fresh policy retry did not publish: %#v", saved)
	}
}

// Hold allocation of the public ID while an administrator opens creation.
// This is before the business transaction and after validation of this normal request.
func TestProjectCreationReadsModerationAfterPreparationIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "modpack.create")
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,'{"modpackCreate":true}')
		on conflict(key) do update set value=jsonb_set(system_settings.value,'{modpackCreate}','true')`, reviewConfigSettingKey); err != nil {
		t.Fatal(err)
	}
	input := perf020ModpackSnapshot("creation-policy", 0)
	status, body := runLockedEdit(t, f, f.editor, http.MethodPost, "/api/v1/modpacks", input,
		"lock table public_routes in access exclusive mode", nil, func() {
			var allocating bool
			if err := f.db.QueryRow(f.ctx, `select exists(select from pg_stat_activity where datname=current_database()
				and cardinality(pg_blocking_pids(pid))>0 and query like 'select exists(select 1 from public_routes where public_id =%')`).Scan(&allocating); err != nil || !allocating {
				t.Fatalf("creation did not wait for public ID allocation: %t, %v", allocating, err)
			}
			if _, err := f.db.Exec(f.ctx, `update system_settings set value=jsonb_set(value,'{modpackCreate}','false') where key=$1`, reviewConfigSettingKey); err != nil {
				t.Fatal(err)
			}
		})
	if status != http.StatusCreated {
		t.Fatalf("create response %d: %s", status, body)
	}
	created := decodeTEST022Data[modpackResponse](t, body)
	if created.ReviewStatus != "approved" || created.PublishedRevisionID == nil {
		t.Fatalf("creation used an earlier moderation policy: %s, published=%v", created.ReviewStatus, created.PublishedRevisionID)
	}
}
