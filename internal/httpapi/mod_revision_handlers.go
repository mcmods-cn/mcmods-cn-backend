package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"mcmods-cn-backend/internal/security"
)

type submitModRevisionRequest struct {
	Snapshot       createModRequest `json:"snapshot"`
	ChangeReason   string           `json:"changeReason"`
	BaseRevisionID *string          `json:"baseRevisionId,omitempty"`
}

type reviewModRevisionRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
}

type modRevisionResponse struct {
	ID                    string           `json:"id"`
	ModID                 string           `json:"modId"`
	ModInternalID         int64            `json:"-"`
	Version               int              `json:"version"`
	Status                string           `json:"status"`
	Snapshot              createModRequest `json:"snapshot"`
	ChangeReason          string           `json:"changeReason"`
	SubmittedByInternalID int64            `json:"-"`
	SubmittedBy           *string          `json:"submittedBy,omitempty"`
	SubmittedByName       string           `json:"submittedByName"`
	ReviewedBy            *string          `json:"reviewedBy,omitempty"`
	ReviewNote            string           `json:"reviewNote"`
	CreatedAt             time.Time        `json:"createdAt"`
	ReviewedAt            *time.Time       `json:"reviewedAt,omitempty"`
	BaseRevisionID        *string          `json:"baseRevisionId,omitempty"`
	ChangeRequestID       string           `json:"changeRequestId"`
	SchemaVersion         int              `json:"schemaVersion"`
	SnapshotHash          string           `json:"snapshotHash"`
}

type modRevisionChangeResponse struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Before    any    `json:"before,omitempty"`
	After     any    `json:"after,omitempty"`
}

type modRevisionComparisonResponse struct {
	Before        modRevisionResponse         `json:"before"`
	After         modRevisionResponse         `json:"after"`
	ChangedFields []string                    `json:"changedFields"`
	Changes       []modRevisionChangeResponse `json:"changes"`
}

func (s *Server) submitModRevision(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load mod")
		return
	}
	if !canEditMod(claims, identity) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}

	var request submitModRevisionRequest
	if err = decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if strings.TrimSpace(request.Snapshot.SiteID) == "" {
		request.Snapshot.SiteID = identity.SiteID
	}
	if err = normalizeAndValidateModRequest(&request.Snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	request.ChangeReason = strings.TrimSpace(request.ChangeReason)
	if len(request.ChangeReason) > 500 {
		writeError(w, http.StatusBadRequest, "change reason is too long")
		return
	}

	ossCfg := s.ossConfigFromSettings(r.Context())
	reviewConfig := loadReviewConfig(r.Context(), s.db)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start revision")
		return
	}
	defer tx.Rollback(r.Context())
	request.Snapshot.IconURL, err = validateStoredProjectIconURL(r.Context(), tx, ossCfg, request.Snapshot.IconURL, "", claims.Subject)
	if errors.Is(err, errInvalidStoredProjectIcon) {
		writeError(w, http.StatusBadRequest, errInvalidStoredProjectIcon.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to validate project icon")
		return
	}
	snapshot, err := json.Marshal(request.Snapshot)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode revision")
		return
	}

	var publishedRevisionID *int64
	if err = tx.QueryRow(r.Context(), `select published_revision_id from mods where id=$1 for update`, identity.ID).Scan(&publishedRevisionID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock mod")
		return
	}
	requestedBaseRevisionID, resolveErr := resolveRevisionPublicID(r.Context(), tx, request.BaseRevisionID)
	if resolveErr != nil {
		writeError(w, http.StatusConflict, "the proposed base revision is no longer current")
		return
	}
	if publishedRevisionID != nil {
		if requestedBaseRevisionID == nil {
			writeError(w, http.StatusConflict, "base revision is required; reload the editor")
			return
		}
		if *requestedBaseRevisionID != *publishedRevisionID {
			writeError(w, http.StatusConflict, "the mod changed while you were editing; reload and compare changes")
			return
		}
	} else if requestedBaseRevisionID != nil {
		writeError(w, http.StatusConflict, "the proposed base revision is no longer current")
		return
	}
	if err = validateModGalleryFilesTx(r.Context(), tx, identity.ID, claims.Subject, request.Snapshot.GalleryImages); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	status := "pending"
	if !reviewConfig.ModEdit || canSkipProjectReview(claims, identity) {
		status = "approved"
	}
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType:    "mod",
		EntityID:      identity.ID,
		AggregateType: "mod",
		AggregateKey:  identity.UniqueID,
		BaseRevision:  publishedRevisionID,
		Snapshot:      snapshot,
		Reason:        request.ChangeReason,
		ActorID:       claims.Subject,
		Source:        "user",
		Status:        status,
		Metadata:      map[string]any{"modId": identity.ID, "siteId": identity.SiteID},
		Request:       r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create revision")
		return
	}
	if status == "approved" {
		canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
		if err = applyModSnapshot(r.Context(), tx, identity.ID, created.RevisionID, claims.Subject,
			canManageAuthors, canManageTeams, request.Snapshot); err != nil {
			if errors.Is(err, errModSiteIDTaken) {
				writeError(w, http.StatusConflict, "mod site ID is already in use")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to publish revision")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record automatic approval")
			return
		}
		if err = enqueueOSSRehomeJobTx(r.Context(), tx, identity.ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to schedule gallery object rehome")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit revision")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "submit_mod_revision", 0,
		s.refreshProjectACLVersion(r.Context())) {
		return
	}
	revision, err := s.modRevisionByID(r.Context(), created.RevisionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read revision")
		return
	}
	// Resolve on the transport copy; the immutable snapshot remains unchanged.
	items := []modRevisionResponse{revision}
	if err = s.resolveModRevisionIconURLs(r.Context(), items); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate revision icon URL")
		return
	}
	writeJSON(w, http.StatusCreated, items[0])
}

func (s *Server) modRevisionHistory(w http.ResponseWriter, r *http.Request) {
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	claims := currentClaims(r)
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load mod")
		return
	}
	request, err := parseModRevisionHistoryPageRequest(r.URL.Query(), identity.UniqueID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	visibility := projectPendingReviewVisibility(claims, identity.UniqueID, canEditMod(claims, identity))
	query, arguments := modRevisionHistoryPageSQL(identity.UniqueID, visibility, request)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load revision history")
		return
	}
	defer rows.Close()
	items := make([]modRevisionResponse, 0)
	for rows.Next() {
		item, scanErr := scanModRevision(rows)
		if scanErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode revision history")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load revision history")
		return
	}
	rows.Close()
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		nextCursor = encodeModRevisionHistoryPageCursor(modRevisionHistoryPageCursor{
			Version: modRevisionHistoryCursorVersion, Scope: request.Scope, RevisionNo: int64(items[len(items)-1].Version),
		})
	}
	if err = s.resolveModRevisionIconURLs(r.Context(), items); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate revision icon URL")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func (s *Server) compareModRevisions(w http.ResponseWriter, r *http.Request) {
	identity, err := s.modIdentity(r.Context(), normalizeModSiteID(r.PathValue("siteId")))
	if err != nil {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	beforeID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("before")))
	afterID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("after")))
	if !validCatalogPublicID(beforeID) || !validCatalogPublicID(afterID) {
		writeError(w, http.StatusBadRequest, "invalid revision comparison")
		return
	}
	before, err := s.modRevisionByPublicID(r.Context(), beforeID)
	if err != nil || before.ModInternalID != identity.ID {
		writeError(w, http.StatusNotFound, "base revision not found")
		return
	}
	after, err := s.modRevisionByPublicID(r.Context(), afterID)
	if err != nil || after.ModInternalID != identity.ID {
		writeError(w, http.StatusNotFound, "target revision not found")
		return
	}
	claims := currentClaims(r)
	visibility := projectPendingReviewVisibility(claims, identity.UniqueID, canEditMod(claims, identity))
	if !visibility.allows(before.Status, before.SubmittedByInternalID) || !visibility.allows(after.Status, after.SubmittedByInternalID) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	changes := revisionChanges(before.Snapshot, after.Snapshot)
	items := []modRevisionResponse{before, after}
	if err = s.resolveModRevisionIconURLs(r.Context(), items); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate revision icon URL")
		return
	}
	before, after = items[0], items[1]
	writeJSON(w, http.StatusOK, modRevisionComparisonResponse{
		Before: before, After: after, ChangedFields: topLevelChangedFields(changes), Changes: changes,
	})
}

func (s *Server) reviewModRevision(w http.ResponseWriter, r *http.Request) {
	revisionPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("revisionId")))
	if !validCatalogPublicID(revisionPublicID) {
		writeError(w, http.StatusBadRequest, "invalid revision")
		return
	}
	var request reviewModRevisionRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request")
		return
	}
	request.Status = strings.TrimSpace(request.Status)
	request.Note = strings.TrimSpace(request.Note)
	if request.Status != "approved" && request.Status != "rejected" {
		writeError(w, http.StatusBadRequest, "invalid review result")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start review")
		return
	}
	defer tx.Rollback(r.Context())
	var changeRequestID, modID, submittedBy int64
	var projectID string
	var status string
	var baseRevisionID, publishedRevisionID *int64
	var snapshotRaw []byte
	err = tx.QueryRow(r.Context(), `
		select request.id,request.status,request.base_revision_id,revision.entity_id,
		       revision.snapshot,mod.published_revision_id,coalesce(request.submitted_by,0),mod.project_code
		from change_requests request
		join content_revisions revision on revision.id=request.proposed_revision_id and revision.aggregate_type='mod'
		join mods mod on mod.id=revision.entity_id
		where revision.public_id=$1 for update of request,mod`, revisionPublicID,
	).Scan(&changeRequestID, &status, &baseRevisionID, &modID, &snapshotRaw, &publishedRevisionID, &submittedBy, &projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "revision not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load review")
		return
	}
	if status != "pending" {
		writeError(w, http.StatusConflict, "revision has already been reviewed")
		return
	}
	claims := currentClaims(r)
	if !canReviewProjectSubmission(claims, projectID, submittedBy) {
		writeError(w, http.StatusForbidden, "permission denied")
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
		writeError(w, http.StatusConflict, "revision is based on an outdated published version")
		return
	}

	if request.Status == "approved" {
		var snapshot createModRequest
		if err = json.Unmarshal(snapshotRaw, &snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode revision")
			return
		}
		var revisionID int64
		if err = tx.QueryRow(r.Context(), `select id from content_revisions where public_id=$1`, revisionPublicID).Scan(&revisionID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resolve revision")
			return
		}
		canManageAuthors, canManageTeams := projectRelationshipPermissionsForClaims(claims)
		if err = applyModSnapshot(r.Context(), tx, modID, revisionID, claims.Subject,
			canManageAuthors, canManageTeams, snapshot); err != nil {
			if errors.Is(err, errModSiteIDTaken) {
				writeError(w, http.StatusConflict, "mod site ID is already in use")
				return
			}
			writeError(w, http.StatusInternalServerError, "failed to publish revision")
			return
		}
	}
	if err = appendReviewResolutionTx(r.Context(), tx, changeRequestID, request.Status, claims.Subject, request.Note, r); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record review")
		return
	}
	if request.Status == "approved" {
		if err = enqueueOSSRehomeJobTx(r.Context(), tx, modID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to schedule gallery object rehome")
			return
		}
	}
	updated, err := scanModRevision(tx.QueryRow(r.Context(), modRevisionSelect+`where revision.public_id=$1 and revision.aggregate_type='mod'`, revisionPublicID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read reviewed revision")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit review")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "review_mod_revision", 0,
		s.refreshProjectACLVersion(r.Context())) {
		return
	}
	items := []modRevisionResponse{updated}
	if err = s.resolveModRevisionIconURLs(r.Context(), items); err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate revision icon URL")
		return
	}
	writeJSON(w, http.StatusOK, items[0])
}

// Resolve only HTTP response copies, after comparisons and transactions have
// finished, so stored snapshots/hashes and semantic diffs retain stable URLs.
func (s *Server) resolveModRevisionIconURLs(ctx context.Context, items []modRevisionResponse) error {
	cfg := s.ossConfigFromSettings(ctx)
	icons := make([]storedProjectRevisionIcon, len(items))
	for i := range items {
		icons[i] = storedProjectRevisionIcon{"mod", items[i].ModInternalID, items[i].ID, items[i].Snapshot.IconURL}
	}
	urls, err := s.resolveStoredProjectRevisionIconURLs(ctx, cfg, icons)
	if err != nil {
		return err
	}
	for i := range items {
		items[i].Snapshot.IconURL = urls[i]
	}
	return nil
}

type modIdentityRecord struct {
	ID            int64
	UniqueID      string
	SiteID        string
	SubmittedByID *int64
}

func (s *Server) modIdentity(ctx context.Context, siteID string) (modIdentityRecord, error) {
	var identity modIdentityRecord
	err := s.db.QueryRow(ctx, `select id,project_code,slug,submitted_by from mods where slug=$1`, normalizeModSiteID(siteID)).Scan(
		&identity.ID, &identity.UniqueID, &identity.SiteID, &identity.SubmittedByID,
	)
	return identity, err
}

func canEditMod(claims security.Claims, identity modIdentityRecord) bool {
	return claimsAllow(claims, "project.edit") ||
		claimsAllow(claims, "project.edit."+identity.UniqueID)
}

func canSkipProjectReview(claims security.Claims, identity modIdentityRecord) bool {
	return projectMutationBypassesReview(claims) || claimsAllow(claims, "project.no-review."+identity.UniqueID)
}

const modRevisionSelect = `select
	revision.public_id,revision.entity_id,revision.aggregate_key,revision.revision_no,request.status,revision.snapshot,request.reason,
	coalesce(request.submitted_by,0),
	submitter.public_id,
	coalesce(nullif(request.submitted_by_snapshot,''),nullif(revision.created_by_snapshot,''),submitter.username,'system'),
	reviewer.public_id,
	coalesce(latest_review.note,''),
	revision.created_at,
	latest_review.created_at,
	base.public_id,
	request.public_id,revision.schema_version,revision.snapshot_hash
	from content_revisions revision
	join change_requests request on request.proposed_revision_id=revision.id
	left join users submitter on submitter.id=request.submitted_by
	left join content_revisions base on base.id=revision.base_revision_id
	left join lateral (
		select event.actor_id,event.note,event.created_at
		from review_events event
		where event.change_request_id=request.id and event.event_type in ('approved','rejected','conflicted')
		order by event.created_at desc,event.id desc limit 1
	) latest_review on true
	left join users reviewer on reviewer.id=latest_review.actor_id `

func (s *Server) modRevisionByID(ctx context.Context, id int64) (modRevisionResponse, error) {
	return scanModRevision(s.db.QueryRow(ctx, modRevisionSelect+`where revision.id=$1 and revision.aggregate_type='mod'`, id))
}

func (s *Server) modRevisionByPublicID(ctx context.Context, publicID string) (modRevisionResponse, error) {
	return scanModRevision(s.db.QueryRow(ctx, modRevisionSelect+`where revision.public_id=$1 and revision.aggregate_type='mod'`, publicID))
}

func scanModRevision(row scanner) (modRevisionResponse, error) {
	var result modRevisionResponse
	var snapshot []byte
	err := row.Scan(
		&result.ID, &result.ModInternalID, &result.ModID, &result.Version, &result.Status, &snapshot, &result.ChangeReason,
		&result.SubmittedByInternalID, &result.SubmittedBy, &result.SubmittedByName, &result.ReviewedBy, &result.ReviewNote, &result.CreatedAt, &result.ReviewedAt,
		&result.BaseRevisionID, &result.ChangeRequestID, &result.SchemaVersion, &result.SnapshotHash,
	)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(snapshot, &result.Snapshot)
	return result, err
}

func applyModSnapshot(ctx context.Context, tx pgx.Tx, modID, revisionID, relationshipActorID int64,
	canManageAuthors, canManageTeams bool, snapshot createModRequest) error {
	var projectCode, currentSiteID string
	var revisionActorID *int64
	if err := tx.QueryRow(ctx, `select project_code,slug from mods where id=$1`, modID).Scan(&projectCode, &currentSiteID); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `select created_by from content_revisions where id=$1`, revisionID).Scan(&revisionActorID); err != nil {
		return err
	}
	actorID := int64(0)
	if revisionActorID != nil {
		actorID = *revisionActorID
	}
	if snapshot.SiteID == "" {
		snapshot.SiteID = currentSiteID
	}
	if err := ensureModSiteIDAvailable(ctx, tx, snapshot.SiteID, modID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update mods set
		primary_name=$2,secondary_name=$3,abbreviation=$4,summary=$5,environment=$6,
		primary_category=$7,official_status=$8,source_status=$9,license=$10,curseforge_project_id=$11,
		modrinth_project_id=$12,github_project_path=$13,icon_url=$14,body_markdown=$15,search_keywords=$16,submission_method=$17,
		review_status='approved',published_revision_id=$18,
		slug=$19,published_at=coalesce(published_at,now()),updated_at=now() where id=$1`,
		modID, snapshot.PrimaryName, snapshot.SecondaryName, snapshot.Abbreviation, snapshot.Summary,
		snapshot.Environment, snapshot.PrimaryCategory, snapshot.OfficialStatus, snapshot.SourceStatus, snapshot.License,
		snapshot.CurseForgeProjectID, snapshot.ModrinthProjectID, snapshot.GitHubProjectPath, snapshot.IconURL, snapshot.BodyMarkdown,
		snapshot.SearchKeywords, snapshot.SubmissionMethod, revisionID, snapshot.SiteID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return errModSiteIDTaken
		}
		return err
	}
	if err = replaceModIdentifiersTx(ctx, tx, modID, snapshot.ModIDs); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `update content_subjects set default_locale=$3,updated_at=now()
		where subject_id=$1 and subject_type=$2`, modID, "mod", snapshot.DefaultLocale); err != nil {
		return err
	}
	if err = publishCatalogLocalizationsTx(ctx, tx, modID, projectCode, "mod", snapshot.Localizations, revisionID, actorID); err != nil {
		return err
	}
	if err = replaceModGalleryImagesTx(ctx, tx, modID, revisionID, actorID, snapshot.GalleryImages); err != nil {
		return err
	}
	for _, statement := range []string{
		`delete from mod_links where mod_id=$1`,
		`delete from mod_tags where mod_id=$1`,
		`delete from mod_loader_compatibilities where mod_id=$1`,
		`delete from unresolved_references unresolved using mod_relationships relationship
			where unresolved.source_type='mod_relationship' and unresolved.source_id=relationship.id and relationship.mod_id=$1`,
		`delete from mod_relationship_groups where mod_id=$1`,
	} {
		if _, err = tx.Exec(ctx, statement, modID); err != nil {
			return err
		}
	}
	for index, link := range snapshot.Links {
		if _, err = tx.Exec(ctx, `insert into mod_links(mod_id,link_type,url,note,display_order) values($1,$2,$3,$4,$5)`, modID, link.Type, link.URL, link.Note, index); err != nil {
			return err
		}
	}
	for _, tag := range snapshot.Tags {
		if _, err = tx.Exec(ctx, `insert into mod_tags(mod_id,tag) values($1,$2)`, modID, tag); err != nil {
			return err
		}
	}
	if err = syncProjectCreatorBindingsTx(ctx, tx, "mod", modID, snapshot.Authors, relationshipActorID,
		true, canManageAuthors, canManageTeams, nil); err != nil {
		return err
	}
	if err = insertModCompatibilities(ctx, tx, modID, snapshot.Compatibilities); err != nil {
		return err
	}
	if err = insertModRelationshipGroups(ctx, tx, modID, snapshot.RelationshipGroups); err != nil {
		return err
	}
	// Approved source visibility and metadata participate in the public global
	// catalog cache. Change its generation in the publication transaction.
	return bumpCatalogDatasetVersionTx(ctx, tx)
}

func appendReviewResolutionTx(ctx context.Context, tx pgx.Tx, requestID int64, status string, actorID int64, note string, r *http.Request) error {
	actorSnapshot, err := actorSnapshotTx(ctx, tx, actorID)
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `update change_requests set status=$2,resolved_at=now() where id=$1 and status in ('pending',$2)`, requestID, status)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("change request is not pending")
	}
	ip, userAgent, traceID := auditRequestValues(r)
	_, err = tx.Exec(ctx, `insert into review_events(change_request_id,event_type,actor_id,actor_snapshot,note,ip,user_agent)
		values($1,$2,$3,$4,$5,$6,$7)`, requestID, status, nullableActorID(actorID), actorSnapshot, note, ip, userAgent)
	if err != nil {
		return err
	}
	var aggregateType, aggregateKey, snapshotHash string
	var entityType *string
	var entityID *int64
	var revisionID int64
	var baseRevisionID *int64
	if err = tx.QueryRow(ctx, `select revision.entity_type,revision.entity_id,revision.aggregate_type,revision.aggregate_key,revision.id,revision.base_revision_id,revision.snapshot_hash
		from change_requests request join content_revisions revision on revision.id=request.proposed_revision_id where request.id=$1`, requestID).Scan(
		&entityType, &entityID, &aggregateType, &aggregateKey, &revisionID, &baseRevisionID, &snapshotHash,
	); err != nil {
		return err
	}
	resolvedEntityType := ""
	resolvedEntityID := int64(0)
	if entityType != nil && entityID != nil {
		resolvedEntityType = *entityType
		resolvedEntityID = *entityID
	}
	if err = appendAuditEventTx(ctx, tx, auditEventParams{
		EntityType: resolvedEntityType, EntityID: resolvedEntityID,
		AggregateType: aggregateType, AggregateKey: aggregateKey, ActorID: actorID, ActorSnapshot: actorSnapshot,
		Action: "content.revision." + status, BeforeHash: revisionHashTx(ctx, tx, baseRevisionID), AfterHash: snapshotHash,
		TraceID: traceID, IP: ip, UserAgent: userAgent,
		Metadata: map[string]any{"revisionId": revisionID, "changeRequestId": requestID},
	}); err != nil {
		return err
	}
	if err = createReviewCompletionNotificationsTx(ctx, tx, requestID, status); err != nil {
		return fmt.Errorf("notify review completion subscribers: %w", err)
	}
	return nil
}

func markChangeRequestConflictedTx(ctx context.Context, tx pgx.Tx, requestID, actorID int64, note string, r *http.Request) error {
	if note == "" {
		note = "published revision changed after this request was submitted"
	}
	return appendReviewResolutionTx(ctx, tx, requestID, "conflicted", actorID, note, r)
}

func sameRevision(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func revisionHashTx(ctx context.Context, tx pgx.Tx, revisionID *int64) string {
	if revisionID == nil {
		return ""
	}
	var hash string
	_ = tx.QueryRow(ctx, `select snapshot_hash from content_revisions where id=$1`, *revisionID).Scan(&hash)
	return hash
}

func revisionChanges(before, after createModRequest) []modRevisionChangeResponse {
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	var beforeValue, afterValue any
	_ = json.Unmarshal(beforeJSON, &beforeValue)
	_ = json.Unmarshal(afterJSON, &afterValue)
	raw := diffJSON("", beforeValue, afterValue)
	changes := make([]modRevisionChangeResponse, 0, len(raw))
	for _, item := range raw {
		changes = append(changes, modRevisionChangeResponse(item))
	}
	return changes
}

func topLevelChangedFields(changes []modRevisionChangeResponse) []string {
	set := map[string]bool{}
	for _, change := range changes {
		field := strings.TrimPrefix(change.Path, "/")
		if index := strings.IndexByte(field, '/'); index >= 0 {
			field = field[:index]
		}
		field = strings.ReplaceAll(strings.ReplaceAll(field, "~1", "/"), "~0", "~")
		if field != "" {
			set[field] = true
		}
	}
	fields := make([]string, 0, len(set))
	for field := range set {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	return fields
}
