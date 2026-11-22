package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/security"
	"mcmods-cn-backend/internal/systemactor"
)

func newBaselineTestDatabase(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	testTarget := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" || testTarget == "" {
		t.Skip("requires explicit owned PostgreSQL integration environment")
	}
	if testTarget != strings.TrimSpace(os.Getenv("DATABASE_URL")) {
		t.Fatal("baseline fixture requires matching explicit test and application targets")
	}
	cfg, err := pgxpool.ParseConfig(testTarget)
	if err != nil {
		t.Fatal("parse owned PostgreSQL configuration")
	}
	if !testLoopback(cfg.ConnConfig.Host) {
		t.Fatal("refusing non-loopback PostgreSQL fixture")
	}
	for _, fallback := range cfg.ConnConfig.Fallbacks {
		if !testLoopback(fallback.Host) {
			t.Fatal("refusing non-loopback PostgreSQL fallback")
		}
	}
	owner, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect owned PostgreSQL fixture parent")
	}
	t.Cleanup(owner.Close)
	var connectedDatabase, connectedUser string
	if err = owner.QueryRow(ctx, `select current_database(),current_user`).Scan(&connectedDatabase, &connectedUser); err != nil ||
		connectedDatabase != cfg.ConnConfig.Database || connectedUser != cfg.ConnConfig.User {
		t.Fatal("owned PostgreSQL fixture connection identity mismatch")
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := "mcmods_baseline_test_" + hex.EncodeToString(nonce[:8])
	marker := "mcmods-owned-baseline-test:" + hex.EncodeToString(nonce[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err = owner.Exec(ctx, "create database "+quoted+" template template0"); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, "comment on database "+quoted+" is '"+marker+"'"); err != nil {
		t.Fatal(err)
	}
	var oid uint32
	if err = owner.QueryRow(ctx, `select oid from pg_database where datname=$1 and datdba=(select oid from pg_roles where rolname=current_user)
		and shobj_description(oid,'pg_database')=$2`, name, marker).Scan(&oid); err != nil {
		t.Fatal("new child database lacks its original owner and marker")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var owns bool
		if err := owner.QueryRow(cleanup, `select exists(select 1 from pg_database where oid=$1 and datname=$2
			and datdba=(select oid from pg_roles where rolname=current_user) and shobj_description(oid,'pg_database')=$3)`, oid, name, marker).Scan(&owns); err != nil || !owns {
			t.Error("refusing cleanup of PostgreSQL child with mismatching original identity")
			return
		}
		if _, err := owner.Exec(cleanup, "drop database "+quoted); err != nil {
			t.Error(err)
		}
	})
	cfg = cfg.Copy()
	cfg.ConnConfig.Database = name
	cfg.MinConns, cfg.MaxConns = 0, 2
	cfg.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("connect dedicated PostgreSQL fixture child")
	}
	t.Cleanup(pool.Close)
	return pool
}

func testLoopback(host string) bool {
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

func TestInitializeConcurrentAndRepeatableIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	pool := newBaselineTestDatabase(t, ctx)
	replica, err := pgxpool.NewWithConfig(ctx, pool.Config())
	if err != nil {
		t.Fatal("open second installer pool")
	}
	defer replica.Close()
	t.Setenv("SEED_ADMIN_PASSWORD", "fixture-password-only")
	t.Setenv("SEED_ADMIN_EMAIL", "bootstrap@example.test")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, installer := range []*pgxpool.Pool{pool, replica} {
		go func() {
			<-start
			results <- Initialize(ctx, installer)
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var accounts, roles, version int
	var adminCreated, bannedDeny bool
	if err := pool.QueryRow(ctx, `select (select count(*) from users),(select count(*) from roles), generation, admin_bootstrapped,
		exists(select 1 from roles r join role_permissions b on b.role_id=r.id join permissions p on p.id=b.permission_id
		where r.code='banned' and p.code='*' and not b.allow) from schema_metadata where singleton`).Scan(&accounts, &roles, &version, &adminCreated, &bannedDeny); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || roles != len(seedRoles) || version != schemaGeneration || adminCreated || !bannedDeny {
		t.Fatalf("initial identities or policy did not install once: accounts=%d roles=%d generation=%d admin=%v denial=%v", accounts, roles, version, adminCreated, bannedDeny)
	}
	start = make(chan struct{})
	for _, installer := range []*pgxpool.Pool{pool, replica} {
		go func() {
			<-start
			results <- BootstrapAdmin(ctx, installer)
		}()
	}
	close(start)
	var adminSuccesses int
	for range 2 {
		err := <-results
		if err == nil {
			adminSuccesses++
		} else if !strings.Contains(err.Error(), "already been bootstrapped") {
			t.Fatal(err)
		}
	}
	if adminSuccesses != 1 {
		t.Fatalf("%d simultaneous administrator requests succeeded; want exactly one", adminSuccesses)
	}
	var status, hash string
	if err := pool.QueryRow(ctx, `select status,password_hash from users where username=$1 and email=$2`, systemactor.AutobotUsername, systemactor.AutobotEmail).Scan(&status, &hash); err != nil ||
		status != systemactor.AutobotStatus || hash != "password-login-disabled" || security.VerifyPassword("fixture-password-only", hash) {
		t.Fatal("autobot is not an unambiguous non-login identity")
	}
	if _, err := pool.Exec(ctx, `delete from user_permissions using users,permissions
		where user_permissions.user_id=users.id and user_permissions.permission_id=permissions.id
		and users.username='admin' and permissions.code='admin.access';
		delete from role_permissions using roles,permissions
		where role_permissions.role_id=roles.id and role_permissions.permission_id=permissions.id
		and roles.code='registered' and permissions.code='content.translate';
		update users set status='disabled' where username='admin';
		update site_page_translations set body_markdown=' operator text ' where locale='en-US';
		update license_policies set redistribution_allowed=false where spdx_id='MIT';
		delete from license_policies where spdx_id='Apache-2.0'`); err != nil {
		t.Fatal(err)
	}
	var rbacVersionBefore int64
	if err := pool.QueryRow(ctx, `select version from runtime_versions where name='rbac'`).Scan(&rbacVersionBefore); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEED_ADMIN_PASSWORD", "fixture-changed-password-only")
	if err := Initialize(ctx, replica); err != nil {
		t.Fatal(err)
	}
	var restoredUserGrant, restoredRoleGrant, changedPolicy, restoredPolicy bool
	var body string
	var rbacVersionAfter int64
	if err := pool.QueryRow(ctx, `select
		exists(select 1 from users u join user_permissions b on b.user_id=u.id join permissions p on p.id=b.permission_id where u.username='admin' and p.code='admin.access'),
		exists(select 1 from roles r join role_permissions b on b.role_id=r.id join permissions p on p.id=b.permission_id where r.code='registered' and p.code='content.translate'),
		(select status from users where username='admin'),
		(select body_markdown from site_page_translations where locale='en-US'),
		(select redistribution_allowed from license_policies where spdx_id='MIT'),
		exists(select 1 from license_policies where spdx_id='Apache-2.0'),
		(select version from runtime_versions where name='rbac')`).Scan(&restoredUserGrant, &restoredRoleGrant, &status, &body, &changedPolicy, &restoredPolicy, &rbacVersionAfter); err != nil {
		t.Fatal(err)
	}
	if restoredUserGrant || restoredRoleGrant || status != "disabled" || body != " operator text " || changedPolicy || restoredPolicy || rbacVersionBefore != rbacVersionAfter {
		t.Fatal("restart modified operator-managed account, policy, content or runtime version state")
	}
	if _, err := pool.Exec(ctx, `delete from users where username='admin'`); err != nil {
		t.Fatal(err)
	}
	if err := BootstrapAdmin(ctx, replica); err == nil || !strings.Contains(err.Error(), "already been bootstrapped") {
		t.Fatal("operator deletion reopened the initial administrator action")
	}
}

func TestInitializeRollsBackAndSupportsExplicitAdminIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	pool := newBaselineTestDatabase(t, ctx)
	t.Setenv("SEED_ADMIN_PASSWORD", "short")
	if err := BootstrapAdmin(ctx, pool); err == nil || !strings.Contains(err.Error(), "SEED_ADMIN_PASSWORD") {
		t.Fatal("invalid setup password did not fail installation")
	}
	var relations int
	if err := pool.QueryRow(ctx, `select count(*) from pg_class where relnamespace='public'::regnamespace`).Scan(&relations); err != nil || relations != 0 {
		t.Fatalf("failed initial seed left %d catalog relations: %v", relations, err)
	}
	t.Setenv("SEED_ADMIN_PASSWORD", "")
	t.Setenv("SEED_ADMIN_EMAIL", "")
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var languages int
	if err := pool.QueryRow(ctx, `select count(*) from site_page_translations where body_markdown like '%'||chr(10)||chr(10)||'%'`).Scan(&languages); err != nil || languages != 8 {
		t.Fatalf("initial governance defaults seeded %d localized Markdown bodies: %v", languages, err)
	}
	t.Setenv("SEED_ADMIN_PASSWORD", "explicit-only-fixture password ")
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var accountCount int
	if err := pool.QueryRow(ctx, `select count(*) from users where username='admin'`).Scan(&accountCount); err != nil || accountCount != 0 {
		t.Fatal("ordinary restart unexpectedly created the first human account")
	}
	t.Setenv("SEED_ADMIN_EMAIL", "invalid-configured-email")
	if err := BootstrapAdmin(ctx, pool); err == nil || !strings.Contains(err.Error(), "SEED_ADMIN_EMAIL") {
		t.Fatal("invalid explicit bootstrap email did not fail")
	}
	t.Setenv("SEED_ADMIN_EMAIL", "bootstrap@example.test")
	if err := BootstrapAdmin(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var hash string
	if err := pool.QueryRow(ctx, `select password_hash from users where username='admin' and email='bootstrap@example.test' and email_verified and status='active'`).Scan(&hash); err != nil || !security.VerifyPassword("explicit-only-fixture password ", hash) {
		t.Fatal("explicit first administrator did not receive the configured password and email")
	}
}

func TestInitializeRejectsUnknownAndDriftedSchemasIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	pool := newBaselineTestDatabase(t, ctx)
	t.Setenv("SEED_ADMIN_PASSWORD", "")
	if _, err := pool.Exec(ctx, `create table operator_fact(id integer not null); insert into operator_fact values(7)`); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err == nil || !strings.Contains(err.Error(), "empty public schema") {
		t.Fatal("an unrecognized nonempty schema was accepted")
	}
	var fact int
	if err := pool.QueryRow(ctx, `select id from operator_fact`).Scan(&fact); err != nil || fact != 7 {
		t.Fatal("refusing an unknown schema did not preserve its contents")
	}
	if _, err := pool.Exec(ctx, `drop table operator_fact`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `create schema audit_shadow;
		create table audit_shadow.notifications(id bigint);
		create table audit_shadow.schema_metadata(generation integer);
		insert into audit_shadow.schema_metadata values(1)`); err != nil {
		t.Fatal(err)
	}
	shadowConfig := pool.Config()
	shadowConfig.ConnConfig.RuntimeParams["search_path"] = "audit_shadow, public"
	shadowPool, err := pgxpool.NewWithConfig(ctx, shadowConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer shadowPool.Close()
	if err := Initialize(ctx, shadowPool); err != nil {
		t.Fatal(err)
	}
	if err := RequireOSSMultipartActivitySchema(ctx, pool); err != nil {
		t.Fatalf("current multipart activity schema: %v", err)
	}
	var shadowColumns, shadowVersion int
	if err := shadowPool.QueryRow(ctx, `select
		(select count(*) from information_schema.columns where table_schema='audit_shadow' and table_name='notifications'),
		(select generation from audit_shadow.schema_metadata)`).Scan(&shadowColumns, &shadowVersion); err != nil || shadowColumns != 1 || shadowVersion != 1 {
		t.Fatal("initialization modified an unrelated caller schema")
	}
	var definition string
	if err := pool.QueryRow(ctx, `select definition_fingerprint from schema_metadata where singleton`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update schema_metadata set definition_fingerprint=repeat('a',64) where singleton`); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err == nil || !strings.Contains(err.Error(), "definition differs") {
		t.Fatal("the recorded generation did not reject a different compiled baseline")
	}
	if _, err := pool.Exec(ctx, `update schema_metadata set definition_fingerprint=$1 where singleton`, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `update schema_metadata set generation=$1 where singleton`, schemaGeneration-1); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err == nil || !strings.Contains(err.Error(), "incompatible with generation") {
		t.Fatal("an obsolete database generation was accepted")
	}
	if _, err := pool.Exec(ctx, `update schema_metadata set generation=$1 where singleton`, schemaGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `alter table notifications disable trigger trg_notifications_revision`); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err == nil || !strings.Contains(err.Error(), "catalog differs") {
		t.Fatal("initialization did not detect a disabled notification revision trigger")
	}
	if _, err := pool.Exec(ctx, `alter table notifications enable trigger trg_notifications_revision`); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `drop index `+CatalogTagPrefixIndexName); err != nil {
		t.Fatal(err)
	}
	if err := Initialize(ctx, pool); err == nil || !strings.Contains(err.Error(), "catalog differs") {
		t.Fatal("the recorded generation did not reject actual catalog drift")
	}
	var backedUp bool
	if err := ApplyCatalogTagPrefixIndex(ctx, pool, pool.Config().ConnConfig.Database, false, func(prior CatalogTagPrefixIndexBackup) error {
		backedUp = !prior.PriorState.Exists && prior.Generation == schemaGeneration
		return nil
	}); err != nil || !backedUp {
		t.Fatalf("index repair did not restore the missing index from the current baseline: %v", err)
	}
	if err := Initialize(ctx, pool); err != nil {
		t.Fatalf("repaired index did not reinstate the installed schema: %v", err)
	}
}
