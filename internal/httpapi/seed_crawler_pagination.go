package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	seedCrawlerCursorVersion      = 1
	seedCrawlerDefaultPageLimit   = 30
	seedCrawlerMaximumPageLimit   = 100
	seedCrawlerMaximumCursorBytes = 2048
)

var seedCrawlerCandidateStatuses = stringSet("candidate", "submitting", "failed", "existing", "draft", "submitted")

type seedCrawlerRunPageRequest struct {
	Limit  int
	Scope  string
	Cursor *seedCrawlerRunPageCursor
}

type seedCrawlerRunPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"createdAt"`
	ID        int64     `json:"id"`
}

type seedCrawlerCandidatePageRequest struct {
	Status string
	Limit  int
	Scope  string
	Cursor *seedCrawlerCandidatePageCursor
}

type seedCrawlerCandidatePageCursor struct {
	Version   int    `json:"v"`
	Scope     string `json:"s"`
	Downloads int64  `json:"downloads"`
	ID        int64  `json:"id"`
}

type seedCrawlerRunSummary struct {
	InternalID int64           `json:"-"`
	ID         string          `json:"id"`
	Status     string          `json:"status"`
	DryRun     bool            `json:"dryRun"`
	Attempts   int             `json:"attempts"`
	Stats      json.RawMessage `json:"stats"`
	LastError  string          `json:"lastError"`
	CreatedAt  time.Time       `json:"createdAt"`
	StartedAt  *time.Time      `json:"startedAt"`
	FinishedAt *time.Time      `json:"finishedAt"`
}

type seedCrawlerCandidateSummary struct {
	InternalID        int64     `json:"-"`
	ExternalProjectID string    `json:"externalProjectId"`
	ProjectType       string    `json:"projectType"`
	Downloads         int64     `json:"downloads"`
	Status            string    `json:"status"`
	FirstSeenRunID    string    `json:"firstSeenRunId"`
	LastSeenRunID     string    `json:"lastSeenRunId"`
	LastError         string    `json:"lastError"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
	DraftID           *string   `json:"draftId"`
}

type seedCrawlerCandidateDetailResponse struct {
	ExternalProjectID string          `json:"externalProjectId"`
	ProjectType       string          `json:"projectType"`
	Downloads         int64           `json:"downloads"`
	Status            string          `json:"status"`
	FirstSeenRunID    string          `json:"firstSeenRunId"`
	LastSeenRunID     string          `json:"lastSeenRunId"`
	Payload           json.RawMessage `json:"payload"`
	LastError         string          `json:"lastError"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
	DraftID           *string         `json:"draftId"`
}

func parseSeedCrawlerRunPageRequest(values url.Values) (seedCrawlerRunPageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return seedCrawlerRunPageRequest{}, errors.New("offset pagination is not supported")
	}
	limit, err := parseSeedCrawlerPageLimit(values.Get("limit"))
	if err != nil {
		return seedCrawlerRunPageRequest{}, err
	}
	scope := seedCrawlerPageScope("runs", "", limit)
	cursor := &seedCrawlerRunPageCursor{}
	if err = decodeSeedCrawlerCursor(values.Get("cursor"), scope, cursor); err != nil {
		return seedCrawlerRunPageRequest{}, err
	}
	if strings.TrimSpace(values.Get("cursor")) == "" {
		cursor = nil
	} else if cursor.CreatedAt.IsZero() || cursor.ID <= 0 {
		return seedCrawlerRunPageRequest{}, errors.New("invalid seed crawler run cursor")
	}
	if cursor != nil {
		cursor.CreatedAt = cursor.CreatedAt.UTC()
	}
	return seedCrawlerRunPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func parseSeedCrawlerCandidatePageRequest(values url.Values) (seedCrawlerCandidatePageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return seedCrawlerCandidatePageRequest{}, errors.New("offset pagination is not supported")
	}
	status := strings.ToLower(strings.TrimSpace(values.Get("status")))
	if status != "" && !seedCrawlerCandidateStatuses[status] {
		return seedCrawlerCandidatePageRequest{}, errors.New("invalid seed crawler candidate status")
	}
	limit, err := parseSeedCrawlerPageLimit(values.Get("limit"))
	if err != nil {
		return seedCrawlerCandidatePageRequest{}, err
	}
	scope := seedCrawlerPageScope("candidates", status, limit)
	cursor := &seedCrawlerCandidatePageCursor{}
	if err = decodeSeedCrawlerCursor(values.Get("cursor"), scope, cursor); err != nil {
		return seedCrawlerCandidatePageRequest{}, err
	}
	if strings.TrimSpace(values.Get("cursor")) == "" {
		cursor = nil
	} else if cursor.Downloads < 0 || cursor.ID <= 0 {
		return seedCrawlerCandidatePageRequest{}, errors.New("invalid seed crawler candidate cursor")
	}
	return seedCrawlerCandidatePageRequest{Status: status, Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func parseSeedCrawlerPageLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return seedCrawlerDefaultPageLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > seedCrawlerMaximumPageLimit {
		return 0, errors.New("invalid seed crawler page limit")
	}
	return limit, nil
}

func seedCrawlerPageScope(kind, status string, limit int) string {
	material, _ := json.Marshal(struct {
		Version int    `json:"version"`
		Kind    string `json:"kind"`
		Status  string `json:"status"`
		Limit   int    `json:"limit"`
	}{seedCrawlerCursorVersion, kind, status, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func decodeSeedCrawlerCursor(raw, scope string, target any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > seedCrawlerMaximumCursorBytes {
		return errors.New("invalid seed crawler cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return errors.New("invalid seed crawler cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return errors.New("invalid seed crawler cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("invalid seed crawler cursor")
	}
	switch cursor := target.(type) {
	case *seedCrawlerRunPageCursor:
		if cursor.Version != seedCrawlerCursorVersion || cursor.Scope != scope {
			return errors.New("invalid seed crawler run cursor")
		}
	case *seedCrawlerCandidatePageCursor:
		if cursor.Version != seedCrawlerCursorVersion || cursor.Scope != scope {
			return errors.New("invalid seed crawler candidate cursor")
		}
	default:
		return errors.New("invalid seed crawler cursor target")
	}
	return nil
}

func encodeSeedCrawlerRunPageCursor(cursor seedCrawlerRunPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func encodeSeedCrawlerCandidatePageCursor(cursor seedCrawlerCandidatePageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func seedCrawlerRunPageSQL(request seedCrawlerRunPageRequest) (string, []any) {
	query := `select id,public_id,status,dry_run,attempts,stats,last_error,created_at,started_at,finished_at
		from seed_crawler_runs`
	args := make([]any, 0, 3)
	if request.Cursor != nil {
		query += ` where (created_at,id)<($1,$2)`
		args = append(args, request.Cursor.CreatedAt, request.Cursor.ID)
	}
	query += ` order by created_at desc,id desc limit $` + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit+1)
	return query, args
}

func seedCrawlerCandidatePageSQL(request seedCrawlerCandidatePageRequest) (string, []any) {
	query := `select candidate.id,candidate.external_project_id,candidate.project_type,candidate.downloads,candidate.status,
		first_seen.public_id,last_seen.public_id,candidate.last_error,candidate.created_at,candidate.updated_at,draft.public_id
		from seed_crawler_candidates candidate
		join seed_crawler_runs first_seen on first_seen.id=candidate.first_seen_run_id
		join seed_crawler_runs last_seen on last_seen.id=candidate.last_seen_run_id
		left join lateral (select value.public_id from user_drafts value
			where value.draft_key='seed-crawler:'||candidate.external_project_id and value.submitted_at is null
			order by value.updated_at desc,value.id desc limit 1) draft on true
		where true`
	args := make([]any, 0, 4)
	if request.Status != "" {
		query += ` and candidate.status=$1`
		args = append(args, request.Status)
	}
	if request.Cursor != nil {
		start := len(args) + 1
		query += ` and (candidate.downloads,candidate.id)<($` + strconv.Itoa(start) + `,$` + strconv.Itoa(start+1) + `)`
		args = append(args, request.Cursor.Downloads, request.Cursor.ID)
	}
	query += ` order by candidate.downloads desc,candidate.id desc limit $` + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit+1)
	return query, args
}

const seedCrawlerCandidateDetailSQL = `select candidate.external_project_id,candidate.project_type,candidate.downloads,candidate.status,
	first_seen.public_id,last_seen.public_id,candidate.payload,candidate.last_error,candidate.created_at,candidate.updated_at,draft.public_id
	from seed_crawler_candidates candidate
	join seed_crawler_runs first_seen on first_seen.id=candidate.first_seen_run_id
	join seed_crawler_runs last_seen on last_seen.id=candidate.last_seen_run_id
	left join lateral (select value.public_id from user_drafts value
		where value.draft_key='seed-crawler:'||candidate.external_project_id and value.submitted_at is null
		order by value.updated_at desc,value.id desc limit 1) draft on true
	where candidate.external_project_id=$1`
