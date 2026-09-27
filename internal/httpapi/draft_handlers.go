package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

const (
	maximumDraftRetentionSeconds   int32 = 365 * 24 * 60 * 60
	maximumDraftPayloadBytes             = 512 * 1024
	maximumActiveUserDrafts              = 64
	maximumCompletedUserDrafts           = 256
	maximumUserDraftStorageBytes         = 32 * 1024 * 1024
	userDraftWriteRateWindow             = 5 * time.Minute
	userDraftSharedWritesPerWindow       = 120
	userDraftLocalWritesPerWindow        = 30
	defaultUserDraftPageSize             = 30
	maximumUserDraftPageSize             = 50
	userDraftCategoryActive              = "active"
	userDraftCategoryCompleted           = "completed"
	userDraftReviewTargetServer          = "server"
)

var (
	errUserDraftPayloadTooLarge = errors.New("draft payload exceeds its byte budget")
	errUserDraftCountQuota      = errors.New("draft count quota exceeded")
	errUserDraftStorageQuota    = errors.New("draft storage quota exceeded")
)

type userDraftQuotaPolicy struct {
	MaximumActive    int64
	MaximumCompleted int64
	MaximumBytes     int64
}

var defaultUserDraftQuotaPolicy = userDraftQuotaPolicy{
	MaximumActive:    maximumActiveUserDrafts,
	MaximumCompleted: maximumCompletedUserDrafts,
	MaximumBytes:     maximumUserDraftStorageBytes,
}

type userDraftQuotaUsage struct {
	Active        int64
	Completed     int64
	Bytes         int64
	ExistingBytes int64
	HasExisting   bool
}

type upsertUserDraftRequest struct {
	DraftKey   string          `json:"draftKey"`
	ProjectKey string          `json:"projectKey"`
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	EditURL    string          `json:"editUrl"`
	Payload    json.RawMessage `json:"payload"`
}

type completeUserDraftRequest struct {
	upsertUserDraftRequest
	ProjectTitle         string `json:"projectTitle"`
	TargetURL            string `json:"targetUrl"`
	ChangeRequestID      string `json:"changeRequestId"`
	ReviewTargetType     string `json:"reviewTargetType"`
	ReviewTargetPublicID string `json:"reviewTargetPublicId"`
}

type userDraftSummary struct {
	internalID   int64
	ID           string     `json:"id"`
	DraftKey     string     `json:"draftKey"`
	ProjectKey   string     `json:"projectKey"`
	ProjectTitle string     `json:"projectTitle"`
	Kind         string     `json:"kind"`
	Title        string     `json:"title"`
	EditURL      string     `json:"editUrl"`
	TargetURL    string     `json:"targetUrl"`
	Status       string     `json:"status"`
	StatusAt     time.Time  `json:"statusAt"`
	SubmittedAt  *time.Time `json:"submittedAt,omitempty"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

type userDraftDetail struct {
	userDraftSummary
	Payload json.RawMessage `json:"payload"`
}

type userDraftCursor struct {
	Category string    `json:"category"`
	StatusAt time.Time `json:"statusAt"`
	ID       int64     `json:"id"`
}

type userDraftListRequest struct {
	Category string
	Limit    int
	Cursor   *userDraftCursor
}

type userDraftQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type resolvedUserDraftAuthority struct {
	ChangeRequestID  any
	ReviewTargetType string
	ReviewTargetID   any
	Status           string
}

const userDraftSummaryQuery = `select draft.id,draft.public_id,draft.draft_key,draft.project_key,draft.project_title,
	draft.kind,draft.title,draft.edit_url,draft.target_url,
	case
		when draft.submitted_at is null then 'draft'
		when request.status='approved' or (request.id is null and server.review_status='approved') or
			(request.id is null and server.id is null and draft.submitted_status='approved') then 'approved'
		when request.status='pending' or (request.id is null and server.review_status='pending') or
			(request.id is null and server.id is null and draft.submitted_status='pending') then 'reviewing'
		else 'rejected'
	end,
	coalesce(request.resolved_at,draft.submitted_at,draft.updated_at),draft.submitted_at,
	draft.expires_at,draft.created_at,draft.updated_at
	from user_drafts draft
	left join change_requests request on request.id=draft.change_request_id
	left join minecraft_servers server on draft.review_target_type='server' and server.id=draft.review_target_id`

type draftSummaryScanner interface {
	Scan(dest ...any) error
}

func scanUserDraftSummary(row draftSummaryScanner, item *userDraftSummary) error {
	return row.Scan(&item.internalID, &item.ID, &item.DraftKey, &item.ProjectKey, &item.ProjectTitle, &item.Kind, &item.Title,
		&item.EditURL, &item.TargetURL, &item.Status, &item.StatusAt, &item.SubmittedAt,
		&item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt)
}

func (s *Server) userDrafts(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	switch r.Method {
	case http.MethodGet:
		request, err := parseUserDraftListRequest(r.URL.Query())
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "DRAFT_LIST_INVALID", "invalid draft list category, limit, or cursor", 0, nil)
			return
		}
		items, nextCursor, err := loadUserDraftPage(r.Context(), s.db, claims.Subject, request)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load drafts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":            items,
			"category":         request.Category,
			"nextCursor":       nextCursor,
			"retentionSeconds": draftRetentionSeconds(claims),
		})
	case http.MethodPost:
		retentionSeconds := draftRetentionSeconds(claims)
		if retentionSeconds <= 0 {
			writeError(w, http.StatusForbidden, "draft storage permission required")
			return
		}
		if !s.consumeUserDraftWriteBudget(w, r, claims.Subject) {
			return
		}
		var request upsertUserDraftRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid draft payload")
			return
		}
		if len(request.Payload) > maximumDraftPayloadBytes {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "DRAFT_PAYLOAD_TOO_LARGE", "draft payload exceeds its byte limit", 0, nil)
			return
		}
		normalizeUserDraftRequest(&request)
		if !validDraftRequest(request) {
			writeError(w, http.StatusBadRequest, "invalid draft fields")
			return
		}
		item, err := s.upsertActiveUserDraft(r, claims.Subject, request, retentionSeconds)
		if err != nil {
			if writeUserDraftQuotaError(w, err) {
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to save draft")
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func parseUserDraftListRequest(values url.Values) (userDraftListRequest, error) {
	request := userDraftListRequest{
		Category: strings.ToLower(strings.TrimSpace(values.Get("category"))),
		Limit:    defaultUserDraftPageSize,
	}
	if request.Category == "" {
		request.Category = userDraftCategoryActive
	}
	if request.Category != userDraftCategoryActive && request.Category != userDraftCategoryCompleted {
		return userDraftListRequest{}, errors.New("invalid draft category")
	}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > maximumUserDraftPageSize {
			return userDraftListRequest{}, errors.New("invalid draft page size")
		}
		request.Limit = limit
	}
	if rawCursor := strings.TrimSpace(values.Get("cursor")); rawCursor != "" {
		cursor, err := decodeUserDraftCursor(rawCursor)
		if err != nil || cursor.Category != request.Category {
			return userDraftListRequest{}, errors.New("invalid draft cursor")
		}
		request.Cursor = &cursor
	}
	return request, nil
}

func encodeUserDraftCursor(cursor userDraftCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeUserDraftCursor(value string) (userDraftCursor, error) {
	if len(value) > 512 {
		return userDraftCursor{}, errors.New("draft cursor is too large")
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return userDraftCursor{}, err
	}
	var cursor userDraftCursor
	if err = json.Unmarshal(raw, &cursor); err != nil {
		return userDraftCursor{}, err
	}
	if (cursor.Category != userDraftCategoryActive && cursor.Category != userDraftCategoryCompleted) ||
		cursor.StatusAt.IsZero() || cursor.ID <= 0 {
		return userDraftCursor{}, errors.New("draft cursor fields are invalid")
	}
	return cursor, nil
}

func loadUserDraftPage(ctx context.Context, queryer userDraftQueryer, userID int64, request userDraftListRequest) ([]userDraftSummary, string, error) {
	var cursorAt any
	var cursorID int64
	if request.Cursor != nil {
		cursorAt = request.Cursor.StatusAt
		cursorID = request.Cursor.ID
	}
	rows, err := queryer.Query(ctx, userDraftSummaryQuery+`
		where draft.user_id=$1 and draft.expires_at>now()
		  and (($2='active' and draft.submitted_at is null) or ($2='completed' and draft.submitted_at is not null))
		  and ($3::timestamptz is null or
			(coalesce(request.resolved_at,draft.submitted_at,draft.updated_at),draft.id)<($3,$4::bigint))
		order by coalesce(request.resolved_at,draft.submitted_at,draft.updated_at) desc,draft.id desc
		limit $5`, userID, request.Category, cursorAt, cursorID, request.Limit+1)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	items := make([]userDraftSummary, 0, request.Limit+1)
	for rows.Next() {
		var item userDraftSummary
		if err = scanUserDraftSummary(rows, &item); err != nil {
			return nil, "", err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, "", err
	}
	if len(items) <= request.Limit {
		return items, "", nil
	}
	last := items[request.Limit-1]
	nextCursor := encodeUserDraftCursor(userDraftCursor{Category: request.Category, StatusAt: last.StatusAt, ID: last.internalID})
	return items[:request.Limit], nextCursor, nil
}

func (s *Server) upsertActiveUserDraft(r *http.Request, userID int64, request upsertUserDraftRequest, retentionSeconds int32) (userDraftSummary, error) {
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		return userDraftSummary{}, err
	}
	defer tx.Rollback(r.Context())
	if err = enforceUserDraftQuota(r.Context(), tx, userID, request.DraftKey, request.Payload, false, defaultUserDraftQuotaPolicy); err != nil {
		return userDraftSummary{}, err
	}
	var item userDraftSummary
	row := tx.QueryRow(r.Context(), `insert into user_drafts(user_id,draft_key,project_key,kind,title,edit_url,payload,expires_at)
		values($1,$2,$3,$4,$5,$6,$7::jsonb,now()+($8::bigint*interval '1 second'))
		on conflict(user_id,draft_key) where submitted_at is null do update set project_key=excluded.project_key,
			kind=excluded.kind,title=excluded.title,edit_url=excluded.edit_url,payload=excluded.payload,
			expires_at=excluded.expires_at,updated_at=now()
		returning id,public_id,draft_key,project_key,project_title,kind,title,edit_url,target_url,
			'draft',updated_at,submitted_at,expires_at,created_at,updated_at`,
		userID, request.DraftKey, request.ProjectKey, request.Kind, request.Title, request.EditURL, request.Payload, retentionSeconds)
	if err := scanUserDraftSummary(row, &item); err != nil {
		return userDraftSummary{}, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return userDraftSummary{}, err
	}
	return item, nil
}

func (s *Server) completeUserDraft(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	retentionSeconds := draftRetentionSeconds(claims)
	if retentionSeconds <= 0 {
		writeError(w, http.StatusForbidden, "draft storage permission required")
		return
	}
	if !s.consumeUserDraftWriteBudget(w, r, claims.Subject) {
		return
	}
	var request completeUserDraftRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid draft payload")
		return
	}
	if len(request.Payload) > maximumDraftPayloadBytes {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "DRAFT_PAYLOAD_TOO_LARGE", "draft payload exceeds its byte limit", 0, nil)
		return
	}
	normalizeCompleteUserDraftRequest(&request)
	if !validDraftRequest(request.upsertUserDraftRequest) || !validCompleteUserDraftRequest(request) {
		writeError(w, http.StatusBadRequest, "invalid draft completion fields")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete draft")
		return
	}
	defer tx.Rollback(r.Context())

	authority, err := resolveUserDraftAuthority(r.Context(), tx, claims.Subject, request)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusBadRequest, "review authority does not belong to the current user")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to resolve draft review authority")
		return
	}
	if err = enforceUserDraftQuota(r.Context(), tx, claims.Subject, request.DraftKey, request.Payload, true, defaultUserDraftQuotaPolicy); err != nil {
		if writeUserDraftQuotaError(w, err) {
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to reserve submitted draft storage")
		return
	}

	var draftID int64
	if err = tx.QueryRow(r.Context(), `insert into user_drafts(user_id,draft_key,project_key,kind,title,edit_url,payload,expires_at)
		values($1,$2,$3,$4,$5,$6,$7::jsonb,now()+($8::bigint*interval '1 second'))
		on conflict(user_id,draft_key) where submitted_at is null do update set project_key=excluded.project_key,
			kind=excluded.kind,title=excluded.title,edit_url=excluded.edit_url,payload=excluded.payload,
			expires_at=excluded.expires_at,updated_at=now()
		returning id`, claims.Subject, request.DraftKey, request.ProjectKey, request.Kind, request.Title,
		request.EditURL, request.Payload, retentionSeconds).Scan(&draftID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save submitted draft")
		return
	}
	if _, err = tx.Exec(r.Context(), `update user_drafts set project_key=$2,project_title=$3,target_url=$4,
		change_request_id=$5,review_target_type=$6,review_target_id=$7,submitted_status=$8,
		submitted_at=now(),updated_at=now() where id=$1`, draftID, request.ProjectKey, request.ProjectTitle,
		request.TargetURL, authority.ChangeRequestID, authority.ReviewTargetType, authority.ReviewTargetID, authority.Status); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete draft")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete draft")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
}

func resolveUserDraftAuthority(ctx context.Context, tx pgx.Tx, userID int64, request completeUserDraftRequest) (resolvedUserDraftAuthority, error) {
	if request.ChangeRequestID != "" {
		var internalID int64
		var status string
		err := tx.QueryRow(ctx, `select id,status from change_requests where public_id=$1 and submitted_by=$2`,
			request.ChangeRequestID, userID).Scan(&internalID, &status)
		if err != nil {
			return resolvedUserDraftAuthority{}, err
		}
		return resolvedUserDraftAuthority{ChangeRequestID: internalID, Status: normalizeDraftAuthorityStatus(status)}, nil
	}
	var internalID int64
	var status string
	err := tx.QueryRow(ctx, `select id,review_status from minecraft_servers where public_id=$1 and submitted_by=$2`,
		request.ReviewTargetPublicID, userID).Scan(&internalID, &status)
	if err != nil {
		return resolvedUserDraftAuthority{}, err
	}
	return resolvedUserDraftAuthority{
		ReviewTargetType: userDraftReviewTargetServer,
		ReviewTargetID:   internalID,
		Status:           normalizeDraftAuthorityStatus(status),
	}, nil
}

func (s *Server) userDraftItem(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	draftID := strings.TrimSpace(r.PathValue("draftId"))
	if !validCatalogPublicID(draftID) {
		writeError(w, http.StatusNotFound, "draft not found")
		return
	}
	switch r.Method {
	case http.MethodGet:
		var item userDraftDetail
		row := s.db.QueryRow(r.Context(), userDraftSummaryQuery+`
			where draft.public_id=$1 and draft.user_id=$2 and draft.expires_at>now()`, draftID, claims.Subject)
		err := scanUserDraftSummary(row, &item.userDraftSummary)
		if err == nil {
			err = s.db.QueryRow(r.Context(), `select payload from user_drafts where public_id=$1 and user_id=$2`, draftID, claims.Subject).Scan(&item.Payload)
		}
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "draft not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load draft")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		command, err := s.db.Exec(r.Context(), `delete from user_drafts where public_id=$1 and user_id=$2`, draftID, claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete draft")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": command.RowsAffected() > 0})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func normalizeUserDraftRequest(request *upsertUserDraftRequest) {
	request.DraftKey = strings.TrimSpace(request.DraftKey)
	request.ProjectKey = strings.TrimSpace(request.ProjectKey)
	request.Kind = strings.TrimSpace(request.Kind)
	request.Title = strings.TrimSpace(request.Title)
	request.EditURL = strings.TrimSpace(request.EditURL)
}

func normalizeCompleteUserDraftRequest(request *completeUserDraftRequest) {
	normalizeUserDraftRequest(&request.upsertUserDraftRequest)
	request.ProjectTitle = strings.TrimSpace(request.ProjectTitle)
	request.TargetURL = strings.TrimSpace(request.TargetURL)
	request.ChangeRequestID = strings.ToLower(strings.TrimSpace(request.ChangeRequestID))
	request.ReviewTargetType = strings.ToLower(strings.TrimSpace(request.ReviewTargetType))
	request.ReviewTargetPublicID = strings.ToLower(strings.TrimSpace(request.ReviewTargetPublicID))
}

func normalizeDraftAuthorityStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "approved":
		return "approved"
	case "pending":
		return "pending"
	default:
		return "rejected"
	}
}

func validCompleteUserDraftRequest(request completeUserDraftRequest) bool {
	if request.ProjectTitle == "" || len([]rune(request.ProjectTitle)) > 200 || !validDraftEditURL(request.TargetURL) {
		return false
	}
	if request.ChangeRequestID != "" && !validCatalogPublicID(request.ChangeRequestID) {
		return false
	}
	if request.ChangeRequestID != "" {
		return request.ReviewTargetType == "" && request.ReviewTargetPublicID == ""
	}
	return request.ReviewTargetType == userDraftReviewTargetServer && validCatalogPublicID(request.ReviewTargetPublicID)
}

func draftRetentionSeconds(claims security.Claims) int32 {
	value := claimsNumericPermissionValue(claims, "user.draft.retention_seconds")
	if value <= 0 {
		return 0
	}
	if value > maximumDraftRetentionSeconds {
		return maximumDraftRetentionSeconds
	}
	return value
}

func validDraftRequest(request upsertUserDraftRequest) bool {
	if request.DraftKey == "" || len(request.DraftKey) > 255 || containsControlCharacter(request.DraftKey) ||
		request.ProjectKey == "" || len(request.ProjectKey) > 255 || containsControlCharacter(request.ProjectKey) ||
		request.Kind == "" || len(request.Kind) > 64 || containsControlCharacter(request.Kind) ||
		len([]rune(request.Title)) > 200 || !validDraftEditURL(request.EditURL) || len(request.Payload) > maximumDraftPayloadBytes {
		return false
	}
	var payload map[string]json.RawMessage
	return len(request.Payload) > 0 && json.Unmarshal(request.Payload, &payload) == nil && payload != nil
}

func writeUserDraftQuotaError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, errUserDraftPayloadTooLarge):
		writeAPIError(w, http.StatusRequestEntityTooLarge, "DRAFT_PAYLOAD_TOO_LARGE", "draft payload exceeds its byte limit", 0, nil)
	case errors.Is(err, errUserDraftCountQuota):
		writeAPIError(w, http.StatusConflict, "DRAFT_COUNT_LIMIT", "draft count limit reached", 0, nil)
	case errors.Is(err, errUserDraftStorageQuota):
		writeAPIError(w, http.StatusConflict, "DRAFT_STORAGE_LIMIT", "draft storage limit reached", 0, nil)
	default:
		return false
	}
	return true
}

func (s *Server) consumeUserDraftWriteBudget(w http.ResponseWriter, r *http.Request, userID int64) bool {
	limit := s.cache.ConsumeRateLimitPolicy(r.Context(), "user-draft-write:"+strconv.FormatInt(userID, 10),
		userDraftSharedWritesPerWindow, userDraftLocalWritesPerWindow, userDraftWriteRateWindow)
	if limit.Allowed {
		return true
	}
	retryAfter := max(1, int(limit.RetryAfter.Round(time.Second)/time.Second))
	writeAPIError(w, http.StatusTooManyRequests, "DRAFT_WRITE_RATE_LIMIT", "draft write rate exceeded", retryAfter, nil)
	return false
}

func enforceUserDraftQuota(ctx context.Context, tx pgx.Tx, userID int64, draftKey string, payload json.RawMessage,
	completing bool, policy userDraftQuotaPolicy) error {
	if len(payload) == 0 || len(payload) > maximumDraftPayloadBytes {
		return errUserDraftPayloadTooLarge
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended('user-draft-quota:'||$1::bigint::text,0))`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from user_drafts where user_id=$1 and expires_at<=now()`, userID); err != nil {
		return err
	}
	var usage userDraftQuotaUsage
	err := tx.QueryRow(ctx, `select
		count(*) filter(where submitted_at is null),
		count(*) filter(where submitted_at is not null),
		coalesce(sum(octet_length(payload::text)),0),
		coalesce(max(octet_length(payload::text)) filter(where submitted_at is null and draft_key=$2),0),
		coalesce(bool_or(submitted_at is null and draft_key=$2),false)
		from user_drafts where user_id=$1`, userID, draftKey).Scan(
		&usage.Active, &usage.Completed, &usage.Bytes, &usage.ExistingBytes, &usage.HasExisting)
	if err != nil {
		return err
	}
	var incomingBytes int64
	if err = tx.QueryRow(ctx, `select octet_length($1::jsonb::text)`, payload).Scan(&incomingBytes); err != nil {
		return err
	}
	if incomingBytes > maximumDraftPayloadBytes {
		return errUserDraftPayloadTooLarge
	}
	return evaluateUserDraftQuota(usage, incomingBytes, completing, policy)
}

func evaluateUserDraftQuota(usage userDraftQuotaUsage, incomingBytes int64, completing bool, policy userDraftQuotaPolicy) error {
	if usage.HasExisting {
		usage.Active--
		usage.Bytes -= usage.ExistingBytes
	}
	if completing {
		usage.Completed++
	} else {
		usage.Active++
	}
	usage.Bytes += incomingBytes
	if usage.Active > policy.MaximumActive || usage.Completed > policy.MaximumCompleted {
		return errUserDraftCountQuota
	}
	if usage.Bytes > policy.MaximumBytes {
		return errUserDraftStorageQuota
	}
	return nil
}

func validDraftEditURL(value string) bool {
	if value == "" || len(value) > 1000 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return false
	}
	parsed, err := url.Parse(value)
	return err == nil && !parsed.IsAbs() && parsed.Host == ""
}

func containsControlCharacter(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
