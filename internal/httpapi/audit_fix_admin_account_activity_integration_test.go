package httpapi

import (
	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/activity"
	"net/http"
	"testing"
)

func auditFixAdminAccountActivityFacts(t *testing.T, f test013Fixture) map[string]string {
	t.Helper()
	facts := f.facts(t)
	for _, table := range []string{"users", "user_role_bindings", "user_permissions", "permissions", "roles", "role_permissions", "permission_audit_logs"} {
		var raw string
		if err := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(fact) order by to_jsonb(fact)::text),'[]'::jsonb)::text from `+pgx.Identifier{table}.Sanitize()+` fact`).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		facts[table] = raw
	}
	return facts
}
func TestAuditFixCMP003AdminAccountActivitySharesIdentityPermissionAndStatusTransaction(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "user.write", "permission.write")
	if _, err := f.db.Exec(f.ctx, `insert into roles(code,name) values('audit_atomic_registered','Synthetic registered'),('audit_atomic_banned','Synthetic banned')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values($1,'{"registeredRole":"audit_atomic_registered","bannedRole":"audit_atomic_banned"}') on conflict(key) do update set value=excluded.value`, permissionDefaultsSettingKey); err != nil {
		t.Fatal(err)
	}
	recover := func(method, path string, body any, want int, action int16) {
		t.Helper()
		prior := auditFixActivityIntentCount(t, f, f.reviewer, action, activity.ObjectUser)
		auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], true)
		before := auditFixAdminAccountActivityFacts(t, f)
		response := f.require(t, f.reviewer, method, path, body, 500)
		auditFixRequireSpecificActivityError(t, response, "failed to persist account governance activity")
		auditFixRequireProjectFactsUnchanged(t, before, auditFixAdminAccountActivityFacts(t, f))
		auditFixInjectActivityFailure(t, f, f.userIDs[f.reviewer], false)
		f.require(t, f.reviewer, method, path, body, want)
		if count := auditFixActivityIntentCount(t, f, f.reviewer, action, activity.ObjectUser); count != prior+1 {
			t.Fatalf("account governance activity=%d prior=%d", count, prior)
		}
	}
	create := adminCreateUserRequest{Username: "audit_atomic_target", Email: "audit-atomic-target@example.invalid", Password: "audit-synthetic-account-password", Status: "active", Roles: []string{}}
	recover(http.MethodPost, "/api/v1/admin/users", create, 201, activity.ActionCreate)
	var id int64
	var publicID string
	if err := f.db.QueryRow(f.ctx, `select id,public_id from users where username=$1`, create.Username).Scan(&id, &publicID); err != nil {
		t.Fatal(err)
	}
	var defaults int
	if err := f.db.QueryRow(f.ctx, `select count(*) from user_role_bindings b join roles r on r.id=b.role_id where b.user_id=$1 and r.code='audit_atomic_registered'`, id).Scan(&defaults); err != nil || defaults != 1 {
		t.Fatalf("registered role bindings=%d err=%v", defaults, err)
	}
	path := "/api/v1/admin/users/" + publicID
	recover(http.MethodPut, path+"/permissions", updateUserPermissionsRequest{Permissions: []userPermissionEntry{{Code: "audit.atomic_permission", Allow: true}}}, 200, activity.ActionEdit)
	var explicit int
	if err := f.db.QueryRow(f.ctx, `select count(*) from user_permissions grant_row join permissions p on p.id=grant_row.permission_id where grant_row.user_id=$1 and p.code='audit.atomic_permission' and grant_row.allow`, id).Scan(&explicit); err != nil || explicit != 1 {
		t.Fatalf("explicit permission rows=%d err=%v", explicit, err)
	}
	for _, status := range []string{"banned", "active"} {
		recover(http.MethodPut, path+"/status", map[string]string{"status": status}, 200, activity.ActionEdit)
		var actual string
		var banned bool
		if err := f.db.QueryRow(f.ctx, `select status,exists(select 1 from user_role_bindings b join roles r on r.id=b.role_id where b.user_id=u.id and r.code='audit_atomic_banned') from users u where id=$1`, id).Scan(&actual, &banned); err != nil || actual != status || banned != (status == "banned") {
			t.Fatalf("status=%s bannedRole=%v err=%v", actual, banned, err)
		}
	}
}

func TestAdminAccountCreationSeparatesRoleAssignmentIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "admin.access", "user.write")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.access", "user.write", "permission.write")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "admin.access", "permission.write")
	const role = "provisioning_test_member"
	if _, err := f.db.Exec(f.ctx, `insert into roles(code,name) values($1,'Synthetic member')`, role); err != nil {
		t.Fatal(err)
	}
	const path = "/api/v1/admin/users"
	account := adminCreateUserRequest{
		Username: "provisioned_default", Email: "provisioned-default@example.invalid", Password: "synthetic-provisioning-password",
	}
	f.require(t, f.editor, http.MethodPost, path, account, http.StatusCreated)

	rolesFor := func(name string, wantManual int) {
		t.Helper()
		var defaults, manual int
		if err := f.db.QueryRow(f.ctx, `select
			count(*) filter(where binding.source='account_status' and binding.source_key='registered'),
			count(*) filter(where binding.source='manual' and role.code=$2)
			from users account join user_role_bindings binding on binding.user_id=account.id
			join roles role on role.id=binding.role_id where account.username=$1`, name, role).Scan(&defaults, &manual); err != nil {
			t.Fatal(err)
		}
		if defaults != 1 || manual != wantManual {
			t.Errorf("account %s: default roles=%d; explicit roles=%d want=%d", name, defaults, manual, wantManual)
		}
	}
	rolesFor(account.Username, 0)

	account.Username, account.Email = "provisioned_member", "provisioned-member@example.invalid"
	account.Roles = []string{role}
	before := auditFixAdminAccountActivityFacts(t, f)
	f.require(t, f.editor, http.MethodPost, path, account, http.StatusForbidden)
	f.require(t, f.otherEditor, http.MethodPost, path, account, http.StatusForbidden)
	auditFixRequireProjectFactsUnchanged(t, before, auditFixAdminAccountActivityFacts(t, f))

	f.require(t, f.reviewer, http.MethodPost, path, account, http.StatusCreated)
	rolesFor(account.Username, 1)
}
