package queue

import (
	"context"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	"mcmods-cn-backend/internal/config"
)

func initializeJetStream(conn *nats.Conn, cfg config.NATSConfig) (nats.JetStreamContext, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, err
	}
	subject := fullSubject(cfg.SubjectPrefix, ">")
	streamCfg := &nats.StreamConfig{Name: cfg.JetStream.Stream, Subjects: []string{subject}, Retention: nats.LimitsPolicy, Storage: nats.FileStorage, Discard: nats.DiscardNew, Duplicates: 10 * time.Minute}
	var info *nats.StreamInfo
	if info, err = js.StreamInfo(cfg.JetStream.Stream); errors.Is(err, nats.ErrStreamNotFound) {
		_, err = js.AddStream(streamCfg)
		if err != nil {
			return nil, err
		}
		// A second publisher may already have a PubAck. A failed candidate
		// configuration can close its own connection, never erase the stream.
		return js, nil
	}
	if err != nil {
		return nil, err
	}
	if !streamCoversSubject(info.Config.Subjects, subject) {
		return nil, errors.New("existing JetStream stream does not cover the configured NATS subject prefix")
	}
	return js, nil
}

func streamCoversSubject(subjects []string, required string) bool {
	for _, subject := range subjects {
		if subject == required {
			return true
		}
	}
	return false
}

type StreamUsage struct {
	Name          string `json:"name"`
	Messages      uint64 `json:"messages"`
	Bytes         uint64 `json:"bytes"`
	MaxBytes      int64  `json:"maxBytes"`
	MaxMessages   int64  `json:"maxMessages"`
	MaxAgeSeconds int64  `json:"maxAgeSeconds"`
	Consumers     int    `json:"consumers"`
	Retention     string `json:"retention"`
	Discard       string `json:"discard"`
}

// StreamUsage is an on-demand administrator probe. A failed or disabled
// broker returns a missing sample and does not hide database-backed metrics.
func (c *Client) StreamUsage(ctx context.Context) *StreamUsage {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	js, cfg := c.jetStream, c.cfg.JetStream
	c.mu.RUnlock()
	if js == nil {
		return nil
	}
	probe, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	info, err := js.StreamInfo(cfg.Stream, nats.Context(probe))
	if err != nil {
		return nil
	}
	return &StreamUsage{Name: info.Config.Name, Messages: info.State.Msgs, Bytes: info.State.Bytes,
		MaxBytes: info.Config.MaxBytes, MaxMessages: info.Config.MaxMsgs, MaxAgeSeconds: int64(info.Config.MaxAge / time.Second),
		Consumers: info.State.Consumers, Retention: info.Config.Retention.String(), Discard: info.Config.Discard.String()}
}
