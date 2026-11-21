package queue

import (
	"context"
	"errors"
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
	var inbox taskInbox
	// A push consumer can assign more messages than one instance has handler
	// slots. Bound locally held deliveries, including those waiting for a slot.
	window := task.MaxConcurrent
	if js != nil {
		window = max(window, 1024) // accommodates the broker default of 1000 outstanding ACKs
	}
	pending := make(chan struct{}, window)
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
		var stopProgress func()
		if js != nil {
			stopProgress = c.keepDeliveryAlive(msg, min(backoff[0], 30*time.Minute), generation)
		}
		inbox.enqueue(task.MaxConcurrent, func() {
			defer func() {
				byteBudget.Release(size)
				<-pending
			}()
			if stopProgress != nil {
				defer stopProgress()
			}
			if !gate.wait() || !c.isGenerationActive(generation) {
				if js != nil {
					_ = msg.Nak()
				}
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
				if js != nil {
					_ = msg.NakWithDelay(backoff[0])
				}
				return
			}
			if c.ctx.Err() != nil {
				return // unfinished work stays unacknowledged during shutdown
			}
			if handleErr != nil {
				c.setGenerationError(generation, handleErr)
				if js != nil {
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
						if err := msg.NakWithDelay(backoff[max(0, min(attempt-1, len(backoff)-1))]); err != nil {
							c.setGenerationError(generation, err)
						}
					}
				}
				return
			}
			if js != nil {
				if err := msg.Ack(); err != nil {
					c.setGenerationError(generation, err)
				}
			}
		})
	}
	var err error
	if js != nil {
		durable := cleanDurable(task.QueueGroup)
		_, err = js.QueueSubscribe(fullSubject(cfg.SubjectPrefix, task.Subject), task.QueueGroup, handle,
			nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.AckWait(cfg.JetStream.AckWait),
			nats.MaxDeliver(cfg.JetStream.MaxDeliver), nats.BackOff(backoff),
			nats.BindStream(cfg.JetStream.Stream))
	} else {
		_, err = conn.QueueSubscribe(fullSubject(cfg.SubjectPrefix, task.Subject), task.QueueGroup, handle)
	}
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
