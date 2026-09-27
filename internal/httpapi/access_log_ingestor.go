package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	accessLogQueueCapacity  = 1024
	accessLogBatchSize      = 128
	accessLogReadSampleRate = 16
	accessLogFlushInterval  = 100 * time.Millisecond
	accessLogWriteTimeout   = time.Second
)

const accessLogBatchInsertSQL = `insert into app_logs(
	category,level,actor_id,action,target,ip,user_agent,method,path,status,latency_ms,payload,created_at)
select record.category,record.level,record.actor_id,record.action,record.target,record.ip,record.user_agent,
	record.method,record.path,record.status,record.latency_ms,record.payload,record.created_at
from jsonb_to_recordset($1::jsonb) as record(
	category text,level text,actor_id bigint,action text,target text,ip text,user_agent text,
	method text,path text,status integer,latency_ms bigint,payload jsonb,created_at timestamptz)`

type accessLogBatchWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type accessLogRecord struct {
	Category  string          `json:"category"`
	Level     string          `json:"level"`
	ActorID   *int64          `json:"actor_id"`
	Action    string          `json:"action"`
	Target    string          `json:"target"`
	IP        string          `json:"ip"`
	UserAgent string          `json:"user_agent"`
	Method    string          `json:"method"`
	Path      string          `json:"path"`
	Status    int             `json:"status"`
	LatencyMS int64           `json:"latency_ms"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type accessLogIngestorOptions struct {
	Capacity      int
	BatchSize     int
	FlushInterval time.Duration
	WriteTimeout  time.Duration
}

type accessLogMetrics struct {
	Enqueued        uint64 `json:"enqueued"`
	SampledOut      uint64 `json:"sampledOut"`
	PolicyDropped   uint64 `json:"policyDropped"`
	OverflowDropped uint64 `json:"overflowDropped"`
	Flushed         uint64 `json:"flushed"`
	Failed          uint64 `json:"failed"`
	Batches         uint64 `json:"batches"`
	WriteFailures   uint64 `json:"writeFailures"`
	QueueDepth      int    `json:"queueDepth"`
	Capacity        int    `json:"capacity"`
}

type accessLogCounters struct {
	enqueued        atomic.Uint64
	sampledOut      atomic.Uint64
	policyDropped   atomic.Uint64
	overflowDropped atomic.Uint64
	flushed         atomic.Uint64
	failed          atomic.Uint64
	batches         atomic.Uint64
	writeFailures   atomic.Uint64
	readSequence    atomic.Uint64
}

type accessLogIngestor struct {
	writer        accessLogBatchWriter
	queue         chan accessLogRecord
	batchSize     int
	flushInterval time.Duration
	writeTimeout  time.Duration
	stop          chan struct{}
	done          chan struct{}
	lifecycleMu   sync.Mutex
	closing       bool
	closeOnce     sync.Once
	closeErrMu    sync.Mutex
	closeErr      error
	metrics       accessLogCounters
}

type accessLogDecision uint8

const (
	accessLogEnqueue accessLogDecision = iota
	accessLogSampleOut
	accessLogPolicyDrop
)

func newAccessLogIngestor(writer accessLogBatchWriter, options accessLogIngestorOptions) *accessLogIngestor {
	if options.Capacity <= 0 {
		options.Capacity = accessLogQueueCapacity
	}
	if options.BatchSize <= 0 || options.BatchSize > options.Capacity {
		options.BatchSize = min(accessLogBatchSize, options.Capacity)
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = accessLogFlushInterval
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = accessLogWriteTimeout
	}
	ingestor := &accessLogIngestor{
		writer: writer, queue: make(chan accessLogRecord, options.Capacity), batchSize: options.BatchSize,
		flushInterval: options.FlushInterval, writeTimeout: options.WriteTimeout,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	go ingestor.run()
	return ingestor
}

func newDefaultAccessLogIngestor(db *pgxpool.Pool) *accessLogIngestor {
	if db == nil {
		return nil
	}
	return newAccessLogIngestor(db, accessLogIngestorOptions{})
}

func (ingestor *accessLogIngestor) Capture(method, path string, status int, record accessLogRecord) {
	if ingestor == nil {
		return
	}
	sequence := uint64(0)
	if status < http.StatusBadRequest && !isHighFrequencyAccessLogPath(path) &&
		(method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions) {
		sequence = ingestor.metrics.readSequence.Add(1)
	}
	switch decideAccessLog(method, path, status, sequence) {
	case accessLogSampleOut:
		ingestor.metrics.sampledOut.Add(1)
	case accessLogPolicyDrop:
		ingestor.metrics.policyDropped.Add(1)
	default:
		ingestor.Enqueue(record)
	}
}

func (ingestor *accessLogIngestor) Enqueue(record accessLogRecord) bool {
	if ingestor == nil {
		return false
	}
	ingestor.lifecycleMu.Lock()
	defer ingestor.lifecycleMu.Unlock()
	if ingestor.closing {
		ingestor.metrics.overflowDropped.Add(1)
		return false
	}
	select {
	case ingestor.queue <- record:
		ingestor.metrics.enqueued.Add(1)
		return true
	default:
		ingestor.metrics.overflowDropped.Add(1)
		return false
	}
}

func (ingestor *accessLogIngestor) Metrics() accessLogMetrics {
	if ingestor == nil {
		return accessLogMetrics{}
	}
	return accessLogMetrics{
		Enqueued: ingestor.metrics.enqueued.Load(), SampledOut: ingestor.metrics.sampledOut.Load(),
		PolicyDropped: ingestor.metrics.policyDropped.Load(), OverflowDropped: ingestor.metrics.overflowDropped.Load(),
		Flushed: ingestor.metrics.flushed.Load(), Failed: ingestor.metrics.failed.Load(),
		Batches: ingestor.metrics.batches.Load(), WriteFailures: ingestor.metrics.writeFailures.Load(),
		QueueDepth: len(ingestor.queue), Capacity: cap(ingestor.queue),
	}
}

func (ingestor *accessLogIngestor) Close(ctx context.Context) error {
	if ingestor == nil {
		return nil
	}
	ingestor.closeOnce.Do(func() {
		ingestor.lifecycleMu.Lock()
		ingestor.closing = true
		close(ingestor.stop)
		ingestor.lifecycleMu.Unlock()
	})
	select {
	case <-ingestor.done:
		ingestor.closeErrMu.Lock()
		defer ingestor.closeErrMu.Unlock()
		return ingestor.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (ingestor *accessLogIngestor) run() {
	defer close(ingestor.done)
	ticker := time.NewTicker(ingestor.flushInterval)
	defer ticker.Stop()
	batch := make([]accessLogRecord, 0, ingestor.batchSize)
	for {
		select {
		case record := <-ingestor.queue:
			batch = append(batch, record)
			if len(batch) >= ingestor.batchSize {
				ingestor.flush(batch)
				batch = batch[:0]
			}
		case <-ticker.C:
			ingestor.flush(batch)
			batch = batch[:0]
		case <-ingestor.stop:
			err := ingestor.drain(batch)
			ingestor.closeErrMu.Lock()
			ingestor.closeErr = err
			ingestor.closeErrMu.Unlock()
			return
		}
	}
}

func (ingestor *accessLogIngestor) drain(batch []accessLogRecord) error {
	for {
		select {
		case record := <-ingestor.queue:
			batch = append(batch, record)
			if len(batch) >= ingestor.batchSize {
				if err := ingestor.flush(batch); err != nil {
					ingestor.failQueuedRecords()
					return err
				}
				batch = batch[:0]
			}
		default:
			return ingestor.flush(batch)
		}
	}
}

func (ingestor *accessLogIngestor) failQueuedRecords() {
	for {
		select {
		case <-ingestor.queue:
			ingestor.metrics.failed.Add(1)
		default:
			return
		}
	}
}

func (ingestor *accessLogIngestor) flush(records []accessLogRecord) error {
	if len(records) == 0 {
		return nil
	}
	raw, err := json.Marshal(records)
	if err == nil {
		writeCtx, cancel := context.WithTimeout(context.Background(), ingestor.writeTimeout)
		_, err = ingestor.writer.Exec(writeCtx, accessLogBatchInsertSQL, string(raw))
		cancel()
	}
	if err != nil {
		ingestor.metrics.failed.Add(uint64(len(records)))
		ingestor.metrics.writeFailures.Add(1)
		return err
	}
	ingestor.metrics.flushed.Add(uint64(len(records)))
	ingestor.metrics.batches.Add(1)
	return nil
}

func decideAccessLog(method, path string, status int, readSequence uint64) accessLogDecision {
	if status >= http.StatusBadRequest {
		return accessLogEnqueue
	}
	if isHighFrequencyAccessLogPath(path) {
		return accessLogPolicyDrop
	}
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		if readSequence == 0 || readSequence%accessLogReadSampleRate != 0 {
			return accessLogSampleOut
		}
	}
	return accessLogEnqueue
}

func isHighFrequencyAccessLogPath(path string) bool {
	return path == "/api/v1/site/presence" ||
		path == "/api/v1/realtime/events" ||
		path == "/api/v1/me/unread-summary" ||
		strings.HasPrefix(path, "/api/v1/content/translations/") ||
		strings.HasPrefix(path, "/api/v1/community/translations/") ||
		strings.HasPrefix(path, "/api/v1/notifications/translations/") ||
		strings.HasPrefix(path, "/api/v1/review-locks/") ||
		(strings.HasPrefix(path, "/api/v1/content-metrics/") && strings.HasSuffix(path, "/view")) ||
		(strings.HasPrefix(path, "/api/v1/messages/conversations/") && strings.HasSuffix(path, "/presence"))
}
