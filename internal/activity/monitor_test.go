package activity

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAddedMarkdownBytes(t *testing.T) {
	tests := []struct {
		name     string
		previous string
		current  string
		want     int
	}{
		{name: "unchanged", previous: "abc", current: "abc", want: 0},
		{name: "append", previous: "abc", current: "abcdef", want: 3},
		{name: "delete", previous: "abcdef", current: "abc", want: 0},
		{name: "replace", previous: "hello old world", current: "hello new text world", want: len("new text")},
		{name: "unicode bytes", previous: "旧内容", current: "旧内容新增", want: len("新增")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AddedMarkdownBytes(test.previous, test.current); got != test.want {
				t.Fatalf("AddedMarkdownBytes() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestMarkdownDeltaBytes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name              string
		previous, current string
		added, deleted    int
	}{
		{name: "unchanged", previous: "abc", current: "abc"},
		{name: "append", previous: "abc", current: "abcdef", added: 3},
		{name: "delete", previous: "abcdef", current: "abc", deleted: 3},
		{name: "replace", previous: "hello old world", current: "hello new text world", added: 8, deleted: 3},
		{name: "utf8", previous: "old", current: "old新", added: len("新")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			added, deleted := MarkdownDeltaBytes(test.previous, test.current)
			if added != test.added || deleted != test.deleted {
				t.Fatalf("MarkdownDeltaBytes() = (%d,%d), want (%d,%d)", added, deleted, test.added, test.deleted)
			}
		})
	}
}

func TestMarkdownDiffFrontierIsBoundedByExactDistanceBudget(t *testing.T) {
	const eightMiB = 8 << 20
	if markdownDeltaMaxExactDistance > 2048 {
		t.Fatalf("exact distance budget = %d, want at most 2048", markdownDeltaMaxExactDistance)
	}
	got := boundedEditFrontierSize(eightMiB, eightMiB, markdownDeltaMaxExactDistance)
	want := 2*markdownDeltaMaxExactDistance + 3
	if got != want {
		t.Fatalf("frontier entries = %d, want %d", got, want)
	}
	if got >= 2*(eightMiB+eightMiB)+3 {
		t.Fatalf("frontier still scales with request bytes: %d entries", got)
	}
}

func TestMarkdownDeltaLargeRewriteFallsBackConservatively(t *testing.T) {
	const eightMiB = 8 << 20
	previous := strings.Repeat("a", eightMiB)
	current := strings.Repeat("b", eightMiB)
	added, deleted := MarkdownDeltaBytes(previous, current)
	if added != eightMiB || deleted != eightMiB {
		t.Fatalf("large rewrite delta = (%d,%d), want (%d,%d)", added, deleted, eightMiB, eightMiB)
	}
}

type fakeActivityStore struct {
	mu           sync.Mutex
	writes       [][]Event
	durable      []Event
	failWrites   int
	writeEntered chan struct{}
	releaseWrite chan struct{}
}

func (store *fakeActivityStore) EnqueueDurable(_ context.Context, event Event) error {
	store.mu.Lock()
	store.durable = append(store.durable, event)
	store.mu.Unlock()
	return nil
}

func (store *fakeActivityStore) WriteBestEffort(ctx context.Context, events []Event) error {
	if store.writeEntered != nil {
		select {
		case store.writeEntered <- struct{}{}:
		default:
		}
	}
	if store.releaseWrite != nil {
		select {
		case <-store.releaseWrite:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	store.writes = append(store.writes, append([]Event(nil), events...))
	if store.failWrites > 0 {
		store.failWrites--
		return errors.New("temporary database failure")
	}
	return nil
}

func (store *fakeActivityStore) DrainDurable(context.Context, int) (int, error) {
	return 0, nil
}

func (store *fakeActivityStore) DurableBacklog(context.Context) (int64, *time.Time, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return int64(len(store.durable)), nil, nil
}

func (store *fakeActivityStore) writeSizes() []int {
	store.mu.Lock()
	defer store.mu.Unlock()
	result := make([]int, len(store.writes))
	for index, events := range store.writes {
		result[index] = len(events)
	}
	return result
}

func testViewEvent() Event {
	return Event{UserID: 1, ActionID: ActionView, ObjectTypeID: ObjectMod, OccurredAt: time.Now().UTC()}
}

func TestMonitorDropsBestEffortWhenBoundedQueueIsFull(t *testing.T) {
	store := &fakeActivityStore{writeEntered: make(chan struct{}, 1), releaseWrite: make(chan struct{})}
	monitor := newMonitor(store, Options{
		BatchSize: 2, QueueCapacity: 2, FlushInterval: time.Hour,
		RetryMinDelay: time.Millisecond, RetryMaxDelay: time.Millisecond,
		WriteTimeout: time.Second, DurableEnqueueTimeout: time.Second,
	})
	for range 2 {
		if !monitor.RecordBestEffort(testViewEvent()) {
			t.Fatal("initial view should enter the bounded pipeline")
		}
	}
	select {
	case <-store.writeEntered:
	case <-time.After(time.Second):
		t.Fatal("monitor did not start the first batch")
	}
	accepted := 0
	for range 3 {
		if monitor.RecordBestEffort(testViewEvent()) {
			accepted++
		}
	}
	if accepted != 2 {
		t.Fatalf("accepted %d queued events while writer was blocked, want 2", accepted)
	}
	if snapshot := monitor.Snapshot(context.Background()); snapshot.DroppedBestEffort != 1 {
		t.Fatalf("dropped count = %d, want 1", snapshot.DroppedBestEffort)
	}
	close(store.releaseWrite)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := monitor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	for _, size := range store.writeSizes() {
		if size > 2 {
			t.Fatalf("writer received oversized batch %d", size)
		}
	}
}

func TestMonitorRetriesWithoutGrowingOrOversizingBatch(t *testing.T) {
	store := &fakeActivityStore{failWrites: 1}
	monitor := newMonitor(store, Options{
		BatchSize: 2, QueueCapacity: 8, FlushInterval: 5 * time.Millisecond,
		RetryMinDelay: 5 * time.Millisecond, RetryMaxDelay: 20 * time.Millisecond,
		WriteTimeout: time.Second, DurableEnqueueTimeout: time.Second,
	})
	for range 5 {
		if !monitor.RecordBestEffort(testViewEvent()) {
			t.Fatal("test queue unexpectedly overflowed")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if monitor.Snapshot(context.Background()).FlushedBestEffort == 5 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	snapshot := monitor.Snapshot(context.Background())
	if snapshot.FlushedBestEffort != 5 || snapshot.Retries == 0 {
		t.Fatalf("unexpected retry snapshot %#v", snapshot)
	}
	for _, size := range store.writeSizes() {
		if size > 2 {
			t.Fatalf("writer received oversized batch %d", size)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := monitor.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorPersistsDurableActionsBeforeAcknowledging(t *testing.T) {
	store := &fakeActivityStore{}
	monitor := newMonitor(store, Options{BatchSize: 2, QueueCapacity: 2, FlushInterval: time.Hour})
	event := Event{UserID: 7, ActionID: ActionCreate, ObjectTypeID: ObjectComment}
	if err := monitor.RecordDurable(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	snapshot := monitor.Snapshot(context.Background())
	if snapshot.EnqueuedDurable != 1 || snapshot.DurableBacklog != 1 {
		t.Fatalf("unexpected durable snapshot %#v", snapshot)
	}
	if !IsDurableAction(ActionCreate) || IsDurableAction(ActionView) {
		t.Fatal("durability classification is incorrect")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := monitor.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := monitor.RecordDurable(context.Background(), event); !errors.Is(err, ErrRecorderClosed) {
		t.Fatalf("record after close returned %v", err)
	}
}
