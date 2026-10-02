package database

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/systemactor"
)

func TestSEC021DefaultUsersAreOnlyBootstrappedIntoAnEmptyDatabase(t *testing.T) {
	pool := newSEC021SeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := seedDefaultUsers(ctx, pool); err != nil {
		t.Fatalf("initial default-user bootstrap: %v", err)
	}
	var userCount int
	if err := pool.QueryRow(ctx, `select count(*) from users`).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != len(seedUsers) {
		t.Fatalf("initial bootstrap created %d users, want %d", userCount, len(seedUsers))
	}

	t.Setenv("SEED_ADMIN_PASSWORD", "restart-must-not-reset-this-password")
	if _, err := pool.Exec(ctx, `update users set email='operator-admin@example.test',password_hash='operator-rotated',
		email_verified=false,status='disabled' where username='admin'`); err != nil {
		t.Fatalf("apply operator-managed admin state: %v", err)
	}
	if _, err := pool.Exec(ctx, `update users set status='disabled' where username=$1`, systemactor.AutobotUsername); err != nil {
		t.Fatalf("disable automation identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `update users set status='deleted' where username='guest'`); err != nil {
		t.Fatalf("delete guest identity: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from user_permissions binding using users account,permissions permission
		where binding.user_id=account.id and binding.permission_id=permission.id
			and account.username='admin' and permission.code='admin.access'`); err != nil {
		t.Fatalf("apply operator-managed security state: %v", err)
	}
	if err := seedDefaultUsers(ctx, pool); err != nil {
		t.Fatalf("repeat default-user bootstrap: %v", err)
	}

	var email, passwordHash, status string
	var emailVerified bool
	if err := pool.QueryRow(ctx, `select email,password_hash,email_verified,status from users where username='admin'`).
		Scan(&email, &passwordHash, &emailVerified, &status); err != nil {
		t.Fatal(err)
	}
	if email != "operator-admin@example.test" || passwordHash != "operator-rotated" || emailVerified || status != "disabled" {
		t.Fatalf("restart changed operator-managed admin state: email=%q hash=%q verified=%v status=%q", email, passwordHash, emailVerified, status)
	}
	var autobotStatus, guestStatus string
	if err := pool.QueryRow(ctx, `select status from users where username=$1`, systemactor.AutobotUsername).Scan(&autobotStatus); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select status from users where username='guest'`).Scan(&guestStatus); err != nil {
		t.Fatal(err)
	}
	if autobotStatus != "disabled" || guestStatus != "deleted" {
		t.Fatalf("restart reactivated a disabled seed identity: autobot=%q guest=%q", autobotStatus, guestStatus)
	}
	var restoredPermission bool
	if err := pool.QueryRow(ctx, `select exists(
		select 1 from user_permissions binding
		join users account on account.id=binding.user_id
		join permissions permission on permission.id=binding.permission_id
		where account.username='admin' and permission.code='admin.access'
			and binding.allow and binding.source='system_seed' and binding.source_key='default_users'
	)`).Scan(&restoredPermission); err != nil {
		t.Fatal(err)
	}
	if restoredPermission {
		t.Fatal("restart restored an operator-revoked seeded permission")
	}

	if _, err := pool.Exec(ctx, `delete from users where username in ('admin',$1)`, systemactor.AutobotUsername); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultUsers(ctx, pool); err != nil {
		t.Fatalf("repeat bootstrap after explicit deletion: %v", err)
	}
	var recreated int
	if err := pool.QueryRow(ctx, `select count(*) from users where username in ('admin',$1)`, systemactor.AutobotUsername).Scan(&recreated); err != nil {
		t.Fatal(err)
	}
	if recreated != 0 {
		t.Fatalf("restart recreated %d explicitly deleted seed identities", recreated)
	}
	if _, err := pool.Exec(ctx, `delete from users`); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultUsers(ctx, pool); err != nil {
		t.Fatalf("repeat bootstrap after every account was explicitly deleted: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from users`).Scan(&recreated); err != nil {
		t.Fatal(err)
	}
	if recreated != 0 {
		t.Fatalf("restart treated an intentionally emptied user table as a pristine installation and recreated %d identities", recreated)
	}
}

func TestSEC021LegacyBootstrapEvidencePreventsRecreatingAnEmptiedUserTable(t *testing.T) {
	pool := newSEC021SeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `insert into system_settings(key,value) values('permission.default_roles','{}'::jsonb)`); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultUsers(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var users, markers int
	if err := pool.QueryRow(ctx, `select count(*) from users`).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from system_settings where key='security.default_users_bootstrapped.v1'`).Scan(&markers); err != nil {
		t.Fatal(err)
	}
	if users != 0 || markers != 1 {
		t.Fatalf("legacy empty-user state was not preserved: users=%d markers=%d", users, markers)
	}
}

func TestSEC021ConcurrentEmptyBootstrapIsAtomic(t *testing.T) {
	pool := newSEC021SeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := make(chan struct{})
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- seedDefaultUsers(ctx, pool)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent bootstrap: %v", err)
		}
	}
	var userCount int
	if err := pool.QueryRow(ctx, `select count(*) from users`).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != len(seedUsers) {
		t.Fatalf("concurrent bootstrap created %d users, want %d", userCount, len(seedUsers))
	}
}

func TestSEC021LegacyAutobotTransitionIsOneTimeAndFailClosed(t *testing.T) {
	t.Run("exact legacy identity transitions and sessions are revoked", func(t *testing.T) {
		pool := newSEC021SeedPool(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var userID int64
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
			values($1,$2,'password-login-disabled',true,'active') returning id`,
			systemactor.AutobotUsername, systemactor.AutobotEmail).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
			values(decode(repeat('11',32),'hex'),$1,1,now()+interval '1 day')`, userID); err != nil {
			t.Fatal(err)
		}
		if err := migrateLegacyAutobotToSystemSubject(ctx, pool); err != nil {
			t.Fatal(err)
		}
		var status string
		var authVersion int64
		var revoked bool
		if err := pool.QueryRow(ctx, `select status,auth_version from users where id=$1`, userID).Scan(&status, &authVersion); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select revoked_at is not null from auth_sessions where user_id=$1`, userID).Scan(&revoked); err != nil {
			t.Fatal(err)
		}
		if status != systemactor.AutobotStatus || authVersion != 2 || !revoked {
			t.Fatalf("legacy transition status=%q auth_version=%d revoked=%v", status, authVersion, revoked)
		}

		if _, err := pool.Exec(ctx, `update users set status='active' where id=$1`, userID); err != nil {
			t.Fatal(err)
		}
		if err := migrateLegacyAutobotToSystemSubject(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select status,auth_version from users where id=$1`, userID).Scan(&status, &authVersion); err != nil {
			t.Fatal(err)
		}
		if status != "active" || authVersion != 2 {
			t.Fatalf("durable migration marker did not preserve later operator state: status=%q auth_version=%d", status, authVersion)
		}
	})

	t.Run("noncanonical security state is never rewritten", func(t *testing.T) {
		pool := newSEC021SeedPool(t)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var userID int64
		if err := pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified,status)
			values($1,$2,'operator-rotated',true,'disabled') returning id`,
			systemactor.AutobotUsername, systemactor.AutobotEmail).Scan(&userID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `insert into auth_sessions(session_hash,user_id,auth_version,expires_at)
			values(decode(repeat('22',32),'hex'),$1,1,now()+interval '1 day')`, userID); err != nil {
			t.Fatal(err)
		}
		if err := migrateLegacyAutobotToSystemSubject(ctx, pool); err != nil {
			t.Fatal(err)
		}
		var status, passwordHash string
		var authVersion int64
		var revoked bool
		if err := pool.QueryRow(ctx, `select status,password_hash,auth_version from users where id=$1`, userID).
			Scan(&status, &passwordHash, &authVersion); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `select revoked_at is not null from auth_sessions where user_id=$1`, userID).Scan(&revoked); err != nil {
			t.Fatal(err)
		}
		if status != "disabled" || passwordHash != "operator-rotated" || authVersion != 1 || revoked {
			t.Fatalf("transition changed noncanonical state: status=%q hash=%q auth_version=%d revoked=%v", status, passwordHash, authVersion, revoked)
		}
	})
}

func newSEC021SeedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an isolated PostgreSQL database to verify default-user bootstrap")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	baseConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	adminPool, err := pgxpool.NewWithConfig(ctx, baseConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)
	schemaName := fmt.Sprintf("sec021_seed_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err = adminPool.Exec(ctx, "create schema "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, dropErr := adminPool.Exec(context.Background(), "drop schema "+quotedSchema+" cascade"); dropErr != nil {
			t.Errorf("drop isolated SEC-021 schema: %v", dropErr)
		}
	})

	scopedConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	scopedConfig.MaxConns = 4
	scopedConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",pg_catalog"
	pool, err := pgxpool.NewWithConfig(ctx, scopedConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if _, err = pool.Exec(ctx, `
		create table users (
			id bigserial primary key,
			username text not null unique,
			email text not null unique,
			password_hash text not null,
			email_verified boolean not null default false,
			status text not null default 'active',
			auth_version bigint not null default 1,
			updated_at timestamptz not null default now()
		);
		create table permissions (
			id bigserial primary key,
			code text not null unique
		);
		create table user_permissions (
			user_id bigint not null references users(id) on delete cascade,
			permission_id bigint not null references permissions(id) on delete cascade,
			allow boolean not null,
			source text not null,
			source_key text not null,
			updated_at timestamptz not null default now(),
			primary key(user_id,permission_id,source,source_key)
		);
		create table auth_sessions (
			id bigserial primary key,
			session_hash bytea not null unique,
			user_id bigint not null references users(id) on delete cascade,
			auth_version bigint not null,
			expires_at timestamptz not null,
			revoked_at timestamptz
		);
		create table system_settings (
			key text primary key,
			value jsonb not null,
			updated_at timestamptz not null default now()
		)`); err != nil {
		t.Fatal(err)
	}
	permissionCodes := make([]string, 0, len(seedPermissions))
	for _, permission := range seedPermissions {
		permissionCodes = append(permissionCodes, permission.Code)
	}
	if _, err = pool.Exec(ctx, `insert into permissions(code) select distinct unnest($1::text[])`, permissionCodes); err != nil {
		t.Fatal(err)
	}
	return pool
}
