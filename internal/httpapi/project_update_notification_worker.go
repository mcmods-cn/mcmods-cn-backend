package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
)

const (
	projectUpdateNotificationBatchSize   = 200
	projectUpdateNotificationMaxAttempts = 8
)

type ProjectUpdateNotificationWorker struct {
	db    *pgxpool.Pool
	queue *queue.Client
	cache *querycache.Cache
}

func NewProjectUpdateNotificationWorker(db *pgxpool.Pool, queueClient *queue.Client, cache *querycache.Cache) *ProjectUpdateNotificationWorker {
	return &ProjectUpdateNotificationWorker{db: db, queue: queueClient, cache: cache}
}

func (worker *ProjectUpdateNotificationWorker) Start(ctx context.Context) error {
	if worker == nil || worker.db == nil {
		return errors.New("project update worker database is required")
	}
	subscribeErr := queue.ErrUnavailable
	if worker.queue != nil {
		subscribeErr = worker.queue.SubscribeTask(projectUpdateNotificationTaskCode, worker.handle)
	}
	go worker.scan(ctx)
	return subscribeErr
}

func (worker *ProjectUpdateNotificationWorker) scan(ctx context.Context) {
	if err := worker.processPending(ctx); err != nil {
		log.Printf("scan pending project update notifications: %v", err)
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := worker.processPending(ctx); err != nil {
				log.Printf("scan pending project update notifications: %v", err)
			}
		}
	}
}

func (worker *ProjectUpdateNotificationWorker) processPending(ctx context.Context) error {
	rows, err := worker.db.Query(ctx, `select event_id from project_update_notification_tasks
		where (status='pending' and next_attempt_at<=now()) or (status='processing' and updated_at<now()-interval '5 minutes')
		order by next_attempt_at,event_id limit 10`)
	if err != nil {
		return err
	}
	ids := make([]int64, 0, 10)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	for _, id := range ids {
		if err = worker.process(ctx, id); err != nil {
			if retryErr := worker.retry(ctx, id, err); retryErr != nil {
				return errors.Join(err, retryErr)
			}
		}
	}
	return nil
}

func (worker *ProjectUpdateNotificationWorker) handle(ctx context.Context, raw []byte) error {
	var message projectUpdateTaskMessage
	if json.Unmarshal(raw, &message) != nil || message.EventID <= 0 {
		return errors.New("invalid project update task")
	}
	if err := worker.process(ctx, message.EventID); err != nil {
		return errors.Join(err, worker.retry(ctx, message.EventID, err))
	}
	return nil
}

func (worker *ProjectUpdateNotificationWorker) process(ctx context.Context, eventID int64) error {
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var routeID, actorID, nextUserID int64
	var updateKind, projectName, projectURL string
	var sections []string
	// A concurrent batch may advance next_attempt_at after this transaction
	// starts. Recheck its due time using the live clock, including after a row
	// lock wait; transaction-stable now() would skip that immediately due batch.
	err = tx.QueryRow(ctx, `select event.project_route_id,coalesce(event.actor_user_id,0),event.update_kind,event.changed_sections,
		task.next_user_id,route.canonical_path from project_update_notification_tasks task
		join project_update_events event on event.id=task.event_id
		join public_routes route on route.id=event.project_route_id
		where task.event_id=$1 and task.status in ('pending','processing') and task.next_attempt_at<=clock_timestamp()
		for update of task`, eventID).Scan(&routeID, &actorID, &updateKind, &sections, &nextUserID, &projectURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update project_update_notification_tasks set status='processing',updated_at=now() where event_id=$1`, eventID); err != nil {
		return err
	}
	projectName, err = publicProjectNameForUpdate(ctx, tx, routeID)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `update project_update_notification_tasks set status='completed',updated_at=now(),last_error='target unavailable' where event_id=$1`, eventID)
		if err == nil {
			err = tx.Commit(ctx)
		}
		return err
	}
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `select follow.user_id,coalesce(nullif(account.preferred_ui_language,''),'zh-CN')
		from project_follows follow join users account on account.id=follow.user_id
		left join user_notification_settings settings on settings.user_id=follow.user_id
		where follow.project_route_id=$1 and follow.notifications_enabled and coalesce(settings.project_updates_enabled,true)
		and follow.user_id>$2 and follow.user_id<>$3 and account.status='active'
		order by follow.user_id limit $4`, routeID, nextUserID, actorID, projectUpdateNotificationBatchSize)
	if err != nil {
		return err
	}
	type recipient struct {
		id     int64
		locale string
	}
	recipients := make([]recipient, 0, projectUpdateNotificationBatchSize)
	for rows.Next() {
		var value recipient
		if err = rows.Scan(&value.id, &value.locale); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, value)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	config, err := loadNotificationTemplateConfig(ctx, tx)
	if err != nil {
		return err
	}
	stableSections := uniqueProjectUpdateSections(sections)
	unknownSections := unknownProjectUpdateSections(stableSections)
	data, _ := json.Marshal(map[string]any{"url": projectURL, "projectUpdateEventId": eventID, "updateKind": updateKind, "changedSections": sections})
	insertedRecipients := make([]int64, 0, len(recipients))
	for _, recipient := range recipients {
		_, sectionLocale, _, selectErr := selectNotificationTemplateForLocale(config, recipient.locale, "project_updated")
		if selectErr != nil {
			return selectErr
		}
		rendered, renderErr := renderNotificationTemplateForLocale(config, recipient.locale, "project_updated", map[string]string{
			"project_name": projectName, "changed_sections": localizedProjectUpdateSectionText(sectionLocale, stableSections),
		})
		if renderErr != nil {
			return renderErr
		}
		params, _ := json.Marshal(rendered.Values)
		tag, execErr := tx.Exec(ctx, `insert into notifications(
			recipient_id,kind,title,body,source_locale,data,template_key,template_version,template_params,project_update_event_id)
			values($1,'system',$2,$3,$4,$5::jsonb,$6,$7,$8::jsonb,$9)
			on conflict(recipient_id,project_update_event_id) where project_update_event_id is not null do nothing`,
			recipient.id, rendered.Title, rendered.Body, rendered.Locale, string(data), rendered.Key, rendered.Version, string(params), eventID)
		if execErr != nil {
			return execErr
		}
		if tag.RowsAffected() == 1 {
			insertedRecipients = append(insertedRecipients, recipient.id)
		}
		nextUserID = recipient.id
	}
	if err = updateProjectUpdateNotificationProgress(ctx, tx, eventID, nextUserID, int64(len(insertedRecipients)),
		len(recipients) < projectUpdateNotificationBatchSize); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if len(unknownSections) > 0 && len(insertedRecipients) > 0 {
		fallbackCount := uint64(len(unknownSections)) * uint64(len(insertedRecipients))
		projectUpdateNotificationObservability.unknownSectionFallbacks.Add(fallbackCount)
		log.Printf("project update notification event %d used localized fallback for unknown sections %v (%d deliveries)", eventID, unknownSections, fallbackCount)
	}
	for _, userID := range insertedRecipients {
		if worker.cache != nil {
			worker.cache.AdjustUnread(ctx, userID, "notifications", 1)
		}
		if worker.queue != nil {
			_ = worker.queue.PublishBroadcast(context.Background(), "user."+strconv.FormatInt(userID, 10), realtimeEvent{ID: randomHex(12), Type: "notification.created", Data: map[string]any{"kind": "system"}})
		}
	}
	return nil
}

func updateProjectUpdateNotificationProgress(ctx context.Context, tx pgx.Tx, eventID, nextUserID, insertedCount int64, completed bool) error {
	status := "pending"
	if completed {
		status = "completed"
	}
	_, err := tx.Exec(ctx, `update project_update_notification_tasks set status=$4,next_user_id=$2,
		notified_count=notified_count+$3,next_attempt_at=case when $4='pending' then now() else next_attempt_at end,
		last_error='',updated_at=now() where event_id=$1`, eventID, nextUserID, insertedCount, status)
	return err
}

func (worker *ProjectUpdateNotificationWorker) retry(ctx context.Context, eventID int64, cause error) error {
	detail := cause.Error()
	if len(detail) > 1000 {
		detail = detail[:1000]
	}
	var attempts int
	var status string
	err := worker.db.QueryRow(ctx, `update project_update_notification_tasks set
		attempt_count=attempt_count+1,
		status=case when attempt_count+1>=$3 then 'failed' else 'pending' end,
		next_attempt_at=case when attempt_count+1>=$3 then next_attempt_at
			else now()+least(300,greatest(5,(attempt_count+1)*(attempt_count+1)*5))*interval '1 second' end,
		last_error=$2,updated_at=now()
		where event_id=$1 and status in ('pending','processing')
		returning attempt_count,status`, eventID, detail, projectUpdateNotificationMaxAttempts).Scan(&attempts, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("persist project update notification retry: %w", err)
	}
	log.Printf("project update notification task %d attempt %d became %s: %v", eventID, attempts, status, cause)
	return nil
}

func publicProjectNameForUpdate(ctx context.Context, tx pgx.Tx, routeID int64) (string, error) {
	var entityType string
	var internalID int64
	if err := tx.QueryRow(ctx, `select entity_type,internal_id from public_routes where id=$1`, routeID).Scan(&entityType, &internalID); err != nil {
		return "", err
	}
	var name string
	var err error
	switch entityType {
	case "mod":
		err = tx.QueryRow(ctx, `select primary_name from mods where id=$1 and review_status='approved'`, internalID).Scan(&name)
	case "modpack":
		err = tx.QueryRow(ctx, `select primary_name from modpacks where id=$1 and review_status='approved'`, internalID).Scan(&name)
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		err = tx.QueryRow(ctx, `select primary_name from simple_projects where id=$1 and project_type=$2 and review_status='approved'`, internalID, entityType).Scan(&name)
	case "minecraft_server":
		err = tx.QueryRow(ctx, `select name from minecraft_servers where id=$1 and review_status='approved'`, internalID).Scan(&name)
	case "community_post":
		err = tx.QueryRow(ctx, `select title from community_posts where id=$1 and status='active' and review_status='approved'`, internalID).Scan(&name)
	case "blueprint":
		err = tx.QueryRow(ctx, `select title from blueprints where id=$1 and status<>'deleted' and review_status in ('approved','not_required')`, internalID).Scan(&name)
	case "skin":
		err = tx.QueryRow(ctx, `select display_name from skin_assets where id=$1 and status='active' and visibility in ('public','unlisted') and review_status='approved'`, internalID).Scan(&name)
	default:
		err = pgx.ErrNoRows
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("project name is empty")
	}
	return name, nil
}
