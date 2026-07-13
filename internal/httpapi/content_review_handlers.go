package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type modExportEntryContentSnapshot struct {
	ModID           int64  `json:"modId"`
	Registry        string `json:"registry"`
	ObjectID        string `json:"objectId"`
	Locale          string `json:"locale"`
	ContentMarkdown string `json:"contentMarkdown"`
}

func (s *Server) reviewContentRevision(w http.ResponseWriter, r *http.Request) {
	revisionID, err := strconv.ParseInt(r.PathValue("revisionId"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid revision")
		return
	}
	var request reviewModRevisionRequest
	if err = decodeJSON(r, &request); err != nil || (request.Status != "approved" && request.Status != "rejected") {
		writeError(w, http.StatusBadRequest, "invalid review result")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start review")
		return
	}
	defer tx.Rollback(r.Context())
	var changeRequestID int64
	var aggregateType, aggregateKey, status string
	var baseRevisionID *int64
	var snapshotRaw []byte
	err = tx.QueryRow(r.Context(), `
		select request.id,request.status,request.base_revision_id,revision.aggregate_type,
		       revision.aggregate_key,revision.snapshot
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where revision.id=$1 for update of request`, revisionID,
	).Scan(&changeRequestID, &status, &baseRevisionID, &aggregateType, &aggregateKey, &snapshotRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load revision")
		return
	}
	if status != "pending" {
		writeError(w, http.StatusConflict, "revision has already been reviewed")
		return
	}
	if aggregateType == "global_tag" || aggregateType == "global_recipe_type" || aggregateType == "global_recipe" {
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock catalog content")
			return
		}
		publishedRevisionID, publishedErr := globalCatalogPublishedRevisionTx(r.Context(), tx, aggregateType, aggregateKey)
		if publishedErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load published catalog content")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record catalog conflict")
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit catalog conflict")
				return
			}
			writeError(w, http.StatusConflict, "catalog content changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = publishGlobalCatalogSnapshotTx(r.Context(), tx, revisionID, aggregateType, snapshotRaw, claims.Subject); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish catalog content")
				return
			}
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record catalog review")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit catalog review")
			return
		}
		s.cache.InvalidatePrefix(r.Context(), "global-tags:")
		s.cache.InvalidatePrefix(r.Context(), "recipe-types:")
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionID, "status": request.Status})
		return
	}
	if aggregateType != "mod_export_entry" {
		writeError(w, http.StatusBadRequest, "unsupported content revision")
		return
	}
	var snapshot modExportEntryContentSnapshot
	if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode revision")
		return
	}
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock content")
		return
	}
	var publishedRevisionID *int64
	err = tx.QueryRow(r.Context(), `select published_revision_id from mod_export_entry_contents
		where mod_id=$1 and registry=$2 and object_id=$3 and locale=$4 for update`,
		snapshot.ModID, snapshot.Registry, snapshot.ObjectID, snapshot.Locale).Scan(&publishedRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		publishedRevisionID = nil
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load published content")
		return
	}
	claims := currentClaims(r)
	if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
		if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record revision conflict")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit revision conflict")
			return
		}
		writeError(w, http.StatusConflict, "content changed after this request was submitted")
		return
	}
	if request.Status == "approved" {
		if err = publishModExportEntryContentTx(r.Context(), tx, revisionID, snapshot.ModID, snapshot.Registry, snapshot.ObjectID, snapshot.Locale, snapshot.ContentMarkdown, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish entry content")
			return
		}
	}
	if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record review")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit review")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionID, "status": request.Status})
}

func globalCatalogPublishedRevisionTx(ctx context.Context, tx pgx.Tx, aggregateType, aggregateKey string) (*int64, error) {
	var revisionID *int64
	var err error
	switch aggregateType {
	case "global_tag":
		registry, tagID, _ := strings.Cut(aggregateKey, globalCatalogKeySeparator)
		err = tx.QueryRow(ctx, `select max(revision_id) from (
			select published_revision_id revision_id from global_tag_contents where registry=$1 and tag_id=$2
			union all select published_revision_id from global_tag_member_overrides where registry=$1 and tag_id=$2) revisions`, registry, tagID).Scan(&revisionID)
	case "global_recipe_type":
		err = tx.QueryRow(ctx, `select max(revision_id) from (
			select published_revision_id revision_id from global_recipe_type_contents where recipe_type_id=$1
			union all select published_revision_id from global_recipe_type_catalyst_overrides where recipe_type_id=$1) revisions`, aggregateKey).Scan(&revisionID)
	case "global_recipe":
		err = tx.QueryRow(ctx, `select published_revision_id from global_recipe_contents where recipe_key=$1`, aggregateKey).Scan(&revisionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
	default:
		return nil, errors.New("unsupported global catalog content")
	}
	return revisionID, err
}
