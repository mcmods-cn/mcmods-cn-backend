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
	Realtime      bool                    `json:"realtime"`
	RealtimeReady bool                    `json:"realtimeReady"`
	Recovering    bool                    `json:"recovering"`
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

type broadcastSubscriptionDefinition struct {
	subject string
	handler broadcastHandler
}

type activationGate struct {
	ready    chan struct{}
	canceled chan struct{}
}

func newActivationGate() *activationGate {
	return &activationGate{ready: make(chan struct{}), canceled: make(chan struct{})}
}

func (gate *activationGate) wait() bool {
	if gate == nil {
		return true
	}
	select {
	case <-gate.ready:
		return true
	case <-gate.canceled:
		return false
	}
}

type DeadLetter struct {
	EventID, EventType, TaskCode, FailureStage, LastError, AggregateType, AggregateID string
	Payload                                                                           []byte
	Attempts                                                                          int
}

type DeadLetterSink func(context.Context, DeadLetter) error

type Client struct {
	mu                     sync.RWMutex
	reconfigureMu          sync.Mutex
	cfg                    config.NATSConfig
	conn                   *nats.Conn
	jetStream              nats.JetStreamContext
	generation             uint64
	lastError              string
	ctx                    context.Context
	cancel                 context.CancelFunc
	done                   chan struct{}
	closed                 bool
	subscriptionsPending   bool
	liveTasks              map[string]struct{}
	liveBroadcasts         map[string]struct{}
	subscriptions          map[string]subscriptionDefinition
	broadcastSubscriptions map[string]broadcastSubscriptionDefinition
	deadLetterSink         DeadLetterSink
}

func New(ctx context.Context, cfg config.NATSConfig) *Client {
	clientCtx, cancel := context.WithCancel(ctx)
	client := &Client{
		cfg:                    NormalizeConfig(cfg),
		ctx:                    clientCtx,
		cancel:                 cancel,
		done:                   make(chan struct{}),
		liveTasks:              make(map[string]struct{}),
		liveBroadcasts:         make(map[string]struct{}),
		subscriptions:          make(map[string]subscriptionDefinition),
		broadcastSubscriptions: make(map[string]broadcastSubscriptionDefinition),
	}
	if err := client.Reconfigure(cfg); err != nil {
		// Startup has no previous live connection to preserve. Keep the desired
		// normalized configuration so the PostgreSQL dispatcher can continue to
		// use registered local handlers while status exposes the connection error.
		client.setLastError(err)
	}
	go client.runRecovery(clientCtx)
	return client
}

// Once connected, nats.go owns reconnect and subscription replay. This loop
// handles the missing initial/terminal connection and failed registrations,
// using the same staged runtime swap as an explicit configuration change.
func (c *Client) runRecovery(ctx context.Context) {
	delay := 2 * time.Second
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			c.Close()
			return
		case <-c.done:
			return
		case <-timer.C:
			err := c.recoverRuntime()
			if err != nil {
				delay = min(delay*2, 30*time.Second)
			} else {
				delay = 2 * time.Second
			}
			timer.Reset(delay)
		}
	}
}

func (c *Client) recoverRuntime() error {
	c.reconfigureMu.Lock()
	defer c.reconfigureMu.Unlock()
	c.mu.RLock()
	cfg, conn, closed, pending := c.cfg, c.conn, c.closed, c.subscriptionsPending
	c.mu.RUnlock()
	if closed || !cfg.Enabled || c.ctx.Err() != nil {
		return nil
	}
	if conn != nil && !conn.IsClosed() && (!conn.IsConnected() || !pending) {
		return nil // Do not compete with the library's active reconnect loop.
	}
	err := c.reconfigureLocked(cfg, nil)
	if err != nil {
		c.setLastError(err) // Still serialized with any explicit configuration swap.
	}
	return err
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
		{
			Code:           "comment_log_attachment",
			Enabled:        true,
			Subject:        "comments.log-attachments",
			QueueGroup:     "mcmods-comment-log-workers",
			MaxConcurrent:  2,
			TimeoutSeconds: 300,
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
	return c.ReconfigureWithPersistence(cfg, nil)
}

// ReconfigureWithPersistence prepares a complete candidate runtime before the
// persistence callback is invoked. The active connection, JetStream context,
// task subscriptions, broadcast subscriptions, and configuration are swapped
// only after both preparation and persistence succeed. The callback receives
// the exact normalized configuration that will become active.
func (c *Client) ReconfigureWithPersistence(cfg config.NATSConfig, persist func(config.NATSConfig) error) error {
	if c == nil {
		return ErrUnavailable
	}
	c.reconfigureMu.Lock()
	defer c.reconfigureMu.Unlock()
	return c.reconfigureLocked(cfg, persist)
}

// Caller holds reconfigureMu, including while reading the desired recovery
// configuration, so retries cannot restore a stale configuration after a PUT.
func (c *Client) reconfigureLocked(cfg config.NATSConfig, persist func(config.NATSConfig) error) error {
	c.mu.RLock()
	closed := c.closed
	c.mu.RUnlock()
	if closed {
		return ErrUnavailable
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}

	cfg = NormalizeConfig(cfg)
	var nextConn *nats.Conn
	var connectErr error
	if cfg.Enabled {
		nextConn, connectErr = c.connect(cfg)
	}
	var nextJetStream nats.JetStreamContext
	cleanupJetStream := func() error { return nil }
	if connectErr == nil && nextConn != nil && cfg.JetStream.Enabled {
		nextJetStream, cleanupJetStream, connectErr = initializeJetStream(nextConn, cfg)
	}
	if connectErr != nil {
		connectErr = errors.Join(connectErr, cleanupJetStream())
		if nextConn != nil {
			nextConn.Close()
		}
		return connectErr
	}

	c.mu.RLock()
	definitions := make([]subscriptionDefinition, 0, len(c.subscriptions))
	for _, definition := range c.subscriptions {
		definitions = append(definitions, definition)
	}
	broadcastDefinitions := make([]broadcastSubscriptionDefinition, 0, len(c.broadcastSubscriptions))
	for _, definition := range c.broadcastSubscriptions {
		broadcastDefinitions = append(broadcastDefinitions, definition)
	}
	nextGeneration := c.generation + 1
	c.mu.RUnlock()

	gate := newActivationGate()
	discard := func(cause error) error {
		close(gate.canceled)
		cleanupErr := cleanupJetStream()
		if nextConn != nil {
			nextConn.Close()
		}
		return errors.Join(cause, cleanupErr)
	}
	liveTasks := make(map[string]struct{}, len(definitions))
	liveBroadcasts := make(map[string]struct{}, len(broadcastDefinitions))
	if nextConn != nil {
		if nextJetStream != nil {
			if err := c.subscribeDeadLettersOn(nextConn, nextJetStream, cfg, nextGeneration, gate); err != nil {
				return discard(err)
			}
		}
		for _, definition := range definitions {
			err := c.subscribeTaskOn(nextConn, nextJetStream, cfg, definition, nextGeneration, gate)
			if err != nil && !errors.Is(err, ErrTaskDisabled) {
				return discard(err)
			}
			if err == nil {
				liveTasks[definition.taskCode] = struct{}{}
			}
		}
		for _, definition := range broadcastDefinitions {
			err := c.subscribeBroadcastOn(nextConn, cfg, definition, nextGeneration, gate)
			if err != nil && !errors.Is(err, ErrTaskDisabled) {
				return discard(err)
			}
			if err == nil {
				liveBroadcasts[definition.subject] = struct{}{}
			}
		}
	}
	if err := c.ctx.Err(); err != nil {
		return discard(err)
	}
	if persist != nil {
		if err := persist(cfg); err != nil {
			return discard(err)
		}
	}

	c.mu.Lock()
	previousConn := c.conn
	c.cfg = cfg
	c.conn = nextConn
	c.jetStream = nextJetStream
	c.generation = nextGeneration
	c.lastError = ""
	c.subscriptionsPending = false
	c.liveTasks = liveTasks
	c.liveBroadcasts = liveBroadcasts
	c.mu.Unlock()
	close(gate.ready)

	if previousConn != nil {
		_ = previousConn.Drain()
		previousConn.Close()
	}
	return nil
}

func (c *Client) connect(cfg config.NATSConfig) (*nats.Conn, error) {
	options := []nats.Option{
		nats.Name("mcmods-cn-backend"),
		nats.Timeout(3 * time.Second),
		nats.ReconnectWait(2 * time.Second),
		nats.MaxReconnects(-1),
		nats.ErrorHandler(func(conn *nats.Conn, _ *nats.Subscription, err error) { c.setAsyncConnectionError(conn, err) }),
		nats.DisconnectErrHandler(func(conn *nats.Conn, err error) {
			if err != nil {
				c.setConnectionError(conn, err)
			}
		}),
		nats.ReconnectHandler(func(conn *nats.Conn) { c.setConnectionError(conn, nil) }),
		nats.ClosedHandler(func(conn *nats.Conn) {
			if err := conn.LastError(); err != nil {
				c.setConnectionError(conn, err)
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
	c.reconfigureMu.Lock()
	defer c.reconfigureMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		close(c.done)
	}
	conn := c.conn
	c.conn = nil
	c.jetStream = nil
	c.liveTasks = nil
	c.liveBroadcasts = nil
	c.generation++
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
	connected := c.conn != nil && c.conn.IsConnected()
	return Status{
		Enabled:       c.cfg.Enabled,
		Connected:     connected,
		Realtime:      c.cfg.Realtime,
		RealtimeReady: connected && c.cfg.Enabled && c.cfg.Realtime && len(c.broadcastSubscriptions) > 0 && len(c.liveBroadcasts) == len(c.broadcastSubscriptions),
		Recovering:    c.cfg.Enabled && !c.closed && (!connected || c.subscriptionsPending),
		URL:           RedactURL(c.cfg.URL),
		SubjectPrefix: c.cfg.SubjectPrefix,
		Tasks:         append([]config.NATSTaskConfig(nil), c.cfg.Tasks...),
		LastError:     redactDiagnostic(c.lastError, c.cfg),
		JetStream:     c.jetStream != nil,
	}
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
	closed := c.closed
	c.mu.RUnlock()
	if closed || !ok {
		return ErrUnavailable
	}
	payload, envelope, err := UnwrapEvent(raw)
	if err != nil {
		return err
	}
	eventID = envelope.EventID
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
	subject = cleanSubjectToken(subject, ">")
	definition := broadcastSubscriptionDefinition{subject: subject, handler: handler}
	c.reconfigureMu.Lock()
	defer c.reconfigureMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrUnavailable
	}
	_, alreadyRegistered := c.broadcastSubscriptions[subject]
	_, live := c.liveBroadcasts[subject]
	c.broadcastSubscriptions[subject] = definition
	conn, cfg, generation := c.conn, c.cfg, c.generation
	c.mu.Unlock()
	if alreadyRegistered {
		if !cfg.Enabled || !cfg.Realtime {
			return ErrTaskDisabled
		}
		if !live || conn == nil || !conn.IsConnected() {
			return ErrUnavailable
		}
		return nil
	}
	err := c.subscribeBroadcastOn(conn, cfg, definition, generation, nil)
	c.mu.Lock()
	if err == nil {
		c.liveBroadcasts[subject] = struct{}{}
	} else if !errors.Is(err, ErrTaskDisabled) {
		c.subscriptionsPending = true
		c.lastError = err.Error()
	}
	c.mu.Unlock()
	return err
}

func (c *Client) subscribeBroadcastOn(conn *nats.Conn, cfg config.NATSConfig, definition broadcastSubscriptionDefinition, generation uint64, gate *activationGate) error {
	if !cfg.Enabled || !cfg.Realtime {
		return ErrTaskDisabled
	}
	if conn == nil || !conn.IsConnected() {
		return ErrUnavailable
	}
	_, err := conn.Subscribe(fullSubject(cfg.SubjectPrefix, "realtime."+definition.subject), func(message *nats.Msg) {
		if !gate.wait() || !c.isGenerationActive(generation) {
			return
		}
		definition.handler(context.Background(), message.Subject, message.Data)
	})
	if err == nil {
		err = conn.FlushTimeout(3 * time.Second)
	}
	if err == nil {
		// SUB permission errors are asynchronous and do not close the socket.
		// The server processes SUB before the Flush PONG, so inspect its error
		// before declaring the registration (or a candidate runtime) active.
		err = conn.LastError()
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
	c.reconfigureMu.Lock()
	defer c.reconfigureMu.Unlock()
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrUnavailable
	}
	_, alreadyRegistered := c.subscriptions[taskCode]
	_, live := c.liveTasks[taskCode]
	c.subscriptions[taskCode] = definition
	conn, js, cfg, generation := c.conn, c.jetStream, c.cfg, c.generation
	c.mu.Unlock()
	if alreadyRegistered {
		if task, exists := findTask(cfg.Tasks, taskCode); !exists || !task.Enabled {
			return ErrTaskDisabled
		}
		if !live || conn == nil || !conn.IsConnected() {
			return ErrUnavailable
		}
		return nil
	}
	err := c.subscribeTaskOn(conn, js, cfg, definition, generation, nil)
	c.mu.Lock()
	if err == nil {
		c.liveTasks[taskCode] = struct{}{}
	} else if !errors.Is(err, ErrTaskDisabled) {
		c.subscriptionsPending = true
		c.lastError = err.Error()
	}
	c.mu.Unlock()
	return err
}

func (c *Client) subscribeTaskOn(conn *nats.Conn, js nats.JetStreamContext, cfg config.NATSConfig, definition subscriptionDefinition, generation uint64, gate *activationGate) error {
	task, exists := findTask(cfg.Tasks, definition.taskCode)
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
			if !gate.wait() || !c.isGenerationActive(generation) {
				if js != nil {
					_ = msg.Nak()
				}
				return
			}
			ctx, cancel := context.WithTimeout(c.ctx, time.Duration(task.TimeoutSeconds)*time.Second)
			defer cancel()
			payload, envelope, handleErr := UnwrapEvent(msg.Data)
			eventID := msg.Header.Get("MCMods-Event-ID")
			if envelope != nil {
				eventID = envelope.EventID
			}
			ctx = WithEventID(ctx, eventID)
			if handleErr == nil {
				handleErr = definition.handler(ctx, payload)
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
			nats.MaxDeliver(cfg.JetStream.MaxDeliver), nats.BackOff(jetStreamBackoff(cfg.JetStream.AckWait, cfg.JetStream.MaxDeliver)),
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

func initializeJetStream(conn *nats.Conn, cfg config.NATSConfig) (nats.JetStreamContext, func() error, error) {
	js, err := conn.JetStream()
	if err != nil {
		return nil, func() error { return nil }, err
	}
	subject := fullSubject(cfg.SubjectPrefix, ">")
	streamCfg := &nats.StreamConfig{Name: cfg.JetStream.Stream, Subjects: []string{subject}, Retention: nats.LimitsPolicy, Storage: nats.FileStorage, Duplicates: 10 * time.Minute}
	var info *nats.StreamInfo
	if info, err = js.StreamInfo(cfg.JetStream.Stream); errors.Is(err, nats.ErrStreamNotFound) {
		_, err = js.AddStream(streamCfg)
		if err != nil {
			return nil, func() error { return nil }, err
		}
		cleanup := func() error { return js.DeleteStream(cfg.JetStream.Stream) }
		return js, cleanup, nil
	}
	if err != nil {
		return nil, func() error { return nil }, err
	}
	if !streamCoversSubject(info.Config.Subjects, subject) {
		return nil, func() error { return nil }, errors.New("existing JetStream stream does not cover the configured NATS subject prefix")
	}
	return js, func() error { return nil }, nil
}

func streamCoversSubject(subjects []string, required string) bool {
	for _, subject := range subjects {
		if subject == required {
			return true
		}
	}
	return false
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

func (c *Client) setConnectionError(conn *nats.Conn, err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != conn {
		return
	}
	if err == nil {
		c.lastError = ""
		return
	}
	c.lastError = err.Error()
}

func (c *Client) setAsyncConnectionError(conn *nats.Conn, err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn != conn {
		return
	}
	c.lastError = err.Error()
	if errors.Is(err, nats.ErrPermissionViolation) && strings.Contains(err.Error(), `Subscription to "`) {
		c.subscriptionsPending = true
		c.liveTasks = make(map[string]struct{})
		c.liveBroadcasts = make(map[string]struct{})
	}
}

func (c *Client) setGenerationError(generation uint64, err error) {
	if c == nil || err == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation == generation {
		c.lastError = err.Error()
	}
}

func (c *Client) isGenerationActive(generation uint64) bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.closed && c.generation == generation
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
