package antiabuse

import "time"

type Outcome string

const (
	Allow         Outcome = "allow"
	AllowWithLog  Outcome = "allow_with_log"
	Challenge     Outcome = "challenge"
	Delay         Outcome = "delay"
	Moderation    Outcome = "moderation"
	TempBlock     Outcome = "temp_block"
	Deny          Outcome = "deny"
	AccountReview Outcome = "account_review"
)

type ActionPolicy struct {
	BurstLimit    int `json:"burstLimit"`
	BurstSeconds  int `json:"burstSeconds"`
	HourLimit     int `json:"hourLimit"`
	DayLimit      int `json:"dayLimit"`
	ObjectLimit   int `json:"objectLimit"`
	ObjectMinutes int `json:"objectMinutes"`
	PendingLimit  int `json:"pendingLimit"`
}

type Settings struct {
	Enabled               bool                    `json:"enabled"`
	EmergencyMode         bool                    `json:"emergencyMode"`
	LogThreshold          int                     `json:"logThreshold"`
	ModerationThreshold   int                     `json:"moderationThreshold"`
	ChallengeThreshold    int                     `json:"challengeThreshold"`
	TempBlockThreshold    int                     `json:"tempBlockThreshold"`
	DenyThreshold         int                     `json:"denyThreshold"`
	NewAccountDays        int                     `json:"newAccountDays"`
	TrustedAccountDays    int                     `json:"trustedAccountDays"`
	TrustedMinimumLevel   int                     `json:"trustedMinimumLevel"`
	DuplicateWindowHours  int                     `json:"duplicateWindowHours"`
	SimilarityThreshold   int                     `json:"similarityThreshold"`
	TemporaryBlockMinutes int                     `json:"temporaryBlockMinutes"`
	Policies              map[string]ActionPolicy `json:"policies"`
}

func DefaultSettings() Settings {
	return Settings{
		Enabled: true, LogThreshold: 20, ModerationThreshold: 40, ChallengeThreshold: 60,
		TempBlockThreshold: 80, DenyThreshold: 100, NewAccountDays: 7, TrustedAccountDays: 90,
		TrustedMinimumLevel: 5, DuplicateWindowHours: 24, SimilarityThreshold: 900,
		TemporaryBlockMinutes: 30,
		Policies: map[string]ActionPolicy{
			"comment.create":   {BurstLimit: 4, BurstSeconds: 10, HourLimit: 60, DayLimit: 240, ObjectLimit: 12, ObjectMinutes: 10},
			"comment.reply":    {BurstLimit: 5, BurstSeconds: 10, HourLimit: 90, DayLimit: 360, ObjectLimit: 15, ObjectMinutes: 10},
			"comment.edit":     {BurstLimit: 6, BurstSeconds: 30, HourLimit: 80, DayLimit: 240, ObjectLimit: 8, ObjectMinutes: 10},
			"message.send":     {BurstLimit: 8, BurstSeconds: 30, HourLimit: 180, DayLimit: 600, ObjectLimit: 30, ObjectMinutes: 10},
			"report.create":    {BurstLimit: 3, BurstSeconds: 60, HourLimit: 20, DayLimit: 50, ObjectLimit: 2, ObjectMinutes: 60},
			"upload.create":    {BurstLimit: 5, BurstSeconds: 60, HourLimit: 60, DayLimit: 200, ObjectLimit: 10, ObjectMinutes: 10},
			"review.submit":    {BurstLimit: 3, BurstSeconds: 60, HourLimit: 20, DayLimit: 60, ObjectLimit: 3, ObjectMinutes: 60, PendingLimit: 20},
			"community.submit": {BurstLimit: 3, BurstSeconds: 60, HourLimit: 15, DayLimit: 40, ObjectLimit: 3, ObjectMinutes: 60, PendingLimit: 15},
			"reaction.write":   {BurstLimit: 20, BurstSeconds: 30, HourLimit: 500, DayLimit: 2000, ObjectLimit: 8, ObjectMinutes: 10},
			"favorite.write":   {BurstLimit: 15, BurstSeconds: 30, HourLimit: 300, DayLimit: 1000, ObjectLimit: 6, ObjectMinutes: 10},
			"follow.write":     {BurstLimit: 8, BurstSeconds: 60, HourLimit: 100, DayLimit: 300, ObjectLimit: 3, ObjectMinutes: 60},
			"write.generic":    {BurstLimit: 12, BurstSeconds: 60, HourLimit: 240, DayLimit: 1000, ObjectLimit: 20, ObjectMinutes: 10},
		},
	}
}

type Evaluation struct {
	UserID    int64
	SessionID string
	IP        string
	DeviceID  string
	UserAgent string
	RequestID string
	Action    string
	RateScope string
	// RateLimitPercent is the permission-derived request allowance. Its zero
	// value retains the safe 100% policy for callers that do not supply it.
	RateLimitPercent int
	ObjectType       string
	ObjectKey        string
	Content          string
	FormToken        string
	Honeypot         string
	ChallengeProof   string
	IdempotencyKey   string
	Administrator    bool
	CrawlerClass     CrawlerClass
	Now              time.Time
}

type Decision struct {
	Outcome    Outcome        `json:"outcome"`
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	RiskScore  int            `json:"-"`
	Rules      []string       `json:"-"`
	RetryAfter time.Duration  `json:"-"`
	Moderation bool           `json:"moderation,omitempty"`
	Challenge  *ChallengeInfo `json:"challenge,omitempty"`
}

type FormToken struct {
	Token     string    `json:"token"`
	FieldName string    `json:"fieldName"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type ChallengeInfo struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Prompt    string    `json:"prompt,omitempty"`
	SiteKey   string    `json:"siteKey,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type CrawlerClass string

const (
	HumanCrawler         CrawlerClass = "human"
	VerifiedSearchEngine CrawlerClass = "verified_search_engine"
	AllowedBot           CrawlerClass = "allowed_bot"
	MonitoringBot        CrawlerClass = "monitoring_bot"
	UnknownCrawler       CrawlerClass = "unknown_crawler"
	SuspiciousBot        CrawlerClass = "suspicious_bot"
	BlockedBot           CrawlerClass = "blocked_bot"
)
