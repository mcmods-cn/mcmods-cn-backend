package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
)

type auditOwnedRewardConstraintTrace struct{ hits atomic.Int64 }

func (*auditOwnedRewardConstraintTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}
func (trace *auditOwnedRewardConstraintTrace) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var e *pgconn.PgError
	if errors.As(data.Err, &e) && e.Code == "23514" && e.ConstraintName == "audit_reject_owned_download_reward" {
		trace.hits.Add(1)
	}
}

func auditFixOwnedDownloadFacts(t *testing.T, f test036Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"content_download_counters", "content_download_reward_counters", "user_currency_balances", "currency_transactions", "oss_download_stats"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003BlueprintDownloadRewardSharesCapabilityTransaction(t *testing.T) {
	f := newTEST036Fixture(t)
	source, _, err := encodeBlueprint(test036Document(), "nbt")
	if err != nil {
		t.Fatal(err)
	}
	upload := f.upload(t, f.editor, "atomic-download-reward.nbt", source)
	if err = f.worker("owned-reward").processJob(f.ctx, upload.jobID); err != nil {
		t.Fatal(err)
	}
	f.approve(t, upload.publicID)
	var variant, objectKey string
	if err = f.db.QueryRow(f.ctx, `select variant.public_id,variant.object_key from blueprint_variants variant join blueprints blueprint on blueprint.id=variant.blueprint_id where blueprint.public_id=$1 and variant.original and variant.status='ready'`, upload.publicID).Scan(&variant, &objectKey); err != nil {
		t.Fatal(err)
	}
	cfg := defaultEconomyConfig()
	cfg.DownloadRewards = []economyDownloadReward{{ObjectType: "blueprint", Currency: "diamond", DownloadsPerReward: 2, Amount: 3}}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,$2::jsonb) on conflict(key) do update set value=excluded.value`, economyConfigSettingKey, encoded); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/blueprints/" + upload.publicID + "/variants/" + variant + "/download"
	invoke := func(actor string, want int) map[string]any {
		t.Helper()
		request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.origin.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if actor != "" {
			request.Header.Set("Authorization", "Bearer "+actor)
		}
		response, err := f.origin.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			var stats, content, ledger, balance int64
			if err = f.db.QueryRow(f.ctx, `select coalesce((select downloads from oss_download_stats where object_key=$1),0),coalesce((select downloads from content_download_counters where owner_id=$2),0),(select count(*) from currency_transactions where user_id=$2),coalesce((select balance from user_currency_balances balance join currencies currency on currency.id=balance.currency_id where balance.user_id=$2 and currency.code='diamond'),0)`, objectKey, f.userIDs[f.editor]).Scan(&stats, &content, &ledger, &balance); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("controlled reward fault HTTP=%d want=%d OSSdisclosures=%d contentdownloads=%d ownerledger=%d balance=%d; response capability deliberately omitted", response.StatusCode, want, stats, content, ledger, balance)
		}
		if want == 500 {
			if !bytes.Contains(body, []byte("failed to persist OSS download")) || bytes.Contains(body, []byte("\"url\"")) {
				t.Fatal("reward failure returned unrelated error or capability")
			}
			return nil
		}
		return decodeTEST022Data[map[string]any](t, body)
	}
	invoke(f.otherEditor, 200) // First legitimate download stays below the configured reward step.
	if _, err = f.db.Exec(f.ctx, `alter table currency_transactions add constraint audit_reject_owned_download_reward check(transaction_type<>'content_download_reward') not valid`); err != nil {
		t.Fatal(err)
	}
	// Capture the exact owned ledger constraint; an unrelated SQL failure must
	// never satisfy this intended financial rejection.
	faults := &auditOwnedRewardConstraintTrace{}
	poolConfig := f.db.Config().Copy()
	poolConfig.ConnConfig.Tracer = faults
	traced, err := pgxpool.NewWithConfig(f.ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(traced.Close)
	f.server.db = traced
	before := auditFixOwnedDownloadFacts(t, f)
	invoke(f.otherEditor, 500)
	if faults.hits.Load() != 1 {
		t.Fatalf("exact controlled ledger constraint hits=%d", faults.hits.Load())
	}
	auditFixRequireProjectFactsUnchanged(t, before, auditFixOwnedDownloadFacts(t, f))
	if _, err = f.db.Exec(f.ctx, `alter table currency_transactions drop constraint audit_reject_owned_download_reward`); err != nil {
		t.Fatal(err)
	}
	second := invoke(f.otherEditor, 200)
	actualURL, _ := second["url"].(string)
	response, err := f.provider.Client().Get(actualURL)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(downloaded, f.object(t, objectKey)) {
		t.Fatal("actual recovered artifact bytes changed")
	}
	invoke(f.otherEditor, 200)
	invoke(f.otherEditor, 200)
	assertReward := func(wantDownloads, wantSteps, wantBalance, wantLedgers int64) {
		t.Helper()
		var downloads, steps, balance, ledgers int64
		if err := f.db.QueryRow(f.ctx, `select (select downloads from content_download_counters where owner_id=$1),(select rewarded_steps from content_download_reward_counters where owner_id=$1),coalesce((select balance from user_currency_balances balance join currencies currency on currency.id=balance.currency_id where balance.user_id=$1 and currency.code='diamond'),0),(select count(*) from currency_transactions where user_id=$1 and transaction_type='content_download_reward')`, f.userIDs[f.editor]).Scan(&downloads, &steps, &balance, &ledgers); err != nil {
			t.Fatal(err)
		}
		if downloads != wantDownloads || steps != wantSteps || balance != wantBalance || ledgers != wantLedgers {
			t.Fatalf("download/rewardstep/balance/ledger=%d/%d/%d/%d want=%d/%d/%d/%d", downloads, steps, balance, ledgers, wantDownloads, wantSteps, wantBalance, wantLedgers)
		}
	}
	assertReward(4, 2, 6, 2)
	if got := auditFixActivityIntentCount(t, f.test013Fixture, f.otherEditor, activity.ActionDownload, activity.ObjectBlueprint); got != 4 {
		t.Fatalf("successful receiver download intents=%d", got)
	}
	invoke(f.editor, 200)
	assertReward(4, 2, 6, 2) // Preserve the existing self-download exclusion.
	invoke("", 200)
	assertReward(5, 2, 6, 2) // Anonymous public downloads retain their existing counter policy.
}
