package activity

import (
	"context"
	"encoding/json"
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
	ObjectRecipe    int16 = 1
	ObjectMod       int16 = 2
	ObjectBlueprint int16 = 3
	ObjectPlugin    int16 = 4
	ObjectAuthor    int16 = 5
	ObjectTeam      int16 = 6
	ObjectUser      int16 = 7
	ObjectComment   int16 = 8
	ObjectTag       int16 = 9
	ObjectFile      int16 = 10
	ObjectEconomy   int16 = 11
	ObjectTask      int16 = 12
	ObjectShopItem  int16 = 13
)

const (
	defaultBatchSize = 256
	defaultInterval  = 2 * time.Second
)

type Event struct {
	UserID             int64
	ActionID           int16
	ObjectTypeID       int16
	ObjectPublicID     string
	MarkdownAddedBytes int
	Metadata           map[string]any
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
	rows := make([][]any, 0, len(events))
	for _, event := range events {
		metadata, err := json.Marshal(event.Metadata)
		if err != nil {
			metadata = []byte(`{}`)
		}
		rows = append(rows, []any{
			nullableUserID(event.UserID),
			event.ActionID,
			event.ObjectTypeID,
			event.ObjectPublicID,
			event.MarkdownAddedBytes,
			string(metadata),
			event.OccurredAt.UTC(),
		})
	}
	_, err := m.db.CopyFrom(
		ctx,
		pgx.Identifier{"user_activity_events"},
		[]string{"user_id", "action_id", "object_type_id", "object_public_id", "markdown_added_bytes", "metadata", "occurred_at"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return err
	}
	if m.processor != nil {
		if err = m.processor(ctx, events); err != nil {
			log.Printf("process activity batch: %v", err)
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
