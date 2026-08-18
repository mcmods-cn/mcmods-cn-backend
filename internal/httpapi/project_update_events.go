package httpapi

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
)

const projectUpdateNotificationTaskCode = "project_update_notifications"

const projectUpdateMergeWindow = 30 * time.Second

type projectUpdateTaskMessage struct {
	EventID int64 `json:"eventId"`
}

func enqueueProjectUpdateEventTx(ctx context.Context, tx pgx.Tx, routeID, revisionID, actorID int64, updateKind string, changedSections []string, publicationBatchID string) error {
	if routeID <= 0 || strings.TrimSpace(publicationBatchID) == "" {
		return nil
	}
	sections := uniqueProjectUpdateSections(changedSections)
	// Serialize event creation per public project so two API instances cannot
	// both miss the merge candidate and create duplicate notification bursts.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1,0))`, "project-update:"+strconv.FormatInt(routeID, 10)); err != nil {
		return err
	}
	var existingEventID int64
	err := tx.QueryRow(ctx, `select id from project_update_events
		where project_route_id=$1 and publication_batch_id=$2`, routeID, publicationBatchID).Scan(&existingEventID)
	if err == nil {
		return nil
	}
	if err != pgx.ErrNoRows {
		return err
	}
	var mergedEventID int64
	var mergedSections []string
	err = tx.QueryRow(ctx, `select event.id,event.changed_sections
		from project_update_events event
		join project_update_notification_tasks task on task.event_id=event.id
		where event.project_route_id=$1
		and event.actor_user_id is not distinct from nullif($2::bigint,0)
		and task.status='pending' and task.next_attempt_at>now()
		and event.created_at>=now()-$3::interval
		order by event.id desc limit 1 for update of event,task`,
		routeID, actorID, projectUpdateMergeWindow.String()).Scan(&mergedEventID, &mergedSections)
	if err == nil {
		mergedSections = uniqueProjectUpdateSections(append(mergedSections, sections...))
		_, err = tx.Exec(ctx, `update project_update_events set changed_sections=$2,
			update_kind=case when update_kind=$3 then update_kind else 'content_updated' end
			where id=$1`, mergedEventID, mergedSections, updateKind)
		return err
	}
	if err != pgx.ErrNoRows {
		return err
	}
	var eventID int64
	err = tx.QueryRow(ctx, `insert into project_update_events(
		project_route_id,revision_id,actor_user_id,update_kind,changed_sections,publication_batch_id)
		values($1,nullif($2::bigint,0)::text,nullif($3::bigint,0),$4,$5,$6)
		on conflict(project_route_id,publication_batch_id) do nothing returning id`,
		routeID, revisionID, actorID, updateKind, sections, publicationBatchID).Scan(&eventID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into project_update_notification_tasks(event_id,next_attempt_at)
		values($1,now()+$2::interval)`, eventID, projectUpdateMergeWindow.String()); err != nil {
		return err
	}
	_, err = queue.EnqueueTx(ctx, tx, projectUpdateNotificationTaskCode, "project.updated", "project_update_event", strconv.FormatInt(eventID, 10), "", projectUpdateTaskMessage{EventID: eventID})
	return err
}

func uniqueProjectUpdateSections(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if slash := strings.IndexByte(value, '/'); slash >= 0 {
			value = value[:slash]
		}
		if value == "" {
			value = "published_content"
		}
		if value == "reason" || value == "updatedAt" || value == "updated_at" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	if len(result) == 0 {
		return []string{"published_content"}
	}
	return result
}

func appendReviewedProjectUpdateEventTx(ctx context.Context, tx pgx.Tx, requestID, actorID int64) error {
	var entityType, requestPublicID, aggregateType string
	var entityID, revisionID int64
	var baseRevisionID *int64
	err := tx.QueryRow(ctx, `select entity_type,entity_id,aggregate_type,public_id,proposed_revision_id,base_revision_id
		from change_requests where id=$1`, requestID).Scan(&entityType, &entityID, &aggregateType, &requestPublicID, &revisionID, &baseRevisionID)
	if err != nil {
		return err
	}
	var routeID int64
	if _, supported := followableProjectTypes[entityType]; supported {
		_ = tx.QueryRow(ctx, `select id from public_routes where entity_type=$1 and internal_id=$2`, entityType, entityID).Scan(&routeID)
	} else if strings.HasPrefix(aggregateType, "mod_content_") {
		_ = tx.QueryRow(ctx, `select route.id from content_revisions revision
			join public_routes route on route.entity_type='mod' and route.public_id=revision.snapshot->>'modPublicId'
			where revision.id=$1`, revisionID).Scan(&routeID)
	}
	if routeID == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `select path from content_change_items where revision_id=$1 order by id`, revisionID)
	if err != nil {
		return err
	}
	sections := make([]string, 0)
	for rows.Next() {
		var path string
		if err = rows.Scan(&path); err != nil {
			rows.Close()
			return err
		}
		sections = append(sections, path)
	}
	rows.Close()
	updateKind := "content_updated"
	if baseRevisionID == nil {
		updateKind = "published"
	}
	return enqueueProjectUpdateEventTx(ctx, tx, routeID, revisionID, actorID, updateKind, sections, "review:"+requestPublicID)
}
