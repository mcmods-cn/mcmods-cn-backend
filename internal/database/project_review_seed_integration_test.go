package database

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestSeedProjectReviewPermissionsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to run the project review permission seed integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	projectID := fmt.Sprintf("r%08d", time.Now().UnixNano()%100_000_000)
	roleCode := "project_editor." + projectID
	permissionCode := "project.review." + projectID
	var roleID int64
	if err = pool.QueryRow(ctx, `insert into roles(code,name,description,weight,status)
		values($1,'Review seed integration','',50,'active') returning id`, roleCode).Scan(&roleID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `delete from roles where id=$1`, roleID)
		_, _ = pool.Exec(cleanupCtx, `delete from permissions where code=$1`, permissionCode)
	})

	if err = seedProjectReviewPermissions(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var allowed bool
	if err = pool.QueryRow(ctx, `select role_permission.allow
		from role_permissions role_permission
		join permissions permission on permission.id=role_permission.permission_id
		where role_permission.role_id=$1 and permission.code=$2`, roleID, permissionCode).Scan(&allowed); err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("project review permission was not enabled for the concrete editor role")
	}
}
