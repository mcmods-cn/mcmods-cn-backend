package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestBUG039AutomatedMaintenancePublishesAuditedRevisionAtomicallyIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify automated maintenance publication")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
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

	stamp := time.Now().UnixNano()
	var actorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'bug039',true) returning id`, fmt.Sprintf("bug039-auto-%d", stamp),
		fmt.Sprintf("bug039-auto-%d@example.invalid", stamp)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 23, 12, 0, 0, 0, time.UTC)
	projectCode := "b039m0001"
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,official_status,review_status,created_at)
		values($1,'bug039-project','BUG039 project','active','approved',$2) returning id`, projectCode, now).
		Scan(&modID); err != nil {
		t.Fatal(err)
	}
	var routeID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, modID).Scan(&routeID); err != nil {
		t.Fatal(err)
	}
	baseRevisionID := seedPublishedMaintenanceModRevision(t, ctx, pool, modID, projectCode, actorID, "active")
	worker := NewProjectAutomationWorker(config.Load(), pool)
	job := projectAutomationJob{RouteID: routeID, InternalID: modID, ProjectType: "mod", ProjectPublicID: projectCode,
		Kind: "changelog", SourceType: "modrinth", ActorID: actorID}

	var auditBefore int
	if err = pool.QueryRow(ctx, `select count(*)::int from audit_events where aggregate_type='mod' and aggregate_key=$1`, projectCode).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	result, err := worker.applyProjectMaintenancePolicy(ctx, job, now.AddDate(0, -7, 0), now)
	if err != nil || !result.Changed || result.CurrentStatus != automatedMaintenanceLowFrequency {
		t.Fatalf("low-frequency transition=%+v error=%v", result, err)
	}

	var status, source, snapshotStatus, requestStatus string
	var publishedRevisionID, changeRequestID int64
	var revisionBaseID *int64
	if err = pool.QueryRow(ctx, `select mod.official_status,mod.published_revision_id,revision.base_revision_id,
		revision.source,revision.snapshot->>'officialStatus',request.id,request.status
		from mods mod join content_revisions revision on revision.id=mod.published_revision_id
		join change_requests request on request.proposed_revision_id=revision.id where mod.id=$1`, modID).
		Scan(&status, &publishedRevisionID, &revisionBaseID, &source, &snapshotStatus, &changeRequestID, &requestStatus); err != nil {
		t.Fatal(err)
	}
	if status != automatedMaintenanceLowFrequency || publishedRevisionID == baseRevisionID || revisionBaseID == nil || *revisionBaseID != baseRevisionID ||
		source != "auto_update" || snapshotStatus != automatedMaintenanceLowFrequency || requestStatus != "approved" {
		t.Errorf("published maintenance revision status=%s id=%d base=%v source=%s snapshot=%s request=%s, base revision=%d",
			status, publishedRevisionID, revisionBaseID, source, snapshotStatus, requestStatus, baseRevisionID)
	}
	var submittedEvents, approvedEvents, publishedEvents, auditAfter int
	if err = pool.QueryRow(ctx, `select
		count(*) filter(where event_type='submitted')::int,
		count(*) filter(where event_type='approved')::int,
		count(*) filter(where event_type='published')::int
		from review_events where change_request_id=$1`, changeRequestID).
		Scan(&submittedEvents, &approvedEvents, &publishedEvents); err != nil {
		t.Fatal(err)
	}
	if submittedEvents != 1 || approvedEvents != 1 || publishedEvents != 1 {
		t.Errorf("review event counts submitted/approved/published=%d/%d/%d, want 1/1/1", submittedEvents, approvedEvents, publishedEvents)
	}
	if err = pool.QueryRow(ctx, `select count(*)::int from audit_events where aggregate_type='mod' and aggregate_key=$1`, projectCode).Scan(&auditAfter); err != nil {
		t.Fatal(err)
	}
	if auditAfter-auditBefore != 2 {
		t.Errorf("maintenance audit event delta=%d, want submitted+approved", auditAfter-auditBefore)
	}
	var eventRevision, eventActor int64
	var changedSections []string
	var updateTasks, outboxEvents int
	if err = pool.QueryRow(ctx, `select event.revision_id,event.actor_user_id,event.changed_sections,
		(select count(*)::int from project_update_notification_tasks task where task.event_id=event.id),
		(select count(*)::int from nats_outbox item where item.aggregate_type='project_update_event' and item.aggregate_id=event.id::text)
		from project_update_events event where event.project_route_id=$1 order by event.id desc limit 1`, routeID).
		Scan(&eventRevision, &eventActor, &changedSections, &updateTasks, &outboxEvents); err != nil {
		t.Errorf("read maintenance project update event: %v", err)
	} else if eventRevision != publishedRevisionID || eventActor != actorID || len(changedSections) != 1 || changedSections[0] != "officialStatus" ||
		updateTasks != 1 || outboxEvents != 1 {
		t.Errorf("project update revision=%d actor=%d sections=%v task/outbox=%d/%d", eventRevision, eventActor, changedSections, updateTasks, outboxEvents)
	}
	var simpleProjectID int64
	var simpleProjectPublicID string
	if err = pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,official_status,review_status,created_at,
		minecraft_versions,loaders,categories,features,search_keywords)
		values('plugin','bug039-plugin','BUG039 plugin','active','approved',$1,'{}','{}','{}','{}','{}') returning id,public_id`, now).
		Scan(&simpleProjectID, &simpleProjectPublicID); err != nil {
		t.Fatal(err)
	}
	var simpleRouteID int64
	if err = pool.QueryRow(ctx, `select id from public_routes where entity_type='plugin' and internal_id=$1`, simpleProjectID).Scan(&simpleRouteID); err != nil {
		t.Fatal(err)
	}
	simpleBaseRevisionID := seedPublishedMaintenanceSimpleProjectRevision(t, ctx, pool, simpleProjectID, simpleProjectPublicID, actorID, "active")
	simpleJob := projectAutomationJob{RouteID: simpleRouteID, InternalID: simpleProjectID, ProjectType: "plugin",
		ProjectPublicID: simpleProjectPublicID, Kind: "site_downloads", SourceType: "curseforge", ActorID: actorID}
	simpleResult, simpleErr := worker.applyProjectMaintenancePolicy(ctx, simpleJob, now.AddDate(0, -7, 0), now)
	if simpleErr != nil || !simpleResult.Changed || simpleResult.CurrentStatus != automatedMaintenanceLowFrequency {
		t.Fatalf("simple-project low-frequency transition=%+v error=%v", simpleResult, simpleErr)
	}
	var simpleRevisionID, simpleRevisionBase, simpleEventRevision int64
	var simpleSource, simpleSnapshotStatus string
	var simpleSections []string
	if err = pool.QueryRow(ctx, `select project.published_revision_id,revision.base_revision_id,revision.source,
		revision.snapshot->>'officialStatus',event.revision_id,event.changed_sections
		from simple_projects project join content_revisions revision on revision.id=project.published_revision_id
		join project_update_events event on event.project_route_id=$2 and event.revision_id=revision.id
		where project.id=$1`, simpleProjectID, simpleRouteID).
		Scan(&simpleRevisionID, &simpleRevisionBase, &simpleSource, &simpleSnapshotStatus, &simpleEventRevision, &simpleSections); err != nil {
		t.Fatal(err)
	}
	if simpleRevisionID == simpleBaseRevisionID || simpleRevisionBase != simpleBaseRevisionID || simpleSource != "auto_update" ||
		simpleSnapshotStatus != automatedMaintenanceLowFrequency || simpleEventRevision != simpleRevisionID ||
		len(simpleSections) != 1 || simpleSections[0] != "officialStatus" {
		t.Errorf("simple maintenance revision=%d base=%d source=%s snapshot=%s event=%d sections=%v, fixture base=%d",
			simpleRevisionID, simpleRevisionBase, simpleSource, simpleSnapshotStatus, simpleEventRevision, simpleSections, simpleBaseRevisionID)
	}

	if _, err = pool.Exec(ctx, `update project_update_notification_tasks set status='completed',updated_at=now()
		where event_id in (select id from project_update_events where project_route_id=$1)`, routeID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table project_update_events add constraint bug039_reject_project_update
		check(false) not valid`); err != nil {
		t.Fatal(err)
	}
	var revisionsBeforeFailure, projectEventsBeforeFailure int
	if err = pool.QueryRow(ctx, `select
		(select count(*)::int from content_revisions where aggregate_type='mod' and aggregate_key=$1),
		(select count(*)::int from project_update_events where project_route_id=$2)`, projectCode, routeID).
		Scan(&revisionsBeforeFailure, &projectEventsBeforeFailure); err != nil {
		t.Fatal(err)
	}
	failedResult, failureErr := worker.applyProjectMaintenancePolicy(ctx, job, time.Time{}, now.AddDate(0, 6, 1))
	if failureErr == nil {
		t.Errorf("event failure transition=%+v error=nil, want transaction failure", failedResult)
	}
	var statusAfterFailure, automatedStatus string
	var publishedAfterFailure int64
	var revisionsAfterFailure, projectEventsAfterFailure int
	if err = pool.QueryRow(ctx, `select mod.official_status,mod.published_revision_id,activity.automated_status,
		(select count(*)::int from content_revisions where aggregate_type='mod' and aggregate_key=$2),
		(select count(*)::int from project_update_events where project_route_id=$3)
		from mods mod join project_automation_activity activity on activity.project_route_id=$3 where mod.id=$1`,
		modID, projectCode, routeID).Scan(&statusAfterFailure, &publishedAfterFailure, &automatedStatus,
		&revisionsAfterFailure, &projectEventsAfterFailure); err != nil {
		t.Fatal(err)
	}
	if statusAfterFailure != automatedMaintenanceLowFrequency || publishedAfterFailure != publishedRevisionID ||
		automatedStatus != automatedMaintenanceLowFrequency || revisionsAfterFailure != revisionsBeforeFailure ||
		projectEventsAfterFailure != projectEventsBeforeFailure {
		t.Errorf("failed publication leaked status=%s revision=%d automated=%s revisions=%d/%d events=%d/%d",
			statusAfterFailure, publishedAfterFailure, automatedStatus, revisionsAfterFailure, revisionsBeforeFailure,
			projectEventsAfterFailure, projectEventsBeforeFailure)
	}
}

func seedPublishedMaintenanceModRevision(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	modID int64, projectCode string, actorID int64, status string) int64 {
	t.Helper()
	var siteID, primaryName string
	if err := pool.QueryRow(ctx, `select slug,primary_name from mods where id=$1`, modID).Scan(&siteID, &primaryName); err != nil {
		t.Fatal(err)
	}
	snapshot := createModRequest{
		SiteID: siteID, PrimaryName: primaryName, DefaultLocale: "zh-CN",
		Localizations: []catalogLocalizationEdit{{Locale: "zh-CN", Name: primaryName}},
		Environment:   "bothRequired", PrimaryCategory: "utility", OfficialStatus: status,
		SourceStatus: "unknown", License: "Custom", SubmissionMethod: "manual", SearchKeywords: []string{},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: "mod", EntityID: modID, AggregateType: "mod", AggregateKey: projectCode,
		Snapshot: raw, Reason: "BUG039 published fixture", ActorID: actorID, Source: "user", Status: "approved",
		Metadata: map[string]any{"fixture": "BUG039"},
	})
	if err == nil {
		err = applyModSnapshot(ctx, tx, modID, created.RevisionID, actorID, true, true, snapshot)
	}
	if err == nil {
		err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", actorID, "BUG039 published fixture", nil)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	return created.RevisionID
}

func seedPublishedMaintenanceSimpleProjectRevision(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	projectID int64, publicID string, actorID int64, status string) int64 {
	t.Helper()
	var projectType, siteID, primaryName string
	if err := pool.QueryRow(ctx, `select project_type,slug,primary_name from simple_projects where id=$1`, projectID).
		Scan(&projectType, &siteID, &primaryName); err != nil {
		t.Fatal(err)
	}
	snapshot := simpleProjectSnapshot{
		ProjectType: projectType, SiteID: siteID, DefaultLocale: "zh-CN",
		Localizations:     []simpleProjectLocalization{{Locale: "zh-CN", Name: primaryName}},
		MinecraftVersions: []string{}, Loaders: []string{}, Categories: []string{}, Features: []string{}, SearchKeywords: []string{},
		OfficialStatus: status, SourceStatus: "unknown", License: "Custom", SubmissionMethod: "manual",
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: projectType, EntityID: projectID, AggregateType: simpleProjectAggregate, AggregateKey: publicID,
		Snapshot: raw, Reason: "BUG039 simple-project fixture", ActorID: actorID, Source: "user", Status: "approved",
		Metadata: map[string]any{"fixture": "BUG039"},
	})
	if err == nil {
		err = applySimpleProjectSnapshotTx(ctx, tx, projectID, created.RevisionID, actorID, true, true, snapshot)
	}
	if err == nil {
		err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", actorID, "BUG039 simple-project fixture", nil)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	return created.RevisionID
}
