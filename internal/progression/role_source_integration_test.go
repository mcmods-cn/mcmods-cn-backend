package progression

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func openRoleSourceTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run role source tests")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestSyncTrackRoleReplacesOnlyLevelSourceAcrossTrackSwitchAndClear(t *testing.T) {
	db := openRoleSourceTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`create temp table users(id bigint primary key) on commit drop`,
		`create temp table permission_role_track_roles(track_code text not null,role_id bigint not null,position integer not null) on commit drop`,
		`create temp table user_role_bindings(
			user_id bigint not null,role_id bigint not null,source text not null default 'manual',source_key text not null default '',
			expires_at timestamptz,created_at timestamptz not null default now(),primary key(user_id,role_id,source,source_key)) on commit drop`,
		`insert into users values(1)`,
		`insert into permission_role_track_roles values('new_track',10,0),('new_track',11,1)`,
		`insert into user_role_bindings(user_id,role_id,source,source_key) values
			(1,10,'manual',''),(1,12,'level_track','old_track')`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = SyncTrackRole(ctx, tx, 1, "new_track", 2); err != nil {
		t.Fatal(err)
	}
	assertRoleSources(t, ctx, tx, 1, map[string]int{"manual:10": 1, "level_track:11": 1})
	if err = SyncTrackRole(ctx, tx, 1, "", 0); err != nil {
		t.Fatal(err)
	}
	assertRoleSources(t, ctx, tx, 1, map[string]int{"manual:10": 1})
}

func assertRoleSources(t *testing.T, ctx context.Context, tx pgx.Tx, userID int64, expected map[string]int) {
	t.Helper()
	rows, err := tx.Query(ctx, `select source,role_id from user_role_bindings where user_id=$1 order by source,role_id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	actual := map[string]int{}
	for rows.Next() {
		var source string
		var roleID int64
		if err = rows.Scan(&source, &roleID); err != nil {
			t.Fatal(err)
		}
		actual[source+":"+strconv.FormatInt(roleID, 10)]++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(actual) != len(expected) {
		t.Fatalf("role sources = %#v, want %#v", actual, expected)
	}
	for key, count := range expected {
		if actual[key] != count {
			t.Fatalf("role sources = %#v, want %#v", actual, expected)
		}
	}
}
