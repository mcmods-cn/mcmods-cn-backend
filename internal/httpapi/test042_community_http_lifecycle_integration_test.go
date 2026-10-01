package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type test042Fixture struct{ test038Fixture }
type test042Mutation struct {
	ID              string `json:"id"`
	ReviewStatus    string `json:"reviewStatus"`
	RevisionID      string `json:"revisionId"`
	ChangeRequestID string `json:"changeRequestId"`
}

func newTEST042Fixture(t *testing.T) test042Fixture {
	t.Helper()
	f := test042Fixture{newTEST038Fixture(t)}
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "community.tutorial.create", "community.issue.create", "community.news.create", "community.discussion.create")
	}
	f.reviewPolicy(t, true)
	return f
}

func (f test042Fixture) reviewPolicy(t *testing.T, required bool) {
	t.Helper()
	cfg := defaultReviewConfig()
	cfg.TutorialCreate = required
	cfg.IssueCreate = required
	cfg.NewsCreate = required
	cfg.DiscussionCreate = required
	cfg.TutorialEdit = false
	cfg.IssueEdit = false
	cfg.NewsEdit = false
	cfg.DiscussionEdit = false
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update system_settings set value=$2::jsonb where key=$1", reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
}

func (f test042Fixture) facts(t *testing.T) map[string]string {
	t.Helper()
	result := f.test038Fixture.facts(t)
	for _, table := range []string{"community_posts", "community_post_catalog", "community_post_bounties", "community_post_project_refs", "community_post_resource_refs", "community_post_translations", "currencies", "user_currency_balances", "currency_transactions", "ai_tasks", "ai_task_logs"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, "select coalesce(jsonb_agg(to_jsonb(fact_row) order by to_jsonb(fact_row)::text),'[]'::jsonb)::text from "+pgx.Identifier{table}.Sanitize()+" fact_row").Scan(&raw); err != nil {
			t.Fatal(err)
		}
		result[table] = raw
	}
	return result
}

func (f test042Fixture) unchanged(t *testing.T, before map[string]string) {
	t.Helper()
	after := f.facts(t)
	if !reflect.DeepEqual(before, after) {
		for table, raw := range before {
			if after[table] != raw {
				t.Errorf("denied/failed community operation mutated %s", table)
			}
		}
		t.FailNow()
	}
}

func (f test042Fixture) snapshot(kind, title string) communityPostSnapshot {
	value := communityPostSnapshot{Kind: kind, Title: title, SourceLocale: "en-US", BodyMarkdown: "Actual TEST042 source body.", MinecraftVersions: []string{}, Projects: []communityPostReference{}, Resources: []communityPostReference{}}
	if kind == "issue" {
		value.Severity = "minor"
		value.MinecraftVersions = []string{"1.21.1"}
		value.ModVersionMin = "1.0.0"
		value.Projects = []communityPostReference{{PublicID: f.modCode, Type: "mod"}}
	}
	return value
}

func (f test042Fixture) create(t *testing.T, token string, snapshot communityPostSnapshot) test042Mutation {
	t.Helper()
	item := decodeTEST022Data[test042Mutation](t, f.require(t, token, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}, 201))
	if item.ID == "" || item.RevisionID == "" || item.ChangeRequestID == "" {
		t.Fatal("actual community creation omitted durable revision identity")
	}
	return item
}

func (f test042Fixture) review(t *testing.T, item test042Mutation, status string) {
	t.Helper()
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+item.RevisionID, map[string]any{"status": status, "note": "test042 independent full HTTP review"}, 200)
}

func (f test042Fixture) detail(t *testing.T, token, id string) communityPostResponse {
	t.Helper()
	return decodeTEST022Data[communityPostResponse](t, f.require(t, token, http.MethodGet, "/api/v1/community/posts/"+id, nil, 200))
}

func (f test042Fixture) comment(t *testing.T, token, post, key string) commentResponse {
	t.Helper()
	return decodeTEST022Data[commentResponse](t, f.require(t, token, http.MethodPost, "/api/v1/comment-targets/community_post/"+post+"/comments", createCommentRequest{Body: "Actual TEST042 answer " + key, IdempotencyKey: "test042-" + key}, 201))
}

func (f test042Fixture) seedMoney(t *testing.T) int64 {
	t.Helper()
	var currencyID int64
	if err := f.db.QueryRow(f.ctx, "insert into currencies(code,name,transfer_tax_bps) values('test042','TEST042 controlled currency',1000) returning id").Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	tx, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	for _, token := range []string{f.editor, f.otherEditor} {
		if _, err = changeCurrencyBalanceByIDTx(f.ctx, tx, f.userIDs[token], currencyID, 1000, "test042_fixture_seed", nil, "test042", "owned", map[string]any{}); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	return currencyID
}

func (f test042Fixture) balance(t *testing.T, token string, currency int64, want int64) {
	t.Helper()
	var actual int64
	if err := f.db.QueryRow(f.ctx, "select coalesce((select balance from user_currency_balances where user_id=$1 and currency_id=$2),0)", f.userIDs[token], currency).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if actual != want {
		t.Fatalf("actual controlled currency balance=%d want=%d", actual, want)
	}
}

func TestTEST042FourKindsCreateReviewAndPermissionMatrixFullHTTPIntegration(t *testing.T) {
	for _, kind := range []string{"tutorial", "issue", "news", "discussion"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST042Fixture(t)
			snapshot := f.snapshot(kind, "Actual "+kind)
			before := f.facts(t)
			f.require(t, "", http.MethodPost, "/api/v1/community/posts", snapshot, 401)
			f.require(t, f.denied, http.MethodPost, "/api/v1/community/posts", snapshot, 403)
			f.unchanged(t, before)
			item := f.create(t, f.editor, snapshot)
			if item.ReviewStatus != "pending" {
				t.Fatal("normal community creation bypassed per-kind create review")
			}
			before = f.facts(t)
			f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+item.ID, nil, 404)
			f.require(t, f.otherEditor, http.MethodGet, "/api/v1/community/posts/"+item.ID, nil, 404)
			owner := f.detail(t, f.editor, item.ID)
			if !owner.CanEdit || owner.SourceLocale != "en-US" {
				t.Fatal("pending owner lost edit capability or source language")
			}
			f.require(t, f.denied, http.MethodPatch, "/api/v1/content-revisions/"+item.RevisionID, map[string]any{"status": "approved"}, 403)
			page := decodeTEST022Data[communityPostPageResponse](t, f.require(t, "", http.MethodGet, "/api/v1/community/posts?kind="+kind, nil, 200))
			if len(page.Items) != 0 {
				t.Fatal("pending post leaked into public projection")
			}
			f.unchanged(t, before)
			f.review(t, item, "approved")
			public := f.detail(t, "", item.ID)
			if public.Title != snapshot.Title || public.BodyMarkdown != snapshot.BodyMarkdown || public.PublishedRevisionID != item.RevisionID || public.CanEdit {
				t.Fatal("approved actual community publication contract changed")
			}
			before = f.facts(t)
			f.require(t, f.otherEditor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: snapshot, BaseRevisionID: &item.RevisionID}, 403)
			f.unchanged(t, before)
		})
	}
}

func TestTEST042BountyHoldRejectRefundResubmitAndConcurrentSelfSolveFullHTTPIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	currency := f.seedMoney(t)
	snapshot := f.snapshot("discussion", "Held question")
	snapshot.BountyCurrency = "test042"
	snapshot.BountyAmount = 100
	item := f.create(t, f.editor, snapshot)
	f.balance(t, f.editor, currency, 900)
	f.review(t, item, "rejected")
	f.balance(t, f.editor, currency, 1000)
	before := f.facts(t)
	f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+item.ID, nil, 404)
	f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts/"+item.ID+"/self-solved", nil, 404)
	f.unchanged(t, before)
	resubmitted := decodeTEST022Data[test042Mutation](t, f.require(t, f.editor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: snapshot}, 200))
	if resubmitted.ReviewStatus != "pending" {
		public := f.parallel(t, "", []test038Operation{{http.MethodGet, "/api/v1/community/posts/" + item.ID, nil}})[0]
		t.Fatalf("never-published rejected create review was bypassed on resubmission: status=%s anonymousHTTP=%d", resubmitted.ReviewStatus, public.status)
	}
	f.balance(t, f.editor, currency, 900)
	f.review(t, resubmitted, "approved")
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodPost, "/api/v1/community/posts/"+item.ID+"/self-solved", nil, 403)
	f.unchanged(t, before)
	operations := make([]test038Operation, 16)
	for index := range operations {
		operations[index] = test038Operation{http.MethodPost, "/api/v1/community/posts/" + item.ID + "/self-solved", nil}
	}
	statuses := map[int]int{}
	for _, row := range f.parallel(t, f.editor, operations) {
		statuses[row.status]++
	}
	if statuses[200] != 1 || statuses[409] != 15 {
		t.Fatalf("concurrent self-solve outcomes=%v", statuses)
	}
	f.balance(t, f.editor, currency, 1000)
	actual := f.detail(t, "", item.ID)
	if actual.ResolutionStatus != "self_solved" || actual.Bounty == nil || actual.Bounty.Status != "refunded" {
		t.Fatal("self-solve omitted persistent refund state")
	}
	var holds, refunds int
	if err := f.db.QueryRow(f.ctx, "select count(*) filter(where transaction_type='question_bounty_hold'),count(*) filter(where transaction_type='question_bounty_refund') from currency_transactions where reference_key=$1", item.ID).Scan(&holds, &refunds); err != nil {
		t.Fatal(err)
	}
	if holds != 2 || refunds != 2 {
		t.Fatalf("hold/refund ledger history=%d/%d", holds, refunds)
	}
}

func TestTEST042BountyAwardLedgerAndNotificationFailureAreAtomicFullHTTPIntegration(t *testing.T) {
	for _, failure := range []string{"ledger", "notification"} {
		t.Run(failure, func(t *testing.T) {
			f := newTEST042Fixture(t)
			f.reviewPolicy(t, false)
			currency := f.seedMoney(t)
			snapshot := f.snapshot("discussion", "Atomic award")
			snapshot.BountyCurrency = "test042"
			snapshot.BountyAmount = 100
			item := f.create(t, f.editor, snapshot)
			answer := f.comment(t, f.otherEditor, item.ID, "atomic-"+failure)
			route := "/api/v1/community/posts/" + item.ID + "/answers/" + answer.ID
			before := f.facts(t)
			f.require(t, f.otherEditor, http.MethodPost, route, nil, 403)
			f.unchanged(t, before)
			if failure == "ledger" {
				if _, err := f.db.Exec(f.ctx, "alter table currency_transactions add constraint test042_fail_award check(transaction_type <> 'question_bounty_award')"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(f.ctx, "alter table nats_outbox add constraint test042_fail_award_notice check(coalesce(payload->>'templateKey','') <> 'question_answer_accepted')"); err != nil {
					t.Fatal(err)
				}
			}
			before = f.facts(t)
			row := f.parallel(t, f.editor, []test038Operation{{http.MethodPost, route, nil}})[0]
			if row.status != 500 || !reflect.DeepEqual(before, f.facts(t)) {
				var resolution, bounty string
				if err := f.db.QueryRow(f.ctx, "select post.resolution_status,bounty.status from community_posts post join community_post_bounties bounty on bounty.post_id=post.id where post.public_id=$1", item.ID).Scan(&resolution, &bounty); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("failed award stage=%s HTTP=%d persisted=%s/%s factsUnchanged=%v body=%s", failure, row.status, resolution, bounty, reflect.DeepEqual(before, f.facts(t)), row.raw)
			}
			if failure == "ledger" {
				if _, err := f.db.Exec(f.ctx, "alter table currency_transactions drop constraint test042_fail_award"); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test042_fail_award_notice"); err != nil {
					t.Fatal(err)
				}
			}
			settled := decodeTEST022Data[struct {
				Tax int64 `json:"taxAmount"`
				Net int64 `json:"netAmount"`
			}](t, f.require(t, f.editor, http.MethodPost, route, nil, 200))
			if settled.Tax != 10 || settled.Net != 90 {
				t.Fatalf("actual bounty tax/net=%d/%d", settled.Tax, settled.Net)
			}
			f.balance(t, f.editor, currency, 900)
			f.balance(t, f.otherEditor, currency, 1090)
			var awards, intents int
			if err := f.db.QueryRow(f.ctx, "select (select count(*) from currency_transactions where reference_key=$1 and transaction_type='question_bounty_award'),(select count(*) from nats_outbox where payload->'data'->>'communityPostId'=$1 and payload->>'templateKey'='question_answer_accepted')", item.ID).Scan(&awards, &intents); err != nil {
				t.Fatal(err)
			}
			if awards != 1 || intents != 1 {
				t.Fatalf("recovered bounty ledger/intents=%d/%d", awards, intents)
			}
			before = f.facts(t)
			f.require(t, f.editor, http.MethodPost, route, nil, 409)
			f.unchanged(t, before)
		})
	}
}

func TestTEST042ConcurrentFullHTTPAnswerAwardAndSelfSolveCannotDoubleSpendIntegration(t *testing.T) {
	for _, race := range []string{"same-answer", "award-vs-refund"} {
		t.Run(race, func(t *testing.T) {
			f := newTEST042Fixture(t)
			f.reviewPolicy(t, false)
			currency := f.seedMoney(t)
			snapshot := f.snapshot("discussion", "Concurrent settlement")
			snapshot.BountyCurrency = "test042"
			snapshot.BountyAmount = 100
			item := f.create(t, f.editor, snapshot)
			answer := f.comment(t, f.otherEditor, item.ID, "settlement-"+race)
			route := "/api/v1/community/posts/" + item.ID + "/answers/" + answer.ID
			count := 16
			if race == "award-vs-refund" {
				count = 2
			}
			operations := make([]test038Operation, count)
			for index := range operations {
				operations[index] = test038Operation{http.MethodPost, route, nil}
			}
			if race == "award-vs-refund" {
				operations[1].route = "/api/v1/community/posts/" + item.ID + "/self-solved"
			}
			statuses := map[int]int{}
			for _, row := range f.parallel(t, f.editor, operations) {
				statuses[row.status]++
			}
			if statuses[200] != 1 || statuses[409] != count-1 {
				t.Fatalf("concurrent bounty outcomes=%v", statuses)
			}
			actual := f.detail(t, "", item.ID)
			var settlements int
			if err := f.db.QueryRow(f.ctx, "select count(*) from currency_transactions where reference_key=$1 and transaction_type in ('question_bounty_award','question_bounty_refund')", item.ID).Scan(&settlements); err != nil {
				t.Fatal(err)
			}
			if settlements != 1 || actual.Bounty == nil {
				t.Fatal("concurrent settlement produced duplicate/missing financial facts")
			}
			if actual.ResolutionStatus == "answered" {
				if actual.Bounty.Status != "awarded" || actual.AcceptedCommentID != answer.ID || actual.Bounty.TaxAmount != 10 || actual.Bounty.NetAmount != 90 {
					t.Fatal("concurrent award inconsistent")
				}
				f.balance(t, f.editor, currency, 900)
				f.balance(t, f.otherEditor, currency, 1090)
			} else {
				if actual.ResolutionStatus != "self_solved" || actual.Bounty.Status != "refunded" {
					t.Fatal("concurrent refund inconsistent")
				}
				f.balance(t, f.editor, currency, 1000)
				f.balance(t, f.otherEditor, currency, 1000)
			}
		})
	}
}

func TestTEST042FullHTTPEditCASLanguageAndPendingReviewConflictPreservePublicationIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	f.reviewPolicy(t, false)
	snapshot := f.snapshot("tutorial", "CAS source")
	item := f.create(t, f.editor, snapshot)
	operations := make([]test038Operation, 16)
	for index := range operations {
		copy := snapshot
		copy.Title = fmt.Sprintf("CAS contender %d", index)
		operations[index] = test038Operation{http.MethodPut, "/api/v1/community/posts/" + item.ID, communityPostMutationRequest{communityPostSnapshot: copy, BaseRevisionID: &item.RevisionID}}
	}
	statuses := map[int]int{}
	for _, row := range f.parallel(t, f.editor, operations) {
		statuses[row.status]++
		if row.status == 409 {
			var problem struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(row.raw, &problem); err != nil || problem.Code != "COMMUNITY_POST_EDIT_CONFLICT" {
				t.Fatalf("CAS conflict contract=%s err=%v", row.raw, err)
			}
		}
	}
	if statuses[200] != 1 || statuses[409] != 15 {
		t.Fatalf("CAS outcomes=%v", statuses)
	}
	current := f.detail(t, f.editor, item.ID)
	if current.PublishedRevisionID == item.RevisionID || current.SourceLocale != "en-US" {
		t.Fatal("successful CAS lost revision/source locale")
	}
	cfg := defaultReviewConfig()
	cfg.TutorialCreate = true
	cfg.TutorialEdit = true
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update system_settings set value=$2::jsonb where key=$1", reviewConfigSettingKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	edit := snapshot
	edit.Title = "新编辑标题"
	edit.SourceLocale = "zh-CN"
	pending := decodeTEST022Data[test042Mutation](t, f.require(t, f.editor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: edit, BaseRevisionID: &current.PublishedRevisionID}, 200))
	if pending.ReviewStatus != "pending" {
		t.Fatal("manual edit review bypassed")
	}
	public := f.detail(t, "", item.ID)
	if public.Title != current.Title || public.SourceLocale != "en-US" {
		t.Fatal("pending edit changed current public title/language")
	}
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: edit, BaseRevisionID: &current.PublishedRevisionID}, 409)
	f.unchanged(t, before)
	f.review(t, pending, "approved")
	public = f.detail(t, "", item.ID)
	if public.Title != edit.Title || public.SourceLocale != "zh-CN" || public.PublishedRevisionID != pending.RevisionID {
		t.Fatal("approved edit lost author-selected source language")
	}
}

func TestTEST042ReferencesRevalidateHiddenProjectsForEveryFullHTTPViewerIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	snapshot := f.snapshot("tutorial", "Visible references")
	snapshot.Projects = []communityPostReference{{PublicID: f.modCode, Type: "mod"}, {PublicID: f.otherModCode, Type: "mod"}}
	item := f.create(t, f.editor, snapshot)
	f.review(t, item, "approved")
	if len(f.detail(t, "", item.ID).Projects) != 2 {
		t.Fatal("approved actual project references omitted")
	}
	if _, err := f.db.Exec(f.ctx, "update mods set review_status='pending' where id=$1", f.otherModID); err != nil {
		t.Fatal(err)
	}
	before := f.facts(t)
	for _, token := range []string{"", f.editor, f.denied} {
		actual := f.detail(t, token, item.ID)
		if len(actual.Projects) != 2 || actual.Projects[0].PublicID != f.modCode || !actual.Projects[1].Unavailable || actual.Projects[1].PublicID != "" || actual.Projects[1].Name != "" || actual.Projects[1].SiteID != "" {
			t.Fatal("hidden project metadata leaked instead of contract unavailable placeholder")
		}
	}
	owner := f.detail(t, f.otherEditor, item.ID)
	if owner.Projects[1].PublicID != f.otherModCode || owner.Projects[1].Unavailable {
		t.Fatal("actual target owner lost private project reference")
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}, 400)
	f.unchanged(t, before)
	// Publication must revalidate the submitter, not use reviewer visibility as a bypass.
	if _, err := f.db.Exec(f.ctx, "update mods set review_status='approved' where id=$1", f.otherModID); err != nil {
		t.Fatal(err)
	}
	pending := f.create(t, f.editor, snapshot)
	if _, err := f.db.Exec(f.ctx, "update mods set review_status='pending' where id=$1", f.otherModID); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+pending.RevisionID, map[string]any{"status": "approved", "note": "private reference rejected"}, 409)
	f.unchanged(t, before)
}

func TestTEST042ReferenceAndBodyBudgetsHavePositiveAndNegativeFullHTTPBoundariesIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	f.reviewPolicy(t, false)
	maximum := f.snapshot("tutorial", "Maximum references")
	for index := range 32 {
		maximum.Projects = append(maximum.Projects, communityPostReference{Type: "mod", Identifier: fmt.Sprintf("test042_project_%d", index)})
	}
	for index := range 64 {
		maximum.Resources = append(maximum.Resources, communityPostReference{Kind: "minecraft.item", Identifier: fmt.Sprintf("test042:item_%d", index)})
	}
	item := f.create(t, f.editor, maximum)
	actual := f.detail(t, "", item.ID)
	if len(actual.Projects) != 32 || len(actual.Resources) != 64 {
		t.Fatal("legal 32/64 references lost during actual publication")
	}
	var unresolved int
	if err := f.db.QueryRow(f.ctx, `select count(*) from unresolved_references where source_type in ('community_post_project','community_post_resource')`).Scan(&unresolved); err != nil {
		t.Fatal(err)
	}
	if unresolved != 96 {
		t.Fatalf("legal full reference unresolved facts=%d", unresolved)
	}
	before := f.facts(t)
	tooManyProjects := maximum
	tooManyProjects.Projects = append(append([]communityPostReference{}, maximum.Projects...), communityPostReference{Type: "mod", Identifier: "extra_project"})
	tooManyResources := maximum
	tooManyResources.Resources = append(append([]communityPostReference{}, maximum.Resources...), communityPostReference{Kind: "minecraft.item", Identifier: "test042:extra"})
	duplicate := f.snapshot("tutorial", "Duplicate references")
	duplicate.Projects = []communityPostReference{{Type: "mod", Identifier: "Same"}, {Type: "mod", Identifier: "same"}}
	long := f.snapshot("tutorial", "Long reference")
	long.Projects = []communityPostReference{{Type: "mod", Identifier: strings.Repeat("a", 129)}}
	for _, snapshot := range []communityPostSnapshot{tooManyProjects, tooManyResources, duplicate, long} {
		f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}, 400)
	}
	oversized := f.snapshot("tutorial", "Over body")
	oversized.BodyMarkdown = strings.Repeat("a", (1<<20)+1)
	f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: oversized}, 400)
	f.unchanged(t, before)
	bounded := f.snapshot("tutorial", "Exact body budget")
	bounded.BodyMarkdown = strings.Repeat("a", 1<<20)
	body := f.create(t, f.editor, bounded)
	published := f.detail(t, "", body.ID)
	if len(published.BodyMarkdown) != 1<<20 {
		t.Fatal("legal one MiB source body was truncated")
	}
	before = f.facts(t)
	pageRaw := f.require(t, "", http.MethodGet, "/api/v1/community/posts?kind=tutorial&limit=100", nil, 200)
	if len(pageRaw) > 128<<10 || bytesTEST042ContainsBody(pageRaw) {
		t.Fatal("community catalog leaked full source body or exceeded bounded projection")
	}
	f.unchanged(t, before)
}

func bytesTEST042ContainsBody(raw []byte) bool {
	var response struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return true
	}
	for _, item := range response.Data.Items {
		if _, exists := item["bodyMarkdown"]; exists {
			return true
		}
	}
	return false
}

func TestTEST042TranslationQuotaPermissionConcurrentIdempotencyAndResultFailuresFullHTTPIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	f.reviewPolicy(t, false)
	for _, token := range []string{f.editor, f.otherEditor} {
		grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[token], "content.translate", "user.ai.daily_token_limit.100000")
	}
	cfg := defaultAIConfig()
	cfg.Providers[0].Enabled = true
	cfg.Providers[0].APIKey = "test042-synthetic-not-a-real-key"
	cfg.Providers[0].BaseURL = f.origin.URL + "/test042-no-external-ai"
	cfg.Models[0].Enabled = true
	for index := range cfg.TaskModels {
		if cfg.TaskModels[index].TaskType == aiTaskContentTranslation {
			cfg.TaskModels[index].ModelKey = "openai/gpt-4.1-mini"
		}
	}
	sealed, err := f.server.sealSystemSetting(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "insert into system_settings(key,value) values('ai.config',$1::jsonb) on conflict(key) do update set value=excluded.value", sealed); err != nil {
		t.Fatal(err)
	}
	item := f.create(t, f.editor, f.snapshot("tutorial", "Translation source"))
	route := "/api/v1/community/posts/" + item.ID + "/translations"
	before := f.facts(t)
	f.require(t, "", http.MethodPost, route, map[string]any{"targetLocale": "fr-FR"}, 401)
	f.require(t, f.denied, http.MethodPost, route, map[string]any{"targetLocale": "fr-FR"}, 403)
	f.require(t, f.editor, http.MethodPost, route, map[string]any{"targetLocale": "en-US"}, 400)
	f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+item.ID+"?locale=fr-FR", nil, 200)
	f.unchanged(t, before)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.denied], "content.translate", "user.ai.daily_token_limit.1")
	before = f.facts(t)
	f.require(t, f.denied, http.MethodPost, route, map[string]any{"targetLocale": "fr-FR"}, 429)
	f.unchanged(t, before)
	operations := make([]test038Operation, 16)
	for index := range operations {
		operations[index] = test038Operation{http.MethodPost, route, map[string]any{"targetLocale": "fr-FR"}}
	}
	var uid string
	for _, row := range f.parallel(t, f.editor, operations) {
		if row.status != 202 {
			t.Fatalf("concurrent actual translation status=%d body=%s", row.status, row.raw)
		}
		result := decodeTEST022Data[struct {
			ID     string `json:"taskId"`
			Status string `json:"status"`
		}](t, row.raw)
		if result.ID == "" || result.Status != "queued" || (uid != "" && result.ID != uid) {
			t.Fatal("concurrent translation did not reuse quota-backed queued identity")
		}
		uid = result.ID
	}
	var taskID, tasks, intents int64
	var payload []byte
	if err = f.db.QueryRow(f.ctx, `select id,payload,(select count(*) from ai_tasks),(select count(*) from nats_outbox where event_type='ai.community_post_translation.requested') from ai_tasks where task_uid=$1`, uid).Scan(&taskID, &payload, &tasks, &intents); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || intents != 1 {
		t.Fatalf("concurrent translation tasks/intents=%d/%d", tasks, intents)
	}
	before = f.facts(t)
	f.require(t, f.otherEditor, http.MethodGet, "/api/v1/community/translations/"+uid, nil, 404)
	f.require(t, f.editor, http.MethodGet, "/api/v1/community/translations/"+uid, nil, 200)
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox add constraint test042_fail_translation_intent check(event_type <> 'ai.community_post_translation.requested') not valid"); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	f.require(t, f.editor, http.MethodPost, route, map[string]any{"targetLocale": "de-DE"}, 503)
	f.unchanged(t, before)
	if _, err = f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test042_fail_translation_intent"); err != nil {
		t.Fatal(err)
	}
	// Actual production persistence and HTTP result contracts; no claim of an AI provider execution or consuming the outbox.
	worker := NewAIWorker(f.db, nil, "")
	before = f.facts(t)
	if err = worker.persistCommunityPostTranslation(f.ctx, taskID, payload, map[string]any{"items": []any{map[string]any{"key": "title", "text": 17}}}); err == nil {
		t.Fatal("corrupt translation result was silently persisted")
	}
	f.unchanged(t, before)
	if err = worker.persistCommunityPostTranslation(f.ctx, taskID, payload, map[string]any{"items": []any{map[string]any{"key": "title", "text": "Titre traduit"}, map[string]any{"key": "bodyMarkdown", "text": "Corps traduit"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, "update ai_tasks set status='completed' where id=$1", taskID); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	result := decodeTEST022Data[struct {
		Status      string            `json:"status"`
		Translation map[string]string `json:"translation"`
	}](t, f.require(t, f.editor, http.MethodGet, "/api/v1/community/translations/"+uid, nil, 200))
	if result.Status != "completed" || result.Translation["title"] != "Titre traduit" {
		t.Fatal("actual completed result lost persisted translation")
	}
	f.require(t, f.editor, http.MethodPost, route, map[string]any{"targetLocale": "fr-FR"}, 200)
	f.unchanged(t, before)
	current := f.detail(t, f.editor, item.ID)
	edit := f.snapshot("tutorial", "Changed translation source")
	f.require(t, f.editor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: edit, BaseRevisionID: &current.PublishedRevisionID}, 200)
	before = f.facts(t)
	if err = worker.persistCommunityPostTranslation(f.ctx, taskID, payload, map[string]any{"items": []any{map[string]any{"key": "title", "text": "Stale title"}, map[string]any{"key": "bodyMarkdown", "text": "Stale body"}}}); err == nil {
		t.Fatal("stale translation resurrected old revision result")
	}
	f.unchanged(t, before)
}

func TestTEST042ReviewNotificationFailureRollsBackPublicationAndHeldRefundFullHTTPIntegration(t *testing.T) {
	for _, status := range []string{"approved", "rejected"} {
		t.Run(status, func(t *testing.T) {
			f := newTEST042Fixture(t)
			currency := f.seedMoney(t)
			snapshot := f.snapshot("discussion", "Review atomic question")
			snapshot.BountyCurrency = "test042"
			snapshot.BountyAmount = 100
			item := f.create(t, f.editor, snapshot)
			if _, err := f.db.Exec(f.ctx, "alter table nats_outbox add constraint test042_fail_review_notice check(coalesce(payload->>'templateKey','') not in ('review_approved','review_rejected'))"); err != nil {
				t.Fatal(err)
			}
			before := f.facts(t)
			row := f.parallel(t, f.reviewer, []test038Operation{{http.MethodPatch, "/api/v1/content-revisions/" + item.RevisionID, map[string]any{"status": status, "note": "test042 review notification failure"}}})[0]
			if row.status != 500 || !reflect.DeepEqual(before, f.facts(t)) {
				var persisted string
				if err := f.db.QueryRow(f.ctx, "select review_status from community_posts where public_id=$1", item.ID).Scan(&persisted); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("failed review HTTP=%d persisted=%s factsUnchanged=%v body=%s", row.status, persisted, reflect.DeepEqual(before, f.facts(t)), row.raw)
			}
			if _, err := f.db.Exec(f.ctx, "alter table nats_outbox drop constraint test042_fail_review_notice"); err != nil {
				t.Fatal(err)
			}
			f.review(t, item, status)
			if status == "approved" {
				f.balance(t, f.editor, currency, 900)
			} else {
				f.balance(t, f.editor, currency, 1000)
			}
			before = f.facts(t)
			f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+item.RevisionID, map[string]any{"status": status, "note": "duplicate review"}, 409)
			f.unchanged(t, before)
		})
	}
}

func TestTEST042HoldDatabaseFailureAndInsufficientBalanceDoNotBecomeClientSuccessFullHTTPIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	currency := f.seedMoney(t)
	snapshot := f.snapshot("discussion", "Hold failure")
	snapshot.BountyCurrency = "test042"
	snapshot.BountyAmount = 1001
	before := f.facts(t)
	f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}, 409)
	f.unchanged(t, before)
	snapshot.BountyAmount = 100
	snapshot.BountyCurrency = "not_available"
	f.require(t, f.editor, http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}, 400)
	f.unchanged(t, before)
	snapshot.BountyCurrency = "test042"
	if _, err := f.db.Exec(f.ctx, "alter table currency_transactions add constraint test042_fail_hold check(transaction_type <> 'question_bounty_hold')"); err != nil {
		t.Fatal(err)
	}
	before = f.facts(t)
	row := f.parallel(t, f.editor, []test038Operation{{http.MethodPost, "/api/v1/community/posts", communityPostMutationRequest{communityPostSnapshot: snapshot}}})[0]
	if row.status != 500 || !reflect.DeepEqual(before, f.facts(t)) {
		t.Fatalf("actual hold ledger failure status=%d factsUnchanged=%v body=%s", row.status, reflect.DeepEqual(before, f.facts(t)), row.raw)
	}
	if _, err := f.db.Exec(f.ctx, "alter table currency_transactions drop constraint test042_fail_hold"); err != nil {
		t.Fatal(err)
	}
	f.create(t, f.editor, snapshot)
	f.balance(t, f.editor, currency, 900)
}

func TestTEST042DisplayedEditCapabilityMatchesActualFullHTTPPermissionIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	f.reviewPolicy(t, false)
	snapshot := f.snapshot("tutorial", "Capability source")
	item := f.create(t, f.editor, snapshot)
	for _, token := range []string{f.reviewer, f.otherEditor} {
		actual := f.detail(t, token, item.ID)
		page := decodeTEST022Data[communityPostPageResponse](t, f.require(t, token, http.MethodGet, "/api/v1/community/posts?kind=tutorial", nil, 200))
		if len(page.Items) != 1 || page.Items[0].ID != item.ID || page.Items[0].CanEdit != actual.CanEdit {
			t.Fatal("directory/detail advertised different actual edit permissions")
		}
		before := f.facts(t)
		want := 403
		if actual.CanEdit {
			want = 200
		}
		f.require(t, token, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: snapshot, BaseRevisionID: &item.RevisionID}, want)
		if want == 403 {
			f.unchanged(t, before)
		}
	}
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "community.edit")
	actual := f.detail(t, f.otherEditor, item.ID)
	if !actual.CanEdit {
		t.Fatal("actual community.edit session advertised no editing")
	}
	snapshot.Title = "Authorized shared edit"
	f.require(t, f.otherEditor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: snapshot, BaseRevisionID: &actual.PublishedRevisionID}, 200)
	if f.detail(t, "", item.ID).Title != snapshot.Title {
		t.Fatal("authorized actual shared edit did not publish")
	}
}

func TestTEST042AllFourUnpublishedResubmissionsRetainCreateReviewAndExplicitBypassFullHTTPIntegration(t *testing.T) {
	for _, kind := range []string{"tutorial", "issue", "news", "discussion"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST042Fixture(t)
			snapshot := f.snapshot(kind, "Never-published "+kind)
			item := f.create(t, f.editor, snapshot)
			f.review(t, item, "rejected")
			resubmit := decodeTEST022Data[test042Mutation](t, f.require(t, f.editor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: snapshot}, 200))
			if resubmit.ReviewStatus != "pending" {
				t.Fatalf("%s resubmission bypassed creation review", kind)
			}
			before := f.facts(t)
			f.require(t, "", http.MethodGet, "/api/v1/community/posts/"+item.ID, nil, 404)
			f.unchanged(t, before)
			f.review(t, resubmit, "rejected")
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "community.no-review")
			bypass := decodeTEST022Data[test042Mutation](t, f.require(t, f.editor, http.MethodPut, "/api/v1/community/posts/"+item.ID, communityPostMutationRequest{communityPostSnapshot: snapshot}, 200))
			if bypass.ReviewStatus != "approved" || f.detail(t, "", item.ID).PublishedRevisionID != bypass.RevisionID {
				t.Fatal("explicit authorized no-review bypass stopped working")
			}
		})
	}
}
