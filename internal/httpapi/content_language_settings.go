package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"mcmods-cn-backend/internal/activity"
)

type contentLanguageSettingsPayload struct {
	PrimaryLocale   string   `json:"primaryLocale"`
	SecondaryLocale string   `json:"secondaryLocale"`
	EditableLocales []string `json:"editableLocales"`
}

type updateContentLanguageSettingsRequest struct {
	PrimaryLocale   string `json:"primaryLocale"`
	SecondaryLocale string `json:"secondaryLocale"`
}

func (s *Server) contentLanguageSettings(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if r.Method == http.MethodGet {
		var response contentLanguageSettingsPayload
		if err := s.db.QueryRow(r.Context(), `
			select preferred_content_language,secondary_content_language from users where id=$1`, claims.Subject).
			Scan(&response.PrimaryLocale, &response.SecondaryLocale); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load content language settings")
			return
		}
		response.PrimaryLocale = normalizeContentLocale(response.PrimaryLocale)
		response.SecondaryLocale = normalizeContentLocale(response.SecondaryLocale)
		response.EditableLocales = supportedContentLocaleList()
		writeJSON(w, http.StatusOK, response)
		return
	}
	var request updateContentLanguageSettingsRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "request body is invalid")
		return
	}
	if !validContentLocaleTag(request.PrimaryLocale) || !validContentLocaleTag(request.SecondaryLocale) {
		writeError(w, http.StatusBadRequest, "primaryLocale and secondaryLocale must be valid BCP-47 language tags")
		return
	}
	request.PrimaryLocale = normalizeContentLocale(request.PrimaryLocale)
	request.SecondaryLocale = normalizeContentLocale(request.SecondaryLocale)
	if !isEditableContentLocale(request.SecondaryLocale) {
		writeError(w, http.StatusBadRequest, "secondaryLocale must be a site-supported content language")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update content language settings")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `
		update users set preferred_content_language=$2,secondary_content_language=$3,updated_at=now() where id=$1`,
		claims.Subject, request.PrimaryLocale, request.SecondaryLocale); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update content language settings")
		return
	}
	metadata, _ := json.Marshal(map[string]any{
		"operation": "content_language_preferences", "primaryLocale": request.PrimaryLocale,
		"secondaryLocale": request.SecondaryLocale,
	})
	if _, err = tx.Exec(r.Context(), `
		insert into user_activity_events(user_id,action_id,object_type_id,object_public_id,metadata,occurred_at)
		values($1,$2,$3,'',$4::jsonb,$5)`, claims.Subject, activity.ActionEdit, activity.ObjectUser, string(metadata), time.Now().UTC()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record content language settings update")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update content language settings")
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, contentLanguageSettingsPayload{
		PrimaryLocale: request.PrimaryLocale, SecondaryLocale: request.SecondaryLocale,
		EditableLocales: supportedContentLocaleList(),
	})
}
