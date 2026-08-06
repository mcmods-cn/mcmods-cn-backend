package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

const maximumDraftRetentionSeconds int32 = 365 * 24 * 60 * 60

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
	ReviewStatus         string `json:"reviewStatus"`
	ChangeRequestID      string `json:"changeRequestId"`
	ReviewTargetType     string `json:"reviewTargetType"`
	ReviewTargetPublicID string `json:"reviewTargetPublicId"`
}

type userDraftSummary struct {
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

const userDraftSummaryQuery = `select draft.public_id,draft.draft_key,draft.project_key,draft.project_title,
	draft.kind,draft.title,draft.edit_url,draft.target_url,
	case
		when draft.submitted_at is null then 'draft'
		when request.status='approved' or (request.id is null and server.review_status='approved') or
			(request.id is null and server.id is null and draft.submitted_status='approved') then 'approved'
		when request.status='pending' or (request.id is null and server.review_status='pending') or
			(request.id is null and server.id is null and draft.submitted_status='pending') then 'reviewing'
		else 'draft'
	end,
	coalesce(request.resolved_at,draft.submitted_at,draft.updated_at),draft.submitted_at,
	draft.expires_at,draft.created_at,draft.updated_at
	from user_drafts draft
	left join change_requests request on request.id=draft.change_request_id
	left join minecraft_servers server on draft.review_target_type='server' and server.public_id=draft.review_target_public_id`

type draftSummaryScanner interface {
	Scan(dest ...any) error
}

func scanUserDraftSummary(row draftSummaryScanner, item *userDraftSummary) error {
	return row.Scan(&item.ID, &item.DraftKey, &item.ProjectKey, &item.ProjectTitle, &item.Kind, &item.Title,
		&item.EditURL, &item.TargetURL, &item.Status, &item.StatusAt, &item.SubmittedAt,
		&item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt)
}

func (s *Server) userDrafts(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	switch r.Method {
	case http.MethodGet:
		rows, err := s.db.Query(r.Context(), userDraftSummaryQuery+`
			where draft.user_id=$1 and draft.expires_at>now()
			order by coalesce(request.resolved_at,draft.submitted_at,draft.updated_at) desc,draft.id desc`, claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load drafts")
			return
		}
		defer rows.Close()
		items := make([]userDraftSummary, 0)
		for rows.Next() {
			var item userDraftSummary
			if err = scanUserDraftSummary(rows, &item); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to load drafts")
				return
			}
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load drafts")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"items":            items,
			"retentionSeconds": draftRetentionSeconds(claims),
		})
	case http.MethodPost:
		retentionSeconds := draftRetentionSeconds(claims)
		if retentionSeconds <= 0 {
			writeError(w, http.StatusForbidden, "draft storage permission required")
			return
		}
		var request upsertUserDraftRequest
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid draft payload")
			return
		}
		normalizeUserDraftRequest(&request)
		if !validDraftRequest(request) {
			writeError(w, http.StatusBadRequest, "invalid draft fields")
			return
		}
		item, err := s.upsertActiveUserDraft(r, claims.Subject, request, retentionSeconds)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save draft")
			return
		}
		writeJSON(w, http.StatusOK, item)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) upsertActiveUserDraft(r *http.Request, userID int64, request upsertUserDraftRequest, retentionSeconds int32) (userDraftSummary, error) {
	var item userDraftSummary
	row := s.db.QueryRow(r.Context(), `insert into user_drafts(user_id,draft_key,project_key,kind,title,edit_url,payload,expires_at)
		values($1,$2,$3,$4,$5,$6,$7::jsonb,now()+($8::bigint*interval '1 second'))
		on conflict(user_id,draft_key) where submitted_at is null do update set project_key=excluded.project_key,
			kind=excluded.kind,title=excluded.title,edit_url=excluded.edit_url,payload=excluded.payload,
			expires_at=excluded.expires_at,updated_at=now()
		returning public_id,draft_key,project_key,project_title,kind,title,edit_url,target_url,
			'draft',updated_at,submitted_at,expires_at,created_at,updated_at`,
		userID, request.DraftKey, request.ProjectKey, request.Kind, request.Title, request.EditURL, request.Payload, retentionSeconds)
	if err := scanUserDraftSummary(row, &item); err != nil {
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
	var request completeUserDraftRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid draft payload")
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

	var changeRequestID any
	if request.ChangeRequestID != "" {
		var internalID int64
		if err = tx.QueryRow(r.Context(), `select id,status from change_requests where public_id=$1 and submitted_by=$2`,
			request.ChangeRequestID, claims.Subject).Scan(&internalID, &request.ReviewStatus); err != nil {
			writeError(w, http.StatusBadRequest, "review request does not belong to the current user")
			return
		}
		changeRequestID = internalID
	}
	if request.ReviewTargetType == "server" {
		if err = tx.QueryRow(r.Context(), `select review_status from minecraft_servers where public_id=$1 and created_by=$2`,
			request.ReviewTargetPublicID, claims.Subject).Scan(&request.ReviewStatus); err != nil {
			writeError(w, http.StatusBadRequest, "review target does not belong to the current user")
			return
		}
	}
	request.ReviewStatus = normalizeDraftSubmissionStatus(request.ReviewStatus)

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
		change_request_id=$5,review_target_type=$6,review_target_public_id=$7,submitted_status=$8,
		submitted_at=now(),updated_at=now() where id=$1`, draftID, request.ProjectKey, request.ProjectTitle,
		request.TargetURL, changeRequestID, request.ReviewTargetType, request.ReviewTargetPublicID, request.ReviewStatus); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete draft")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete draft")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"completed": true})
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
	request.ReviewStatus = normalizeDraftSubmissionStatus(request.ReviewStatus)
	request.ChangeRequestID = strings.ToLower(strings.TrimSpace(request.ChangeRequestID))
	request.ReviewTargetType = strings.ToLower(strings.TrimSpace(request.ReviewTargetType))
	request.ReviewTargetPublicID = strings.ToLower(strings.TrimSpace(request.ReviewTargetPublicID))
}

func normalizeDraftSubmissionStatus(value string) string {
	if strings.ToLower(strings.TrimSpace(value)) == "approved" {
		return "approved"
	}
	return "pending"
}

func validCompleteUserDraftRequest(request completeUserDraftRequest) bool {
	if request.ProjectTitle == "" || len([]rune(request.ProjectTitle)) > 200 || !validDraftEditURL(request.TargetURL) {
		return false
	}
	if request.ChangeRequestID != "" && !validCatalogPublicID(request.ChangeRequestID) {
		return false
	}
	if request.ReviewTargetType == "" {
		return request.ReviewTargetPublicID == ""
	}
	return request.ReviewTargetType == "server" && validCatalogPublicID(request.ReviewTargetPublicID)
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
		len([]rune(request.Title)) > 200 || !validDraftEditURL(request.EditURL) {
		return false
	}
	var payload map[string]json.RawMessage
	return len(request.Payload) > 0 && json.Unmarshal(request.Payload, &payload) == nil && payload != nil
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
