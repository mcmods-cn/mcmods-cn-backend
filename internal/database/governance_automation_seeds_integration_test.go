package database

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestGovernanceSeedsUseMarkdownNewlines(t *testing.T) {
	pool := newGovernanceSeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := seedGovernanceAutomationDefaults(ctx, pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `select locale,body_markdown from site_page_translations order by locale`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var locale, body string
		if err := rows.Scan(&locale, &body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body, "\n\n") || strings.Contains(body, `\n`) {
			t.Errorf("%s seeded Markdown has no paragraph break: %q", locale, body)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 8 {
		t.Fatalf("seeded %d languages, want 8", count)
	}
}

func TestGovernanceSeedsPreserveExistingPolicyAndContent(t *testing.T) {
	pool := newGovernanceSeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := seedGovernanceAutomationDefaults(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update license_policies set redistribution_allowed=false,notes='operator restriction' where spdx_id='MIT';
		update ban_reasons set translations='{"en-US":"Operator reason"}',sort_order=700,active=false where code='spam_bot';
		update site_page_translations set body_markdown='Operator content' where locale='en-US';
		delete from license_policies where spdx_id='Apache-2.0'
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := seedGovernanceAutomationDefaults(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	var allowed, active bool
	var notes, reason, body string
	var sortOrder, restoredDefaults int
	if err := pool.QueryRow(ctx, `select redistribution_allowed,notes from license_policies where spdx_id='MIT'`).Scan(&allowed, &notes); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select translations->>'en-US',sort_order,active from ban_reasons where code='spam_bot'`).Scan(&reason, &sortOrder, &active); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select body_markdown from site_page_translations where locale='en-US'`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from license_policies where spdx_id='Apache-2.0' and redistribution_allowed`).Scan(&restoredDefaults); err != nil {
		t.Fatal(err)
	}
	if allowed || notes != "operator restriction" || reason != "Operator reason" || sortOrder != 700 || active || body != "Operator content" {
		t.Fatalf("restart changed existing policy/content: allowed=%v notes=%q reason=%q order=%d active=%v body=%q", allowed, notes, reason, sortOrder, active, body)
	}
	if restoredDefaults != 1 {
		t.Fatalf("missing default policy was not initialized: count=%d", restoredDefaults)
	}
}

func TestGovernanceSeedsRepairOnlyUneditedLegacyDefaultMarkdown(t *testing.T) {
	pool := newGovernanceSeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := seedGovernanceAutomationDefaults(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		update site_page_translations set body_markdown=replace(body_markdown,E'\n',E'\\n');
		update site_page_translations set revision=2 where locale='en-US';
		update site_page_translations set title='Operator title' where locale='fr-FR';
		insert into users(username,email,password_hash,status) values('operator','operator@example.test','password-login-disabled','active');
		update site_page_translations set updated_by=(select id from users where username='operator') where locale='zh-CN';
		update site_page_translations set status='draft' where locale='zh-TW'
	`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := seedGovernanceAutomationDefaults(ctx, pool); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := pool.Query(ctx, `select locale,body_markdown,revision from site_page_translations order by locale`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var locale, body string
		var revision int64
		if err := rows.Scan(&locale, &body, &revision); err != nil {
			t.Fatal(err)
		}
		switch locale {
		case "en-US", "fr-FR", "zh-CN", "zh-TW":
			if !strings.Contains(body, `\n`) {
				t.Errorf("seed overwrote protected %s content", locale)
			}
		default:
			if !strings.Contains(body, "\n\n") || strings.Contains(body, `\n`) || revision != 2 {
				t.Errorf("%s unedited default was not repaired exactly once: revision=%d body=%q", locale, revision, body)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func newGovernanceSeedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newSEC021SeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Install the real table declarations in the independently owned schema
	// created by newSEC021SeedPool; no production/public table is changed.
	for _, statement := range governanceAutomationSchemaStatements() {
		for _, table := range []string{"ban_reasons", "site_pages", "site_page_translations", "seed_crawler_configs", "license_policies"} {
			if strings.HasPrefix(statement, "create table "+table+" (") {
				if _, err := pool.Exec(ctx, statement); err != nil {
					t.Fatalf("install governance seed table %s: %v", table, err)
				}
			}
		}
	}
	if _, err := pool.Exec(ctx, `set standard_conforming_strings=on`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestDefaultRoleSeedsPreserveRevokedGrantsAndBannedDenials(t *testing.T) {
	pool := newRoleSeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := seedDefaultRoles(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		delete from role_permissions using roles,permissions
		where role_permissions.role_id=roles.id and role_permissions.permission_id=permissions.id
			and roles.code='registered' and permissions.code='content.translate';
		update roles set name='Operator role',status='disabled' where code='registered';
		delete from role_permissions using roles,permissions
		where role_permissions.role_id=roles.id and role_permissions.permission_id=permissions.id
			and roles.code='banned' and permissions.code='*'
	`); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultRoles(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var restoredGrant, bannedDeny bool
	var name, status string
	if err := pool.QueryRow(ctx, `select exists(select 1 from role_permissions binding
		join roles role on role.id=binding.role_id join permissions permission on permission.id=binding.permission_id
		where role.code='registered' and permission.code='content.translate')`).Scan(&restoredGrant); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select exists(select 1 from role_permissions binding
		join roles role on role.id=binding.role_id join permissions permission on permission.id=binding.permission_id
		where role.code='banned' and permission.code='*' and not binding.allow)`).Scan(&bannedDeny); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select name,status from roles where code='registered'`).Scan(&name, &status); err != nil {
		t.Fatal(err)
	}
	if restoredGrant || !bannedDeny || name != "Operator role" || status != "disabled" {
		t.Fatalf("restart authority mismatch: restoredGrant=%v bannedDeny=%v name=%q status=%q", restoredGrant, bannedDeny, name, status)
	}
}

func TestDefaultRoleSeedsAreAtomicOnFailure(t *testing.T) {
	pool := newRoleSeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := pool.Exec(ctx, `alter table role_permissions add constraint reject_seed_grant check(not allow)`); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultRoles(ctx, pool); err == nil {
		t.Fatal("injected grant failure did not fail bootstrap")
	}
	var roles int
	if err := pool.QueryRow(ctx, `select count(*) from roles`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if roles != 0 {
		t.Fatalf("failed bootstrap left %d partially initialized roles", roles)
	}
	if _, err := pool.Exec(ctx, `alter table role_permissions drop constraint reject_seed_grant`); err != nil {
		t.Fatal(err)
	}
	if err := seedDefaultRoles(ctx, pool); err != nil {
		t.Fatalf("retry after failed bootstrap: %v", err)
	}
}

func TestDefaultRoleSeedsConcurrentInitialization(t *testing.T) {
	pool := newRoleSeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var workers sync.WaitGroup
	errors := make(chan error, 2)
	start := make(chan struct{})
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			errors <- seedDefaultRoles(ctx, pool)
		}()
	}
	close(start)
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	var roles, registeredGrants int
	if err := pool.QueryRow(ctx, `select count(*) from roles`).Scan(&roles); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from role_permissions binding join roles role on role.id=binding.role_id
		where role.code='registered' and binding.allow`).Scan(&registeredGrants); err != nil {
		t.Fatal(err)
	}
	if roles != len(seedRoles) || registeredGrants != len(seedRoles[0].Permissions) {
		t.Fatalf("concurrent bootstrap roles=%d grants=%d", roles, registeredGrants)
	}
}

func newRoleSeedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newSEC021SeedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, statement := range baselineSchemaStatements() {
		for _, table := range []string{"roles", "role_permissions"} {
			if strings.HasPrefix(statement, "create table if not exists "+table+" (") {
				if _, err := pool.Exec(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return pool
}
