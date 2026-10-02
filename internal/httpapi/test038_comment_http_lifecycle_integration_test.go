package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type test038Fixture struct{ test037Fixture }

func newTEST038Fixture(t *testing.T) test038Fixture {
	t.Helper()
	f := test038Fixture{newTEST037Fixture(t)}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "comment.create", "comment.react", "comment.watch", "comment.edit.own", "comment.delete.own")
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.comment.pin."+f.modCode)
	return f
}

func (f test038Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := f.test037Fixture.facts(t)
	for _, table := range []string{"comments", "comment_target_counts", "comment_target_author_counts", "comment_floor_counters", "comment_closure", "comment_reactions", "comment_watches", "comment_watch_replies", "comment_heat_refresh_queue", "comment_attachments", "comment_log_bindings", "comment_log_attachment_jobs", "log_shares", "log_share_entries"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		result[table] = raw
	}
	return result
}

func (f test038Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if after[table] != raw {
				t.Errorf("failed/denied comment operation mutated %s", table)
			}
		}
		t.FailNow()
	}
}

func (f test038Fixture) comment(t *testing.T, token, target, key, body, parent string, attachments ...string) commentResponse {
	t.Helper()
	raw := f.require(t, token, http.MethodPost, "/api/v1/comment-targets/mod/"+target+"/comments", createCommentRequest{Body: body, ParentID: parent, IdempotencyKey: "test038-" + key, AttachmentFileIDs: attachments}, 201)
	item := decodeTEST022Data[commentResponse](t, raw)
	if item.ID == "" || item.Author.ID == "" || item.Body != body || !item.CanEdit || !item.CanReply {
		t.Fatalf("actual comment response missing normal author/capability facts: %s", raw)
	}
	return item
}

func TestTEST038PrivateTargetRevocationProtectsEveryFullHTTPCommentSubrouteIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	root := f.comment(t, f.editor, f.otherModCode, "private-root", "test038 private root", "")
	f.comment(t, f.otherEditor, f.otherModCode, "private-reply", "test038 owner reply", root.ID)
	watch := decodeTEST022Data[commentWatchState](t, f.require(t, f.editor, http.MethodPut, "/api/v1/comments/"+root.ID+"/watch", nil, 200))
	if watch.ID == "" || !watch.Active {
		t.Fatal("actual private watch was not created")
	}
	f.require(t, "", http.MethodGet, "/api/v1/comments/"+root.ID+"/thread", nil, 200)
	if _, err := f.db.Exec(f.ctx, "update mods set review_status='pending' where id=$1", f.otherModID); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	for _, token := range []string{"", f.editor} {
		for _, route := range []string{"/api/v1/comments/" + root.ID + "/replies", "/api/v1/comments/" + root.ID + "/thread", "/api/v1/comment-targets/mod/" + f.otherModCode + "/comments"} {
			f.require(t, token, http.MethodGet, route, nil, 404)
		}
	}
	for _, operation := range []struct {
		method, suffix string
		body           any
	}{
		{http.MethodGet, "/watch", nil}, {http.MethodPut, "/watch", nil}, {http.MethodDelete, "/watch", nil},
		{http.MethodPut, "/reaction", map[string]any{"reaction": "heart"}}, {http.MethodPut, "/pin", nil},
		{http.MethodPatch, "", updateCommentRequest{Body: "tampered hidden", BaseUpdatedAt: root.UpdatedAt}}, {http.MethodDelete, "", nil},
	} {
		f.require(t, f.editor, operation.method, "/api/v1/comments/"+root.ID+operation.suffix, operation.body, 404)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/comment-watches/"+watch.ID+"/read", nil, 404)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/comment-watches/"+watch.ID, map[string]any{"mute": "forever"}, 404)
	listed := f.require(t, f.editor, http.MethodGet, "/api/v1/users/me/comment-watches", nil, 200)
	if bytes.Contains(listed, []byte(root.ID)) || bytes.Contains(listed, []byte("test038 private root")) {
		t.Fatal("hidden target escaped own watch list visibility")
	}
	f.unchanged(t, before)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/comments/"+root.ID+"/replies", nil, 200)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/comments/"+root.ID+"/thread", nil, 200)
	f.unchanged(t, before)
}

func TestTEST038IdempotentCommentCannotBeReplayedIntoAnotherVisibleTargetFullHTTPIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	parent := f.comment(t, f.otherEditor, f.otherModCode, "scope-parent", "initial public parent", "")
	root := f.comment(t, f.editor, f.otherModCode, "scope-replay", "private comment payload", parent.ID)
	body := createCommentRequest{Body: root.Body, ParentID: parent.ID, IdempotencyKey: "test038-scope-replay"}
	replayed := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.otherModCode+"/comments", body, 200))
	if replayed.ID != root.ID {
		t.Fatal("same-target retry did not preserve original comment")
	}
	before := f.facts(t)
	row := f.parallel(t, f.editor, []test038Operation{{http.MethodPost, "/api/v1/comment-targets/mod/" + f.modCode + "/comments", body}})[0]
	if row.status != 409 {
		t.Errorf("cross-target idempotent retry status=%d body=%s", row.status, row.raw)
	}
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "update mods set review_status='pending' where id=$1", f.otherModID); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.otherEditor, http.MethodPatch, "/api/v1/comments/"+parent.ID, updateCommentRequest{Body: "new private parent secret", BaseUpdatedAt: parent.UpdatedAt}, 200)
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.otherModCode+"/comments", body, 404)
	f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 409)
	f.unchanged(t, before)
}

type test038Operation struct {
	method, route string
	body          any
}

func (f test038Fixture) parallel(t *testing.T, token string, operations []test038Operation) []test037HTTPResult {
	t.Helper()
	start := make(chan struct{})
	results := make(chan test037HTTPResult, len(operations))
	for _, operation := range operations {
		raw, err := json.Marshal(operation.body)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			<-start
			status, response, requestErr := test048HTTPRequest(f.ctx, f.origin.Client(), f.origin.URL, token, operation.method, operation.route, string(raw))
			results <- test037HTTPResult{status, response, requestErr}
		}()
	}
	close(start)
	rows := make([]test037HTTPResult, 0, len(operations))
	for range operations {
		row := <-results
		if row.err != nil {
			t.Fatal(row.err)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestTEST038ConcurrentFullHTTPWatchQuotaReactivationAndPrivateCYIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	// Active watches may survive deletion. Seed that real, still-visible
	// historical state; keep all 32 new competing targets published.
	if _, err := f.db.Exec(f.ctx, `insert into comments(public_id,target_type,target_id,author_id,body,idempotency_key,status,deleted_at)
		select 'c'||lpad(n::text,8,'0'),'mod',$1,$2,
		case when n<=1999 then '' else 'quota fixture root '||n end,'test038-seed-'||n,
		case when n<=1999 then 'deleted' else 'published' end,case when n<=1999 then now() end
		from generate_series(1,2031) n`, f.modID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into comment_watches(user_id,comment_id)
		select $1,id from comments where idempotency_key like 'test038-seed-%' order by id limit 1999`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	rows, err := f.db.Query(f.ctx, `select public_id from comments order by id offset 1999 limit 32`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 32 {
		t.Fatal("owned quota boundary fixtures incomplete")
	}
	var operations []test038Operation
	for _, id := range ids {
		operations = append(operations, test038Operation{http.MethodPut, "/api/v1/comments/" + id + "/watch", nil})
	}
	accepted, limited := 0, 0
	var winner commentWatchState
	for _, row := range f.parallel(t, f.editor, operations) {
		switch row.status {
		case 200:
			accepted++
			winner = decodeTEST022Data[commentWatchState](t, row.raw)
		case 400:
			limited++
		default:
			t.Fatalf("watch quota status=%d body=%s", row.status, row.raw)
		}
	}
	var active int
	if err = f.db.QueryRow(f.ctx, "select count(*) from comment_watches where user_id=$1 and status='active'", f.userIDs[f.editor]).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if accepted != 1 || limited != 31 || active != 2000 || !winner.Active {
		t.Fatalf("full HTTP quota accepted/limited/active=%d/%d/%d", accepted, limited, active)
	}
	var winningID string
	if err = f.db.QueryRow(f.ctx, "select comment.public_id from comment_watches watch join comments comment on comment.id=watch.comment_id where watch.public_id=$1", winner.ID).Scan(&winningID); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPut, "/api/v1/comments/"+winningID+"/watch", nil, 200)
	cy := f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", createCommentRequest{Body: "CY", ParentID: winningID, IdempotencyKey: "test038-private-cy"}, 200)
	if !bytes.Contains(cy, []byte(`"watchOnly":true`)) {
		t.Fatal("CY became a public reply rather than private watch")
	}
	f.unchanged(t, before)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/comment-watches/"+winner.ID+"/read", nil, 404)
	f.unchanged(t, before)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/comments/"+winningID+"/watch", nil, 200)
	f.require(t, f.editor, http.MethodPut, "/api/v1/comments/"+winningID+"/watch", nil, 200)
	if err = f.db.QueryRow(f.ctx, "select count(*) from comment_watches where user_id=$1 and status='active'", f.userIDs[f.editor]).Scan(&active); err != nil || active != 2000 {
		t.Fatalf("cancel/reactivate quota active=%d error=%v", active, err)
	}
}

func TestTEST038ConcurrentFullHTTPEditCASAndDeletedBodyRemainVersionedIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	root := f.comment(t, f.editor, f.modCode, "edit-root", "original root", "")
	var operations []test038Operation
	for index := range 16 {
		operations = append(operations, test038Operation{http.MethodPatch, "/api/v1/comments/" + root.ID, updateCommentRequest{Body: fmt.Sprintf("winner candidate %d", index), BaseUpdatedAt: root.UpdatedAt}})
	}
	accepted, conflicted := 0, 0
	var winner commentResponse
	for _, row := range f.parallel(t, f.editor, operations) {
		switch row.status {
		case 200:
			accepted++
			winner = decodeTEST022Data[commentResponse](t, row.raw)
		case 409:
			conflicted++
			var conflict struct {
				Code    string              `json:"code"`
				Details commentEditConflict `json:"details"`
			}
			if err := json.Unmarshal(row.raw, &conflict); err != nil {
				t.Fatal(err)
			}
			if conflict.Code != "COMMENT_EDIT_CONFLICT" || conflict.Details.Body == "" || !conflict.Details.UpdatedAt.After(root.UpdatedAt) {
				t.Fatalf("version conflict lost current body/version: %s", row.raw)
			}
		default:
			t.Fatalf("concurrent PATCH status=%d body=%s", row.status, row.raw)
		}
	}
	if accepted != 1 || conflicted != 15 || !winner.UpdatedAt.After(root.UpdatedAt) {
		t.Fatalf("CAS winner/conflict=%d/%d", accepted, conflicted)
	}
	boundary := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPatch, "/api/v1/comments/"+root.ID, updateCommentRequest{Body: strings.Repeat("界", 10000), BaseUpdatedAt: winner.UpdatedAt}, 200))
	if boundary.Body != strings.Repeat("界", 10000) || !boundary.UpdatedAt.After(winner.UpdatedAt) {
		t.Fatal("legal 10000-rune edit boundary lost data or version")
	}
	winner = boundary
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/comments/"+root.ID, updateCommentRequest{Body: "missing baseline"}, 400)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/comments/"+root.ID, updateCommentRequest{Body: strings.Repeat("界", 10001), BaseUpdatedAt: winner.UpdatedAt}, 400)
	f.require(t, f.otherEditor, http.MethodPatch, "/api/v1/comments/"+root.ID, updateCommentRequest{Body: "cross owner", BaseUpdatedAt: winner.UpdatedAt}, 403)
	f.unchanged(t, before)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/comments/"+root.ID, nil, 200)
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/comments/"+root.ID, updateCommentRequest{Body: "resurrect deleted", BaseUpdatedAt: winner.UpdatedAt}, 409)
	f.unchanged(t, before)
	var body, status string
	if err := f.db.QueryRow(f.ctx, "select body,status from comments where public_id=$1", root.ID).Scan(&body, &status); err != nil || body != "" || status != "deleted" {
		t.Fatalf("deleted comment body/status=%q/%q error=%v", body, status, err)
	}
}

func TestTEST038DeepThreadFullHTTPBudgetAndScopedForwardCursorIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	root := f.comment(t, f.editor, f.modCode, "deep-root", "root", "")
	target := commentTargetInfo{Type: "mod", Key: f.modCode, InternalID: f.modID}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	focus := root.ID
	for depth := range 25 {
		inserted, insertErr := insertCommentTree(f.ctx, tx, target, f.userIDs[f.editor], createCommentRequest{Body: fmt.Sprintf("ancestor %d", depth), ParentID: focus, IdempotencyKey: fmt.Sprintf("test038-depth-%d", depth), Status: "published"})
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		focus = inserted.PublicID
	}
	expected := make(map[string]bool, 199)
	for index := range 200 {
		author := f.userIDs[f.editor]
		if index == 73 {
			author = f.userIDs[f.otherEditor]
		}
		inserted, insertErr := insertCommentTree(f.ctx, tx, target, author, createCommentRequest{Body: fmt.Sprintf("reply %03d ", index) + strings.Repeat("界", 9000), ParentID: focus, IdempotencyKey: fmt.Sprintf("test038-large-reply-%d", index), Status: "published"})
		if insertErr != nil {
			t.Fatal(insertErr)
		}
		if index != 73 {
			expected[inserted.PublicID] = true
		}
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "insert into user_blocks(blocker_id,blocked_id) values($1,$2)", f.userIDs[f.editor], f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	seen := make(map[string]bool, 199)
	route := "/api/v1/comments/" + focus + "/thread"
	firstCursor := ""
	pages := 0
	for {
		raw := f.require(t, f.editor, http.MethodGet, route, nil, 200)
		page := decodeTEST022Data[commentThreadPageResponse](t, raw)
		if len(raw) > 512<<10 || len(page.Items) > 64 || len(page.Items) == 0 || page.FocusID != focus {
			t.Fatalf("real encoded thread budget invalid bytes=%d nodes=%d", len(raw), len(page.Items))
		}
		if pages == 0 {
			if !page.PathTruncated {
				t.Fatal("deep ancestor path was not bounded")
			}
			found := false
			for _, item := range page.Items {
				if item.ID == focus {
					found = true
				}
			}
			if !found {
				t.Fatal("first neighborhood lost focus")
			}
			firstCursor = page.NextCursor
		}
		advanced := 0
		for _, item := range page.Items {
			if expected[item.ID] {
				if seen[item.ID] {
					t.Fatalf("duplicate reply across actual cursor pages: %s", item.ID)
				}
				seen[item.ID] = true
				advanced++
			} else if item.Depth > 25 {
				t.Fatal("blocked reply escaped thread visibility")
			}
		}
		if advanced == 0 {
			t.Fatal("thread cursor did not advance with an actual reply")
		}
		pages++
		if pages > 200 {
			t.Fatal("thread cursor failed to terminate")
		}
		if page.NextCursor == "" {
			break
		}
		route = "/api/v1/comments/" + focus + "/thread?cursor=" + url.QueryEscape(page.NextCursor)
	}
	if len(seen) != 199 || firstCursor == "" || pages < 2 {
		t.Fatalf("bounded complete thread traversal seen=%d pages=%d", len(seen), pages)
	}
	f.require(t, f.editor, http.MethodGet, "/api/v1/comments/"+root.ID+"/thread?cursor="+url.QueryEscape(firstCursor), nil, 400)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/comments/"+focus+"/thread?cursor="+url.QueryEscape(firstCursor), nil, 400)
	f.require(t, f.editor, http.MethodGet, "/api/v1/comments/"+focus+"/thread?cursor=not-a-cursor", nil, 400)
	f.unchanged(t, before)
}

func TestTEST038FullHTTPRootKeysetPagesAreCompleteStableAndScopeBoundIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	if _, err := f.db.Exec(f.ctx, `insert into comments(target_type,target_id,author_id,body,created_at,idempotency_key)
		select 'mod',$1,$2,'keyset root '||n,now()+n*interval '1 microsecond','test038-root-page-'||n from generate_series(1,501) n`, f.modID, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	type pageResponse struct {
		Items      []commentResponse `json:"items"`
		NextCursor string            `json:"nextCursor"`
		Total      int               `json:"total"`
	}
	base := "/api/v1/comment-targets/mod/" + f.modCode + "/comments?sort=oldest&limit=7"
	route := base
	firstCursor := ""
	seen := make(map[string]bool, 501)
	pages := 0
	for {
		page := decodeTEST022Data[pageResponse](t, f.require(t, f.editor, http.MethodGet, route, nil, 200))
		if len(page.Items) == 0 || len(page.Items) > 7 || page.Total != 501 {
			t.Fatalf("actual root page size/total=%d/%d", len(page.Items), page.Total)
		}
		for _, item := range page.Items {
			if seen[item.ID] {
				t.Fatal("duplicate root in keyset traversal")
			}
			seen[item.ID] = true
		}
		if pages == 0 {
			firstCursor = page.NextCursor
		}
		pages++
		if pages > 80 {
			t.Fatal("root keyset traversal failed to terminate")
		}
		if page.NextCursor == "" {
			break
		}
		route = base + "&cursor=" + url.QueryEscape(page.NextCursor)
	}
	if len(seen) != 501 || firstCursor == "" {
		t.Fatalf("full root pagination returned %d/501", len(seen))
	}
	f.require(t, f.otherEditor, http.MethodGet, base+"&cursor="+url.QueryEscape(firstCursor), nil, 400)
	f.require(t, f.editor, http.MethodGet, "/api/v1/comment-targets/mod/"+f.otherModCode+"/comments?sort=oldest&cursor="+url.QueryEscape(firstCursor), nil, 400)
	f.require(t, f.editor, http.MethodGet, "/api/v1/comment-targets/mod/"+f.modCode+"/comments?sort=hot&cursor="+url.QueryEscape(firstCursor), nil, 400)
	f.unchanged(t, before)
}

func TestTEST038ReplyAndWatchDeliveryIntentRollbackWithFullHTTPCommentIntegration(t *testing.T) {
	for _, kind := range []string{"reply_mention", "comment_watch_reply"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST038Fixture(t)
			root := f.comment(t, f.otherEditor, f.modCode, "notify-root", "root owned by another commenter", "")
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "comment.watch")
			f.require(t, f.denied, http.MethodPut, "/api/v1/comments/"+root.ID+"/watch", nil, 200)
			if _, err := f.db.Exec(f.ctx, "alter table nats_outbox add constraint test038_reject_notification check (coalesce(payload->>'kind','')<>$quoted$"+kind+"$quoted$)"); err != nil {
				t.Fatal(err)
			}
			before := f.facts(t)
			body := createCommentRequest{Body: "reply with durable delivery", ParentID: root.ID, IdempotencyKey: "test038-notify-" + kind}
			row := f.parallel(t, f.editor, []test038Operation{{http.MethodPost, "/api/v1/comment-targets/mod/" + f.modCode + "/comments", body}})[0]
			after := f.facts(t)
			if row.status != 500 || !reflect.DeepEqual(before, after) {
				var comments, intents int
				if err := f.db.QueryRow(f.ctx, "select (select count(*) from comments),(select count(*) from nats_outbox where payload->>'kind'=$1)", kind).Scan(&comments, &intents); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("failed %s delivery committed comment status=%d comments=%d rejected-intents=%d factsUnchanged=%v body=%s", kind, row.status, comments, intents, reflect.DeepEqual(before, after), row.raw)
			}
			if _, err := f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test038_reject_notification"); err != nil {
				t.Fatal(err)
			}
			created := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 201))
			if created.ParentID != root.ID {
				t.Fatal("recovered reply lost parent")
			}
			var direct, watch, replies, unread int
			if err := f.db.QueryRow(f.ctx, `select
			(select count(*) from nats_outbox where payload->>'kind'='reply_mention'),
			(select count(*) from nats_outbox where payload->>'kind'='comment_watch_reply'),
			(select count(*) from comment_watch_replies),
			(select coalesce(sum(unread_count),0) from comment_watches)`).Scan(&direct, &watch, &replies, &unread); err != nil {
				t.Fatal(err)
			}
			if direct != 1 || watch != 1 || replies != 1 || unread != 1 {
				t.Fatalf("reply delivery authority direct/watch/replies/unread=%d/%d/%d/%d", direct, watch, replies, unread)
			}
			before = f.facts(t)
			f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 200)
			f.unchanged(t, before)
		})
	}
}

func TestTEST038ActualUploadedLogAttachmentRollbackRetryLeaseRecoveryAndDeleteFullHTTPIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	raw := []byte("access_token=test038-secret\nplayer joined\n")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "latest.log", "text/plain", "comment", raw)
	f.put(t, ticket, raw)
	completed := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if completed.ID == "" {
		t.Fatal("actual log upload did not register file")
	}
	if _, err := f.db.Exec(f.ctx, "alter table nats_outbox add constraint test038_reject_log_job check(event_type<>'comment.log_attachment.requested')"); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	body := createCommentRequest{Body: "actual log attachment", IdempotencyKey: "test038-log-root", AttachmentFileIDs: []string{completed.ID}}
	f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 500)
	f.unchanged(t, before)
	if _, err := f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test038_reject_log_job"); err != nil {
		t.Fatal(err)
	}
	root := decodeTEST022Data[commentResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/comment-targets/mod/"+f.modCode+"/comments", body, 201))
	if len(root.Attachments) != 1 || root.Attachments[0].Kind != "log" || root.Attachments[0].Status != "processing" {
		t.Fatalf("log processing DTO incomplete: %+v", root.Attachments)
	}
	var jobID int64
	if err := f.db.QueryRow(f.ctx, "select id from comment_log_attachment_jobs where comment_id=(select id from comments where public_id=$1)", root.ID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	worker := NewCommentLogAttachmentWorker(f.server.cfg, f.db, nil)
	if _, err := f.db.Exec(f.ctx, "alter table log_shares add constraint test038_reject_log_share check(source_file_id is null)"); err != nil {
		t.Fatal(err)
	}
	if err := worker.processJob(f.ctx, jobID); err == nil {
		t.Fatal("real log share persistence fault was swallowed")
	}
	var status string
	var attempts, shares int
	if err := f.db.QueryRow(f.ctx, "select status,attempts,(select count(*) from log_shares) from comment_log_attachment_jobs where id=$1", jobID).Scan(&status, &attempts, &shares); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || attempts != 1 || shares != 0 {
		t.Fatalf("durable retry state=%s attempts=%d shares=%d", status, attempts, shares)
	}
	if _, err := f.db.Exec(f.ctx, "alter table log_shares drop constraint test038_reject_log_share; update comment_log_attachment_jobs set next_attempt_at=now()"); err != nil {
		t.Fatal(err)
	}
	stale, claimed, err := worker.claimJob(f.ctx, jobID)
	if err != nil || !claimed || stale.attempts != 2 {
		t.Fatalf("second real claim=%v error=%v", claimed, err)
	}
	if _, err = f.db.Exec(f.ctx, "update comment_log_attachment_jobs set lease_expires_at=now()-interval '1 second' where id=$1", jobID); err != nil {
		t.Fatal(err)
	}
	restarted := NewCommentLogAttachmentWorker(f.server.cfg, f.db, nil)
	if recovered, recoveryErr := restarted.recoverDueJobs(f.ctx); recoveryErr != nil || recovered != 1 {
		t.Fatalf("actual worker restart recovered=%d error=%v", recovered, recoveryErr)
	}
	assertBUG082Completed(t, f.ctx, f.db, jobID, 3)
	var code, text string
	if err = f.db.QueryRow(f.ctx, "select share.public_code,entry.sanitized_text from log_shares share join log_share_entries entry on entry.log_share_id=share.id").Scan(&code, &text); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "test038-secret") || !strings.Contains(text, "player joined") {
		t.Fatalf("actual redaction result=%q", text)
	}
	before = f.facts(t)
	if err = worker.completeJob(f.ctx, stale, code); err == nil {
		t.Fatal("stale log worker completed current incarnation")
	}
	if err = restarted.processJob(f.ctx, jobID); err != nil {
		t.Fatal(err)
	}
	f.unchanged(t, before)
	page := decodeTEST022Data[commentThreadPageResponse](t, f.require(t, "", http.MethodGet, "/api/v1/comments/"+root.ID+"/thread", nil, 200))
	if len(page.Items) != 1 || len(page.Items[0].Attachments) != 1 || page.Items[0].Attachments[0].URL != "/log/s/"+code || page.Items[0].Attachments[0].Status != "ready" {
		t.Fatal("actual ready attachment lost redacted log share")
	}
	f.unchanged(t, before)
	f.require(t, f.editor, http.MethodDelete, "/api/v1/comments/"+root.ID, nil, 200)
	before = f.facts(t)
	f.require(t, "", http.MethodGet, "/api/v1/comments/"+root.ID+"/attachments/"+completed.ID+"/download", nil, 404)
	page = decodeTEST022Data[commentThreadPageResponse](t, f.require(t, "", http.MethodGet, "/api/v1/comments/"+root.ID+"/thread", nil, 200))
	if len(page.Items) != 1 || !page.Items[0].Deleted || page.Items[0].Body != "" || len(page.Items[0].Attachments) != 0 {
		t.Fatal("deleted comment exposed body or attached log")
	}
	f.unchanged(t, before)
}

func TestTEST038ActualPGCommentDecorationFailureIsNotAFalse404FullHTTPIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	raw := []byte("ordinary comment attachment\n")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "notes.txt", "text/plain", "comment", raw)
	f.put(t, ticket, raw)
	completed := decodeTEST022Data[struct {
		ID string `json:"id"`
	}](t, f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	if _, err := f.db.Exec(f.ctx, "update oss_files set scan_status='clean' where public_id=$1", completed.ID); err != nil {
		t.Fatal(err)
	}
	root := f.comment(t, f.editor, f.modCode, "attachment-read-root", "ordinary attached root", "", completed.ID)
	f.comment(t, f.editor, f.modCode, "attachment-read-child", "ordinary attached reply", root.ID, completed.ID)
	f.require(t, f.editor, http.MethodPut, "/api/v1/comments/"+root.ID+"/watch", nil, 200)
	before := f.facts(t)
	if _, err := f.db.Exec(f.ctx, `alter table comment_attachments rename to test038_actual_attachments;
		create function test038_reject_attachment_read() returns boolean language plpgsql stable as $$begin raise exception 'TEST038 owned attachment row stream failure';end$$;
		create view comment_attachments as select * from test038_actual_attachments where test038_reject_attachment_read()`); err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{"/api/v1/comment-targets/mod/" + f.modCode + "/comments", "/api/v1/comments/" + root.ID + "/thread", "/api/v1/comments/" + root.ID + "/replies", "/api/v1/users/me/comment-watches", "/api/v1/comments/" + root.ID + "/attachments/" + completed.ID + "/download"} {
		f.require(t, f.editor, http.MethodGet, route, nil, 500)
	}
	if _, err := f.db.Exec(f.ctx, "drop view comment_attachments; drop function test038_reject_attachment_read(); alter table test038_actual_attachments rename to comment_attachments"); err != nil {
		t.Fatal(err)
	}
	f.unchanged(t, before)
	client := &http.Client{Transport: f.origin.Client().Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+"/api/v1/comments/"+root.ID+"/attachments/"+completed.ID+"/download", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 307 || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("actual attachment access status=%d", response.StatusCode)
	}
	downloaded := f.get(t, response.Header.Get("Location"), 200)
	if !bytes.Equal(downloaded, raw) {
		t.Fatal("actual signed attachment access lost bytes")
	}
	f.unchanged(t, before)
}

func TestTEST038BulkCommentFixtureRetainsAllSchemaTriggersIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	var jit string
	if err := f.db.QueryRow(f.ctx, "show jit").Scan(&jit); err != nil {
		t.Fatal(err)
	}
	t.Logf("owned database default jit=%s", jit)
	rows, err := f.db.Query(f.ctx, `explain (analyze,buffers) insert into comments(public_id,target_type,target_id,author_id,body,idempotency_key)
		select 'd'||lpad(n::text,8,'0'),'mod',$1,$2,'diagnostic root '||n,'test038-diagnostic-'||n from generate_series(1,20) n`, f.modID, f.userIDs[f.editor])
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var line string
		if err = rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		t.Log(line)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	var comments, routes, counts int
	if err = f.db.QueryRow(f.ctx, `select (select count(*) from comments),(select count(*) from public_routes where entity_type='comment'),
		(select visible_count from comment_target_counts where target_type='mod' and target_id=$1)`, f.modID).Scan(&comments, &routes, &counts); err != nil {
		t.Fatal(err)
	}
	if comments != 20 || routes != 20 || counts != 20 {
		t.Fatalf("fixture preserved comments/routes/counts=%d/%d/%d", comments, routes, counts)
	}
}

func TestTEST038TransactionalCommentDeliveryPreservesMutedBlockedAndRecipientDedupFullHTTPIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "comment.watch")
	root := f.comment(t, f.otherEditor, f.modCode, "delivery-policy-root", "policy root", "")
	child := f.comment(t, f.otherEditor, f.modCode, "delivery-policy-child", "policy child", root.ID)
	var watches []commentWatchState
	for _, id := range []string{root.ID, child.ID} {
		watches = append(watches, decodeTEST022Data[commentWatchState](t, f.require(t, f.denied, http.MethodPut, "/api/v1/comments/"+id+"/watch", nil, 200)))
	}
	assertIntents := func(commentID string, wantDirect, wantWatch int) {
		t.Helper()
		var direct, watch int
		if err := f.db.QueryRow(f.ctx, `select count(*) filter(where payload->>'kind'='reply_mention'),count(*) filter(where payload->>'kind'='comment_watch_reply')
			from nats_outbox where payload->'data'->>'commentId'=$1`, commentID).Scan(&direct, &watch); err != nil {
			t.Fatal(err)
		}
		if direct != wantDirect || watch != wantWatch {
			t.Fatalf("delivery policy direct/watch=%d/%d want=%d/%d", direct, watch, wantDirect, wantWatch)
		}
	}
	reply := f.comment(t, f.editor, f.modCode, "delivery-policy-normal", "one intent per recipient", child.ID)
	assertIntents(reply.ID, 1, 1)
	var unread int
	if err := f.db.QueryRow(f.ctx, "select sum(unread_count) from comment_watches where user_id=$1", f.userIDs[f.denied]).Scan(&unread); err != nil || unread != 2 {
		t.Fatalf("two watched ancestors unread=%d error=%v", unread, err)
	}
	for _, watch := range watches {
		f.require(t, f.denied, http.MethodPatch, "/api/v1/comment-watches/"+watch.ID, map[string]any{"mute": "forever"}, 200)
	}
	reply = f.comment(t, f.editor, f.modCode, "delivery-policy-muted", "muted reply remains tracked", child.ID)
	assertIntents(reply.ID, 1, 0)
	if err := f.db.QueryRow(f.ctx, "select sum(unread_count) from comment_watches where user_id=$1", f.userIDs[f.denied]).Scan(&unread); err != nil || unread != 4 {
		t.Fatalf("muted watches unread=%d error=%v", unread, err)
	}
	if _, err := f.db.Exec(f.ctx, "insert into user_blocks(blocker_id,blocked_id) values($1,$2)", f.userIDs[f.denied], f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	for _, watch := range watches {
		f.require(t, f.denied, http.MethodPatch, "/api/v1/comment-watches/"+watch.ID, map[string]any{"mute": "none"}, 200)
	}
	reply = f.comment(t, f.editor, f.modCode, "delivery-policy-blocked", "blocked actor does not ping watcher", child.ID)
	assertIntents(reply.ID, 1, 0)
	if err := f.db.QueryRow(f.ctx, "select sum(unread_count) from comment_watches where user_id=$1", f.userIDs[f.denied]).Scan(&unread); err != nil || unread != 4 {
		t.Fatalf("blocked watches changed unread=%d error=%v", unread, err)
	}
}
