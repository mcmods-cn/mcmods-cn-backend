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
	DraftKey string          `json:"draftKey"`
	Kind     string          `json:"kind"`
	Title    string          `json:"title"`
	EditURL  string          `json:"editUrl"`
	Payload  json.RawMessage `json:"payload"`
}

type userDraftSummary struct {
	ID        string    `json:"id"`
	DraftKey  string    `json:"draftKey"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	EditURL   string    `json:"editUrl"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type userDraftDetail struct {
	userDraftSummary
	Payload json.RawMessage `json:"payload"`
}

func (s *Server) userDrafts(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	s.deleteExpiredDrafts(r)
	switch r.Method {
	case http.MethodGet:
		rows, err := s.db.Query(r.Context(), `select public_id,draft_key,kind,title,edit_url,expires_at,created_at,updated_at
			from user_drafts where user_id=$1 and expires_at>now() order by updated_at desc,id desc`, claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load drafts")
			return
		}
		defer rows.Close()
		items := make([]userDraftSummary, 0)
		for rows.Next() {
			var item userDraftSummary
			if err = rows.Scan(&item.ID, &item.DraftKey, &item.Kind, &item.Title, &item.EditURL, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
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
		request.DraftKey = strings.TrimSpace(request.DraftKey)
		request.Kind = strings.TrimSpace(request.Kind)
		request.Title = strings.TrimSpace(request.Title)
		request.EditURL = strings.TrimSpace(request.EditURL)
		if !validDraftRequest(request) {
			writeError(w, http.StatusBadRequest, "invalid draft fields")
			return
		}
		var item userDraftSummary
		err := s.db.QueryRow(r.Context(), `insert into user_drafts(user_id,draft_key,kind,title,edit_url,payload,expires_at)
			values($1,$2,$3,$4,$5,$6::jsonb,now()+($7::bigint*interval '1 second'))
			on conflict(user_id,draft_key) do update set kind=excluded.kind,title=excluded.title,
				edit_url=excluded.edit_url,payload=excluded.payload,expires_at=excluded.expires_at,updated_at=now()
			returning public_id,draft_key,kind,title,edit_url,expires_at,created_at,updated_at`,
			claims.Subject, request.DraftKey, request.Kind, request.Title, request.EditURL, request.Payload, retentionSeconds,
		).Scan(&item.ID, &item.DraftKey, &item.Kind, &item.Title, &item.EditURL, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save draft")
			return
		}
		writeJSON(w, http.StatusOK, item)
	case http.MethodDelete:
		draftKey := strings.TrimSpace(r.URL.Query().Get("draftKey"))
		if draftKey == "" || len(draftKey) > 255 {
			writeError(w, http.StatusBadRequest, "draftKey is required")
			return
		}
		command, err := s.db.Exec(r.Context(), `delete from user_drafts where user_id=$1 and draft_key=$2`, claims.Subject, draftKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete draft")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"deleted": command.RowsAffected() > 0})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) userDraftItem(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	draftID := strings.TrimSpace(r.PathValue("draftId"))
	if !validCatalogPublicID(draftID) {
		writeError(w, http.StatusNotFound, "draft not found")
		return
	}
	s.deleteExpiredDrafts(r)
	switch r.Method {
	case http.MethodGet:
		var item userDraftDetail
		err := s.db.QueryRow(r.Context(), `select public_id,draft_key,kind,title,edit_url,expires_at,created_at,updated_at,payload
			from user_drafts where public_id=$1 and user_id=$2 and expires_at>now()`, draftID, claims.Subject).
			Scan(&item.ID, &item.DraftKey, &item.Kind, &item.Title, &item.EditURL, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt, &item.Payload)
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

func (s *Server) deleteExpiredDrafts(r *http.Request) {
	_, _ = s.db.Exec(r.Context(), `delete from user_drafts where expires_at<=now()`)
}
