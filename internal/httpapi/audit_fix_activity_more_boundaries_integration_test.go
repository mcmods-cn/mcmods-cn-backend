package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func auditFixInjectActivityFailure(t *testing.T, f test013Fixture, actor int64, on bool) {
	t.Helper()
	statement := `alter table activity_event_outbox drop constraint audit_reject_activity_boundary`
	if on {
		statement = fmt.Sprintf(`alter table activity_event_outbox add constraint audit_reject_activity_boundary check(user_id<>%d) not valid`, actor)
	}
	if _, err := f.db.Exec(f.ctx, statement); err != nil {
		t.Fatal(err)
	}
}
func auditFixRequireSpecificActivityError(t *testing.T, raw []byte, message string) {
	t.Helper()
	if !bytes.Contains(raw, []byte(message)) {
		t.Fatalf("unrelated error satisfied activity failure assertion: %s", raw)
	}
}

func TestAuditFixCMP003ServerCreateUpdateActivityIsAtomic(t *testing.T) {
	f := newTEST015ServerFixture(t)
	proof := f.proof(t, f.editor, "active", "clean", 100)
	request := test015Submission("atomic-activity.invalid:25565", proof)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before := f.serverFacts(t)
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/servers", request, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist server activity")
	f.serverUnchanged(t, before)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	internalID, publicID := f.create(t, f.editor, request)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+publicID)
	update := updateMinecraftServerRequest{Name: "Atomic updated server", ShortDescription: "summary", BodyMarkdown: "An updated server body with additional words", MinecraftVersions: []string{"1.21.1"}, Languages: []string{"en-US"}, PrimaryTag: "survival", OnlineMode: true, Links: []createServerLinkRequest{{Kind: "website", URL: "https://updated.example.invalid"}}, Mods: []createServerModRequest{{ID: "atomic_updated_mod", Version: "v2"}}}
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
	before = f.serverFacts(t)
	raw = f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicID, update, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist server activity")
	f.serverUnchanged(t, before)
	auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
	f.require(t, f.editor, http.MethodPatch, "/api/v1/servers/"+publicID, update, 200)
	var creates, edits, added, removed int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$2),count(*) filter(where action_id=$3),
 coalesce(sum(markdown_added_bytes) filter(where action_id=$3),0),coalesce(sum(markdown_deleted_bytes) filter(where action_id=$3),0)
 from activity_event_outbox where object_entity_type='minecraft_server' and object_internal_id=$1`, internalID, activity.ActionCreate, activity.ActionEdit).Scan(&creates, &edits, &added, &removed); err != nil {
		t.Fatal(err)
	}
	wantAdded, wantRemoved := activity.MarkdownDeltaBytes(request.BodyMarkdown, update.BodyMarkdown)
	if creates != 1 || edits != 1 || added != wantAdded || removed != wantRemoved {
		t.Fatalf("server create/edit/markdown=%d/%d/%d/%d want1/1/%d/%d", creates, edits, added, removed, wantAdded, wantRemoved)
	}
}

func TestAuditFixCMP003AdminBalanceAdjustmentActivityAndLedgerAreAtomic(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "economy.balance.write")
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.denied]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/admin/users/" + target + "/balances"
	request := adminBalanceAdjustmentRequest{CurrencyCode: "diamond", Mode: "adjust", Amount: 30, Reason: "owned atomic activity fixture"}
	facts := func() (balance, ledgers int) {
		t.Helper()
		if err := f.db.QueryRow(f.ctx, `select coalesce((select balance.balance from user_currency_balances balance join currencies currency on currency.id=balance.currency_id where balance.user_id=$1 and currency.code='diamond'),0),(select count(*) from currency_transactions where user_id=$1)`, f.userIDs[f.denied]).Scan(&balance, &ledgers); err != nil {
			t.Fatal(err)
		}
		return
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
	raw := f.require(t, f.reviewer, http.MethodPost, path, request, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist balance activity")
	if b, l := facts(); b != 0 || l != 0 {
		t.Fatalf("failed activity changed balance/ledger=%d/%d", b, l)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
	f.require(t, f.reviewer, http.MethodPost, path, request, 200)
	if b, l := facts(); b != 30 || l != 1 {
		t.Fatalf("recovered balance/ledger=%d/%d", b, l)
	}
	request.Mode = "set" // Preserve the legacy no-op activity semantics, with zero extra money movement.
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
	raw = f.require(t, f.reviewer, http.MethodPost, path, request, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist balance activity")
	auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
	f.require(t, f.reviewer, http.MethodPost, path, request, 200)
	if b, l := facts(); b != 30 || l != 1 {
		t.Fatalf("set-to-current changed financial facts=%d/%d", b, l)
	}
	var intents int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1`, f.userIDs[f.reviewer]).Scan(&intents); err != nil || intents != 2 {
		t.Fatalf("balance activity intents=%d want2 err=%v", intents, err)
	}
}

func TestAuditFixCMP003RatingCreateUpdateDeleteActivityIsAtomic(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "rating.create")
	request := ratingUpsertRequest{OverallScore: 5, Scores: map[string]int{}, Message: "Owned atomic rating"}
	for _, dimension := range ratingDimensions["mod"] {
		request.Scores[dimension.Code] = 5
	}
	path := "/api/v1/ratings/mod/" + f.modCode
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	raw := f.require(t, f.editor, http.MethodPut, path, request, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist rating activity")
	var rows, scores int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from content_ratings),(select count(*) from content_rating_scores)`).Scan(&rows, &scores); err != nil || rows != 0 || scores != 0 {
		t.Fatalf("failed create rating/scores=%d/%d err=%v", rows, scores, err)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	f.require(t, f.editor, http.MethodPut, path, request, 200)
	request.OverallScore = 4
	for code := range request.Scores {
		request.Scores[code] = 4
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	raw = f.require(t, f.editor, http.MethodPut, path, request, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist rating activity")
	var overall int
	if err := f.db.QueryRow(f.ctx, `select overall_score from content_ratings where author_id=$1`, f.userIDs[f.editor]).Scan(&overall); err != nil || overall != 5 {
		t.Fatalf("failed update overall=%d err=%v", overall, err)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	f.require(t, f.editor, http.MethodPut, path, request, 200)
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
	raw = f.require(t, f.editor, http.MethodDelete, path, nil, 500)
	auditFixRequireSpecificActivityError(t, raw, "failed to persist rating activity")
	if err := f.db.QueryRow(f.ctx, `select overall_score from content_ratings where author_id=$1`, f.userIDs[f.editor]).Scan(&overall); err != nil || overall != 4 {
		t.Fatalf("failed delete lost rating=%d err=%v", overall, err)
	}
	auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
	f.require(t, f.editor, http.MethodDelete, path, nil, 204)
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from content_ratings),(select count(*) from content_rating_scores)`).Scan(&rows, &scores); err != nil || rows != 0 || scores != 0 {
		t.Fatalf("rating delete rows/scores=%d/%d err=%v", rows, scores, err)
	}
	var creates, edits, deletes int
	if err := f.db.QueryRow(f.ctx, `select count(*) filter(where action_id=$3),count(*) filter(where action_id=$4),count(*) filter(where action_id=$5)
 from activity_event_outbox where object_public_id=$1 and object_type_id=$2`, f.modCode, activity.ObjectRating, activity.ActionCreate, activity.ActionEdit, activity.ActionDelete).Scan(&creates, &edits, &deletes); err != nil || creates != 1 || edits != 1 || deletes != 1 {
		t.Fatalf("rating create/edit/delete intents=%d/%d/%d err=%v", creates, edits, deletes, err)
	}
}
