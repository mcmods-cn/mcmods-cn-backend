package activity

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type postgresStore struct {
	db          *pgxpool.Pool
	processor   BatchProcessor
	afterCommit BatchCommittedObserver
}

func newPostgresStore(db *pgxpool.Pool, processor BatchProcessor, observers ...BatchCommittedObserver) activityStore {
	if db == nil {
		return nil
	}
	store := &postgresStore{db: db, processor: processor}
	if len(observers) > 0 {
		store.afterCommit = observers[0]
	}
	return store
}

func (store *postgresStore) EnqueueDurable(ctx context.Context, event Event) error {
	_, err := store.db.Exec(ctx, `insert into activity_event_outbox(
		user_id,action_id,object_type_id,object_route_id,object_public_id,
		object_entity_type,object_internal_id,markdown_added_bytes,
		markdown_deleted_bytes,occurred_at
	) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event.UserID, event.ActionID, event.ObjectTypeID, nullablePositive(event.ObjectRouteID),
		event.ObjectPublicID, event.ObjectEntityType, nullablePositive(event.ObjectInternalID),
		event.MarkdownAddedBytes, event.MarkdownDeletedBytes, event.OccurredAt.UTC())
	return err
}

func (store *postgresStore) WriteBestEffort(ctx context.Context, events []Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err = resolveObjectRouteIDs(ctx, tx, events); err != nil {
		return err
	}
	if err = copyActivityEvents(ctx, tx, events); err != nil {
		return err
	}
	if err = store.processProjections(ctx, tx, events); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	store.notifyCommitted(ctx, events)
	return nil
}

func (store *postgresStore) DrainDurable(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		return 0, nil
	}
	tx, err := store.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `select id,user_id,action_id,object_type_id,object_route_id,
		object_public_id,object_entity_type,object_internal_id,markdown_added_bytes,
		markdown_deleted_bytes,occurred_at
		from activity_event_outbox where available_at<=now()
		order by available_at,id limit $1 for update skip locked`, limit)
	if err != nil {
		return 0, err
	}
	ids := make([]int64, 0, limit)
	events := make([]Event, 0, limit)
	for rows.Next() {
		var id int64
		var event Event
		var routeID, internalID sql.NullInt64
		if err = rows.Scan(&id, &event.UserID, &event.ActionID, &event.ObjectTypeID, &routeID,
			&event.ObjectPublicID, &event.ObjectEntityType, &internalID, &event.MarkdownAddedBytes,
			&event.MarkdownDeletedBytes, &event.OccurredAt); err != nil {
			rows.Close()
			return 0, err
		}
		if routeID.Valid {
			event.ObjectRouteID = routeID.Int64
		}
		if internalID.Valid {
			event.ObjectInternalID = internalID.Int64
		}
		ids = append(ids, id)
		events = append(events, event)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	if len(events) == 0 {
		if err = tx.Commit(ctx); err != nil {
			return 0, err
		}
		return 0, nil
	}
	if err = resolveObjectRouteIDs(ctx, tx, events); err == nil {
		err = copyActivityEvents(ctx, tx, events)
	}
	if err == nil {
		err = store.processProjections(ctx, tx, events)
	}
	if err == nil {
		_, err = tx.Exec(ctx, `delete from activity_event_outbox where id=any($1::bigint[])`, ids)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		_ = tx.Rollback(context.Background())
		store.markDurableFailure(ids, err)
		return 0, err
	}
	store.notifyCommitted(ctx, events)
	return len(events), nil
}

func (store *postgresStore) markDurableFailure(ids []int64, cause error) {
	if len(ids) == 0 || cause == nil {
		return
	}
	message := cause.Error()
	if len(message) > 1000 {
		message = message[:1000]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = store.db.Exec(ctx, `update activity_event_outbox
		set attempts=attempts+1,last_error=$2,
			available_at=now()+make_interval(secs=>least(300.0,power(2.0,least(attempts,8)::double precision)))
		where id=any($1::bigint[])`, ids, message)
}

func (store *postgresStore) DurableBacklog(ctx context.Context) (int64, *time.Time, error) {
	var count int64
	var oldest sql.NullTime
	err := store.db.QueryRow(ctx, `select count(*),min(created_at) from activity_event_outbox`).Scan(&count, &oldest)
	if err != nil {
		return 0, nil, err
	}
	if !oldest.Valid {
		return count, nil, nil
	}
	value := oldest.Time.UTC()
	return count, &value, nil
}

func (store *postgresStore) PoolSnapshot() PoolSnapshot {
	stats := store.db.Stat()
	return PoolSnapshot{
		MaxConns: stats.MaxConns(), TotalConns: stats.TotalConns(), AcquiredConns: stats.AcquiredConns(),
		IdleConns: stats.IdleConns(), EmptyAcquireCount: stats.EmptyAcquireCount(),
		CanceledAcquireCount: stats.CanceledAcquireCount(), AcquireDurationMs: stats.AcquireDuration().Milliseconds(),
	}
}

func (store *postgresStore) processProjections(ctx context.Context, tx pgx.Tx, events []Event) error {
	eventTimes := make([]time.Time, 0, len(events))
	actorIDs := make([]int64, 0, len(events))
	for _, event := range events {
		if event.UserID <= 0 {
			continue
		}
		eventTimes = append(eventTimes, event.OccurredAt.UTC())
		actorIDs = append(actorIDs, event.UserID)
	}
	if len(eventTimes) > 0 {
		if _, err := tx.Exec(ctx, `select record_site_activity_batch($1,$2)`, eventTimes, actorIDs); err != nil {
			return err
		}
	}
	if store.processor != nil {
		if err := store.processor(ctx, tx, events); err != nil {
			return err
		}
	}
	return nil
}

func (store *postgresStore) notifyCommitted(ctx context.Context, events []Event) {
	if store.afterCommit != nil {
		if err := store.afterCommit(ctx, events); err != nil {
			log.Printf("refresh committed activity cache: %v", err)
		}
	}
}

type routeQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type activityCopier interface {
	CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error)
}

func copyActivityEvents(ctx context.Context, target activityCopier, events []Event) error {
	rows := make([][]any, 0, len(events))
	for _, event := range events {
		rows = append(rows, []any{
			event.UserID, event.ActionID, event.ObjectTypeID, nullablePositive(event.ObjectRouteID),
			event.MarkdownAddedBytes, event.MarkdownDeletedBytes, event.OccurredAt.UTC(),
		})
	}
	_, err := target.CopyFrom(ctx, pgx.Identifier{"user_activity_events"}, []string{
		"user_id", "action_id", "object_type_id", "object_route_id",
		"markdown_added_bytes", "markdown_deleted_bytes", "occurred_at",
	}, pgx.CopyFromRows(rows))
	return err
}

func resolveObjectRouteIDs(ctx context.Context, queryer routeQueryer, events []Event) error {
	publicIDs := make([]string, 0, len(events))
	seen := make(map[string]struct{}, len(events))
	for _, event := range events {
		if event.ObjectRouteID > 0 || event.ObjectPublicID == "" {
			continue
		}
		if _, exists := seen[event.ObjectPublicID]; exists {
			continue
		}
		seen[event.ObjectPublicID] = struct{}{}
		publicIDs = append(publicIDs, event.ObjectPublicID)
	}
	resolvedPublicIDs := make(map[string]int64, len(publicIDs))
	if len(publicIDs) > 0 {
		rows, err := queryer.Query(ctx, `select public_id,id from public_routes where public_id=any($1)`, publicIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var publicID string
			var routeID int64
			if err = rows.Scan(&publicID, &routeID); err != nil {
				rows.Close()
				return err
			}
			resolvedPublicIDs[publicID] = routeID
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	type internalKey struct {
		entityType string
		internalID int64
	}
	type resolvedRoute struct {
		id       int64
		publicID string
	}
	entityTypes := make([]string, 0, len(events))
	internalIDs := make([]int64, 0, len(events))
	seenInternal := make(map[internalKey]struct{}, len(events))
	for _, event := range events {
		if event.ObjectRouteID > 0 || event.ObjectEntityType == "" || event.ObjectInternalID <= 0 {
			continue
		}
		key := internalKey{entityType: event.ObjectEntityType, internalID: event.ObjectInternalID}
		if _, exists := seenInternal[key]; exists {
			continue
		}
		seenInternal[key] = struct{}{}
		entityTypes = append(entityTypes, key.entityType)
		internalIDs = append(internalIDs, key.internalID)
	}
	resolvedInternalIDs := make(map[internalKey]resolvedRoute, len(entityTypes))
	if len(entityTypes) > 0 {
		rows, err := queryer.Query(ctx, `select route.entity_type,route.internal_id,route.id,route.public_id
			from public_routes route
			join unnest($1::text[],$2::bigint[]) input(entity_type,internal_id)
			  on input.entity_type=route.entity_type and input.internal_id=route.internal_id`, entityTypes, internalIDs)
		if err != nil {
			return err
		}
		for rows.Next() {
			var key internalKey
			var route resolvedRoute
			if err = rows.Scan(&key.entityType, &key.internalID, &route.id, &route.publicID); err != nil {
				rows.Close()
				return err
			}
			resolvedInternalIDs[key] = route
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
	}
	for index := range events {
		if events[index].ObjectRouteID > 0 {
			continue
		}
		if events[index].ObjectPublicID != "" {
			events[index].ObjectRouteID = resolvedPublicIDs[events[index].ObjectPublicID]
		}
		if events[index].ObjectRouteID <= 0 && events[index].ObjectInternalID > 0 {
			key := internalKey{entityType: events[index].ObjectEntityType, internalID: events[index].ObjectInternalID}
			route := resolvedInternalIDs[key]
			events[index].ObjectRouteID = route.id
			if events[index].ObjectPublicID == "" {
				events[index].ObjectPublicID = route.publicID
			}
		}
	}
	return nil
}

func nullablePositive(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}
