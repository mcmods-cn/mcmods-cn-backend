package httpapi

import (
	"context"
	"testing"
	"time"
)

func TestReplaceManualAuthorizationPreservesSystemSourcesAndExpiry(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, statement := range []string{
		`create temp table users(id bigint primary key) on commit drop`,
		`create temp table roles(id bigint primary key,code text not null unique,status text not null default 'active') on commit drop`,
		`create temp table permissions(id bigint primary key,code text not null unique) on commit drop`,
		`create temp table user_role_bindings(
			user_id bigint not null,role_id bigint not null,source text not null default 'manual',source_key text not null default '',
			expires_at timestamptz,created_at timestamptz not null default now(),primary key(user_id,role_id,source,source_key)) on commit drop`,
		`create temp table user_permissions(
			user_id bigint not null,permission_id bigint not null,allow boolean not null,source text not null default 'manual',
			source_key text not null default '',expires_at timestamptz,created_at timestamptz not null default now(),
			updated_at timestamptz not null default now(),primary key(user_id,permission_id,source,source_key)) on commit drop`,
		`insert into users values(1)`,
		`insert into roles(id,code) values(10,'manual_old'),(11,'manual_new'),(12,'level_role')`,
		`insert into permissions(id,code) values(20,'manual.old'),(21,'manual.new'),(22,'user.message.receive')`,
		`insert into user_role_bindings(user_id,role_id,source,source_key,expires_at) values
			(1,10,'manual','',null),(1,12,'level_track','old_track',null)`,
		`insert into user_permissions(user_id,permission_id,allow,source,source_key) values
			(1,20,true,'manual',''),(1,22,false,'user_preference','profile_settings')`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	expiresAt := time.Date(2027, 1, 2, 3, 4, 5, 0, time.UTC)
	err = replaceManualUserAuthorizationTx(ctx, tx, 1,
		[]manualRoleGrant{{Code: "manual_new", ExpiresAt: &expiresAt}},
		[]manualPermissionGrant{{Code: "manual.new", Allow: false, ExpiresAt: &expiresAt}})
	if err != nil {
		t.Fatal(err)
	}

	var systemRole, preference, oldManual int
	if err = tx.QueryRow(ctx, `select
		count(*) filter(where source='level_track' and source_key='old_track' and role_id=12),
		count(*) filter(where source='manual' and role_id=10),
		(select count(*) from user_permissions where source='user_preference' and source_key='profile_settings'
			and permission_id=22 and not allow)
		from user_role_bindings where user_id=1`).Scan(&systemRole, &oldManual, &preference); err != nil {
		t.Fatal(err)
	}
	if systemRole != 1 || preference != 1 || oldManual != 0 {
		t.Fatalf("source isolation failed: systemRole=%d preference=%d oldManual=%d", systemRole, preference, oldManual)
	}
	var roleExpiry, permissionExpiry time.Time
	var allow bool
	if err = tx.QueryRow(ctx, `select expires_at from user_role_bindings
		where user_id=1 and role_id=11 and source='manual' and source_key=''`).Scan(&roleExpiry); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select allow,expires_at from user_permissions
		where user_id=1 and permission_id=21 and source='manual' and source_key=''`).Scan(&allow, &permissionExpiry); err != nil {
		t.Fatal(err)
	}
	if !roleExpiry.Equal(expiresAt) || allow || !permissionExpiry.Equal(expiresAt) {
		t.Fatalf("manual grant semantics changed: roleExpiry=%s allow=%t permissionExpiry=%s", roleExpiry, allow, permissionExpiry)
	}

	if _, err = tx.Exec(ctx, `insert into user_role_bindings(user_id,role_id,source,source_key)
		values(1,10,'account_status','banned')`); err != nil {
		t.Fatal(err)
	}
	if err = replaceAccountStatusRoleTx(ctx, tx, 1, "banned", "manual_new"); err != nil {
		t.Fatal(err)
	}
	var oldDefault, newDefault, retainedManual int
	if err = tx.QueryRow(ctx, `select
		count(*) filter(where source='account_status' and role_id=10),
		count(*) filter(where source='account_status' and role_id=11),
		count(*) filter(where source='manual' and role_id=11)
		from user_role_bindings where user_id=1`).Scan(&oldDefault, &newDefault, &retainedManual); err != nil {
		t.Fatal(err)
	}
	if oldDefault != 0 || newDefault != 1 || retainedManual != 1 {
		t.Fatalf("default-role reconciliation crossed sources: old=%d new=%d manual=%d", oldDefault, newDefault, retainedManual)
	}
	if err = replaceAccountStatusRoleTx(ctx, tx, 1, "banned", ""); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, `select count(*) from user_role_bindings
		where user_id=1 and source='account_status' and source_key='banned'`).Scan(&newDefault); err != nil {
		t.Fatal(err)
	}
	if newDefault != 0 {
		t.Fatalf("account-status role was not removed by its stable source: %d", newDefault)
	}
}

func TestRoleDeletionBlockersCoverGlobalAuthorizationDependencies(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`create temp table roles(id bigint primary key,code text not null unique,parents text[] not null default '{}') on commit drop`,
		`create temp table user_role_bindings(user_id bigint not null,role_id bigint not null) on commit drop`,
		`create temp table permission_role_track_roles(track_code text not null,role_id bigint not null) on commit drop`,
		`create temp table system_settings(key text primary key,value jsonb not null) on commit drop`,
		`insert into roles values(1,'target','{}'),(2,'child','{target}'),(3,'free','{}')`,
		`insert into user_role_bindings values(10,1)`,
		`insert into permission_role_track_roles values('staff',1)`,
		`insert into system_settings values('permission.default_roles','{"registeredRole":"target","bannedRole":"banned"}')`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	blockers, err := roleDeletionBlockersTx(ctx, tx, 1, "target")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"default role configuration", "parent role child", "role track staff", "user bindings"} {
		if !hasExactString(blockers, expected) {
			t.Fatalf("role deletion blockers %#v are missing %q", blockers, expected)
		}
	}
	blockers, err = roleDeletionBlockersTx(ctx, tx, 3, "free")
	if err != nil || len(blockers) != 0 {
		t.Fatalf("unreferenced role blockers = %#v, %v", blockers, err)
	}
}

func hasExactString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
