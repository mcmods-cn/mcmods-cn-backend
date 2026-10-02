package httpapi

import (
	"log/slog"
	"net/http"

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

func logContentLanguageSettingsFailure(stage string, userID int64, err error) {
	slog.Error("content language settings failure", "stage", stage, "user_id", userID, "error", err)
}

func (s *Server) contentLanguageSettings(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if r.Method == http.MethodGet {
		var response contentLanguageSettingsPayload
		if err := s.db.QueryRow(r.Context(), `
			select preferred_content_language,secondary_content_language from users where id=$1`, claims.Subject).
			Scan(&response.PrimaryLocale, &response.SecondaryLocale); err != nil {
			logContentLanguageSettingsFailure("read", claims.Subject, err)
			writeAPIError(w, http.StatusInternalServerError, "CONTENT_LANGUAGE_SETTINGS_READ_FAILED", "failed to load content language settings", 0, nil)
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
		writeAPIError(w, http.StatusBadRequest, "CONTENT_LANGUAGE_REQUEST_INVALID", "request body is invalid", 0, nil)
		return
	}
	if !validContentLocaleTag(request.PrimaryLocale) || !validContentLocaleTag(request.SecondaryLocale) {
		writeAPIError(w, http.StatusBadRequest, "CONTENT_LANGUAGE_TAG_INVALID", "primaryLocale and secondaryLocale must be valid BCP-47 language tags", 0, nil)
		return
	}
	request.PrimaryLocale = normalizeContentLocale(request.PrimaryLocale)
	request.SecondaryLocale = normalizeContentLocale(request.SecondaryLocale)
	if !isEditableContentLocale(request.SecondaryLocale) {
		writeAPIError(w, http.StatusBadRequest, "CONTENT_LANGUAGE_SECONDARY_UNSUPPORTED", "secondaryLocale must be a site-supported content language", 0, nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		logContentLanguageSettingsFailure("begin", claims.Subject, err)
		writeAPIError(w, http.StatusInternalServerError, "CONTENT_LANGUAGE_SETTINGS_UPDATE_FAILED", "failed to update content language settings", 0, nil)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `
		update users set preferred_content_language=$2,secondary_content_language=$3,updated_at=now() where id=$1`,
		claims.Subject, request.PrimaryLocale, request.SecondaryLocale); err != nil {
		logContentLanguageSettingsFailure("update", claims.Subject, err)
		writeAPIError(w, http.StatusInternalServerError, "CONTENT_LANGUAGE_SETTINGS_UPDATE_FAILED", "failed to update content language settings", 0, nil)
		return
	}
	if _, err = tx.Exec(r.Context(), `
		insert into user_activity_events(user_id,action_id,object_type_id,object_route_id,occurred_at)
		select $1,$2,$3,route.id,now() from public_routes route
		where route.entity_type='user' and route.internal_id=$1`, claims.Subject, activity.ActionEdit, activity.ObjectUser); err != nil {
		logContentLanguageSettingsFailure("audit", claims.Subject, err)
		writeAPIError(w, http.StatusInternalServerError, "CONTENT_LANGUAGE_SETTINGS_AUDIT_FAILED", "failed to record content language settings update", 0, nil)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		logContentLanguageSettingsFailure("commit", claims.Subject, err)
		writeAPIError(w, http.StatusInternalServerError, "CONTENT_LANGUAGE_SETTINGS_UPDATE_FAILED", "failed to update content language settings", 0, nil)
		return
	}
	skipRequestActivity(r)
	writeJSON(w, http.StatusOK, contentLanguageSettingsPayload{
		PrimaryLocale: request.PrimaryLocale, SecondaryLocale: request.SecondaryLocale,
		EditableLocales: supportedContentLocaleList(),
	})
}
