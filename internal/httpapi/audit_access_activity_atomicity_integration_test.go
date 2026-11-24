package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
)

func auditAccessActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"creators", "creator_claims", "creator_claim_attachments", "creator_links", "content_localizations", "user_inventory", "user_currency_balances", "currency_transactions", "shop_purchases", "content_heat_promotions", "content_heat_promotion_counters"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(row) order by to_jsonb(row)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` row`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}

func auditAccessFailActivityThenRecover(t *testing.T, f test013Fixture, path, method string, body any, want int, action int16, objectType int16) {
	t.Helper()
	if _, err := f.db.Exec(f.ctx, `create function audit_access_outbox_unavailable() returns trigger language plpgsql as $$ begin raise exception 'AUDIT_ACCESS_ACTIVITY_OUTBOX_UNAVAILABLE' using errcode='XX001'; end $$; create trigger audit_access_outbox_unavailable before insert on activity_event_outbox for each row execute function audit_access_outbox_unavailable()`); err != nil {
		t.Fatal(err)
	}
	before := auditAccessActivityFacts(t, f)
	failed := f.require(t, f.editor, method, path, body, 500)
	if !strings.Contains(string(failed), "failed to persist") {
		t.Fatalf("failure did not reach explicit activity-intent boundary: %s", failed)
	}
	if after := auditAccessActivityFacts(t, f); !reflect.DeepEqual(before, after) {
		t.Fatal("activity-intent storage failure committed business or outbox facts")
	}
	if _, err := f.db.Exec(f.ctx, `drop trigger audit_access_outbox_unavailable on activity_event_outbox; drop function audit_access_outbox_unavailable()`); err != nil {
		t.Fatal(err)
	}
	var prior, count int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], action, objectType).Scan(&prior); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, method, path, body, want)
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1 and action_id=$2 and object_type_id=$3`, f.userIDs[f.editor], action, objectType).Scan(&count); err != nil || count != prior+1 {
		t.Fatalf("recovery activity count=%d prior=%d err=%v", count, prior, err)
	}
}

func TestAuditCMP003CreatorMutationActivityIsAtomicFullHTTPIntegration(t *testing.T) {
	for _, operation := range []string{"create", "update", "claim"} {
		t.Run(operation, func(t *testing.T) {
			f := newTEST013Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.create", "creator.edit", "creator.claim")
			snapshot := creatorSnapshot{Kind: "author", Name: "Audit atomic author", DescriptionMarkdown: "Synthetic updated description", DefaultLocale: "en-US", Localizations: []creatorLocalizationEdit{{Locale: "en-US", ContentMarkdown: "Synthetic updated description"}}, Links: []creatorLinkPayload{}}
			path := "/api/v1/creators"
			method := http.MethodPost
			want := 201
			action := int16(activity.ActionCreate)
			var body any = snapshot
			if operation != "create" {
				var publicID string
				if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,description_markdown,review_status) values('author','Audit atomic author','audit atomic author','Synthetic old description','approved') returning public_id`).Scan(&publicID); err != nil {
					t.Fatal(err)
				}
				path += "/" + publicID
				if operation == "update" {
					method = http.MethodPut
					want = 200
					action = activity.ActionEdit
				} else {
					path += "/claims"
					action = activity.ActionClaim
					body = map[string]any{"proofMarkdown": "Synthetic atomic claim proof"}
				}
			}
			auditAccessFailActivityThenRecover(t, f, path, method, body, want, action, activity.ObjectAuthor)
		})
	}
}

func TestAuditCMP003ShopActivityRollsBackMoneyAndConsumptionFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "shop.purchase", "shop.use", "shop.project_heat_boost.purchase", "shop.project_heat_boost.use")
	if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) select $1,id,1000 from currencies where code='diamond'`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	auditAccessFailActivityThenRecover(t, f, "/api/v1/users/me/shop/purchase", http.MethodPost, map[string]any{"itemCode": "project_heat_boost", "quantity": 1}, 200, activity.ActionCreate, activity.ObjectShopItem)
	auditAccessFailActivityThenRecover(t, f, "/api/v1/users/me/shop/use", http.MethodPost, map[string]any{"itemCode": "project_heat_boost", "targetType": "mod", "targetId": f.modCode}, 200, activity.ActionUse, activity.ObjectShopItem)
	var quantity, promotions, ledger int
	if err := f.db.QueryRow(f.ctx, `select (select quantity from user_inventory where user_id=$1),(select count(*) from content_heat_promotions where applied_by=$1),(select count(*) from currency_transactions where user_id=$1 and transaction_type='shop_purchase')`, f.userIDs[f.editor]).Scan(&quantity, &promotions, &ledger); err != nil || quantity != 0 || promotions != 1 || ledger != 1 {
		t.Fatalf("quantity=%d promotions=%d ledger=%d err=%v", quantity, promotions, ledger, err)
	}
	// The durable intent is a real persisted row; its consumer/reward recovery is
	// covered separately. This test does not label an outbox row as a worker ACK.
	var matches int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox event
		join shop_items item on item.public_id=event.object_public_id
		where event.user_id=$1 and event.action_id=$2 and event.object_type_id=$3 and item.code='project_heat_boost'`,
		f.userIDs[f.editor], activity.ActionUse, activity.ObjectShopItem).Scan(&matches); err != nil || matches != 1 {
		t.Fatalf("activity did not reference the consumed shop item: matches=%d err=%v", matches, err)
	}
}
