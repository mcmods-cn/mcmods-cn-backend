package httpapi

import (
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
	var aggregateType, aggregateKey, status, revisionEntityType string
	var revisionEntityID int64
	var baseRevisionID *int64
	var snapshotRaw, metadataRaw []byte
	err = tx.QueryRow(r.Context(), `
		select revision.id,request.id,coalesce(request.submitted_by,0),request.status,request.base_revision_id,revision.aggregate_type,
		       revision.aggregate_key,revision.snapshot,coalesce(revision.entity_type,''),coalesce(revision.entity_id,0),request.metadata
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id
		where revision.public_id=$1 for update of request`, revisionPublicID,
	).Scan(&revisionID, &changeRequestID, &submittedBy, &status, &baseRevisionID, &aggregateType, &aggregateKey, &snapshotRaw, &revisionEntityType, &revisionEntityID, &metadataRaw)
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
	claims := currentClaims(r)
	if !canReviewContentSubmission(r.Context(), tx, claims, submittedBy, revisionEntityType, revisionEntityID, aggregateType, aggregateKey, snapshotRaw, metadataRaw) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	if aggregateType == projectChangelogAggregate {
		var snapshot projectChangelogSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode changelog revision")
			return
		}
		var changelogID, targetRouteID int64
		var publishedRevisionID *int64
		var targetName, targetURL string
		if err = tx.QueryRow(r.Context(), `select entry.id,entry.object_route_id,entry.published_revision_id,
			coalesce(mod.primary_name,modpack.primary_name,project.primary_name,server.name,target.public_id),target.canonical_path
			from project_changelogs entry join public_routes target on target.id=entry.object_route_id
			left join mods mod on target.entity_type='mod' and mod.id=target.internal_id
			left join modpacks modpack on target.entity_type='modpack' and modpack.id=target.internal_id
			left join simple_projects project on target.entity_type=project.project_type and project.id=target.internal_id
			left join minecraft_servers server on target.entity_type='minecraft_server' and server.id=target.internal_id
			where entry.public_id=$1 for update of entry`, aggregateKey).
			Scan(&changelogID, &targetRouteID, &publishedRevisionID, &targetName, &targetURL); err != nil {
			writeError(w, http.StatusNotFound, "changelog not found")
			return
		}
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err == nil {
				err = tx.Commit(r.Context())
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record changelog conflict")
				return
			}
			writeError(w, http.StatusConflict, "changelog changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = applyProjectChangelogSnapshotTx(r.Context(), tx, changelogID, targetRouteID, revisionID, claims.Subject, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish changelog")
				return
			}
		} else if _, err = tx.Exec(r.Context(), `update project_changelogs set
			review_status=case when published_revision_id is null then 'rejected' else 'approved' end,updated_at=now() where id=$1`, changelogID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject changelog")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record changelog review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record changelog publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit changelog review")
			return
		}
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{
			"name": targetName + " - " + snapshot.ProjectVersion, "reason": request.Note,
		}, map[string]any{"changelogId": aggregateKey, "targetLabel": targetName, "url": targetURL + "?tab=changelog"})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status, "publicId": aggregateKey})
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
	if aggregateType == communityPostAggregate {
		var snapshot communityPostSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode community post revision")
			return
		}
		var postID, postAuthorID int64
		var publishedRevisionID *int64
		var title string
		if err = tx.QueryRow(r.Context(), `select id,author_id,published_revision_id,title from community_posts where public_id=$1 for update`, aggregateKey).
			Scan(&postID, &postAuthorID, &publishedRevisionID, &title); err != nil {
			writeError(w, http.StatusNotFound, "community post not found")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err == nil {
				err = tx.Commit(r.Context())
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record community post conflict")
				return
			}
			writeError(w, http.StatusConflict, "community post changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			if err = applyCommunityPostSnapshotTx(r.Context(), tx, postID, revisionID, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish community post")
				return
			}
		} else {
			if publishedRevisionID == nil {
				if err = refundCommunityPostBountyTx(r.Context(), tx, postID, postAuthorID, aggregateKey, "review_rejected"); err != nil {
					writeError(w, http.StatusInternalServerError, "failed to refund rejected question bounty")
					return
				}
			}
			if _, err = tx.Exec(r.Context(), `update community_posts set
				review_status=case when published_revision_id is null then 'rejected' else 'approved' end,updated_at=now() where id=$1`, postID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to reject community post")
				return
			}
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record community post review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record community post publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit community post review")
			return
		}
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": title, "reason": request.Note}, map[string]any{
			"communityPostId": aggregateKey, "targetLabel": title, "url": communityPostPath(snapshot.Kind, aggregateKey),
		})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status, "publicId": aggregateKey})
		return
	}
	if aggregateType == modpackAggregate {
		var snapshot createModpackRequest
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode modpack revision")
			return
		}
		var modpackID int64
		var publishedRevisionID *int64
		var title string
		if err = tx.QueryRow(r.Context(), `select id,published_revision_id,primary_name from modpacks where public_id=$1 for update`, aggregateKey).
			Scan(&modpackID, &publishedRevisionID, &title); err != nil {
			writeError(w, http.StatusNotFound, "modpack not found")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err == nil {
				err = tx.Commit(r.Context())
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record modpack conflict")
				return
			}
			writeError(w, http.StatusConflict, "modpack changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
			if err = applyModpackSnapshotTx(r.Context(), tx, modpackID, revisionID, claims.Subject,
				canManageAuthors, canManageTeams, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish modpack")
				return
			}
		} else if _, err = tx.Exec(r.Context(), `update modpacks set
			review_status=case when published_revision_id is null then 'rejected' else 'approved' end,updated_at=now() where id=$1`, modpackID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject modpack")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record modpack review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record modpack publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit modpack review")
			return
		}
		_ = s.refreshProjectACLVersion(r.Context())
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": title, "reason": request.Note}, map[string]any{
			"modpackId": aggregateKey, "targetLabel": title, "url": "/modpacks/" + snapshot.SiteID,
		})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status, "publicId": aggregateKey})
		return
	}
	if aggregateType == simpleProjectAggregate {
		var snapshot simpleProjectSnapshot
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil || normalizeSimpleProjectType(snapshot.ProjectType) == "" {
			writeError(w, http.StatusInternalServerError, "failed to decode project revision")
			return
		}
		var projectID int64
		var publishedRevisionID *int64
		var title string
		if err = tx.QueryRow(r.Context(), `select id,published_revision_id,primary_name from simple_projects
			where public_id=$1 and project_type=$2 for update`, aggregateKey, snapshot.ProjectType).
			Scan(&projectID, &publishedRevisionID, &title); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
		claims := currentClaims(r)
		if request.Status == "approved" && !sameRevision(baseRevisionID, publishedRevisionID) {
			if err = markChangeRequestConflictedTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err == nil {
				err = tx.Commit(r.Context())
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record project conflict")
				return
			}
			writeError(w, http.StatusConflict, "project changed after this request was submitted")
			return
		}
		if request.Status == "approved" {
			canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
			if err = applySimpleProjectSnapshotTx(r.Context(), tx, projectID, revisionID, claims.Subject,
				canManageAuthors, canManageTeams, snapshot); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to publish project")
				return
			}
		} else if _, err = tx.Exec(r.Context(), `update simple_projects set
			review_status=case when published_revision_id is null then 'rejected' else 'approved' end,updated_at=now()
			where id=$1`, projectID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to reject project")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record project review")
			return
		}
		if request.Status == "approved" {
			if err = appendCatalogPublishedReviewEventTx(r.Context(), tx, changeRequestID, claims.Subject, request.Note, r); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record project publication")
				return
			}
		}
		if err = tx.Commit(r.Context()); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to commit project review")
			return
		}
		_ = s.refreshProjectACLVersion(r.Context())
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": title, "reason": request.Note}, map[string]any{
			"projectId": aggregateKey, "projectType": snapshot.ProjectType, "targetLabel": title,
			"url": simpleProjectWebPath(snapshot.ProjectType, snapshot.SiteID),
		})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status, "publicId": aggregateKey})
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
			canManageMembers := claimsAllow(claims, "team.members.review") || claimsAllow(claims, "team.members.manage") || claimsAllow(claims, "admin.*")
			if err = s.applyCreatorSnapshotTx(r.Context(), tx, creatorID, revisionID, claims.Subject,
				submittedBy, canManageMembers, snapshot); err != nil {
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
		_ = s.refreshProjectACLVersion(r.Context())
		code := "review_approved"
		if request.Status == "rejected" {
			code = "review_rejected"
		}
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": name, "reason": request.Note}, map[string]any{
			"creatorId": aggregateKey, "targetLabel": name, "url": creatorPath(kind, aggregateKey),
		})
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
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": snapshot.Title, "reason": request.Note}, map[string]any{
			"blueprintId": aggregateKey, "targetLabel": snapshot.Title, "url": "/blueprints/" + aggregateKey,
		})
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
		s.sendTemplatedNotification(r.Context(), submittedBy, code, map[string]string{"name": snapshot.Name, "reason": request.Note}, map[string]any{
			"skinId": aggregateKey, "targetLabel": snapshot.Name, "url": "/skins/" + aggregateKey,
		})
		writeJSON(w, http.StatusOK, map[string]any{"revisionId": revisionPublicID, "status": request.Status})
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
