package progression

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
)

func TestActivityRewardRollbackRecoveryAndConcurrentWorkersIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("requires owned isolated PostgreSQL")
	}
	cfg := config.Load()
	if cfg.Env != "test" || cfg.DB.URL != "" || cfg.DB.Host != "127.0.0.1" {
		t.Fatal("requires owned loopback test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, cfg.DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	name := fmt.Sprintf("reward-%d", time.Now().UnixNano())
	var userID, currencyID, taskID int64
	var publicID string
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash) values($1,$2,'synthetic') returning id,public_id`, name, name+"@example.invalid").Scan(&userID, &publicID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		for _, q := range []string{`delete from activity_event_outbox where user_id=$1`, `delete from user_activity_events where user_id=$1`, `delete from experience_transactions where user_id=$1`, `delete from currency_transactions where user_id=$1`, `delete from users where id=$1`} {
			if _, err := pool.Exec(cleanup, q, userID); err != nil {
				t.Error(err)
			}
		}
		if _, err := pool.Exec(cleanup, `delete from task_definitions where id=$1`, taskID); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(cleanup, `delete from currencies where id=$1`, currencyID); err != nil {
			t.Error(err)
		}
	}()
	if err = pool.QueryRow(ctx, `insert into currencies(code,name,status) values($1,'synthetic reward','disabled') returning id`, name).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	condition, err := json.Marshal(taskCondition{Action: "create", ObjectType: "user", ObjectPublicID: publicID, Metric: "count", Target: 2})
	if err != nil {
		t.Fatal(err)
	}
	rewards, err := json.Marshal(taskRewards{Experience: 7, Currencies: map[string]int64{name: 11}})
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into task_definitions(code,name,condition,rewards) values($1,'synthetic reward',$2,$3) returning id`, name, condition, rewards).Scan(&taskID); err != nil {
		t.Fatal(err)
	}
	// Both events exist before the worker starts, so the failed batch has to
	// roll back facts, progress, experience and its first reward ledger entry.
	if _, err = pool.Exec(ctx, `insert into activity_event_outbox(user_id,action_id,object_type_id,object_public_id,occurred_at) select $1,$2,$3,$4,now() from generate_series(1,2)`, userID, activity.ActionCreate, activity.ObjectUser, publicID); err != nil {
		t.Fatal(err)
	}
	svc := NewService(pool, nil)
	opts := activity.Options{BatchSize: 256, FlushInterval: 10 * time.Millisecond, RetryMinDelay: 10 * time.Millisecond, RetryMaxDelay: 20 * time.Millisecond}
	m1 := activity.NewMonitor(pool, svc.ProcessActivityBatchTx, opts)
	defer func() {
		shutdown, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		if err := m1.Close(shutdown); err != nil {
			t.Error(err)
		}
	}()
	for m1.Snapshot(ctx).FlushFailures == 0 {
		select {
		case <-ctx.Done():
			t.Fatal("failure not observed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	var queued, facts, progress, experience int
	if err = pool.QueryRow(ctx, `select (select count(*) from activity_event_outbox where user_id=$1),(select count(*) from user_activity_events where user_id=$1),(select count(*) from user_task_progress where user_id=$1),(select count(*) from experience_transactions where user_id=$1)`, userID).Scan(&queued, &facts, &progress, &experience); err != nil {
		t.Fatal(err)
	}
	if queued != 2 || facts != 0 || progress != 0 || experience != 0 {
		t.Fatalf("failed reward partially committed: queued=%d facts=%d progress=%d xp=%d", queued, facts, progress, experience)
	}
	if _, err = pool.Exec(ctx, `update currencies set status='active' where id=$1`, currencyID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update activity_event_outbox set available_at=now() where user_id=$1`, userID); err != nil {
		t.Fatal(err)
	}
	m2 := activity.NewMonitor(pool, svc.ProcessActivityBatchTx, opts)
	defer func() {
		shutdown, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		if err := m2.Close(shutdown); err != nil {
			t.Error(err)
		}
	}()
	for {
		if err = pool.QueryRow(ctx, `select count(*) from activity_event_outbox where user_id=$1`, userID).Scan(&queued); err != nil {
			t.Fatal(err)
		}
		if queued == 0 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("recovery did not drain")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for _, m := range []*activity.Monitor{m1, m2} {
		if err = m.RecordDurable(ctx, activity.Event{UserID: userID, ActionID: activity.ActionCreate, ObjectTypeID: activity.ObjectUser, ObjectPublicID: publicID}); err != nil {
			t.Fatal(err)
		}
	}
	for {
		if err = pool.QueryRow(ctx, `select count(*) from user_activity_events where user_id=$1`, userID).Scan(&facts); err != nil {
			t.Fatal(err)
		}
		if facts == 4 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("concurrent workers did not drain")
		case <-time.After(10 * time.Millisecond):
		}
	}
	var balance, xp int64
	var currencyEntries, experienceEntries int
	if err = pool.QueryRow(ctx, `select (select balance from user_currency_balances where user_id=$1 and currency_id=$2),(select experience from user_experience where user_id=$1),(select count(*) from currency_transactions where user_id=$1),(select count(*) from experience_transactions where user_id=$1)`, userID, currencyID).Scan(&balance, &xp, &currencyEntries, &experienceEntries); err != nil {
		t.Fatal(err)
	}
	if balance != 11 || xp != 7 || currencyEntries != 1 || experienceEntries != 1 {
		t.Fatalf("reward repeated or lost: balance=%d xp=%d currency_entries=%d xp_entries=%d", balance, xp, currencyEntries, experienceEntries)
	}
}
