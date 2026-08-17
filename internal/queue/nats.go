package queue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"mcmods-cn-backend/internal/config"
)

var (
	ErrUnavailable  = errors.New("nats queue is unavailable")
	ErrTaskDisabled = errors.New("nats task is disabled")
)

type Status struct {
	Enabled       bool                    `json:"enabled"`
	Connected     bool                    `json:"connected"`
	URL           string                  `json:"url"`
	SubjectPrefix string                  `json:"subjectPrefix"`
	Tasks         []config.NATSTaskConfig `json:"tasks"`
	LastError     string                  `json:"lastError,omitempty"`
	JetStream     bool                    `json:"jetStream"`
}

type taskHandler func(context.Context, []byte) error

type subscriptionDefinition struct {
	taskCode string
	handler  taskHandler
}

type DeadLetter struct {
	EventID, EventType, TaskCode, FailureStage, LastError string
	Payload                                               []byte
	Attempts                                              int
}

type DeadLetterSink func(context.Context, DeadLetter) error

type Client struct {
	mu             sync.RWMutex
	cfg            config.NATSConfig
	conn           *nats.Conn
	jetStream      nats.JetStreamContext
	lastError      string
	subscriptions  map[string]subscriptionDefinition
	deadLetterSink DeadLetterSink
}

func New(ctx context.Context, cfg config.NATSConfig) *Client {
	client := &Client{subscriptions: make(map[string]subscriptionDefinition)}
	_ = client.Reconfigure(cfg)
	go func() {
		<-ctx.Done()
		client.Close()
	}()
	return client
}

func (c *Client) SetDeadLetterSink(sink DeadLetterSink) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.deadLetterSink = sink
	c.mu.Unlock()
}

func DefaultTaskConfigs() []config.NATSTaskConfig {
	return []config.NATSTaskConfig{
		{
			Code:           "ai",
			Enabled:        true,
			Subject:        "ai.tasks",
			QueueGroup:     "mcmods-ai-workers",
			MaxConcurrent:  2,
			TimeoutSeconds: 300,
		},
		{
			Code:           "notifications",
			Enabled:        true,
			Subject:        "notifications.events",
			QueueGroup:     "mcmods-notification-workers",
			MaxConcurrent:  8,
			TimeoutSeconds: 300,
		},
		{
			Code:           "mod_export_import",
			Enabled:        true,
			Subject:        "export.import.requested",
			QueueGroup:     "mcmods-export-import-workers",
			MaxConcurrent:  2,
			TimeoutSeconds: 3600,
		},
		{
			Code:           "mod_metadata_import",
			Enabled:        true,
			Subject:        "mods.metadata.import",
			QueueGroup:     "mcmods-mod-metadata-import-workers",
			MaxConcurrent:  4,
			TimeoutSeconds: 180,
		},
		{
			Code:           "blueprint_convert",
			Enabled:        true,
			Subject:        "blueprints.convert",
			QueueGroup:     "mcmods-blueprint-workers",
			MaxConcurrent:  4,
			TimeoutSeconds: 1800,
		},
	}
}

func NormalizeConfig(cfg config.NATSConfig) config.NATSConfig {
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		cfg.URL = nats.DefaultURL
	}
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.SubjectPrefix = cleanSubjectToken(cfg.SubjectPrefix, "mcmods")
	if strings.TrimSpace(cfg.JetStream.Stream) == "" {
		cfg.JetStream.Stream = "MCMODS_TASKS"
	}
	if cfg.JetStream.MaxDeliver <= 0 {
		cfg.JetStream.MaxDeliver = 8
	}
	if cfg.JetStream.AckWait <= 0 {
		cfg.JetStream.AckWait = 5 * time.Minute
	}
	if cfg.JetStream.PublishTimeout <= 0 {
		cfg.JetStream.PublishTimeout = 5 * time.Second
	}
	useDefaultTasks := cfg.Tasks == nil
	if useDefaultTasks {
		cfg.Tasks = DefaultTaskConfigs()
	}

	seen := make(map[string]struct{}, len(cfg.Tasks))
	tasks := make([]config.NATSTaskConfig, 0, len(cfg.Tasks))
	for _, task := range cfg.Tasks {
		task.Code = cleanTaskCode(task.Code)
		if task.Code == "" {
			continue
		}
		if _, exists := seen[task.Code]; exists {
			continue
		}
		seen[task.Code] = struct{}{}
		task.Subject = cleanSubjectToken(task.Subject, task.Code+".tasks")
		task.QueueGroup = strings.TrimSpace(task.QueueGroup)
		if task.QueueGroup == "" {
			task.QueueGroup = "mcmods-" + strings.ReplaceAll(task.Code, ".", "-") + "-workers"
		}
		if task.MaxConcurrent <= 0 {
			task.MaxConcurrent = 1
		}
		if task.MaxConcurrent > 1000 {
			task.MaxConcurrent = 1000
		}
		if task.TimeoutSeconds <= 0 {
			task.TimeoutSeconds = 300
		}
		if task.TimeoutSeconds > 86400 {
			task.TimeoutSeconds = 86400
		}
		tasks = append(tasks, task)
	}
	for _, required := range DefaultTaskConfigs() {
		if _, exists := seen[required.Code]; exists {
			continue
		}
		seen[required.Code] = struct{}{}
		tasks = append(tasks, required)
	}
	cfg.Tasks = tasks
	return cfg
}

func (c *Client) Reconfigure(cfg config.NATSConfig) error {
	cfg = NormalizeConfig(cfg)
	var nextConn *nats.Conn
	var connectErr error
	if cfg.Enabled {
		nextConn, connectErr = c.connect(cfg)
	}
	var nextJetStream nats.JetStreamContext
	if connectErr == nil && nextConn != nil && cfg.JetStream.Enabled {
		nextJetStream, connectErr = initializeJetStream(nextConn, cfg)
		if connectErr != nil {
			nextConn.Close()
			nextConn = nil
		}
	}

	c.mu.Lock()
	previousConn := c.conn
	c.cfg = cfg
	c.conn = nextConn
	c.jetStream = nextJetStream
	if connectErr != nil {
		c.lastError = connectErr.Error()
	} else {
		c.lastError = ""
	}
	definitions := make([]subscriptionDefinition, 0, len(c.subscriptions))
	for _, definition := range c.subscriptions {
		definitions = append(definitions, definition)
	}
	c.mu.Unlock()

	if previousConn != nil {
		_ = previousConn.Drain()
		previousConn.Close()
	}
	if connectErr != nil {
		return connectErr
	}
	for _, definition := range definitions {
		if err := c.subscribe(definition); err != nil && !errors.Is(err, ErrTaskDisabled) {
			return err
		}
	}
	return nil
}

func (c *Client) connect(cfg config.NATSConfig) (*nats.Conn, error) {
	options := []nats.Option{
		nats.Name("mcmods-cn-backend"),
		nats.Timeout(3 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			if err != nil {
				c.setLastError(err)
			}
		}),
		nats.ReconnectHandler(func(_ *nats.Conn) { c.setLastError(nil) }),
		nats.ClosedHandler(func(conn *nats.Conn) {
			if err := conn.LastError(); err != nil {
				c.setLastError(err)
			}
		}),
	}
	if cfg.Token != "" {
		options = append(options, nats.Token(cfg.Token))
	} else if cfg.Username != "" || cfg.Password != "" {
		options = append(options, nats.UserInfo(cfg.Username, cfg.Password))
	}
	return nats.Connect(cfg.URL, options...)
}

func (c *Client) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Drain()
		conn.Close()
	}
}

func (c *Client) Status() Status {
	if c == nil {
		return Status{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Status{
		Enabled:       c.cfg.Enabled,
		Connected:     c.conn != nil && c.conn.IsConnected(),
		URL:           c.cfg.URL,
		SubjectPrefix: c.cfg.SubjectPrefix,
		Tasks:         append([]config.NATSTaskConfig(nil), c.cfg.Tasks...),
		LastError:     c.lastError,
		JetStream:     c.jetStream != nil,
	}
}

func (c *Client) PublishTask(ctx context.Context, taskCode string, payload any) error {
	if c == nil {
		return ErrUnavailable
	}
	c.mu.RLock()
	conn := c.conn
	cfg := c.cfg
	task, exists := findTask(cfg.Tasks, taskCode)
	c.mu.RUnlock()
	if !exists || !task.Enabled {
		return ErrTaskDisabled
	}
	if conn == nil || !conn.IsConnected() {
		return ErrUnavailable
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.publishRaw(ctx, conn, cfg, task, randomEventID(), raw)
}

type broadcastHandler func(context.Context, string, []byte)

// PublishEvent publishes an already-encoded event envelope. JetStream's
// acknowledgement is required before the caller may mark an outbox row sent.
func (c *Client) PublishEvent(ctx context.Context, taskCode, eventID string, raw []byte) error {
	if c == nil {
		return ErrUnavailable
	}
	c.mu.RLock()
	conn, cfg, js := c.conn, c.cfg, c.jetStream
	task, exists := findTask(cfg.Tasks, taskCode)
	c.mu.RUnlock()
	if !exists || !task.Enabled {
		return ErrTaskDisabled
	}
	if conn == nil || !conn.IsConnected() {
		return ErrUnavailable
	}
	if strings.TrimSpace(eventID) == "" {
		return errors.New("event ID is required")
	}
	if js != nil {
		message := nats.NewMsg(fullSubject(cfg.SubjectPrefix, task.Subject))
		message.Data = raw
		message.Header.Set(nats.MsgIdHdr, eventID)
		message.Header.Set("MCMods-Event-ID", eventID)
		publishCtx, cancel := context.WithTimeout(ctx, cfg.JetStream.PublishTimeout)
		defer cancel()
		_, err := js.PublishMsg(message, nats.Context(publishCtx))
		c.setLastError(err)
		return err
	}
	return c.publishRaw(ctx, conn, cfg, task, eventID, raw)
}

// HandleLocally is the reliable no-JetStream fallback used by the PostgreSQL
// outbox dispatcher. Core NATS may still be used as a wake-up signal, but the
// task is processed from the claimed database row before it is marked sent.
func (c *Client) HandleLocally(ctx context.Context, taskCode, eventID string, raw []byte) error {
	if c == nil {
		return ErrUnavailable
	}
	c.mu.RLock()
	definition, ok := c.subscriptions[cleanTaskCode(taskCode)]
	c.mu.RUnlock()
	if !ok {
		return ErrUnavailable
	}
	payload, envelope := UnwrapEvent(raw)
	if envelope != nil {
		eventID = envelope.EventID
	}
	return definition.handler(WithEventID(ctx, eventID), payload)
}

func (c *Client) PublishBroadcast(ctx context.Context, subject string, payload any) error {
	if c == nil {
		return ErrUnavailable
	}
	c.mu.RLock()
	conn, cfg := c.conn, c.cfg
	c.mu.RUnlock()
	if conn == nil || !conn.IsConnected() || !cfg.Realtime {
		return ErrUnavailable
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		done <- conn.Publish(fullSubject(cfg.SubjectPrefix, "realtime."+cleanSubjectToken(subject, "event")), raw)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err = <-done:
		return err
	}
}

func (c *Client) SubscribeBroadcast(subject string, handler broadcastHandler) error {
	if c == nil || handler == nil {
		return ErrUnavailable
	}
	c.mu.RLock()
	conn, cfg := c.conn, c.cfg
	c.mu.RUnlock()
	if conn == nil || !conn.IsConnected() || !cfg.Realtime {
		return ErrUnavailable
	}
	_, err := conn.Subscribe(fullSubject(cfg.SubjectPrefix, "realtime."+cleanSubjectToken(subject, ">")), func(message *nats.Msg) {
		handler(context.Background(), message.Subject, message.Data)
	})
	if err == nil {
		err = conn.Flush()
	}
	return err
}

func (c *Client) publishRaw(ctx context.Context, conn *nats.Conn, cfg config.NATSConfig, task config.NATSTaskConfig, eventID string, raw []byte) error {
	message := nats.NewMsg(fullSubject(cfg.SubjectPrefix, task.Subject))
	message.Data = raw
	message.Header.Set("MCMods-Event-ID", eventID)
	done := make(chan error, 1)
	go func() { done <- conn.PublishMsg(message) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		c.setLastError(err)
		return err
	}
}

func (c *Client) SubscribeTask(taskCode string, handler taskHandler) error {
	if c == nil {
		return ErrUnavailable
	}
	taskCode = cleanTaskCode(taskCode)
	if taskCode == "" || handler == nil {
		return nil
	}
	definition := subscriptionDefinition{taskCode: taskCode, handler: handler}
	c.mu.Lock()
	_, alreadyRegistered := c.subscriptions[taskCode]
	c.subscriptions[taskCode] = definition
	c.mu.Unlock()
	if alreadyRegistered {
		return nil
	}
	return c.subscribe(definition)
}

func (c *Client) subscribe(definition subscriptionDefinition) error {
	c.mu.RLock()
	conn, js := c.conn, c.jetStream
	cfg := c.cfg
	task, exists := findTask(cfg.Tasks, definition.taskCode)
	c.mu.RUnlock()
	if !exists || !task.Enabled {
		return ErrTaskDisabled
	}
	if conn == nil || !conn.IsConnected() {
		return ErrUnavailable
	}
	sem := make(chan struct{}, task.MaxConcurrent)
	handle := func(msg *nats.Msg) {
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.TimeoutSeconds)*time.Second)
			defer cancel()
			payload, envelope := UnwrapEvent(msg.Data)
			eventID := msg.Header.Get("MCMods-Event-ID")
			if envelope != nil {
				eventID = envelope.EventID
			}
			ctx = WithEventID(ctx, eventID)
			if err := definition.handler(ctx, payload); err != nil {
				c.setLastError(err)
				if js != nil {
					metadata, _ := msg.Metadata()
					if metadata != nil && int(metadata.NumDelivered) >= cfg.JetStream.MaxDeliver {
						c.mu.RLock()
						sink := c.deadLetterSink
						c.mu.RUnlock()
						if eventID == "" {
							eventID = randomEventID()
						}
						eventType := definition.taskCode + ".failed"
						if envelope != nil && envelope.EventType != "" {
							eventType = envelope.EventType
						}
						if sink != nil && sink(ctx, DeadLetter{
							EventID: eventID, EventType: eventType, TaskCode: definition.taskCode,
							FailureStage: "consumer:" + definition.taskCode, LastError: err.Error(),
							Payload: msg.Data, Attempts: int(metadata.NumDelivered),
						}) != nil {
							_ = msg.Nak()
							return
						}
						dead := nats.NewMsg(fullSubject(cfg.SubjectPrefix, "dead-letter."+definition.taskCode))
						dead.Data, dead.Header = msg.Data, msg.Header
						_, _ = js.PublishMsg(dead)
						_ = msg.Term()
					} else {
						_ = msg.Nak()
					}
				}
				return
			}
			if js != nil {
				_ = msg.Ack()
			}
		}()
	}
	var err error
	if js != nil {
		durable := cleanDurable(task.QueueGroup)
		_, err = js.QueueSubscribe(fullSubject(cfg.SubjectPrefix, task.Subject), task.QueueGroup, handle,
			nats.Durable(durable), nats.ManualAck(), nats.AckExplicit(), nats.AckWait(cfg.JetStream.AckWait),
			nats.MaxDeliver(cfg.JetStream.MaxDeliver), nats.BindStream(cfg.JetStream.Stream))
	} else {
		_, err = conn.QueueSubscribe(fullSubject(cfg.SubjectPrefix, task.Subject), task.QueueGroup, handle)
	}
	if err != nil {
		c.setLastError(err)
		return err
	}
	if err := conn.Flush(); err != nil {
		c.setLastError(err)
		return err
	}
	return nil
}

func initializeJetStream(conn *nats.Conn, cfg config.NATSConfig) (nats.JetStreamContext, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, err
	}
	subject := fullSubject(cfg.SubjectPrefix, ">")
	streamCfg := &nats.StreamConfig{Name: cfg.JetStream.Stream, Subjects: []string{subject}, Retention: nats.LimitsPolicy, Storage: nats.FileStorage, Duplicates: 10 * time.Minute}
	if _, err = js.StreamInfo(cfg.JetStream.Stream); errors.Is(err, nats.ErrStreamNotFound) {
		_, err = js.AddStream(streamCfg)
	} else if err == nil {
		_, err = js.UpdateStream(streamCfg)
	}
	return js, err
}

func randomEventID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return hex.EncodeToString(value)
}

func cleanDurable(value string) string {
	value = strings.NewReplacer(".", "-", " ", "-").Replace(strings.TrimSpace(value))
	if value == "" {
		return "mcmods-workers"
	}
	return value
}

func (c *Client) setLastError(err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		c.lastError = ""
		return
	}
	c.lastError = err.Error()
}

func findTask(tasks []config.NATSTaskConfig, code string) (config.NATSTaskConfig, bool) {
	code = cleanTaskCode(code)
	for _, task := range tasks {
		if task.Code == code {
			return task, true
		}
	}
	return config.NATSTaskConfig{}, false
}

func fullSubject(prefix string, subject string) string {
	prefix = cleanSubjectToken(prefix, "")
	subject = cleanSubjectToken(subject, "")
	if prefix == "" {
		return subject
	}
	if subject == "" {
		return prefix
	}
	return prefix + "." + subject
}

func cleanTaskCode(value string) string {
	return strings.ToLower(cleanSubjectToken(value, ""))
}

func cleanSubjectToken(value string, fallback string) string {
	value = strings.Trim(strings.TrimSpace(value), ".")
	value = strings.ReplaceAll(value, " ", ".")
	if value == "" {
		return fallback
	}
	return value
}
