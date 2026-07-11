package queue

import (
	"context"
	"encoding/json"
	"errors"
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
}

type taskHandler func(context.Context, []byte) error

type subscriptionDefinition struct {
	taskCode string
	handler  taskHandler
}

type Client struct {
	mu            sync.RWMutex
	cfg           config.NATSConfig
	conn          *nats.Conn
	lastError     string
	subscriptions map[string]subscriptionDefinition
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
	}
}

func NormalizeConfig(cfg config.NATSConfig) config.NATSConfig {
	cfg.URL = strings.TrimSpace(cfg.URL)
	if cfg.URL == "" {
		cfg.URL = nats.DefaultURL
	}
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.SubjectPrefix = cleanSubjectToken(cfg.SubjectPrefix, "mcmods")
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

	c.mu.Lock()
	previousConn := c.conn
	c.cfg = cfg
	c.conn = nextConn
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
	done := make(chan error, 1)
	go func() {
		done <- conn.Publish(fullSubject(cfg.SubjectPrefix, task.Subject), raw)
	}()
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
	conn := c.conn
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
	_, err := conn.QueueSubscribe(fullSubject(cfg.SubjectPrefix, task.Subject), task.QueueGroup, func(msg *nats.Msg) {
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(task.TimeoutSeconds)*time.Second)
			defer cancel()
			if err := definition.handler(ctx, msg.Data); err != nil {
				c.setLastError(err)
			}
		}()
	})
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
