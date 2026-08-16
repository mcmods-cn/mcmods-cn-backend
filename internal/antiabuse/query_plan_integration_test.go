package antiabuse

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestAntiAbuseQueryPlans(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated development database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	plans := map[string]string{
		"risk events by action":       `select id from anti_abuse_events where action='comment.create' order by created_at desc,id desc limit 100`,
		"fingerprints by user/action": `select id from anti_abuse_content_fingerprints where action='comment.create' and user_id=1 and created_at>now()-interval '24 hours' order by created_at desc,id desc limit 200`,
		"active user restrictions":    `select id from anti_abuse_restrictions where user_id=1 and lifted_at is null and starts_at<=now() and (ends_at is null or ends_at>now()) order by starts_at desc limit 1`,
	}
	for name, query := range plans {
		rows, queryErr := pool.Query(ctx, "explain (costs true, format text) "+query)
		if queryErr != nil {
			t.Fatalf("%s: %v", name, queryErr)
		}
		lines := make([]string, 0)
		for rows.Next() {
			var line string
			if rows.Scan(&line) == nil {
				lines = append(lines, line)
			}
		}
		rows.Close()
		if len(lines) == 0 {
			t.Fatalf("%s returned an empty plan", name)
		}
		t.Logf("%s\n%s", name, strings.Join(lines, "\n"))
	}
}
