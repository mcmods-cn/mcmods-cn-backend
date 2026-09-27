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
		t.Fatal("message restriction was not enforced after the comment-scoped cache lookup")
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
	service.InvalidateAccountState(ctx, userID)
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
	service.InvalidateAccountState(ctx, userID)
	globalProfile, err := service.account(ctx, userID, "", "", "upload.create")
	if err != nil {
		t.Fatal(err)
	}
	if !restrictionApplies(globalProfile, now) || globalProfile.RestrictionMode != "read_only" {
		t.Fatalf("global read-only restriction did not apply: %+v", globalProfile)
	}
}
