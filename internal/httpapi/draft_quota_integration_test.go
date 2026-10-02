package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestDraftQuotaCountsAndReclaimsAtomically(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `create temp table user_drafts(
		id bigserial primary key,user_id bigint not null,draft_key text not null,
		payload jsonb not null check(jsonb_typeof(payload)='object') check(octet_length(payload::text)<=524288),
		submitted_at timestamptz,expires_at timestamptz not null) on commit drop`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into user_drafts(user_id,draft_key,payload,expires_at)
		values(1,'a','{"v":"aaaaaaaaaa"}',now()+interval '1 day'),
		(1,'b','{"v":"bbbbbbbbbb"}',now()+interval '1 day'),
		(1,'expired','{"v":"old"}',now()-interval '1 second')`); err != nil {
		t.Fatal(err)
	}
	policy := userDraftQuotaPolicy{MaximumActive: 2, MaximumCompleted: 2, MaximumBytes: 1024}
	if err = enforceUserDraftQuota(ctx, tx, 1, "c", json.RawMessage(`{"v":"c"}`), false, policy); !errors.Is(err, errUserDraftCountQuota) {
		t.Fatalf("third active draft quota error = %v", err)
	}
	if err = enforceUserDraftQuota(ctx, tx, 1, "a", json.RawMessage(`{"v":"replacement"}`), false, policy); err != nil {
		t.Fatalf("active replacement failed: %v", err)
	}
	var expired int
	if err = tx.QueryRow(ctx, `select count(*) from user_drafts where user_id=1 and draft_key='expired'`).Scan(&expired); err != nil || expired != 0 {
		t.Fatalf("expired quota row count = %d/%v", expired, err)
	}
	if _, err = tx.Exec(ctx, `insert into user_drafts(user_id,draft_key,payload,submitted_at,expires_at)
		values(2,'done-a','{"v":"a"}',now(),now()+interval '1 day'),
		(2,'done-b','{"v":"b"}',now(),now()+interval '1 day')`); err != nil {
		t.Fatal(err)
	}
	if err = enforceUserDraftQuota(ctx, tx, 2, "done-c", json.RawMessage(`{"v":"c"}`), true, policy); !errors.Is(err, errUserDraftCountQuota) {
		t.Fatalf("third completed draft quota error = %v", err)
	}
}

func TestDraftQuotaAdvisoryLockSerializesOneUser(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	first, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Rollback(ctx)
	second, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(ctx)
	const userID int64 = 982451653
	if _, err = first.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('user-draft-quota:'||$1::bigint::text,0))`, userID); err != nil {
		t.Fatal(err)
	}
	var acquired bool
	if err = second.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended('user-draft-quota:'||$1::bigint::text,0))`, userID).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("concurrent quota reservation acquired the same user lock")
	}
	if err = first.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = second.QueryRow(ctx, `select pg_try_advisory_xact_lock(hashtextextended('user-draft-quota:'||$1::bigint::text,0))`, userID).Scan(&acquired); err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("quota lock was not reusable after the reserving transaction committed")
	}
}
