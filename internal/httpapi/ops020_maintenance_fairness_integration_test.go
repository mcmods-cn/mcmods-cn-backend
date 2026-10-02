package httpapi

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOPS020MaintenanceTimeoutDoesNotStarveBansAndTTLIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify maintenance fairness")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	if _, err := pool.Exec(ctx, `
		insert into roles(code,name) values('banned','Banned fixture') on conflict(code) do nothing;
		insert into ban_reasons(code,translations) values('malicious_spam','{"zh-CN":"测试","en-US":"Fixture"}') on conflict(code) do nothing;
		insert into users(username,email,password_hash,email_verified)
		values('ops020_expired','ops020_expired@example.invalid','test-only',true),
		      ('ops020_future','ops020_future@example.invalid','test-only',true);
		insert into ban_records(user_id,moderator_id,reason_code,username_snapshot,starts_at,ends_at)
		select id,id,'malicious_spam',username,now()-interval '2 hours',
			case when username='ops020_expired' then now()-interval '1 hour' else now()+interval '1 hour' end
		from users where username like 'ops020_%';
		insert into user_role_bindings(user_id,role_id,source,source_key,expires_at)
		select ban.user_id,role.id,'governance_ban',ban.id::text,ban.ends_at
		from ban_records ban cross join roles role where role.code='banned' and ban.username_snapshot like 'ops020_%';
		insert into user_role_bindings(user_id,role_id,source,source_key)
		select actor.id,role.id,'manual','' from users actor cross join roles role
		where actor.username='ops020_expired' and role.code='banned';
		insert into user_drafts(user_id,draft_key,project_key,kind,edit_url,payload,expires_at)
		select actor.id,'ops020-expired-'||ordinal,'ops020','mod','/edit','{}',now()-interval '1 hour'
		from users actor cross join generate_series(1,4005) ordinal where actor.username='ops020_expired';
		insert into user_drafts(user_id,draft_key,project_key,kind,edit_url,payload,expires_at)
		select id,'ops020-future','ops020','mod','/edit','{}',now()+interval '1 hour' from users where username='ops020_future';
		insert into log_shares(public_code,source_type,status,expires_at)
		values('ops020expired','paste','ready',now()-interval '1 hour'),('ops020futurex','paste','ready',now()+interval '1 hour');
		insert into log_share_entries(log_share_id,entry_index,sanitized_text)
		select id,0,'fixture' from log_shares where public_code in ('ops020expired','ops020futurex');
		insert into player_profiles(user_id,uuid,name)
		select id,'00000000-0000-0000-0000-000000000020','OPS020' from users where username='ops020_expired';
		insert into yggdrasil_tokens(access_token_hash,user_id,player_profile_id,client_token,expires_at)
		select repeat('a',64),user_id,id,'ops020',now()+interval '1 hour' from player_profiles where name='OPS020';
		insert into yggdrasil_join_sessions(server_id,token_id,player_profile_id,expires_at)
		select key,token.id,profile.id,expiry from yggdrasil_tokens token join player_profiles profile on profile.id=token.player_profile_id
		cross join (values('ops020-expired',now()-interval '1 hour'),('ops020-future',now()+interval '1 hour')) fixture(key,expiry)
		where token.client_token='ops020';
		insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
		select decode(case when username='ops020_expired' then repeat('a',64) else repeat('b',64) end,'hex'),id,auth_version,now()+interval '1 hour'
		from users where username like 'ops020_%';
		insert into user_presence_sessions(session_hash,user_id,last_active_at)
		select session.session_hash,session.user_id,case when actor.username='ops020_expired' then now()-interval '2 days' else now() end
		from auth_sessions session join users actor on actor.id=session.user_id where actor.username like 'ops020_%';
	`); err != nil {
		t.Fatal(err)
	}
	var originalAuthVersion int64
	if err := pool.QueryRow(ctx, `select auth_version from users where username='ops020_expired'`).Scan(&originalAuthVersion); err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, `lock table blueprints in access exclusive mode`); err != nil {
		t.Fatal(err)
	}
	worker := &MaintenanceWorker{db: pool}
	worker.pruneWithTimeout(ctx, time.Second)
	assertOPS020MaintenanceState(t, ctx, pool, originalAuthVersion, 5)
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	// A retained backlog must remain resumable, including competing workers.
	if _, err = pool.Exec(ctx, `insert into user_drafts(user_id,draft_key,project_key,kind,edit_url,payload,expires_at)
		select actor.id,'ops020-more-'||ordinal,'ops020','mod','/edit','{}',now()-interval '1 hour'
		from users actor cross join generate_series(1,6000) ordinal where actor.username='ops020_expired'`); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			(&MaintenanceWorker{db: pool}).pruneWithTimeout(ctx, 5*time.Second)
		}()
	}
	workers.Wait()
	worker.pruneWithTimeout(ctx, 5*time.Second)
	assertOPS020MaintenanceState(t, ctx, pool, originalAuthVersion, 0)
	// A canceled parent must stop the whole round instead of creating fresh
	// background contexts that keep deleting after shutdown.
	if _, err = pool.Exec(ctx, `insert into user_drafts(user_id,draft_key,project_key,kind,edit_url,payload,expires_at)
		select id,'ops020-cancelled','ops020','mod','/edit','{}',now()-interval '1 hour'
		from users where username='ops020_expired'`); err != nil {
		t.Fatal(err)
	}
	stopped, stop := context.WithCancel(ctx)
	stop()
	worker.pruneWithTimeout(stopped, time.Second)
	assertOPS020MaintenanceState(t, ctx, pool, originalAuthVersion, 1)
}

func assertOPS020MaintenanceState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, authVersion int64, expiredDrafts int) {
	t.Helper()
	var expiredBan, futureBan, expiredShare, futureShare string
	var expiredBindings, manualBindings, futureBindings, drafts, futureDrafts, entries, joins, presence, sessions int
	var currentAuthVersion int64
	err := pool.QueryRow(ctx, `select
		(select status from ban_records where username_snapshot='ops020_expired'),
		(select status from ban_records where username_snapshot='ops020_future'),
		(select count(*) from user_role_bindings binding join users actor on actor.id=binding.user_id where actor.username='ops020_expired' and source='governance_ban'),
		(select count(*) from user_role_bindings binding join users actor on actor.id=binding.user_id where actor.username='ops020_expired' and source='manual'),
		(select count(*) from user_role_bindings binding join users actor on actor.id=binding.user_id where actor.username='ops020_future' and source='governance_ban'),
		(select status from log_shares where public_code='ops020expired'),
		(select status from log_shares where public_code='ops020futurex'),
		(select count(*) from user_drafts where draft_key like 'ops020-%' and expires_at<now()),
		(select count(*) from user_drafts where draft_key='ops020-future'),
		(select count(*) from log_share_entries entry join log_shares share on share.id=entry.log_share_id where share.public_code in ('ops020expired','ops020futurex')),
		(select count(*) from yggdrasil_join_sessions where server_id like 'ops020-%'),
		(select count(*) from user_presence_sessions presence join users actor on actor.id=presence.user_id where actor.username like 'ops020_%'),
		(select count(*) from auth_sessions session join users actor on actor.id=session.user_id where actor.username like 'ops020_%'),
		(select auth_version from users where username='ops020_expired')`).Scan(
		&expiredBan, &futureBan, &expiredBindings, &manualBindings, &futureBindings, &expiredShare, &futureShare,
		&drafts, &futureDrafts, &entries, &joins, &presence, &sessions, &currentAuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	if expiredBan != "expired" || futureBan != "active" || expiredBindings != 0 || manualBindings != 1 || futureBindings != 1 ||
		expiredShare != "expired" || futureShare != "ready" || drafts != expiredDrafts || futureDrafts != 1 || entries != 1 || joins != 1 || presence != 1 || sessions != 2 || currentAuthVersion != authVersion {
		t.Fatalf("maintenance bans=%s/%s bindings=%d/%d/%d shares=%s/%s drafts=%d/%d entries=%d joins=%d presence=%d sessions=%d auth=%d; want expired drafts=%d auth=%d",
			expiredBan, futureBan, expiredBindings, manualBindings, futureBindings, expiredShare, futureShare,
			drafts, futureDrafts, entries, joins, presence, sessions, currentAuthVersion, expiredDrafts, authVersion)
	}
}
