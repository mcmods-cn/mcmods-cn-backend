package antiabuse

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestAccountRestrictionsAreSelectedForEveryRequestedActionIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify action-scoped restriction selection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop ephemeral schema: %v", err)
		}
	}()

	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('restriction_matrix','restriction_matrix@example.invalid','test-only',true) returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = pool.Exec(ctx, `insert into anti_abuse_restrictions(user_id,actions,mode,source,reason,starts_at,ends_at) values
		($1,array['comment.create'],'cooldown','administrator','older comment restriction',$2,$4),
		($1,array['message.send'],'cooldown','administrator','newer message restriction',$3,$4)`,
		userID, now.Add(-10*time.Minute), now.Add(-5*time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	service := New(ctx, config.AntiAbuseConfig{}, pool, cache)
	commentProfile, err := service.account(ctx, userID, "", "", "comment.create")
	if err != nil {
		t.Fatal(err)
	}
	if !restrictionApplies(commentProfile, now) || commentProfile.RestrictionMode != "cooldown" {
		t.Fatal("older comment restriction was hidden by the newer message restriction")
	}
	messageProfile, err := service.account(ctx, userID, "", "", "message.send")
	if err != nil {
		t.Fatal(err)
	}
	if !restrictionApplies(messageProfile, now) || messageProfile.RestrictionMode != "cooldown" {
		t.Fatal("message restriction was not enforced when evaluated after a comment")
	}
	service.cfg.Enabled = true
	for _, action := range []string{"comment.create", "message.send"} {
		decision, err := service.Evaluate(ctx, Evaluation{UserID: userID, Action: action, Now: now})
		if err != nil {
			t.Fatalf("evaluate %s: %v", action, err)
		}
		if decision.Code != "action_restricted" || decision.Outcome != TempBlock || decision.RetryAfter <= 0 {
			t.Fatalf("%s decision=%+v; want active temporary restriction", action, decision)
		}
	}
	unrelatedProfile, err := service.account(ctx, userID, "", "", "upload.create")
	if err != nil {
		t.Fatal(err)
	}
	if restrictionApplies(unrelatedProfile, now) || unrelatedProfile.RestrictionChallengeEnd.After(now) || unrelatedProfile.RestrictionModerationEnd.After(now) {
		t.Fatalf("unrelated action inherited restriction effects: %+v", unrelatedProfile)
	}

	if _, err = pool.Exec(ctx, `insert into anti_abuse_restrictions(user_id,actions,mode,source,reason,starts_at,ends_at) values
		($1,array['review.submit'],'challenge','administrator','review challenge',$2,$4),
		($1,array['review.submit'],'moderation','administrator','review moderation',$3,$4),
		($1,array['comment.create'],'cooldown','administrator','newest comment restriction',$3,$4)`,
		userID, now.Add(-8*time.Minute), now.Add(-3*time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	reviewProfile, err := service.account(ctx, userID, "", "", "review.submit")
	if err != nil {
		t.Fatal(err)
	}
	if restrictionApplies(reviewProfile, now) || !reviewProfile.RestrictionChallengeEnd.After(now) || !reviewProfile.RestrictionModerationEnd.After(now) {
		t.Fatalf("challenge and moderation restrictions were not combined: %+v", reviewProfile)
	}
	commentProfile, err = service.account(ctx, userID, "", "", "comment.create")
	if err != nil || !restrictionApplies(commentProfile, now) {
		t.Fatalf("newest relevant restriction was not selected: %+v err=%v", commentProfile, err)
	}

	if _, err = pool.Exec(ctx, `insert into anti_abuse_restrictions(user_id,actions,mode,source,reason,starts_at,ends_at) values
		($1,'{}'::text[],'read_only','administrator','older global read only',$2,$4),
		($1,array['*'],'cooldown','administrator','newer wildcard cooldown',$3,$4)`,
		userID, now.Add(-20*time.Minute), now.Add(-time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	globalProfile, err := service.account(ctx, userID, "", "", "upload.create")
	if err != nil {
		t.Fatal(err)
	}
	if !restrictionApplies(globalProfile, now) || globalProfile.RestrictionMode != "read_only" {
		t.Fatalf("global read-only restriction did not apply: %+v", globalProfile)
	}

	// A temporary hard restriction and a scheduled one share the same boundary.
	// No administrator writes or invalidates the cache between these evaluations.
	var changingID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values('timed_rules','timed_rules@example.invalid','test-only',true) returning id`).Scan(&changingID); err != nil {
		t.Fatal(err)
	}
	var transition time.Time
	if err = pool.QueryRow(ctx, `insert into anti_abuse_restrictions(user_id,mode,source,reason,ends_at)
		values($1,'temporary_ban','administrator','temporary',clock_timestamp()+interval '5 seconds') returning ends_at`, changingID).Scan(&transition); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into anti_abuse_restrictions(user_id,actions,mode,source,reason,starts_at)
		values($1,array['comment.create'],'no_comment','administrator','ongoing',now()),
		      ($1,array['review.submit'],'moderation','administrator','scheduled',$2)`, changingID, transition); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"comment.create", "review.submit"} {
		before, err := service.account(ctx, changingID, "", "", action)
		if err != nil || before.RestrictionMode != "temporary_ban" || before.RestrictionModerationEnd.After(time.Now()) {
			t.Fatalf("initial %s rules=%+v err=%v", action, before, err)
		}
	}
	if _, err := pool.Exec(ctx, "select pg_sleep_until($1)", transition); err != nil {
		t.Fatal(err)
	}
	commentProfile, err = service.account(ctx, changingID, "", "", "comment.create")
	if err != nil || commentProfile.RestrictionMode != "no_comment" {
		t.Fatalf("longer restriction after temporary ban: mode=%s err=%v", commentProfile.RestrictionMode, err)
	}
	reviewProfile, err = service.account(ctx, changingID, "", "", "review.submit")
	if err != nil || reviewProfile.RestrictionMode != "" || !reviewProfile.RestrictionModerationEnd.After(time.Now()) {
		t.Fatalf("scheduled moderation after temporary ban: profile=%+v err=%v", reviewProfile, err)
	}

}

func TestAccountRiskReadsCommittedStateAcrossInstancesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires two PostgreSQL connections to a task-owned schema")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, err := database.NewIsolatedTestSchema(ctx, config.Load().DB.ConnString(), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fixture.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	peerPool, err := pgxpool.NewWithConfig(ctx, fixture.Pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer peerPool.Close()
	var userID int64
	if err := fixture.Pool.QueryRow(ctx, `insert into users(username,email,password_hash)
		values('peer_risk','peer_risk@example.invalid','test-only') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	writerCache := querycache.New(config.RedisConfig{})
	defer writerCache.Close()
	readerCache := querycache.New(config.RedisConfig{})
	defer readerCache.Close()
	writer := New(ctx, config.AntiAbuseConfig{}, fixture.Pool, writerCache)
	defer writer.Close(ctx)
	reader := New(ctx, config.AntiAbuseConfig{}, peerPool, readerCache)
	defer reader.Close(ctx)
	checkBoth := func(want string, trusted bool) {
		t.Helper()
		for _, service := range []*Service{writer, reader} {
			p, err := service.account(ctx, userID, "network-fixture", "device-fixture", "comment.create")
			if err != nil || p.RestrictionMode != want || p.ManuallyTrusted != trusted {
				t.Fatalf("current risk mode=%q trusted=%v err=%v, want %q/%v", p.RestrictionMode, p.ManuallyTrusted, err, want, trusted)
			}
		}
	}
	checkBoth("", false)
	if _, err := fixture.Pool.Exec(ctx, `insert into anti_abuse_user_states(user_id,manually_trusted) values($1,true)`, userID); err != nil {
		t.Fatal(err)
	}
	checkBoth("", true)
	if _, err := fixture.Pool.Exec(ctx, `insert into anti_abuse_restrictions(device_hash,mode,source,reason)
		values('device-fixture','read_only','security_import','shared device rule')`); err != nil {
		t.Fatal(err)
	}
	checkBoth("read_only", true)
	if _, err := fixture.Pool.Exec(ctx, `update anti_abuse_restrictions set lifted_at=now() where device_hash='device-fixture'`); err != nil {
		t.Fatal(err)
	}
	checkBoth("", true)
}
