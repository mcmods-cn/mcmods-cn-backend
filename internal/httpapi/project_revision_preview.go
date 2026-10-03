package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Reviewers read the immutable proposal for this target, rather than widening
// public project detail access. Typed snapshots deliberately omit unknown data.
func (s *Server) projectRevisionPreview(w http.ResponseWriter, r *http.Request) {
	revisionID := strings.ToLower(strings.TrimSpace(r.PathValue("revisionId")))
	if !validCatalogPublicID(revisionID) {
		writeAPIError(w, http.StatusBadRequest, "PROJECT_REVIEW_PREVIEW_INVALID", "invalid revision", 0, nil)
		return
	}
	claims := currentClaims(r)
	if claims.Subject <= 0 {
		writeAPIError(w, http.StatusUnauthorized, "PROJECT_REVIEW_PREVIEW_AUTH_REQUIRED", "authentication required", 0, nil)
		return
	}
	var entityID, submittedBy int64
	var entityType, projectID, status string
	var raw []byte
	err := s.db.QueryRow(r.Context(), `select revision.entity_type,revision.entity_id,coalesce(changelog_target.public_id,route.public_id),request.status,coalesce(request.submitted_by,0),revision.snapshot
  from content_revisions revision
  join change_requests request on request.proposed_revision_id=revision.id
    and request.entity_type=revision.entity_type and request.entity_id=revision.entity_id
    and request.aggregate_type=revision.aggregate_type and request.aggregate_key=revision.aggregate_key
  join public_routes route on route.entity_type=revision.entity_type and route.internal_id=revision.entity_id
  left join project_changelogs changelog on revision.entity_type='project_changelog' and changelog.id=revision.entity_id
  left join public_routes changelog_target on changelog_target.id=changelog.object_route_id
  where revision.public_id=$1 and revision.aggregate_key=route.public_id and request.status='pending' and (
    (revision.entity_type='mod' and revision.aggregate_type='mod' and exists(select 1 from mods where id=revision.entity_id))
    or (revision.entity_type='modpack' and revision.aggregate_type='modpack' and exists(select 1 from modpacks where id=revision.entity_id))
    or (revision.entity_type in ('plugin','map','resource_pack','shader_pack','datapack','addon') and revision.aggregate_type='simple_project'
      and exists(select 1 from simple_projects where id=revision.entity_id and project_type=revision.entity_type))
    or (revision.entity_type='project_changelog' and revision.aggregate_type='project_changelog' and changelog.status='active' and changelog.public_id=revision.aggregate_key and (
      (changelog_target.entity_type='mod' and exists(select 1 from mods where id=changelog_target.internal_id))
      or (changelog_target.entity_type='modpack' and exists(select 1 from modpacks where id=changelog_target.internal_id))
      or (changelog_target.entity_type in ('plugin','map','resource_pack','shader_pack','datapack','addon')
        and exists(select 1 from simple_projects where id=changelog_target.internal_id and project_type=changelog_target.entity_type))
      or (changelog_target.entity_type='minecraft_server' and exists(select 1 from minecraft_servers where id=changelog_target.internal_id))
    ))
  )`, revisionID).Scan(&entityType, &entityID, &projectID, &status, &submittedBy, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "PROJECT_REVIEW_PREVIEW_NOT_FOUND", "revision not found", 0, nil)
		return
	}
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "PROJECT_REVIEW_PREVIEW_UNAVAILABLE", "failed to load revision preview", 0, nil)
		return
	}
	if !canReviewProjectSubmission(claims, projectID, submittedBy) {
		writeAPIError(w, http.StatusForbidden, "PROJECT_REVIEW_PREVIEW_FORBIDDEN", "permission denied", 0, nil)
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		writeAPIError(w, http.StatusInternalServerError, "PROJECT_REVIEW_PREVIEW_INVALID_SNAPSHOT", "failed to decode revision preview", 0, nil)
		return
	}
	var snapshot any
	var icon *string
	switch entityType {
	case "mod":
		value := new(createModRequest)
		err = json.Unmarshal(raw, value)
		snapshot = value
		icon = &value.IconURL
	case "modpack":
		value := new(createModpackRequest)
		err = json.Unmarshal(raw, value)
		snapshot = value
		icon = &value.IconURL
	case "project_changelog":
		value := new(projectChangelogSnapshot)
		err = json.Unmarshal(raw, value)
		snapshot = value
	default:
		value := new(simpleProjectSnapshot)
		err = json.Unmarshal(raw, value)
		snapshot = value
		icon = &value.IconURL
		if err == nil && value.ProjectType != entityType {
			err = errors.New("snapshot project type mismatch")
		}
	}
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "PROJECT_REVIEW_PREVIEW_INVALID_SNAPSHOT", "failed to decode revision preview", 0, nil)
		return
	}
	if icon != nil {
		cfg := s.ossConfigFromSettings(r.Context())
		*icon, err = s.resolveStoredProjectRevisionIconURL(r.Context(), cfg, entityType, entityID, revisionID, *icon)
		if err != nil {
			writeAPIError(w, http.StatusServiceUnavailable, "PROJECT_REVIEW_PREVIEW_UNAVAILABLE", "failed to generate revision icon preview", 0, nil)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"id": revisionID, "entityType": entityType, "projectId": projectID, "status": status, "snapshot": snapshot})
}
