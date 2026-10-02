package antiabuse

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/querycache"
)

func TestAutomaticRestrictionFailsClosedAndRollsBackIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify automatic restriction failure semantics")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var userID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'restriction-reliability-test',true) returning id`,
		fmt.Sprintf("restriction-reliability-%d", suffix), fmt.Sprintf("restriction-reliability-%d@example.invalid", suffix)).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, fmt.Sprintf(`alter table anti_abuse_user_states
		add constraint reject_arch007_user check(user_id<>%d)`, userID)); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		cfg: config.AntiAbuseConfig{Enabled: true, IPHashSecret: "arch007-private-hash-secret-at-least-32-bytes"},
		db:  pool, cache: querycache.New(config.RedisConfig{}), now: time.Now,
		events: make(chan queuedRiskEvent, 2), successes: make(chan queuedSuccessRecord, 2),
	}
	input := Evaluation{UserID: userID, Action: "comment.create", Now: time.Now().UTC()}
	decision := Decision{Outcome: TempBlock, RiskScore: 90, Rules: []string{"arch007_injected"}}
	if err = service.RecordDecision(ctx, input, decision, HumanCrawler); err == nil {
		t.Fatal("automatic restriction state failure was hidden")
	}
	assertAutomaticRestrictionState(t, ctx, pool, userID, 0, 0)
	if metrics := service.AsyncMetrics(); metrics.RestrictionFailed != 1 {
		t.Fatalf("restriction failure metric=%d; want 1", metrics.RestrictionFailed)
	}

	if _, err = pool.Exec(ctx, `alter table anti_abuse_user_states drop constraint reject_arch007_user`); err != nil {
		t.Fatal(err)
	}
	if err = service.RecordDecision(ctx, input, decision, HumanCrawler); err != nil {
		t.Fatal(err)
	}
	assertAutomaticRestrictionState(t, ctx, pool, userID, 1, 1)
}

func assertAutomaticRestrictionState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64, wantRestrictions, wantState int) {
	t.Helper()
	var restrictions, states int
	if err := pool.QueryRow(ctx, `select
		(select count(*) from anti_abuse_restrictions where user_id=$1 and automatic),
		(select count(*) from anti_abuse_user_states where user_id=$1)`, userID).Scan(&restrictions, &states); err != nil {
		t.Fatal(err)
	}
	if restrictions != wantRestrictions || states != wantState {
		t.Fatalf("automatic restriction/state=%d/%d; want %d/%d", restrictions, states, wantRestrictions, wantState)
	}
}
