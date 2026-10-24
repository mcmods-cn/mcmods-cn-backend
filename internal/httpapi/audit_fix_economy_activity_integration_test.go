package httpapi

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixEconomyActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"user_checkins", "user_currency_balances", "currency_transactions"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003CheckinAndTransferActivityShareMoneyTransaction(t *testing.T) {
	for _, operation := range []string{"checkin", "transfer"} {
		t.Run(operation, func(t *testing.T) {
			f := newTEST013Fixture(t)
			grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "economy.transfer", "economy.checkin")
			var currency int64
			if err := f.db.QueryRow(f.ctx, `update currencies set transfer_tax_bps=0 where code='diamond' returning id`).Scan(&currency); err != nil {
				t.Fatal(err)
			}
			if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$2,1000)`, f.userIDs[f.editor], currency); err != nil {
				t.Fatal(err)
			}
			config := economyConfigPayload{Checkin: economyCheckinConfig{Enabled: true, Currency: "diamond", Amount: 7, MinimumHours: 20}, DownloadRewards: []economyDownloadReward{}}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, economyConfigSettingKey, encoded); err != nil {
				t.Fatal(err)
			}
			path := "/api/v1/users/me/economy/" + operation
			var body any
			action := int16(activity.ActionCheckIn)
			if operation == "transfer" {
				var recipient string
				if err = f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.otherEditor]).Scan(&recipient); err != nil {
					t.Fatal(err)
				}
				body = map[string]any{"recipientPublicId": recipient, "currency": "diamond", "amount": 100}
				action = activity.ActionTransfer
			}
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], true)
			before := auditFixEconomyActivityFacts(t, f)
			response := f.require(t, f.editor, http.MethodPost, path, body, 500)
			auditFixRequireSpecificActivityError(t, response, "failed to persist economy activity")
			auditFixRequireProjectFactsUnchanged(t, before, auditFixEconomyActivityFacts(t, f))
			auditFixInjectActivityFailure(t, f, f.userIDs[f.editor], false)
			f.require(t, f.editor, http.MethodPost, path, body, 200)
			want := int64(1007)
			wantLedgers := 1
			if operation == "transfer" {
				want = 900
				wantLedgers = 2
				assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{f.userIDs[f.otherEditor]: 100})
			}
			assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{f.userIDs[f.editor]: want})
			var ledgers int
			if err = f.db.QueryRow(f.ctx, `select count(*) from currency_transactions`).Scan(&ledgers); err != nil || ledgers != wantLedgers {
				t.Fatalf("money ledger rows=%d want%d err=%v", ledgers, wantLedgers, err)
			}
			if count := auditFixActivityIntentCount(t, f, f.editor, action, activity.ObjectEconomy); count != 1 {
				t.Fatalf("money activity=%d want1", count)
			}
			if operation == "checkin" {
				before = auditFixEconomyActivityFacts(t, f)
				f.require(t, f.editor, http.MethodPost, path, body, 409)
				auditFixRequireProjectFactsUnchanged(t, before, auditFixEconomyActivityFacts(t, f))
			}
		})
	}
}
