package activity

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ActionEdit     int16 = 1
	ActionCreate   int16 = 2
	ActionView     int16 = 3
	ActionDelete   int16 = 4
	ActionClaim    int16 = 5
	ActionDownload int16 = 6
	ActionUpload   int16 = 7
	ActionPurchase int16 = 8
	ActionTransfer int16 = 9
	ActionCheckIn  int16 = 10
	ActionUse      int16 = 11
)

const (
	ObjectRecipe        int16 = 1
	ObjectMod           int16 = 2
	ObjectBlueprint     int16 = 3
	ObjectPlugin        int16 = 4
	ObjectAuthor        int16 = 5
	ObjectTeam          int16 = 6
	ObjectUser          int16 = 7
	ObjectComment       int16 = 8
	ObjectTag           int16 = 9
	ObjectFile          int16 = 10
	ObjectEconomy       int16 = 11
	ObjectTask          int16 = 12
	ObjectShopItem      int16 = 13
	ObjectResource      int16 = 14
	ObjectModpack       int16 = 15
	ObjectServer        int16 = 16
	ObjectMap           int16 = 17
	ObjectResourcePack  int16 = 18
	ObjectShaderPack    int16 = 19
	ObjectDatapack      int16 = 20
	ObjectAddon         int16 = 21
	ObjectCommunityPost int16 = 22
	ObjectReview        int16 = 23
	ObjectSkin          int16 = 24
	ObjectPlayerProfile int16 = 25
	ObjectChangelog     int16 = 26
	ObjectRating        int16 = 27
)

const (
	defaultBatchSize = 256
	defaultInterval  = 2 * time.Second
)

type Event struct {
	UserID        int64
	ActionID      int16
	ObjectTypeID  int16
	ObjectRouteID int64
	// ObjectPublicID is a transient lookup key. It is resolved in batches and
	// never persisted in the high-volume activity table.
	ObjectPublicID string
	// ObjectEntityType and ObjectInternalID are the numeric internal lookup
	// alternative used when a handler already resolved the business entity.
	ObjectEntityType   string
	ObjectInternalID   int64
	MarkdownAddedBytes int
	OccurredAt         time.Time
}

type BatchProcessor func(context.Context, []Event) error

type closeRequest struct {
	ctx  context.Context
	done chan error
}

type Monitor struct {
	db        *pgxpool.Pool
	processor BatchProcessor
	incoming  chan Event
	close     chan closeRequest
	wake      chan struct{}

	overflowMu sync.Mutex
	overflow   []Event
	closeOnce  sync.Once
}

func NewMonitor(db *pgxpool.Pool, processor BatchProcessor) *Monitor {
	monitor := &Monitor{
		db:        db,
		processor: processor,
		incoming:  make(chan Event, defaultBatchSize*4),
		close:     make(chan closeRequest),
		wake:      make(chan struct{}, 1),
	}
	go monitor.run()
	return monitor
}

func (m *Monitor) Record(event Event) {
	if m == nil || m.db == nil || event.UserID <= 0 || event.ActionID <= 0 || event.ObjectTypeID <= 0 {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	if event.MarkdownAddedBytes < 0 {
		event.MarkdownAddedBytes = 0
	}
	select {
	case m.incoming <- event:
	default:
		m.overflowMu.Lock()
		m.overflow = append(m.overflow, event)
		m.overflowMu.Unlock()
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
}

func (m *Monitor) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	var result error
	m.closeOnce.Do(func() {
		done := make(chan error, 1)
		select {
		case m.close <- closeRequest{ctx: ctx, done: done}:
			select {
			case result = <-done:
			case <-ctx.Done():
				result = ctx.Err()
			}
		case <-ctx.Done():
			result = ctx.Err()
		}
	})
	return result
}

func (m *Monitor) run() {
	ticker := time.NewTicker(defaultInterval)
	defer ticker.Stop()
	pending := make([]Event, 0, defaultBatchSize)
	for {
		select {
		case event := <-m.incoming:
			pending = append(pending, event)
			if len(pending) >= defaultBatchSize {
				pending = m.flush(context.Background(), pending)
			}
		case <-m.wake:
			pending = append(pending, m.takeOverflow()...)
			if len(pending) >= defaultBatchSize {
				pending = m.flush(context.Background(), pending)
			}
		case <-ticker.C:
			pending = append(pending, m.takeOverflow()...)
			pending = m.flush(context.Background(), pending)
		case request := <-m.close:
			for {
				select {
				case event := <-m.incoming:
					pending = append(pending, event)
				default:
					pending = append(pending, m.takeOverflow()...)
					request.done <- m.flushAll(request.ctx, pending)
					return
				}
			}
		}
	}
}

func (m *Monitor) takeOverflow() []Event {
	m.overflowMu.Lock()
	defer m.overflowMu.Unlock()
	if len(m.overflow) == 0 {
		return nil
	}
	result := m.overflow
	m.overflow = nil
	return result
}

func (m *Monitor) flush(ctx context.Context, pending []Event) []Event {
	if len(pending) == 0 {
		return pending[:0]
	}
	if err := m.writeBatch(ctx, pending); err != nil {
		return pending
	}
	return pending[:0]
}

func (m *Monitor) flushAll(ctx context.Context, pending []Event) error {
	for len(pending) > 0 {
		if err := m.writeBatch(ctx, pending); err != nil {
			return err
		}
		pending = pending[:0]
	}
	return nil
}

func (m *Monitor) writeBatch(ctx context.Context, events []Event) error {
	if err := m.resolveObjectRouteIDs(ctx, events); err != nil {
		return err
	}
	rows := make([][]any, 0, len(events))
	for _, event := range events {
		rows = append(rows, []any{
			nullableUserID(event.UserID),
			event.ActionID,
			event.ObjectTypeID,
			nullableObjectRouteID(event.ObjectRouteID),
			event.MarkdownAddedBytes,
			event.OccurredAt.UTC(),
		})
	}
	_, err := m.db.CopyFrom(
		ctx,
		pgx.Identifier{"user_activity_events"},
		[]string{"user_id", "action_id", "object_type_id", "object_route_id", "markdown_added_bytes", "occurred_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return err
	}
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
		if _, aggregateErr := m.db.Exec(ctx, `select record_site_activity_batch($1,$2)`, eventTimes, actorIDs); aggregateErr != nil {
			// Activity rows are authoritative. The periodic reconciliation job can
			// rebuild this compact projection if an aggregation write is interrupted.
			log.Printf("aggregate site activity batch: %v", aggregateErr)
		}
	}
	if m.processor != nil {
		if err = m.processor(ctx, events); err != nil {
			log.Printf("process activity batch: %v", err)
		}
	}
	return nil
}

func (m *Monitor) resolveObjectRouteIDs(ctx context.Context, events []Event) error {
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
		rows, err := m.db.Query(ctx, `select public_id,id from public_routes where public_id=any($1)`, publicIDs)
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
	type resolvedRoute struct {
		id       int64
		publicID string
	}
	resolvedInternalIDs := make(map[internalKey]resolvedRoute, len(entityTypes))
	if len(entityTypes) > 0 {
		rows, err := m.db.Query(ctx, `select route.entity_type,route.internal_id,route.id,route.public_id
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

func nullableUserID(userID int64) any {
	if userID <= 0 {
		return nil
	}
	return userID
}

func nullableObjectRouteID(routeID int64) any {
	if routeID <= 0 {
		return nil
	}
	return routeID
}

// AddedMarkdownBytes returns the number of inserted UTF-8 bytes in a minimal
// insert/delete edit script. Replaced or moved text only counts newly inserted
// bytes, never bytes removed from the previous document.
func AddedMarkdownBytes(previous, current string) int {
	if previous == current {
		return 0
	}
	oldBytes := []byte(previous)
	newBytes := []byte(current)
	prefix := 0
	for prefix < len(oldBytes) && prefix < len(newBytes) && oldBytes[prefix] == newBytes[prefix] {
		prefix++
	}
	oldBytes = oldBytes[prefix:]
	newBytes = newBytes[prefix:]
	suffix := 0
	for suffix < len(oldBytes) && suffix < len(newBytes) &&
		oldBytes[len(oldBytes)-1-suffix] == newBytes[len(newBytes)-1-suffix] {
		suffix++
	}
	oldBytes = oldBytes[:len(oldBytes)-suffix]
	newBytes = newBytes[:len(newBytes)-suffix]
	if len(oldBytes) == 0 {
		return len(newBytes)
	}
	if len(newBytes) == 0 {
		return 0
	}
	distance, ok := insertDeleteDistance(oldBytes, newBytes, 8192)
	if !ok {
		return len(newBytes)
	}
	insertions := (distance + len(newBytes) - len(oldBytes)) / 2
	if insertions < 0 {
		return 0
	}
	return insertions
}

func insertDeleteDistance(oldBytes, newBytes []byte, limit int) (int, bool) {
	maximum := len(oldBytes) + len(newBytes)
	if maximum == 0 {
		return 0, true
	}
	if limit <= 0 || limit > maximum {
		limit = maximum
	}
	offset := maximum + 1
	frontier := make([]int, maximum*2+3)
	frontier[offset+1] = 0
	for distance := 0; distance <= limit; distance++ {
		for diagonal := -distance; diagonal <= distance; diagonal += 2 {
			index := offset + diagonal
			var x int
			if diagonal == -distance || (diagonal != distance && frontier[index-1] < frontier[index+1]) {
				x = frontier[index+1]
			} else {
				x = frontier[index-1] + 1
			}
			y := x - diagonal
			for x < len(oldBytes) && y < len(newBytes) && oldBytes[x] == newBytes[y] {
				x++
				y++
			}
			frontier[index] = x
			if x >= len(oldBytes) && y >= len(newBytes) {
				return distance, true
			}
		}
	}
	return 0, false
}
