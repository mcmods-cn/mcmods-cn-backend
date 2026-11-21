package queue

import (
	"context"
	"errors"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"golang.org/x/sync/semaphore"
	"mcmods-cn-backend/internal/config"
)

// A push subscription hands off work in arrival order. Settled workers exit
// when the queue empties, including after a configuration swap.
type taskInbox struct {
	mu      sync.Mutex
	running int
	waiting []func()
}

func (inbox *taskInbox) enqueue(limit int, job func()) {
	inbox.mu.Lock()
	if inbox.running >= limit {
		inbox.waiting = append(inbox.waiting, job)
		inbox.mu.Unlock()
		return
	}
	inbox.running++
	inbox.mu.Unlock()
	go func() {
		for {
			job()
			inbox.mu.Lock()
			if len(inbox.waiting) == 0 {
				inbox.running--
				inbox.mu.Unlock()
				return
			}
			job, inbox.waiting[0] = inbox.waiting[0], nil
			inbox.waiting = inbox.waiting[1:]
			inbox.mu.Unlock()
		}
	}()
}

func (c *Client) subscribeTaskOn(conn *nats.Conn, js nats.JetStreamContext, cfg config.NATSConfig, definition subscriptionDefinition, generation uint64, gate *activationGate) error {
	task, exists := findTask(cfg.Tasks, definition.taskCode)
	if !exists || !task.Enabled {
		return ErrTaskDisabled
	}
	if conn == nil || !conn.IsConnected() {
		return ErrUnavailable
	}
	// In Core-only mode, the PostgreSQL dispatcher invokes this registered
	// handler. A Core task subscription would also receive another instance's
	// JetStream publication on the same subject.
	if js == nil {
		return nil
	}
	var inbox taskInbox
	// A push consumer can assign more messages than one instance has handler
	// slots. Bound locally held deliveries, including those waiting for a slot.
	pending := make(chan struct{}, 1024) // accommodates the broker default of 1000 outstanding ACKs
	byteBudget := semaphore.NewWeighted(nats.DefaultSubPendingBytesLimit)
	backoff := jetStreamBackoff(cfg.JetStream.AckWait, cfg.JetStream.MaxDeliver)
	handle := func(msg *nats.Msg) {
		select {
		case pending <- struct{}{}:
		case <-c.ctx.Done():
			return
		}
		size := min(int64(msg.Size()), nats.DefaultSubPendingBytesLimit)
		if err := byteBudget.Acquire(c.ctx, size); err != nil {
			<-pending
			return
		}
		stopProgress := c.keepDeliveryAlive(msg, min(backoff[0], 30*time.Minute), generation)
		inbox.enqueue(task.MaxConcurrent, func() {
			defer func() {
				byteBudget.Release(size)
				<-pending
			}()
			defer stopProgress()
			if !gate.wait() || !c.isGenerationActive(generation) {
				_ = msg.Nak()
				return
			}
			payload, envelope, handleErr := UnwrapEvent(msg.Data)
			eventID := msg.Header.Get("MCMods-Event-ID")
			if envelope != nil {
				eventID = envelope.EventID
			}
			if handleErr == nil {
				handleErr = c.RunTask(WithEventID(c.ctx, eventID), task.Code, func(work context.Context) error {
					if !c.isGenerationActive(generation) {
						return errDeliveryModeChanged
					}
					return definition.handler(work, payload)
				})
			}
			if errors.Is(handleErr, ErrTaskDisabled) || errors.Is(handleErr, errDeliveryModeChanged) {
				_ = msg.NakWithDelay(jitteredRetryDelay(backoff[0]))
				return
			}
			if c.ctx.Err() != nil {
				return // unfinished work stays unacknowledged during shutdown
			}
			if handleErr != nil {
				c.setGenerationError(generation, handleErr)
				metadata, _ := msg.Metadata()
				if metadata != nil && int(metadata.NumDelivered) >= cfg.JetStream.MaxDeliver {
					if eventID == "" {
						eventID = cfg.JetStream.Stream + ":" + strconv.FormatUint(metadata.Sequence.Stream, 10)
					}
					eventType := definition.taskCode + ".failed"
					if envelope != nil && envelope.EventType != "" {
						eventType = envelope.EventType
					}
					aggregateType, aggregateID := "", ""
					if envelope != nil {
						aggregateType, aggregateID = envelope.AggregateType, envelope.AggregateID
					}
					letter := DeadLetter{
						EventID: eventID, EventType: eventType, TaskCode: definition.taskCode,
						FailureStage: "consumer:" + definition.taskCode, LastError: redactDiagnostic(handleErr.Error(), cfg),
						AggregateType: aggregateType, AggregateID: aggregateID,
						Payload: msg.Data, Attempts: int(metadata.NumDelivered),
					}
					// Persist to JetStream before terminating the exhausted task.
					// Database recovery has its own consumer and never reruns the
					// task handler (which may already have incurred provider fees).
					if err := c.publishDeadLetter(js, cfg, letter); err != nil {
						c.setGenerationError(generation, err)
						return
					}
					if err := msg.Term(); err != nil {
						c.setGenerationError(generation, err)
					}
				} else {
					attempt := 1
					if metadata != nil {
						attempt = int(metadata.NumDelivered)
					}
					if err := msg.NakWithDelay(jitteredRetryDelay(backoff[max(0, min(attempt-1, len(backoff)-1))])); err != nil {
						c.setGenerationError(generation, err)
					}
				}
				return
			}
			if err := msg.Ack(); err != nil {
				c.setGenerationError(generation, err)
			}
		})
	}
	durable := cleanDurable(task.QueueGroup)
	_, err := js.QueueSubscribe(fullSubject(cfg.SubjectPrefix, task.Subject), task.QueueGroup, handle,
		nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.AckWait(cfg.JetStream.AckWait),
		nats.MaxDeliver(cfg.JetStream.MaxDeliver), nats.BackOff(backoff),
		nats.BindStream(cfg.JetStream.Stream))
	if err != nil {
		return err
	}
	if err := conn.FlushTimeout(3 * time.Second); err != nil {
		return err
	}
	return conn.LastError()
}

// InProgress extends the broker ACK deadline during healthy work and local slot
// waits. A stopped instance sends no further progress and permits redelivery.
func (c *Client) keepDeliveryAlive(msg *nats.Msg, ackWait time.Duration, generation uint64) func() {
	stop, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(max(ackWait/3, time.Nanosecond))
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-c.ctx.Done():
				return
			case <-ticker.C:
				if err := msg.InProgress(); err != nil {
					if !errors.Is(err, nats.ErrMsgAlreadyAckd) {
						c.setGenerationError(generation, err)
					}
					return
				}
			}
		}
	}()
	return func() { close(stop); <-stopped }
}

func jetStreamBackoff(initial time.Duration, maxDeliver int) []time.Duration {
	if initial <= 0 {
		initial = time.Second
	}
	if maxDeliver < 1 {
		maxDeliver = 1
	}
	const maximum = 30 * time.Minute
	result := make([]time.Duration, maxDeliver)
	delay := initial
	for index := range result {
		result[index] = delay
		if delay >= maximum {
			delay = maximum
		} else if delay > maximum/2 {
			delay = maximum
		} else {
			delay *= 2
		}
	}
	return result
}

// Spread simultaneous retries across up to a tenth of the interval, keeping
// the existing upper bound for both NAKs and database retry scheduling.
func jitteredRetryDelay(delay time.Duration) time.Duration {
	spread := int64(delay / 10)
	if spread <= 0 {
		return delay
	}
	return delay - time.Duration(rand.Int64N(spread+1))
}
