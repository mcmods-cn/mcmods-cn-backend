package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

type modExportEntryContentSnapshot struct {
	PublicID        string `json:"publicId"`
	ModID           string `json:"modId"`
	Registry        string `json:"registry"`
	ObjectID        string `json:"objectId"`
	Locale          string `json:"locale"`
	ContentMarkdown string `json:"contentMarkdown"`
}

func (s *Server) reviewContentRevision(w http.ResponseWriter, r *http.Request) {
	revisionPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("revisionId")))
	if !validCatalogPublicID(revisionPublicID) {
		writeError(w, http.StatusBadRequest, "invalid revision")
		return
	}
	var request reviewModRevisionRequest
	if err := decodeJSON(r, &request); err != nil || (request.Status != "approved" && request.Status != "rejected") {
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
	var revisionID int64
	var submittedBy int64
	var aggregateType, aggregateKey, status string
	var baseRevisionID *int64
	var snapshotRaw []byte
	err = tx.QueryRow(r.Context(), `
		select revision.id,request.id,coalesce(request.submitted_by,0),request.status,request.base_revision_id,revision.aggregate_type,
		       revision.aggregate_key,revision.snapshot
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where revision.public_id=$1 for update of request`, revisionPublicID,
	).Scan(&revisionID, &changeRequestID, &submittedBy, &status, &baseRevisionID, &aggregateType, &aggregateKey, &snapshotRaw)
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
	if modContentAggregate(aggregateType) {
		var snapshot modContentSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode mod content revision")
			return
		}
		if err = tx.QueryRow(r.Context(), `select id,slug from mods where project_code=$1`, snapshot.ModPublicID).
			Scan(&snapshot.ModID, &snapshot.ModSiteID); err != nil {
			writeError(w, http.StatusNotFound, "mod content owner not found")
			return
		}
		if modContentSnapshotAggregateKey(snapshot) != aggregateKey {
			writeError(w, http.StatusConflict, "mod content revision identity mismatch")
			return
		}
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock mod content")
			return
		}
		publishedRevisionID, publishedErr := publishedModContentRevisionTx(r.Context(), tx, snapshot)
		if publishedErr != nil {
			writeError(w, http.StatusNotFound, "mod content subject not found")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record mod content conflict")
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit mod content conflict")
				return
			}
			writeError(w, http.StatusConflict, "mod content changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = publishModContentSnapshotTx(r.Context(), tx, revisionID, snapshot, claims.Subject); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish mod content")
				return
			}
		} else if err = rejectPendingModContentCreateTx(r.Context(), tx, snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject mod content")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record mod content review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record mod content publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit mod content review")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status, "publicId": snapshot.PublicID})
		return
	}
	if catalogEditorAggregate(aggregateType) {
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock catalog content")
			return
		}
		publishedRevisionID, publishedErr := publishedCatalogEditorRevisionTx(r.Context(), tx, aggregateKey)
		if publishedErr != nil {
			writeError(w, catalogEditorHTTPStatus(publishedErr), catalogEditorErrorMessage(publishedErr))
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
			if err = publishCatalogEditorSnapshotTx(r.Context(), tx, revisionID, snapshotRaw, claims.Subject); err != nil {
				writeError(w, catalogEditorHTTPStatus(err), catalogEditorErrorMessage(err))
				return
			}
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record catalog review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record catalog publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit catalog review")
			return
		}
		s.cache.InvalidatePrefix(r.Context(), "global-tags:")
		s.cache.InvalidatePrefix(r.Context(), "recipe-types:")
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
		return
	}
	if aggregateType == catalogAggregateLocalization {
		var snapshot catalogLocalizationSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode localization revision")
			return
		}
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock localization")
			return
		}
		publicID := strings.ToLower(strings.TrimSpace(snapshot.SubjectPublicID))
		entityType := strings.TrimSpace(snapshot.SubjectType)
		entityID := snapshot.EntityID
		if publicID == "" || entityType == "" {
			if entityID <= 0 {
				writeError(w, http.StatusNotFound, "localized content subject not found")
				return
			}
			if err = tx.QueryRow(r.Context(), `select public_id,entity_type from catalog_entities where id=$1`, entityID).Scan(&publicID, &entityType); err != nil {
				writeError(w, http.StatusNotFound, "localized content subject not found")
				return
			}
		}
		var routedEntityID int64
		if err = tx.QueryRow(r.Context(), `select internal_id from public_routes where public_id=$1 and entity_type=$2`, publicID, entityType).Scan(&routedEntityID); err != nil || (entityID > 0 && entityID != routedEntityID) {
			writeError(w, http.StatusNotFound, "localized content subject not found")
			return
		}
		entityID = routedEntityID
		var publishedRevisionID *int64
		err = tx.QueryRow(r.Context(), `select published_revision_id from content_localizations
			where subject_id=$1 and subject_type=$2 and locale=$3 for update`, entityID, entityType, normalizeContentLocale(snapshot.Locale)).Scan(&publishedRevisionID)
		if errors.Is(err, pgx.ErrNoRows) {
			publishedRevisionID, err = nil, nil
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read published localization")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && snapshot.Provenance == "ai" && snapshot.SourceRevisionNo > 0 {
			var sourceRevisionNo int64
			sourceErr := tx.QueryRow(r.Context(), `select revision_no from content_localizations
				where subject_id=$1 and subject_type=$2 and locale=$3 and review_status='approved'`,
				entityID, entityType, normalizeContentLocale(snapshot.SourceLocale)).Scan(&sourceRevisionNo)
			if sourceErr != nil || sourceRevisionNo != snapshot.SourceRevisionNo {
				if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, "translation source changed after this request was submitted", r); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to record localization source conflict")
					return
				}
				if err = tx.Commit(r.Context()); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to commit localization source conflict")
					return
				}
				writeError(w, http.StatusConflict, "translation source changed after this request was submitted")
				return
			}
		}
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record localization conflict")
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit localization conflict")
				return
			}
			writeError(w, http.StatusConflict, "localization changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = publishCatalogLocalizationSnapshotTx(r.Context(), tx, revisionID, snapshotRaw, claims.Subject); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish localization")
				return
			}
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record localization review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record localization publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit localization review")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
		return
	}
	if aggregateType == "creator" {
		var snapshot creatorSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creator revision")
			return
		}
		var creatorID int64
		var kind, name string
		var publishedRevisionID *int64
		if err = tx.QueryRow(r.Context(), `select id,kind,name,published_revision_id
			from creators where public_id=$1 for update`, aggregateKey).
			Scan(&creatorID, &kind, &name, &publishedRevisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load creator")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record creator conflict")
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit creator conflict")
				return
			}
			writeError(w, http.StatusConflict, "creator changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = applyCreatorSnapshotTx(r.Context(), tx, creatorID, revisionID, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish creator")
				return
			}
		} else if _, err = tx.Exec(r.Context(), `update creators set
			review_status=case when published_revision_id is null then 'rejected' else 'approved' end,
			updated_at=now() where id=$1`, creatorID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject creator")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record creator review")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit creator review")
			return
		}
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": name, "reason": request.Note}, map[string]any{
			"creatorId": aggregateKey, "url": creatorPath(kind, aggregateKey),
		})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
		return
	}
	if aggregateType == "catalog_tag" || aggregateType == "catalog_recipe_type" || aggregateType == "catalog_recipe" {
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock catalog content")
			return
		}
		publishedRevisionID, publishedErr := catalogPublishedRevisionTx(r.Context(), tx, aggregateType, aggregateKey)
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
			if _, err = tx.Exec(r.Context(), `update catalog_entities set published_revision_id=$2,updated_at=now() where public_id=$1`, aggregateKey, revisionID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to update published catalog revision")
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
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
		return
	}
	if aggregateType == "blueprint" {
		var snapshot blueprintContentSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode blueprint revision")
			return
		}
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock blueprint")
			return
		}
		var blueprintID int64
		var publishedRevisionID *int64
		if err = tx.QueryRow(r.Context(), `select id,published_revision_id from blueprints where public_id=$1 for update`, aggregateKey).Scan(&blueprintID, &publishedRevisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load published blueprint")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record blueprint conflict")
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit blueprint conflict")
				return
			}
			writeError(w, http.StatusConflict, "blueprint changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = applyBlueprintContentSnapshotTx(r.Context(), tx, blueprintID, revisionID, submittedBy, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish blueprint")
				return
			}
		} else if _, err = tx.Exec(r.Context(), `update blueprints set review_status=case when published_revision_id is null then 'rejected' else 'approved' end,updated_at=now() where public_id=$1`, aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject blueprint")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record blueprint review")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit blueprint review")
			return
		}
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": snapshot.Title, "reason": request.Note}, map[string]any{"blueprintId": aggregateKey, "url": "/blueprints/" + aggregateKey})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
		return
	}
	if aggregateType == "skin" {
		var snapshot skinAssetContentSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode skin revision")
			return
		}
		if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to lock skin revision")
			return
		}
		var assetID, ownerID int64
		var publishedRevisionID *int64
		if err = tx.QueryRow(r.Context(), `select id,owner_id,published_revision_id from skin_assets
			where public_id=$1 and status='active' for update`, aggregateKey).Scan(&assetID, &ownerID, &publishedRevisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load published skin")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record skin conflict")
				return
			}
			if err = tx.Commit(r.Context()); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to commit skin conflict")
				return
			}
			writeError(w, http.StatusConflict, "skin changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = applySkinAssetSnapshotTx(r.Context(), tx, assetID, ownerID, revisionID, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish skin revision")
				return
			}
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record skin review")
			return
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit skin review")
			return
		}
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": snapshot.DisplayName, "reason": request.Note}, map[string]any{"skinId": aggregateKey, "url": "/skins/" + aggregateKey})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionID, "status": request.Status})
		return
	}
	if aggregateType != "catalog_resource" {
		writeError(w, http.StatusBadRequest, "unsupported content revision")
		return
	}
	var snapshot modExportEntryContentSnapshot
	if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to decode revision")
		return
	}
	var entityID int64
	if err = tx.QueryRow(r.Context(), `select id from catalog_entities where public_id=$1 and entity_type='resource'`, snapshot.PublicID).Scan(&entityID); err != nil {
		writeError(w, http.StatusNotFound, "content resource not found")
		return
	}
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtext($1))`, aggregateType+":"+aggregateKey); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock content")
		return
	}
	var publishedRevisionID *int64
	err = tx.QueryRow(r.Context(), `select published_revision_id from knowledge_pages
		where entity_id=$1 and locale=$2 for update`, entityID, snapshot.Locale).Scan(&publishedRevisionID)
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
		if err = publishModExportEntryContentTx(r.Context(), tx, revisionID, entityID, snapshot.Locale, snapshot.ContentMarkdown, claims.Subject); err != nil {
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
	writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
}

func catalogPublishedRevisionTx(ctx context.Context, tx pgx.Tx, aggregateType, entityID string) (*int64, error) {
	var revisionID *int64
	var internalID int64
	if err := tx.QueryRow(ctx, `select id from catalog_entities where public_id=$1`, entityID).Scan(&internalID); err != nil {
		return nil, err
	}
	var err error
	switch aggregateType {
	case "catalog_tag":
		err = tx.QueryRow(ctx, `select max(revision_id) from (
			select published_revision_id revision_id from knowledge_pages where entity_id=$1
			union all select published_revision_id from tag_member_overrides where tag_id=$1) revisions`, internalID).Scan(&revisionID)
	case "catalog_recipe_type":
		err = tx.QueryRow(ctx, `select max(revision_id) from (
			select published_revision_id revision_id from knowledge_pages where entity_id=$1
			union all select published_revision_id from recipe_type_catalyst_overrides where recipe_type_id=$1) revisions`, internalID).Scan(&revisionID)
	case "catalog_recipe":
		err = tx.QueryRow(ctx, `select published_revision_id from recipe_content_overrides where recipe_id=$1`, internalID).Scan(&revisionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
	default:
		return nil, errors.New("unsupported global catalog content")
	}
	return revisionID, err
}
