package config

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultDevelopmentSettingsEncryptionKey = "development-only-settings-encryption-key"
const defaultDevelopmentAntiAbuseHMACSecret = "development-only-change-this-anti-abuse-hmac-secret"
const defaultDevelopmentAntiAbuseIPSecret = "development-only-change-this-anti-abuse-ip-hash-secret"

type Config struct {
	Addr                  string
	Env                   string
	ReplicaCount          int
	FrontendOrigin        string
	TrustedProxyCIDRs     []string
	JWTSecret             string
	SettingsEncryptionKey string
	JWTTTL                time.Duration
	DB                    DBConfig
	Activity              ActivityConfig
	Favorite              FavoriteConfig
	FavoriteExport        FavoriteExportConfig
	Sticker               StickerConfig
	SMTP                  SMTPConfig
	NATS                  NATSConfig
	Redis                 RedisConfig
	AntiAbuse             AntiAbuseConfig
	Typesense             TypesenseConfig
	Yggdrasil             YggdrasilConfig
}

type FavoriteConfig struct {
	MaxCollectionsPerUser int
	MaxItemsPerUser       int
}

type FavoriteExportConfig struct {
	MaxActivePerUser int
	MaxDailyPerUser  int
	ArtifactTTL      time.Duration
	LeaseTTL         time.Duration
	MaxBuildAttempts int
}

type StickerConfig struct {
	MaxBytes            int64
	MaxEdge             int
	MaxPixels           int64
	MaxGIFFrames        int
	MaxGIFDecodedPixels int64
	MaxGIFDuration      time.Duration
	MaxPacks            int
	MaxStickersPerPack  int
	MaxCatalogItems     int
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
	ResetConfirm string
	MaxConns     int32
	MinConns     int32
}

// ActivityConfig controls the bounded best-effort view pipeline and the
// durable outbox used by actions that must survive an application restart.
// Durations are intentionally expressed as time.Duration after environment
// parsing so callers cannot disagree about units.
type ActivityConfig struct {
	BatchSize             int
	QueueCapacity         int
	FlushInterval         time.Duration
	RetryMinDelay         time.Duration
	RetryMaxDelay         time.Duration
	WriteTimeout          time.Duration
	DurableEnqueueTimeout time.Duration
	DBMaxConns            int32
	DBMinConns            int32
}

type SMTPConfig struct {
	Enabled  bool
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
	OutboxEnabled bool             `json:"outboxEnabled"`
	JetStream     JetStreamConfig  `json:"jetStream"`
	Realtime      bool             `json:"realtime"`
}

type JetStreamConfig struct {
	Enabled        bool          `json:"enabled"`
	Stream         string        `json:"stream"`
	MaxDeliver     int           `json:"maxDeliver"`
	AckWait        time.Duration `json:"ackWait"`
	PublishTimeout time.Duration `json:"publishTimeout"`
}

type RedisConfig struct {
	Enabled                  bool
	Required                 bool
	RateLimitFailClosed      bool
	Addr                     string
	Username                 string
	Password                 string
	DB                       int
	Prefix                   string
	Namespace                string
	TTL                      time.Duration
	PoolSize                 int
	MinIdleConns             int
	DialTimeout              time.Duration
	ReadTimeout              time.Duration
	WriteTimeout             time.Duration
	AuthSessionCacheEnabled  bool
	RBACCacheEnabled         bool
	AuthRateLimitEnabled     bool
	PresenceEnabled          bool
	UnreadCounterEnabled     bool
	SettingsCacheEnabled     bool
	UserCardCacheEnabled     bool
	SessionTTL               time.Duration
	RBACCacheTTL             time.Duration
	PresenceTTL              time.Duration
	PresenceSnapshotInterval time.Duration
	UnreadTTL                time.Duration
	UnreadReconcileInterval  time.Duration
	UnreadReconcileBatchSize int
	UserCardTTL              time.Duration
}

type AntiAbuseConfig struct {
	Enabled                  bool
	HMACSecret               string
	IPHashSecret             string
	ChallengeProvider        string
	TurnstileSiteKey         string
	TurnstileSecretKey       string
	TurnstileVerifyURL       string
	FormTokenTTL             time.Duration
	FormMinimumAge           time.Duration
	ChallengeTTL             time.Duration
	EventRetentionDays       int
	FingerprintRetentionDays int
	DNSLookupTimeout         time.Duration
	DNSCacheTTL              time.Duration
}

type TypesenseConfig struct {
	Enabled          bool
	URL              string
	APIKey           string
	CollectionPrefix string
	Timeout          time.Duration
}

type YggdrasilConfig struct {
	Enabled           bool
	PublicBaseURL     string
	TextureBaseURL    string
	ServerName        string
	PrivateKeyBase64  string
	TrustedProxyCIDRs []string
	TokenTTL          time.Duration
	MaxTokens         int
	JoinTTL           time.Duration
	TextureMaxBytes   int64
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
	// Isolated test tools must not silently import a developer's real service
	// credentials from this directory or its ancestors.
	if !getenvBool("MCMODS_SKIP_DOTENV", false) {
		loadDotEnvUpwards(".env")
	}
	jwtSecret := getenv("JWT_SECRET", "change-this-in-production")
	replicaCount := getenvInt("APP_REPLICA_COUNT", 1)

	cfg := Config{
		Addr:                  getenv("APP_ADDR", ":8080"),
		Env:                   getenv("APP_ENV", "development"),
		ReplicaCount:          replicaCount,
		FrontendOrigin:        strings.TrimRight(getenv("FRONTEND_ORIGIN", "http://localhost:3000"), "/"),
		TrustedProxyCIDRs:     splitCommaSeparated(os.Getenv("TRUSTED_PROXY_CIDRS")),
		JWTSecret:             jwtSecret,
		SettingsEncryptionKey: getenv("SETTINGS_ENCRYPTION_KEY", defaultDevelopmentSettingsEncryptionKey),
		JWTTTL:                time.Duration(getenvInt("JWT_TTL_HOURS", 24)) * time.Hour,
		DB: DBConfig{
			Host:         getenv("DB_HOST", "127.0.0.1"),
			Port:         getenv("DB_PORT", "5432"),
			Name:         getenv("DB_NAME", "mcmods"),
			User:         getenv("DB_USER", "mcmods"),
			Password:     os.Getenv("DB_PASSWORD"),
			SSLMode:      getenv("DB_SSLMODE", "disable"),
			URL:          os.Getenv("DATABASE_URL"),
			ResetOnStart: getenvBool("DB_RESET_ON_START", false),
			ResetConfirm: strings.TrimSpace(os.Getenv("DB_RESET_CONFIRM")),
			MaxConns:     int32(getenvInt("DB_MAX_CONNS", 12)),
			MinConns:     int32(getenvInt("DB_MIN_CONNS", 2)),
		},
		Activity: ActivityConfig{
			BatchSize:             getenvInt("ACTIVITY_BATCH_SIZE", 256),
			QueueCapacity:         getenvInt("ACTIVITY_QUEUE_CAPACITY", 4096),
			FlushInterval:         time.Duration(getenvInt("ACTIVITY_FLUSH_INTERVAL_MS", 1000)) * time.Millisecond,
			RetryMinDelay:         time.Duration(getenvInt("ACTIVITY_RETRY_MIN_MS", 250)) * time.Millisecond,
			RetryMaxDelay:         time.Duration(getenvInt("ACTIVITY_RETRY_MAX_MS", 30000)) * time.Millisecond,
			WriteTimeout:          time.Duration(getenvInt("ACTIVITY_WRITE_TIMEOUT_MS", 10000)) * time.Millisecond,
			DurableEnqueueTimeout: time.Duration(getenvInt("ACTIVITY_DURABLE_ENQUEUE_TIMEOUT_MS", 1500)) * time.Millisecond,
			DBMaxConns:            int32(getenvInt("ACTIVITY_DB_MAX_CONNS", 4)),
			DBMinConns:            int32(getenvInt("ACTIVITY_DB_MIN_CONNS", 1)),
		},
		Favorite: FavoriteConfig{
			MaxCollectionsPerUser: getenvInt("FAVORITE_MAX_COLLECTIONS_PER_USER", 100),
			MaxItemsPerUser:       getenvInt("FAVORITE_MAX_ITEMS_PER_USER", 10_000),
		},
		FavoriteExport: FavoriteExportConfig{
			MaxActivePerUser: getenvInt("FAVORITE_EXPORT_MAX_ACTIVE_PER_USER", 2),
			MaxDailyPerUser:  getenvInt("FAVORITE_EXPORT_MAX_DAILY_PER_USER", 20),
			ArtifactTTL:      time.Duration(getenvInt("FAVORITE_EXPORT_ARTIFACT_TTL_HOURS", 168)) * time.Hour,
			LeaseTTL:         time.Duration(getenvInt("FAVORITE_EXPORT_LEASE_MINUTES", 15)) * time.Minute,
			MaxBuildAttempts: getenvInt("FAVORITE_EXPORT_MAX_BUILD_ATTEMPTS", 3),
		},
		Sticker: StickerConfig{
			MaxBytes:            int64(getenvInt("STICKER_MAX_BYTES", 4<<20)),
			MaxEdge:             getenvInt("STICKER_MAX_EDGE", 1024),
			MaxPixels:           int64(getenvInt("STICKER_MAX_PIXELS", 4_194_304)),
			MaxGIFFrames:        getenvInt("STICKER_MAX_GIF_FRAMES", 120),
			MaxGIFDecodedPixels: int64(getenvInt("STICKER_MAX_GIF_DECODED_PIXELS", 64_000_000)),
			MaxGIFDuration:      time.Duration(getenvInt("STICKER_MAX_GIF_DURATION_SECONDS", 30)) * time.Second,
			MaxPacks:            getenvInt("STICKER_MAX_PACKS", 64),
			MaxStickersPerPack:  getenvInt("STICKER_MAX_PER_PACK", 128),
			MaxCatalogItems:     getenvInt("STICKER_MAX_CATALOG_ITEMS", 1024),
		},
		SMTP: SMTPConfig{
			Enabled:  getenvBool("SMTP_ENABLED", strings.TrimSpace(os.Getenv("SMTP_HOST")) != ""),
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
			OutboxEnabled: getenvBool("NATS_OUTBOX_ENABLED", true),
			Realtime:      getenvBool("REALTIME_ENABLED", true),
			JetStream: JetStreamConfig{
				Enabled:        getenvBool("NATS_JETSTREAM_ENABLED", true),
				Stream:         getenv("NATS_JETSTREAM_STREAM", "MCMODS_TASKS"),
				MaxDeliver:     getenvInt("NATS_JETSTREAM_MAX_DELIVER", 8),
				AckWait:        time.Duration(getenvInt("NATS_JETSTREAM_ACK_WAIT_SECONDS", 300)) * time.Second,
				PublishTimeout: time.Duration(getenvInt("NATS_JETSTREAM_PUBLISH_TIMEOUT_SECONDS", 5)) * time.Second,
			},
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
				{
					Code:           "favorite_modpack_export",
					Enabled:        true,
					Subject:        "favorite.modpack-export.tasks",
					QueueGroup:     getenv("FAVORITE_EXPORT_NATS_QUEUE_GROUP", "mcmods-favorite-export-workers"),
					MaxConcurrent:  getenvInt("FAVORITE_EXPORT_MAX_CONCURRENT", 2),
					TimeoutSeconds: 600,
				},
				{
					Code:           "project_update_notifications",
					Enabled:        true,
					Subject:        "project.update.notifications",
					QueueGroup:     getenv("PROJECT_UPDATE_NATS_QUEUE_GROUP", "mcmods-project-update-workers"),
					MaxConcurrent:  getenvInt("PROJECT_UPDATE_MAX_CONCURRENT", 4),
					TimeoutSeconds: 300,
				},
			},
		},
		Redis: RedisConfig{
			Enabled:                  getenvBool("REDIS_ENABLED", false),
			Required:                 getenvBool("REDIS_REQUIRED", false),
			RateLimitFailClosed:      getenvBool("REDIS_RATE_LIMIT_FAIL_CLOSED", replicaCount > 1),
			Addr:                     getenv("REDIS_ADDR", "127.0.0.1:6379"),
			Username:                 os.Getenv("REDIS_USERNAME"),
			Password:                 os.Getenv("REDIS_PASSWORD"),
			DB:                       getenvInt("REDIS_DB", 0),
			Prefix:                   getenv("REDIS_PREFIX", "mcmods"),
			Namespace:                getenv("REDIS_NAMESPACE", getenv("APP_ENV", "development")),
			TTL:                      time.Duration(getenvInt("REDIS_QUERY_TTL_SECONDS", 120)) * time.Second,
			PoolSize:                 getenvInt("REDIS_POOL_SIZE", 32),
			MinIdleConns:             getenvInt("REDIS_MIN_IDLE_CONNS", 2),
			DialTimeout:              time.Duration(getenvInt("REDIS_DIAL_TIMEOUT_MS", 1000)) * time.Millisecond,
			ReadTimeout:              time.Duration(getenvInt("REDIS_READ_TIMEOUT_MS", 750)) * time.Millisecond,
			WriteTimeout:             time.Duration(getenvInt("REDIS_WRITE_TIMEOUT_MS", 750)) * time.Millisecond,
			AuthSessionCacheEnabled:  getenvBool("REDIS_AUTH_SESSION_CACHE_ENABLED", true),
			RBACCacheEnabled:         getenvBool("REDIS_RBAC_CACHE_ENABLED", true),
			AuthRateLimitEnabled:     getenvBool("REDIS_AUTH_RATE_LIMIT_ENABLED", true),
			PresenceEnabled:          getenvBool("REDIS_PRESENCE_ENABLED", true),
			UnreadCounterEnabled:     getenvBool("REDIS_UNREAD_COUNTER_ENABLED", true),
			SettingsCacheEnabled:     getenvBool("REDIS_SETTINGS_CACHE_ENABLED", true),
			UserCardCacheEnabled:     getenvBool("REDIS_USER_CARD_CACHE_ENABLED", true),
			SessionTTL:               time.Duration(getenvInt("REDIS_AUTH_SESSION_TTL_SECONDS", 60)) * time.Second,
			RBACCacheTTL:             time.Duration(getenvInt("REDIS_RBAC_TTL_SECONDS", 120)) * time.Second,
			PresenceTTL:              time.Duration(getenvInt("REDIS_PRESENCE_TTL_SECONDS", 150)) * time.Second,
			PresenceSnapshotInterval: time.Duration(getenvInt("PRESENCE_SNAPSHOT_INTERVAL_SECONDS", 600)) * time.Second,
			UnreadTTL:                time.Duration(getenvInt("REDIS_UNREAD_TTL_SECONDS", 300)) * time.Second,
			UnreadReconcileInterval:  time.Duration(getenvInt("REDIS_UNREAD_RECONCILE_MINUTES", 30)) * time.Minute,
			UnreadReconcileBatchSize: getenvInt("REDIS_UNREAD_RECONCILE_BATCH_SIZE", 50),
			UserCardTTL:              time.Duration(getenvInt("REDIS_USER_CARD_TTL_SECONDS", 45)) * time.Second,
		},
		AntiAbuse: AntiAbuseConfig{
			Enabled:                  getenvBool("ANTI_ABUSE_ENABLED", true),
			HMACSecret:               getenv("ANTI_ABUSE_HMAC_SECRET", defaultDevelopmentAntiAbuseHMACSecret),
			IPHashSecret:             getenv("ANTI_ABUSE_IP_HASH_SECRET", defaultDevelopmentAntiAbuseIPSecret),
			ChallengeProvider:        strings.ToLower(getenv("ANTI_ABUSE_CHALLENGE_PROVIDER", "proof")),
			TurnstileSiteKey:         strings.TrimSpace(os.Getenv("TURNSTILE_SITE_KEY")),
			TurnstileSecretKey:       strings.TrimSpace(os.Getenv("TURNSTILE_SECRET_KEY")),
			TurnstileVerifyURL:       getenv("TURNSTILE_VERIFY_URL", "https://challenges.cloudflare.com/turnstile/v0/siteverify"),
			FormTokenTTL:             time.Duration(getenvInt("ANTI_ABUSE_FORM_TOKEN_TTL_MINUTES", 30)) * time.Minute,
			FormMinimumAge:           time.Duration(getenvInt("ANTI_ABUSE_FORM_MINIMUM_AGE_MS", 800)) * time.Millisecond,
			ChallengeTTL:             time.Duration(getenvInt("ANTI_ABUSE_CHALLENGE_TTL_MINUTES", 10)) * time.Minute,
			EventRetentionDays:       getenvInt("ANTI_ABUSE_EVENT_RETENTION_DAYS", 180),
			FingerprintRetentionDays: getenvInt("ANTI_ABUSE_FINGERPRINT_RETENTION_DAYS", 30),
			DNSLookupTimeout:         time.Duration(getenvInt("ANTI_ABUSE_DNS_TIMEOUT_MS", 750)) * time.Millisecond,
			DNSCacheTTL:              time.Duration(getenvInt("ANTI_ABUSE_DNS_CACHE_HOURS", 6)) * time.Hour,
		},
		Typesense: TypesenseConfig{
			Enabled:          getenvBool("TYPESENSE_ENABLED", false),
			URL:              strings.TrimRight(getenv("TYPESENSE_URL", "http://127.0.0.1:8108"), "/"),
			APIKey:           os.Getenv("TYPESENSE_API_KEY"),
			CollectionPrefix: getenv("TYPESENSE_COLLECTION_PREFIX", "mcmods"),
			Timeout:          time.Duration(getenvInt("TYPESENSE_TIMEOUT_SECONDS", 3)) * time.Second,
		},
		Yggdrasil: YggdrasilConfig{
			Enabled:           getenvBool("YGGDRASIL_ENABLED", true),
			PublicBaseURL:     getenv("YGGDRASIL_PUBLIC_BASE_URL", "http://127.0.0.1:8080/api/yggdrasil/"),
			TextureBaseURL:    getenv("YGGDRASIL_TEXTURE_BASE_URL", "http://127.0.0.1:8080/api/yggdrasil/textures/"),
			ServerName:        getenv("YGGDRASIL_SERVER_NAME", "Mcmods-cn"),
			PrivateKeyBase64:  strings.TrimSpace(os.Getenv("YGGDRASIL_PRIVATE_KEY_BASE64")),
			TrustedProxyCIDRs: splitCommaSeparated(os.Getenv("YGGDRASIL_TRUSTED_PROXY_CIDRS")),
			TokenTTL:          time.Duration(getenvInt("YGGDRASIL_TOKEN_TTL_HOURS", 360)) * time.Hour,
			MaxTokens:         getenvInt("YGGDRASIL_MAX_TOKENS", 10),
			JoinTTL:           time.Duration(getenvInt("YGGDRASIL_JOIN_TTL_SECONDS", 30)) * time.Second,
			TextureMaxBytes:   int64(getenvInt("YGGDRASIL_TEXTURE_MAX_BYTES", 2*1024*1024)),
		},
	}
	return cfg
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

// EffectiveName returns the database name that the connection string will
// actually use. Destructive development reset validation must not trust
// DB_NAME when DATABASE_URL overrides it.
func (db DBConfig) EffectiveName() (string, error) {
	if strings.TrimSpace(db.URL) == "" {
		name := strings.TrimSpace(db.Name)
		if name == "" {
			return "", errors.New("database name is empty")
		}
		return name, nil
	}
	parsed, err := url.Parse(db.URL)
	if err != nil {
		// URL parse errors can include the complete connection string. This
		// diagnostic is included in startup/reset validation logs.
		return "", errors.New("DATABASE_URL is not a valid URL")
	}
	name := strings.TrimPrefix(strings.TrimSpace(parsed.Path), "/")
	if name == "" || strings.Contains(name, "/") {
		return "", errors.New("DATABASE_URL does not contain one database name")
	}
	return name, nil
}

func (cfg Config) Validate() error {
	var problems []error
	environment := strings.ToLower(strings.TrimSpace(cfg.Env))
	switch environment {
	case "development", "test", "staging", "production":
	default:
		problems = append(problems, fmt.Errorf("unsupported APP_ENV %q", cfg.Env))
	}
	if cfg.DB.ResetOnStart && environment != "development" {
		problems = append(problems, errors.New("DB_RESET_ON_START is only allowed in development"))
	}
	if cfg.DB.ResetOnStart {
		databaseName, err := cfg.DB.EffectiveName()
		if err != nil {
			problems = append(problems, fmt.Errorf("cannot validate reset database name: %w", err))
		} else {
			lowerName := strings.ToLower(databaseName)
			if strings.Contains(lowerName, "prod") || strings.Contains(lowerName, "production") {
				problems = append(problems, errors.New("refusing to reset a database whose name contains prod or production"))
			}
			if cfg.DB.ResetConfirm != "RESET "+databaseName {
				problems = append(problems, fmt.Errorf("DB_RESET_CONFIRM must equal %q", "RESET "+databaseName))
			}
		}
	}
	if cfg.ReplicaCount < 1 || cfg.ReplicaCount > 1000 {
		problems = append(problems, errors.New("APP_REPLICA_COUNT must be between 1 and 1000"))
	}
	if cfg.ReplicaCount > 1 && !cfg.Redis.Enabled {
		problems = append(problems, errors.New("REDIS_ENABLED must be true when APP_REPLICA_COUNT is greater than 1 so cross-instance throttles remain correct"))
	}
	if cfg.ReplicaCount > 1 && !cfg.Redis.Required {
		problems = append(problems, errors.New("REDIS_REQUIRED must be true when APP_REPLICA_COUNT is greater than 1 so a replica cannot start without shared throttles"))
	}
	if cfg.ReplicaCount > 1 && !cfg.Redis.RateLimitFailClosed {
		problems = append(problems, errors.New("REDIS_RATE_LIMIT_FAIL_CLOSED must be true when APP_REPLICA_COUNT is greater than 1 so runtime Redis failures cannot multiply quotas"))
	}
	if cfg.ReplicaCount > 1 && !cfg.Redis.AuthRateLimitEnabled {
		problems = append(problems, errors.New("REDIS_AUTH_RATE_LIMIT_ENABLED must be true when APP_REPLICA_COUNT is greater than 1"))
	}
	if cfg.Redis.Required && !cfg.Redis.Enabled {
		problems = append(problems, errors.New("REDIS_ENABLED must be true when REDIS_REQUIRED=true"))
	}
	if cfg.Redis.RateLimitFailClosed && !cfg.Redis.Enabled {
		problems = append(problems, errors.New("REDIS_ENABLED must be true when REDIS_RATE_LIMIT_FAIL_CLOSED=true"))
	}
	if cfg.Redis.Enabled {
		if strings.TrimSpace(cfg.Redis.Addr) == "" {
			problems = append(problems, errors.New("REDIS_ADDR is required when Redis is enabled"))
		}
		if cfg.Redis.PoolSize < 1 || cfg.Redis.PoolSize > 1000 || cfg.Redis.MinIdleConns < 0 || cfg.Redis.MinIdleConns > cfg.Redis.PoolSize {
			problems = append(problems, errors.New("REDIS_POOL_SIZE and REDIS_MIN_IDLE_CONNS define an invalid pool"))
		}
		if cfg.Redis.DialTimeout < 50*time.Millisecond || cfg.Redis.ReadTimeout < 50*time.Millisecond || cfg.Redis.WriteTimeout < 50*time.Millisecond {
			problems = append(problems, errors.New("Redis timeouts must be at least 50 milliseconds"))
		}
		if cfg.Redis.UnreadCounterEnabled && (cfg.Redis.UnreadReconcileInterval < time.Minute || cfg.Redis.UnreadReconcileBatchSize < 1 || cfg.Redis.UnreadReconcileBatchSize > 1000) {
			problems = append(problems, errors.New("Redis unread reconciliation interval and batch size are invalid"))
		}
		if namespace := strings.TrimSpace(cfg.Redis.Namespace); namespace == "" || strings.IndexFunc(namespace, func(value rune) bool {
			return !(value == '_' || value == '-' || value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z')
		}) >= 0 {
			problems = append(problems, errors.New("REDIS_NAMESPACE may only contain letters, numbers, underscores, and hyphens"))
		}
	}
	if cfg.NATS.JetStream.Enabled {
		if !cfg.NATS.Enabled {
			problems = append(problems, errors.New("NATS_ENABLED must be true when NATS_JETSTREAM_ENABLED=true"))
		}
		if strings.TrimSpace(cfg.NATS.JetStream.Stream) == "" || cfg.NATS.JetStream.MaxDeliver < 2 || cfg.NATS.JetStream.MaxDeliver > 100 {
			problems = append(problems, errors.New("JetStream stream and maximum delivery settings are invalid"))
		}
		if cfg.NATS.JetStream.AckWait < time.Second || cfg.NATS.JetStream.PublishTimeout < 100*time.Millisecond {
			problems = append(problems, errors.New("JetStream acknowledgement and publish timeouts are invalid"))
		}
	}
	if cfg.SMTP.Enabled && (strings.TrimSpace(cfg.SMTP.Host) == "" || strings.TrimSpace(cfg.SMTP.From) == "" || cfg.SMTP.Port < 1 || cfg.SMTP.Port > 65535) {
		problems = append(problems, errors.New("SMTP_HOST, SMTP_FROM, and a valid SMTP_PORT are required when SMTP_ENABLED=true"))
	}
	if cfg.DB.MaxConns < 2 || cfg.DB.MaxConns > 500 || cfg.DB.MinConns < 0 || cfg.DB.MinConns > cfg.DB.MaxConns {
		problems = append(problems, errors.New("DB_MIN_CONNS and DB_MAX_CONNS must define a valid pool between 2 and 500 connections"))
	}
	if cfg.Activity.BatchSize < 16 || cfg.Activity.BatchSize > 5000 {
		problems = append(problems, errors.New("ACTIVITY_BATCH_SIZE must be between 16 and 5000"))
	}
	if cfg.Activity.QueueCapacity < cfg.Activity.BatchSize || cfg.Activity.QueueCapacity > 1000000 {
		problems = append(problems, errors.New("ACTIVITY_QUEUE_CAPACITY must be at least one batch and no more than 1000000"))
	}
	if cfg.Activity.FlushInterval < 50*time.Millisecond || cfg.Activity.FlushInterval > time.Minute {
		problems = append(problems, errors.New("ACTIVITY_FLUSH_INTERVAL_MS must be between 50 and 60000"))
	}
	if cfg.Activity.RetryMinDelay < 10*time.Millisecond || cfg.Activity.RetryMaxDelay < cfg.Activity.RetryMinDelay || cfg.Activity.RetryMaxDelay > 5*time.Minute {
		problems = append(problems, errors.New("activity retry delays are invalid"))
	}
	if cfg.Activity.WriteTimeout < time.Second || cfg.Activity.WriteTimeout > time.Minute || cfg.Activity.DurableEnqueueTimeout < 100*time.Millisecond || cfg.Activity.DurableEnqueueTimeout > 10*time.Second {
		problems = append(problems, errors.New("activity write timeouts are invalid"))
	}
	if cfg.Activity.DBMaxConns < 1 || cfg.Activity.DBMaxConns > 50 || cfg.Activity.DBMinConns < 0 || cfg.Activity.DBMinConns > cfg.Activity.DBMaxConns {
		problems = append(problems, errors.New("ACTIVITY_DB_MIN_CONNS and ACTIVITY_DB_MAX_CONNS must define a valid pool between 1 and 50 connections"))
	}
	secret := strings.TrimSpace(cfg.JWTSecret)
	settingsSecret := strings.TrimSpace(cfg.SettingsEncryptionKey)
	if environment != "development" && environment != "test" {
		if secret == "" || secret == "change-this-in-production" || len(secret) < 32 {
			problems = append(problems, errors.New("JWT_SECRET must contain at least 32 characters outside development"))
		}
	}
	if settingsSecret == "" || len(settingsSecret) < 32 {
		problems = append(problems, errors.New("SETTINGS_ENCRYPTION_KEY must contain at least 32 characters"))
	} else if environment != "development" && environment != "test" && settingsSecret == defaultDevelopmentSettingsEncryptionKey {
		problems = append(problems, errors.New("SETTINGS_ENCRYPTION_KEY must be changed outside development"))
	}
	if cfg.JWTTTL < 5*time.Minute || cfg.JWTTTL > 30*24*time.Hour {
		problems = append(problems, errors.New("JWT_TTL_HOURS must produce a duration between 5 minutes and 30 days"))
	}
	if cfg.Typesense.Enabled {
		typesenseURL, parseErr := url.Parse(strings.TrimSpace(cfg.Typesense.URL))
		if parseErr != nil || typesenseURL.Scheme == "" || typesenseURL.Host == "" || typesenseURL.User != nil ||
			(typesenseURL.Scheme != "http" && typesenseURL.Scheme != "https") || typesenseURL.RawQuery != "" || typesenseURL.Fragment != "" {
			problems = append(problems, errors.New("TYPESENSE_URL must be an absolute HTTP(S) URL without credentials, query, or fragment"))
		}
		if strings.TrimSpace(cfg.Typesense.APIKey) == "" {
			problems = append(problems, errors.New("TYPESENSE_API_KEY is required when TYPESENSE_ENABLED=true"))
		}
		if cfg.Typesense.Timeout < time.Second || cfg.Typesense.Timeout > 30*time.Second {
			problems = append(problems, errors.New("TYPESENSE_TIMEOUT_SECONDS must be between 1 and 30"))
		}
		if prefix := strings.TrimSpace(cfg.Typesense.CollectionPrefix); prefix == "" || strings.IndexFunc(prefix, func(value rune) bool {
			return !(value == '_' || value == '-' || value >= '0' && value <= '9' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z')
		}) >= 0 {
			problems = append(problems, errors.New("TYPESENSE_COLLECTION_PREFIX may only contain letters, numbers, underscores, and hyphens"))
		}
	}
	// Public presence signs anonymous identities even when mutation anti-abuse
	// is disabled. Its production secret must not depend on that feature flag.
	if cfg.AntiAbuse.Enabled || (environment != "development" && environment != "test") {
		if len(strings.TrimSpace(cfg.AntiAbuse.HMACSecret)) < 32 {
			problems = append(problems, errors.New("ANTI_ABUSE_HMAC_SECRET must contain at least 32 characters"))
		}
		if environment != "development" && environment != "test" && strings.TrimSpace(cfg.AntiAbuse.HMACSecret) == defaultDevelopmentAntiAbuseHMACSecret {
			problems = append(problems, errors.New("ANTI_ABUSE_HMAC_SECRET must be changed outside development"))
		}
	}
	if cfg.AntiAbuse.Enabled {
		if len(strings.TrimSpace(cfg.AntiAbuse.IPHashSecret)) < 32 {
			problems = append(problems, errors.New("ANTI_ABUSE_IP_HASH_SECRET must contain at least 32 characters"))
		}
		if environment != "development" && environment != "test" {
			if cfg.AntiAbuse.IPHashSecret == defaultDevelopmentAntiAbuseIPSecret {
				problems = append(problems, errors.New("ANTI_ABUSE_IP_HASH_SECRET must be changed outside development"))
			}
		}
		if cfg.ReplicaCount > 1 && !cfg.Redis.Enabled {
			problems = append(problems, errors.New("REDIS_ENABLED must be true for multi-replica anti-abuse rate limiting"))
		}
		switch cfg.AntiAbuse.ChallengeProvider {
		case "proof", "disabled":
		case "turnstile":
			if cfg.AntiAbuse.TurnstileSiteKey == "" || cfg.AntiAbuse.TurnstileSecretKey == "" {
				problems = append(problems, errors.New("TURNSTILE_SITE_KEY and TURNSTILE_SECRET_KEY are required for the Turnstile challenge provider"))
			}
		default:
			problems = append(problems, errors.New("ANTI_ABUSE_CHALLENGE_PROVIDER must be proof, turnstile, or disabled"))
		}
		if cfg.AntiAbuse.FormTokenTTL < time.Minute || cfg.AntiAbuse.FormTokenTTL > 24*time.Hour {
			problems = append(problems, errors.New("ANTI_ABUSE_FORM_TOKEN_TTL_MINUTES must be between 1 minute and 24 hours"))
		}
		if cfg.AntiAbuse.FormMinimumAge < 0 || cfg.AntiAbuse.FormMinimumAge > time.Minute {
			problems = append(problems, errors.New("ANTI_ABUSE_FORM_MINIMUM_AGE_MS must be between 0 and 60000"))
		}
		if cfg.AntiAbuse.EventRetentionDays < 7 || cfg.AntiAbuse.EventRetentionDays > 3650 ||
			cfg.AntiAbuse.FingerprintRetentionDays < 1 || cfg.AntiAbuse.FingerprintRetentionDays > 365 {
			problems = append(problems, errors.New("anti-abuse retention values are outside their supported ranges"))
		}
	}
	frontend, err := url.Parse(strings.TrimSpace(cfg.FrontendOrigin))
	if err != nil || frontend.Scheme == "" || frontend.Host == "" || frontend.User != nil ||
		(frontend.Scheme != "http" && frontend.Scheme != "https") ||
		frontend.RawQuery != "" || frontend.Fragment != "" || (frontend.Path != "" && frontend.Path != "/") {
		problems = append(problems, errors.New("FRONTEND_ORIGIN must be an absolute HTTP(S) origin without path, query, or fragment"))
	} else if environment == "production" && frontend.Scheme != "https" {
		problems = append(problems, errors.New("FRONTEND_ORIGIN must use HTTPS in production"))
	}
	return errors.Join(problems...)
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

func splitCommaSeparated(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func loadDotEnv(path string) {
	file, err := os.Open(path) // #nosec G304 -- callers supply fixed .env names or paths rooted at the executable directory.
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
