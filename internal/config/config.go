package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr           string
	Env            string
	FrontendOrigin string
	JWTSecret      string
	JWTTTL         time.Duration
	DB             DBConfig
	SMTP           SMTPConfig
	NATS           NATSConfig
	Redis          RedisConfig
}

type DBConfig struct {
	Host         string
	Port         string
	Name         string
	User         string
	Password     string
	SSLMode      string
	URL          string
	ResetOnStart bool
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	UseTLS   bool
}

type NATSConfig struct {
	Enabled       bool             `json:"enabled"`
	URL           string           `json:"url"`
	Username      string           `json:"username"`
	Password      string           `json:"password,omitempty"`
	Token         string           `json:"token,omitempty"`
	SubjectPrefix string           `json:"subjectPrefix"`
	Tasks         []NATSTaskConfig `json:"tasks"`
}

type RedisConfig struct {
	Enabled  bool
	Addr     string
	Username string
	Password string
	DB       int
	Prefix   string
	TTL      time.Duration
}

type NATSTaskConfig struct {
	Code           string `json:"code"`
	Enabled        bool   `json:"enabled"`
	Subject        string `json:"subject"`
	QueueGroup     string `json:"queueGroup"`
	MaxConcurrent  int    `json:"maxConcurrent"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

func Load() Config {
	loadDotEnvUpwards(".env")

	return Config{
		Addr:           getenv("APP_ADDR", ":8080"),
		Env:            getenv("APP_ENV", "development"),
		FrontendOrigin: getenv("FRONTEND_ORIGIN", "http://localhost:3000"),
		JWTSecret:      getenv("JWT_SECRET", "change-this-in-production"),
		JWTTTL:         time.Duration(getenvInt("JWT_TTL_HOURS", 24)) * time.Hour,
		DB: DBConfig{
			Host:         getenv("DB_HOST", "127.0.0.1"),
			Port:         getenv("DB_PORT", "5432"),
			Name:         getenv("DB_NAME", "mcmods"),
			User:         getenv("DB_USER", "mcmods"),
			Password:     os.Getenv("DB_PASSWORD"),
			SSLMode:      getenv("DB_SSLMODE", "disable"),
			URL:          os.Getenv("DATABASE_URL"),
			ResetOnStart: getenvBool("DB_RESET_ON_START", false),
		},
		SMTP: SMTPConfig{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     getenvInt("SMTP_PORT", 587),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     getenv("SMTP_FROM", "no-reply@mcmods.cn"),
			UseTLS:   getenvBool("SMTP_USE_TLS", true),
		},
		NATS: NATSConfig{
			Enabled:       getenvBool("NATS_ENABLED", true),
			URL:           getenv("NATS_URL", "nats://127.0.0.1:4222"),
			Username:      os.Getenv("NATS_USERNAME"),
			Password:      os.Getenv("NATS_PASSWORD"),
			Token:         os.Getenv("NATS_TOKEN"),
			SubjectPrefix: getenv("NATS_SUBJECT_PREFIX", "mcmods"),
			Tasks: []NATSTaskConfig{
				{
					Code:           "ai",
					Enabled:        true,
					Subject:        "ai.tasks",
					QueueGroup:     getenv("NATS_QUEUE_GROUP", "mcmods-ai-workers"),
					MaxConcurrent:  getenvInt("AI_MAX_CONCURRENT", 2),
					TimeoutSeconds: 300,
				},
				{
					Code:           "notifications",
					Enabled:        true,
					Subject:        "notifications.events",
					QueueGroup:     getenv("NOTIFICATION_NATS_QUEUE_GROUP", "mcmods-notification-workers"),
					MaxConcurrent:  getenvInt("NOTIFICATION_MAX_CONCURRENT", 8),
					TimeoutSeconds: 300,
				},
			},
		},
		Redis: RedisConfig{
			Enabled:  getenvBool("REDIS_ENABLED", false),
			Addr:     getenv("REDIS_ADDR", "127.0.0.1:6379"),
			Username: os.Getenv("REDIS_USERNAME"),
			Password: os.Getenv("REDIS_PASSWORD"),
			DB:       getenvInt("REDIS_DB", 0),
			Prefix:   getenv("REDIS_PREFIX", "mcmods:query:"),
			TTL:      time.Duration(getenvInt("REDIS_QUERY_TTL_SECONDS", 120)) * time.Second,
		},
	}
}

func (db DBConfig) ConnString() string {
	if strings.TrimSpace(db.URL) != "" {
		return db.URL
	}

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(db.User, db.Password),
		Host:   fmt.Sprintf("%s:%s", db.Host, db.Port),
		Path:   db.Name,
	}
	q := u.Query()
	q.Set("sslmode", db.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}

func getenv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getenvBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimPrefix(strings.TrimSpace(key), "\ufeff")
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func loadDotEnvUpwards(name string) {
	dir, err := os.Getwd()
	if err != nil {
		loadDotEnv(name)
		return
	}

	for {
		loadDotEnv(filepath.Join(dir, name))
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}
