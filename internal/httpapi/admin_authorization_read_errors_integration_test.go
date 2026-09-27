package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestAdminAuthorizationDatabaseFailuresAreNotEditableEmptyDataIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify admin authorization read failures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	counter := &integrationQueryCounter{}
	poolConfig.ConnConfig.Tracer = counter
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
	if _, err = pool.Exec(ctx, `insert into roles(code,name,weight)
		select 'budget_role_'||lpad(value::text,3,'0'),'Budget role '||value,value
		from generate_series(1,100) value;
		insert into users(username,email,password_hash,email_verified)
		select 'budget_user_'||lpad(value::text,3,'0'),'budget-user-'||value||'@example.invalid','test-only',true
		from generate_series(1,100) value`); err != nil {
		t.Fatal(err)
	}

	server := &Server{db: pool}
	counter.queries.Store(0)
	usersResponse := httptest.NewRecorder()
	server.adminUsers(usersResponse, httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil).WithContext(ctx))
	if usersResponse.Code != http.StatusOK {
		t.Fatalf("100-user admin list status=%d body=%s", usersResponse.Code, usersResponse.Body.String())
	}
	if got := counter.queries.Load(); got != 7 {
		t.Fatalf("100-user admin list used %d queries; want 7", got)
	}
	counter.queries.Store(0)
	rolesResponse := httptest.NewRecorder()
	server.permissionCatalog(rolesResponse, httptest.NewRequest(http.MethodGet, "/api/v1/admin/permissions", nil).WithContext(ctx))
	if rolesResponse.Code != http.StatusOK {
		t.Fatalf("large role catalog status=%d body=%s", rolesResponse.Code, rolesResponse.Body.String())
	}
	if got := counter.queries.Load(); got != 3 {
		t.Fatalf("large role catalog used %d queries; want 3", got)
	}

	var roleID, permissionID int64
	if err = pool.QueryRow(ctx, `insert into roles(code,name,weight) values('broken_read','Broken read',100000) returning id`).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into permissions(code,module,name) values('test.broken_read','test','Broken read') returning id`).Scan(&permissionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into role_permissions(role_id,permission_id,allow,expires_at)
		values($1,$2,true,now())`, roleID, permissionID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table role_permissions alter column expires_at type jsonb using to_jsonb(expires_at)`); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update role_permissions set expires_at='"not-a-time"'::jsonb where role_id=$1`, roleID); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	server.permissionCatalog(response, httptest.NewRequest(http.MethodGet, "/api/v1/admin/permissions", nil).WithContext(ctx))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("permission catalog status=%d body=%s", response.Code, response.Body.String())
	}
	if _, err = server.roleByCode(ctx, "broken_read"); err == nil {
		t.Fatal("role detail converted a nested permission scan error into an empty permission list")
	}

	if _, err = pool.Exec(ctx, `create function pg_temp.reject_role_insert() returns trigger as $$
		begin raise exception 'injected role insert failure'; end
		$$ language plpgsql;
		create trigger trg_reject_role_insert before insert on roles
		for each row execute function pg_temp.reject_role_insert()`); err != nil {
		t.Fatal(err)
	}
	createResponse := httptest.NewRecorder()
	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/roles", strings.NewReader(
		`{"code":"injected_role","name":"Injected role","description":"","weight":1,"parents":[],"permissions":[],"permissionEntries":[]}`,
	)).WithContext(ctx)
	server.createRole(createResponse, createRequest)
	if createResponse.Code != http.StatusInternalServerError {
		t.Fatalf("non-unique role insert failure status=%d body=%s", createResponse.Code, createResponse.Body.String())
	}
}
