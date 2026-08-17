package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/systemactor"
)

func TestProjectMaintenanceAutomationTransitionsAndRestoresOwnedStatus(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify project maintenance transitions against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
	projectCode := "a" + suffix
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,official_status,review_status,created_at)
		values($1,$2,'Automation maintenance test','active','approved',$3) returning id`,
		projectCode, "automation-maintenance-"+suffix, now).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `delete from mods where id=$1`, modID)

	var routeID, actorID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from users where username=$1 and email=$2`, systemactor.AutobotUsername, systemactor.AutobotEmail).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	worker := NewProjectAutomationWorker(config.Load(), pool)
	job := projectAutomationJob{RouteID: routeID, InternalID: modID, ProjectType: "mod", ProjectPublicID: projectCode,
		Kind: "changelog", SourceType: "modrinth", ActorID: actorID}
	low, err := worker.applyProjectMaintenancePolicy(ctx, job, now.AddDate(0, -7, 0), now)
	if err != nil || !low.Changed || low.CurrentStatus != automatedMaintenanceLowFrequency {
		t.Fatalf("low-frequency transition = %+v, err=%v", low, err)
	}
	stopped, err := worker.applyProjectMaintenancePolicy(ctx, job, time.Time{}, now.AddDate(0, 6, 1))
	if err != nil || !stopped.Changed || stopped.CurrentStatus != automatedMaintenanceDiscontinued {
		t.Fatalf("discontinued transition = %+v, err=%v", stopped, err)
	}
	restored, err := worker.applyProjectMaintenancePolicy(ctx, job, now.AddDate(0, 6, 0), now.AddDate(0, 6, 1))
	if err != nil || !restored.Changed || !restored.Restored || restored.CurrentStatus != "active" {
		t.Fatalf("active restoration = %+v, err=%v", restored, err)
	}
	var changedBy int64
	if err = pool.QueryRow(ctx, `select changed_by from project_automation_activity where project_route_id=$1`, routeID).Scan(&changedBy); err != nil || changedBy != actorID {
		t.Fatalf("maintenance actor = %d, want %d, err=%v", changedBy, actorID, err)
	}
}

func TestProjectMaintenanceAutomationPreservesManualOverride(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify manual maintenance overrides against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	suffix := fmt.Sprintf("%08x", uint32(time.Now().UnixNano()))
	projectCode := "b" + suffix
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,official_status,review_status,created_at)
		values($1,$2,'Automation override test','active','approved',$3) returning id`,
		projectCode, "automation-override-"+suffix, time.Now().UTC().AddDate(-2, 0, 0)).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	defer pool.Exec(context.Background(), `delete from mods where id=$1`, modID)
	var routeID, actorID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `select id from users where username=$1`, systemactor.AutobotUsername).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	worker := NewProjectAutomationWorker(config.Load(), pool)
	job := projectAutomationJob{RouteID: routeID, InternalID: modID, ProjectType: "mod", ProjectPublicID: projectCode,
		Kind: "minecraft_versions", SourceType: "modrinth", ActorID: actorID}
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	if _, err = worker.applyProjectMaintenancePolicy(ctx, job, now.AddDate(0, -7, 0), now); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update mods set official_status='development' where id=$1`, modID); err != nil {
		t.Fatal(err)
	}
	result, err := worker.applyProjectMaintenancePolicy(ctx, job, time.Time{}, now.AddDate(0, 0, 1))
	if err != nil || result.CurrentStatus != "development" || !result.ManualOverride || result.Changed {
		t.Fatalf("manual override result = %+v, err=%v", result, err)
	}
}
