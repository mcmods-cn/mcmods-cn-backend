package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"

	"mcmods-cn-backend/internal/config"
)

const deadLetterInfoHeader = "MCMods-Dead-Letter-Info"

// The event body remains the original envelope. Recovery metadata is separate
// so the existing authenticated administrator replay can still decode it.
type deadLetterInfo struct {
	EventID       string `json:"eventId"`
	EventType     string `json:"eventType"`
	TaskCode      string `json:"taskCode"`
	AggregateType string `json:"aggregateType"`
	AggregateID   string `json:"aggregateId"`
	LastError     string `json:"lastError"`
	Attempts      int    `json:"attempts"`
}

func (c *Client) publishDeadLetter(js nats.JetStreamContext, cfg config.NATSConfig, letter DeadLetter) error {
	info := deadLetterInfo{letter.EventID, letter.EventType, letter.TaskCode,
		letter.AggregateType, letter.AggregateID, letter.LastError, letter.Attempts}
	if len(info.LastError) > 1000 {
		info.LastError = info.LastError[:1000]
	}
	raw, err := json.Marshal(info)
	if err != nil {
		return err
	}
	message := nats.NewMsg(fullSubject(cfg.SubjectPrefix, "dead-letter."+letter.TaskCode))
	message.Data = letter.Payload
	message.Header.Set(nats.MsgIdHdr, "dead-letter:"+letter.TaskCode+":"+letter.EventID)
	message.Header.Set("MCMods-Event-ID", letter.EventID)
	message.Header.Set(deadLetterInfoHeader, string(raw))
	ctx, cancel := context.WithTimeout(c.ctx, cfg.JetStream.PublishTimeout)
	defer cancel()
	_, err = js.PublishMsg(message, nats.Context(ctx))
	return err
}

func (c *Client) subscribeDeadLettersOn(conn *nats.Conn, js nats.JetStreamContext, cfg config.NATSConfig, generation uint64, gate *activationGate) error {
	group := cleanDurable(cfg.SubjectPrefix + "-dead-letter-recovery")
	_, err := js.QueueSubscribe(fullSubject(cfg.SubjectPrefix, "dead-letter.>"), group, func(message *nats.Msg) {
		if !gate.wait() || !c.isGenerationActive(generation) {
			_ = message.NakWithDelay(time.Second)
			return
		}
		var info deadLetterInfo
		if err := json.Unmarshal([]byte(message.Header.Get(deadLetterInfoHeader)), &info); err != nil ||
			info.EventID == "" || info.TaskCode == "" || info.Attempts < 1 ||
			message.Subject != fullSubject(cfg.SubjectPrefix, "dead-letter."+info.TaskCode) {
			// Older broker records have no metadata. Preserve them for manual
			// inspection instead of inventing an event identity or discarding them.
			c.setGenerationError(generation, errors.New("dead-letter recovery metadata is invalid or unsupported"))
			// Term ends this consumer's unsupported delivery, but LimitsPolicy
			// retains the record in the stream. A poison legacy record must not
			// occupy the single recovery slot and starve newer valid records.
			if err := message.Term(); err != nil {
				c.setGenerationError(generation, err)
			}
			return
		}
		c.mu.RLock()
		sink := c.deadLetterSink
		c.mu.RUnlock()
		if sink == nil {
			_ = message.NakWithDelay(time.Second)
			return
		}
		ctx, cancel := context.WithTimeout(c.ctx, 3*time.Second)
		defer cancel()
		ctx = WithEventID(ctx, info.EventID)
		err := sink(ctx, DeadLetter{EventID: info.EventID, EventType: info.EventType, TaskCode: info.TaskCode,
			FailureStage: "consumer:" + info.TaskCode, LastError: info.LastError, AggregateType: info.AggregateType,
			AggregateID: info.AggregateID, Payload: message.Data, Attempts: info.Attempts})
		if err != nil {
			c.setGenerationError(generation, err)
			metadata, _ := message.Metadata()
			delay := time.Second
			if metadata != nil {
				for attempt := uint64(1); attempt < min(metadata.NumDelivered, 6); attempt++ {
					delay *= 2
				}
			}
			_ = message.NakWithDelay(min(delay, 30*time.Second))
			return
		}
		if err := message.AckSync(nats.Context(ctx)); err != nil {
			c.setGenerationError(generation, err)
		}
	}, nats.Durable(group), nats.ManualAck(), nats.AckExplicit(), nats.MaxAckPending(1),
		nats.AckWait(10*time.Second), nats.MaxDeliver(-1), nats.BindStream(cfg.JetStream.Stream))
	if err != nil {
		return err
	}
	if err := conn.FlushTimeout(3 * time.Second); err != nil {
		return err
	}
	return conn.LastError()
}
