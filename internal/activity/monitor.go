package activity

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/activitycatalog"
)

var (
	ErrRecorderUnavailable = errors.New("activity recorder is unavailable")
	ErrRecorderClosed      = errors.New("activity recorder is closed")
	ErrInvalidEvent        = errors.New("activity event is invalid")
)

const (
	ActionEdit     = activitycatalog.ActionEdit
	ActionCreate   = activitycatalog.ActionCreate
	ActionView     = activitycatalog.ActionView
	ActionDelete   = activitycatalog.ActionDelete
	ActionClaim    = activitycatalog.ActionClaim
	ActionDownload = activitycatalog.ActionDownload
	ActionUpload   = activitycatalog.ActionUpload
	ActionPurchase = activitycatalog.ActionPurchase
	ActionTransfer = activitycatalog.ActionTransfer
	ActionCheckIn  = activitycatalog.ActionCheckIn
	ActionUse      = activitycatalog.ActionUse
)

const (
	ObjectRecipe        = activitycatalog.ObjectRecipe
	ObjectMod           = activitycatalog.ObjectMod
	ObjectBlueprint     = activitycatalog.ObjectBlueprint
	ObjectPlugin        = activitycatalog.ObjectPlugin
	ObjectAuthor        = activitycatalog.ObjectAuthor
	ObjectTeam          = activitycatalog.ObjectTeam
	ObjectUser          = activitycatalog.ObjectUser
	ObjectComment       = activitycatalog.ObjectComment
	ObjectTag           = activitycatalog.ObjectTag
	ObjectFile          = activitycatalog.ObjectFile
	ObjectEconomy       = activitycatalog.ObjectEconomy
	ObjectTask          = activitycatalog.ObjectTask
	ObjectShopItem      = activitycatalog.ObjectShopItem
	ObjectResource      = activitycatalog.ObjectResource
	ObjectModpack       = activitycatalog.ObjectModpack
	ObjectServer        = activitycatalog.ObjectServer
	ObjectMap           = activitycatalog.ObjectMap
	ObjectResourcePack  = activitycatalog.ObjectResourcePack
	ObjectShaderPack    = activitycatalog.ObjectShaderPack
	ObjectDatapack      = activitycatalog.ObjectDatapack
	ObjectAddon         = activitycatalog.ObjectAddon
	ObjectCommunityPost = activitycatalog.ObjectCommunityPost
	ObjectReview        = activitycatalog.ObjectReview
	ObjectSkin          = activitycatalog.ObjectSkin
	ObjectPlayerProfile = activitycatalog.ObjectPlayerProfile
	ObjectChangelog     = activitycatalog.ObjectChangelog
	ObjectRating        = activitycatalog.ObjectRating
)

type DictionaryEntry = activitycatalog.Entry

func ActionDefinitions() []DictionaryEntry { return activitycatalog.ActionDefinitions() }

func ObjectTypeDefinitions() []DictionaryEntry { return activitycatalog.ObjectTypeDefinitions() }

func ActionIDs() map[string]int16 { return activitycatalog.ActionIDs() }

func ObjectTypeIDs() map[string]int16 { return activitycatalog.ObjectTypeIDs() }

func ActionID(code string) int16 { return activitycatalog.ActionID(code) }

func ObjectTypeID(code string) int16 { return activitycatalog.ObjectTypeID(code) }

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
	ObjectEntityType     string
	ObjectInternalID     int64
	MarkdownAddedBytes   int
	MarkdownDeletedBytes int
	OccurredAt           time.Time
}

// ProjectionProcessor keeps durable activity facts and their business
// projections inside one database commit. The non-transactional entry point is
// reserved for explicitly lossy best-effort activity such as anonymous views.
type ProjectionProcessor interface {
	ProcessActivityBatch(context.Context, []Event) error
	ProcessActivityBatchTx(context.Context, pgx.Tx, []Event) error
	ActivityBatchCommitted(context.Context, []Event)
}

type Options struct {
	BatchSize             int
	QueueCapacity         int
	FlushInterval         time.Duration
	RetryMinDelay         time.Duration
	RetryMaxDelay         time.Duration
	WriteTimeout          time.Duration
	DurableEnqueueTimeout time.Duration
}

func DefaultOptions() Options {
	return Options{
		BatchSize: 256, QueueCapacity: 4096, FlushInterval: time.Second,
		RetryMinDelay: 250 * time.Millisecond, RetryMaxDelay: 30 * time.Second,
		WriteTimeout: 10 * time.Second, DurableEnqueueTimeout: 1500 * time.Millisecond,
	}
}

func normalizeOptions(options Options) Options {
	defaults := DefaultOptions()
	if options.BatchSize <= 0 {
		options.BatchSize = defaults.BatchSize
	}
	if options.QueueCapacity < options.BatchSize {
		options.QueueCapacity = max(defaults.QueueCapacity, options.BatchSize)
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = defaults.FlushInterval
	}
	if options.RetryMinDelay <= 0 {
		options.RetryMinDelay = defaults.RetryMinDelay
	}
	if options.RetryMaxDelay < options.RetryMinDelay {
		options.RetryMaxDelay = max(defaults.RetryMaxDelay, options.RetryMinDelay)
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = defaults.WriteTimeout
	}
	if options.DurableEnqueueTimeout <= 0 {
		options.DurableEnqueueTimeout = defaults.DurableEnqueueTimeout
	}
	return options
}

// IsDurableAction deliberately keeps only views in the lossy path. Mutations,
// downloads, purchases and other user actions first enter PostgreSQL's durable
// outbox and therefore survive a process restart.
func IsDurableAction(actionID int16) bool { return actionID != ActionView }

type activityStore interface {
	EnqueueDurable(context.Context, Event) error
	WriteBestEffort(context.Context, []Event) error
	DrainDurable(context.Context, int) (int, error)
	DurableBacklog(context.Context) (int64, *time.Time, error)
}

type closeRequest struct {
	ctx  context.Context
	done chan error
}

type Monitor struct {
	store    activityStore
	options  Options
	incoming chan Event
	close    chan closeRequest
	wake     chan struct{}

	closeOnce sync.Once
	closed    atomic.Bool
	metrics   monitorMetrics
}

func NewMonitor(db *pgxpool.Pool, processor ProjectionProcessor, options Options) *Monitor {
	return newMonitor(newPostgresStore(db, processor), options)
}

func newMonitor(store activityStore, options Options) *Monitor {
	options = normalizeOptions(options)
	monitor := &Monitor{
		store: store, options: options, incoming: make(chan Event, options.QueueCapacity),
		close: make(chan closeRequest), wake: make(chan struct{}, 1),
	}
	monitor.metrics.lastError.Store("")
	if store == nil {
		return monitor
	}
	go monitor.run()
	return monitor
}

func normalizeEvent(event Event) (Event, bool) {
	if event.UserID <= 0 || event.ActionID <= 0 || event.ObjectTypeID <= 0 {
		return Event{}, false
	}
	event.ObjectPublicID = trimField(event.ObjectPublicID, 128)
	event.ObjectEntityType = trimField(event.ObjectEntityType, 64)
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	} else {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	event.MarkdownAddedBytes = max(0, event.MarkdownAddedBytes)
	event.MarkdownDeletedBytes = max(0, event.MarkdownDeletedBytes)
	return event, true
}

func trimField(value string, maximum int) string {
	if len(value) <= maximum {
		return value
	}
	return value[:maximum]
}

// RecordBestEffort never blocks the request. Once the bounded queue is full it
// drops the view and increments DroppedBestEffort instead of growing memory.
func (m *Monitor) RecordBestEffort(event Event) bool {
	if m == nil || m.store == nil || m.closed.Load() {
		return false
	}
	var valid bool
	if event, valid = normalizeEvent(event); !valid {
		return false
	}
	select {
	case m.incoming <- event:
		m.metrics.acceptedBestEffort.Add(1)
		return true
	default:
		m.metrics.droppedBestEffort.Add(1)
		return false
	}
}

// RecordDurable acknowledges an action only after PostgreSQL has accepted it
// into the outbox. The outbox is drained with row locks, making this safe for
// multiple application instances.
func (m *Monitor) RecordDurable(ctx context.Context, event Event) error {
	if m == nil || m.store == nil {
		return ErrRecorderUnavailable
	}
	if m.closed.Load() {
		return ErrRecorderClosed
	}
	var valid bool
	if event, valid = normalizeEvent(event); !valid {
		return ErrInvalidEvent
	}
	writeCtx, cancel := boundedContext(ctx, m.options.DurableEnqueueTimeout)
	defer cancel()
	if err := m.store.EnqueueDurable(writeCtx, event); err != nil {
		m.metrics.durableEnqueueFailures.Add(1)
		m.recordError("enqueue durable activity", err)
		return err
	}
	m.metrics.enqueuedDurable.Add(1)
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

func (m *Monitor) Close(ctx context.Context) error {
	if m == nil || m.store == nil {
		return nil
	}
	var result error
	m.closeOnce.Do(func() {
		m.closed.Store(true)
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
	ticker := time.NewTicker(m.options.FlushInterval)
	defer ticker.Stop()
	pending := make([]Event, 0, m.options.BatchSize)
	var bestRetryAt, durableRetryAt time.Time
	var bestRetryDelay, durableRetryDelay time.Duration
	for {
		incoming := (<-chan Event)(m.incoming)
		if len(pending) >= m.options.BatchSize {
			incoming = nil
		}
		select {
		case event := <-incoming:
			pending = append(pending, event)
			m.metrics.pendingMemory.Store(int64(len(pending)))
			if len(pending) >= m.options.BatchSize && !time.Now().Before(bestRetryAt) {
				pending, bestRetryAt, bestRetryDelay = m.flushBestEffort(pending, bestRetryDelay)
			}
		case <-m.wake:
			if !time.Now().Before(durableRetryAt) {
				durableRetryAt, durableRetryDelay = m.drainDurable(durableRetryDelay)
			}
		case <-ticker.C:
			now := time.Now()
			if len(pending) > 0 && !now.Before(bestRetryAt) {
				pending, bestRetryAt, bestRetryDelay = m.flushBestEffort(pending, bestRetryDelay)
			}
			if !now.Before(durableRetryAt) {
				durableRetryAt, durableRetryDelay = m.drainDurable(durableRetryDelay)
			}
		case request := <-m.close:
			for {
				select {
				case event := <-m.incoming:
					pending = append(pending, event)
				default:
					request.done <- m.flushAll(request.ctx, pending)
					return
				}
			}
		}
	}
}

func (m *Monitor) flushBestEffort(pending []Event, previousDelay time.Duration) ([]Event, time.Time, time.Duration) {
	count := min(len(pending), m.options.BatchSize)
	ctx, cancel := context.WithTimeout(context.Background(), m.options.WriteTimeout)
	err := m.store.WriteBestEffort(ctx, pending[:count])
	cancel()
	if err != nil {
		m.metrics.flushFailures.Add(1)
		m.metrics.retries.Add(1)
		m.recordError("flush best-effort activity", err)
		delay := m.nextRetryDelay(previousDelay)
		return pending, jitteredRetryAt(delay), delay
	}
	m.metrics.flushedBestEffort.Add(uint64(count))
	m.metrics.flushBatches.Add(1)
	m.metrics.lastFlushUnix.Store(time.Now().UTC().Unix())
	m.metrics.pendingMemory.Store(int64(len(pending) - count))
	m.metrics.lastError.Store("")
	return pending[count:], time.Time{}, 0
}

func (m *Monitor) flushAll(ctx context.Context, pending []Event) error {
	for len(pending) > 0 {
		count := min(len(pending), m.options.BatchSize)
		if err := m.store.WriteBestEffort(ctx, pending[:count]); err != nil {
			return err
		}
		m.metrics.flushedBestEffort.Add(uint64(count))
		m.metrics.flushBatches.Add(1)
		pending = pending[count:]
	}
	m.metrics.pendingMemory.Store(0)
	return nil
}

func (m *Monitor) drainDurable(previousDelay time.Duration) (time.Time, time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), m.options.WriteTimeout)
	count, err := m.store.DrainDurable(ctx, m.options.BatchSize)
	cancel()
	if err != nil {
		m.metrics.flushFailures.Add(1)
		m.metrics.retries.Add(1)
		m.recordError("drain durable activity", err)
		delay := m.nextRetryDelay(previousDelay)
		return jitteredRetryAt(delay), delay
	}
	if count > 0 {
		m.metrics.flushedDurable.Add(uint64(count))
		m.metrics.flushBatches.Add(1)
		m.metrics.lastFlushUnix.Store(time.Now().UTC().Unix())
		m.metrics.lastError.Store("")
	}
	if count == m.options.BatchSize {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
	return time.Time{}, 0
}

func (m *Monitor) nextRetryDelay(previous time.Duration) time.Duration {
	if previous <= 0 {
		return m.options.RetryMinDelay
	}
	return min(previous*2, m.options.RetryMaxDelay)
}

func jitteredRetryAt(delay time.Duration) time.Time {
	jitterRange := delay / 5
	if jitterRange <= 0 {
		return time.Now().Add(delay)
	}
	jitter := time.Duration(time.Now().UnixNano() % int64(jitterRange))
	return time.Now().Add(delay + jitter)
}

func boundedContext(parent context.Context, maximum time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if deadline, ok := parent.Deadline(); ok && time.Until(deadline) <= maximum {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, maximum)
}

type monitorMetrics struct {
	acceptedBestEffort     atomic.Uint64
	droppedBestEffort      atomic.Uint64
	enqueuedDurable        atomic.Uint64
	durableEnqueueFailures atomic.Uint64
	flushedBestEffort      atomic.Uint64
	flushedDurable         atomic.Uint64
	flushBatches           atomic.Uint64
	flushFailures          atomic.Uint64
	retries                atomic.Uint64
	pendingMemory          atomic.Int64
	lastFlushUnix          atomic.Int64
	lastFailureLogUnix     atomic.Int64
	lastError              atomic.Value
}

type Snapshot struct {
	QueueCapacity          int           `json:"queueCapacity"`
	QueueDepth             int           `json:"queueDepth"`
	BatchSize              int           `json:"batchSize"`
	PendingMemory          int64         `json:"pendingMemory"`
	DurableBacklog         int64         `json:"durableBacklog"`
	OldestDurableAt        *time.Time    `json:"oldestDurableAt,omitempty"`
	AcceptedBestEffort     uint64        `json:"acceptedBestEffort"`
	DroppedBestEffort      uint64        `json:"droppedBestEffort"`
	EnqueuedDurable        uint64        `json:"enqueuedDurable"`
	DurableEnqueueFailures uint64        `json:"durableEnqueueFailures"`
	FlushedBestEffort      uint64        `json:"flushedBestEffort"`
	FlushedDurable         uint64        `json:"flushedDurable"`
	FlushBatches           uint64        `json:"flushBatches"`
	FlushFailures          uint64        `json:"flushFailures"`
	Retries                uint64        `json:"retries"`
	LastFlushAt            *time.Time    `json:"lastFlushAt,omitempty"`
	LastError              string        `json:"lastError,omitempty"`
	BacklogQueryError      string        `json:"backlogQueryError,omitempty"`
	Pool                   *PoolSnapshot `json:"pool,omitempty"`
}

type PoolSnapshot struct {
	MaxConns             int32 `json:"maxConns"`
	TotalConns           int32 `json:"totalConns"`
	AcquiredConns        int32 `json:"acquiredConns"`
	IdleConns            int32 `json:"idleConns"`
	EmptyAcquireCount    int64 `json:"emptyAcquireCount"`
	CanceledAcquireCount int64 `json:"canceledAcquireCount"`
	AcquireDurationMs    int64 `json:"acquireDurationMs"`
}

type poolSnapshotProvider interface{ PoolSnapshot() PoolSnapshot }

func (m *Monitor) Snapshot(ctx context.Context) Snapshot {
	if m == nil {
		return Snapshot{}
	}
	queueDepth := len(m.incoming)
	result := Snapshot{
		QueueCapacity: m.options.QueueCapacity, QueueDepth: queueDepth, BatchSize: m.options.BatchSize,
		PendingMemory: m.metrics.pendingMemory.Load() + int64(queueDepth), AcceptedBestEffort: m.metrics.acceptedBestEffort.Load(),
		DroppedBestEffort: m.metrics.droppedBestEffort.Load(), EnqueuedDurable: m.metrics.enqueuedDurable.Load(),
		DurableEnqueueFailures: m.metrics.durableEnqueueFailures.Load(), FlushedBestEffort: m.metrics.flushedBestEffort.Load(),
		FlushedDurable: m.metrics.flushedDurable.Load(), FlushBatches: m.metrics.flushBatches.Load(),
		FlushFailures: m.metrics.flushFailures.Load(), Retries: m.metrics.retries.Load(),
	}
	if value, _ := m.metrics.lastError.Load().(string); value != "" {
		result.LastError = value
	}
	if value := m.metrics.lastFlushUnix.Load(); value > 0 {
		timestamp := time.Unix(value, 0).UTC()
		result.LastFlushAt = &timestamp
	}
	if m.store != nil {
		count, oldest, err := m.store.DurableBacklog(ctx)
		if err != nil {
			result.BacklogQueryError = err.Error()
		} else {
			result.DurableBacklog, result.OldestDurableAt = count, oldest
		}
		if provider, ok := m.store.(poolSnapshotProvider); ok {
			pool := provider.PoolSnapshot()
			result.Pool = &pool
		}
	}
	return result
}

func (m *Monitor) recordError(operation string, err error) {
	if err == nil {
		return
	}
	m.metrics.lastError.Store(err.Error())
	now := time.Now().Unix()
	last := m.metrics.lastFailureLogUnix.Load()
	if now-last >= 60 && m.metrics.lastFailureLogUnix.CompareAndSwap(last, now) {
		log.Printf("%s: %v", operation, err)
	}
}

// AddedMarkdownBytes returns the number of inserted UTF-8 bytes in a minimal
// insert/delete edit script. Replaced or moved text only counts newly inserted
// bytes, never bytes removed from the previous document.
func AddedMarkdownBytes(previous, current string) int {
	added, _ := MarkdownDeltaBytes(previous, current)
	return added
}

const markdownDeltaMaxExactDistance = 1024

// MarkdownDeltaBytes returns inserted and deleted UTF-8 bytes. It computes the
// exact minimal insert/delete script only while the edit distance stays inside
// a fixed work and memory budget; larger rewrites conservatively count the
// unmatched middle as deleted plus inserted. The fallback preserves net growth
// and never lets activity accounting scale memory with the JSON request limit.
func MarkdownDeltaBytes(previous, current string) (added int, deleted int) {
	if previous == current {
		return 0, 0
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
		return len(newBytes), 0
	}
	if len(newBytes) == 0 {
		return 0, len(oldBytes)
	}
	distance, ok := insertDeleteDistance(oldBytes, newBytes, markdownDeltaMaxExactDistance)
	if !ok {
		return len(newBytes), len(oldBytes)
	}
	insertions := (distance + len(newBytes) - len(oldBytes)) / 2
	deletions := distance - insertions
	return max(0, insertions), max(0, deletions)
}

func insertDeleteDistance(oldBytes, newBytes []byte, limit int) (int, bool) {
	maximum := len(oldBytes) + len(newBytes)
	if maximum == 0 {
		return 0, true
	}
	if limit <= 0 || limit > maximum {
		limit = maximum
	}
	offset := limit + 1
	frontier := make([]int, boundedEditFrontierSize(len(oldBytes), len(newBytes), limit))
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

func boundedEditFrontierSize(oldLength, newLength, limit int) int {
	maximum := oldLength + newLength
	if maximum <= 0 {
		return 0
	}
	if limit <= 0 || limit > maximum {
		limit = maximum
	}
	return limit*2 + 3
}
