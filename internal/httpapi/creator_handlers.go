package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
)

type creatorLinkPayload struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Label string `json:"label"`
}

type creatorMemberPayload struct {
	CreatorID string `json:"creatorId"`
	RoleID    int64  `json:"roleId"`
	Title     string `json:"title"`
}

type creatorSnapshot struct {
	Kind                string                 `json:"kind"`
	Name                string                 `json:"name"`
	DescriptionMarkdown string                 `json:"descriptionMarkdown"`
	AvatarURL           string                 `json:"avatarUrl"`
	AvatarFileID        *int64                 `json:"avatarFileId,omitempty"`
	Links               []creatorLinkPayload   `json:"links"`
	CollaboratorIDs     []string               `json:"collaboratorIds"`
	Members             []creatorMemberPayload `json:"members"`
}

type creatorSummary struct {
	PublicID     string `json:"publicId"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	AvatarURL    string `json:"avatarUrl"`
	ReviewStatus string `json:"reviewStatus,omitempty"`
	WorkCount    int    `json:"workCount"`
	Claimed      bool   `json:"claimed"`
}

type creatorRoleResponse struct {
	ID           int64          `json:"id"`
	Code         string         `json:"code"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Translations map[string]any `json:"translations"`
	Custom       bool           `json:"custom"`
}

func (s *Server) creators(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	kind := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("kind")))
	if kind != "" && kind != "author" && kind != "team" {
		writeError(w, http.StatusBadRequest, "invalid creator kind")
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	limit := boundedInt(r.URL.Query().Get("limit"), 40, 1, 100)
	admin := hasPermission(claims.Permissions, "admin.*") || hasPermission(claims.Permissions, "content.review")
	rows, err := s.db.Query(r.Context(), `
		select creator.public_id,creator.kind,creator.name,creator.avatar_url,creator.review_status,
		       creator.claimed_by is not null,count(distinct mod.project_code)
		from creators creator
		left join content_creator_bindings binding on binding.creator_id=creator.id and binding.subject_type='mod'
		left join mods mod on mod.project_code=binding.subject_public_id and mod.review_status='approved'
		where ($1='' or creator.kind=$1)
		  and ($2='' or creator.name ilike '%' || $2 || '%')
		  and (creator.review_status='approved' or creator.created_by=$3 or creator.claimed_by=$3 or $4)
		group by creator.id
		order by creator.review_status='approved' desc,lower(creator.name),creator.id
		limit $5`, kind, query, claims.Subject, admin, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creators")
		return
	}
	defer rows.Close()
	items := make([]creatorSummary, 0)
	for rows.Next() {
		var item creatorSummary
		if err = rows.Scan(&item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &item.ReviewStatus, &item.Claimed, &item.WorkCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creators")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creators")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) creatorDetail(w http.ResponseWriter, r *http.Request) {
	publicID := strings.TrimSpace(strings.ToLower(r.PathValue("publicId")))
	claims := currentClaims(r)
	admin := hasPermission(claims.Permissions, "admin.*") || hasPermission(claims.Permissions, "content.review")
	var id int64
	var item creatorSummary
	var description string
	var createdBy, claimedBy *int64
	var publishedRevisionID *int64
	err := s.db.QueryRow(r.Context(), `
		select id,public_id,kind,name,avatar_url,description_markdown,review_status,created_by,claimed_by,published_revision_id
		from creators
		where public_id=$1 and (review_status='approved' or created_by=$2 or claimed_by=$2 or $3)`,
		publicID, claims.Subject, admin,
	).Scan(&id, &item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &description, &item.ReviewStatus, &createdBy, &claimedBy, &publishedRevisionID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	item.Claimed = claimedBy != nil
	links, err := s.creatorLinks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator links")
		return
	}
	collaborators, err := s.creatorCollaborators(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load collaborators")
		return
	}
	members, err := s.creatorMembers(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load team members")
		return
	}
	works, err := s.creatorWorks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator works")
		return
	}
	var claimedUser any
	if claimedBy != nil {
		var userPublicID, username, displayName, avatarURL string
		if scanErr := s.db.QueryRow(r.Context(), `select public_id,username,display_name,avatar_url from users where id=$1`, *claimedBy).
			Scan(&userPublicID, &username, &displayName, &avatarURL); scanErr == nil {
			claimedUser = map[string]any{
				"id": *claimedBy, "publicId": userPublicID, "username": username,
				"displayName": displayName, "avatarUrl": avatarURL,
			}
		}
	}
	canEdit := admin || claims.Subject > 0 &&
		((createdBy != nil && *createdBy == claims.Subject) || (claimedBy != nil && *claimedBy == claims.Subject)) ||
		hasPermission(claims.Permissions, "creator.edit")
	writeJSON(w, http.StatusOK, map[string]any{
		"creator": item, "descriptionMarkdown": description, "links": links,
		"collaborators": collaborators, "members": members, "works": works,
		"claimedUser": claimedUser, "canEdit": canEdit,
		"canClaim":            claims.Subject > 0 && claimedBy == nil && item.ReviewStatus == "approved",
		"publishedRevisionId": publishedRevisionID,
	})
}

func (s *Server) createCreator(w http.ResponseWriter, r *http.Request) {
	var snapshot creatorSnapshot
	if err := decodeJSON(r, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid creator payload")
		return
	}
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims := currentClaims(r)
	reviewRequired := creatorReviewRequired(loadReviewConfig(r.Context(), s.db), snapshot.Kind, "create") &&
		!hasPermission(claims.Permissions, "admin.*")
	status := "approved"
	if reviewRequired {
		status = "pending"
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create creator")
		return
	}
	defer tx.Rollback(r.Context())
	if err = s.resolveCreatorAvatarTx(r.Context(), tx, claims.Subject, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, publicID, revisionID, err := s.createCreatorTx(r.Context(), tx, snapshot, claims.Subject, status, r)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "creator already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create creator")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit creator")
		return
	}
	annotateActivity(r, activity.ActionCreate, creatorObjectType(snapshot.Kind), publicID, len(snapshot.DescriptionMarkdown), map[string]any{"creatorId": id})
	writeJSON(w, http.StatusCreated, map[string]any{"publicId": publicID, "revisionId": revisionID, "reviewStatus": status})
}

func (s *Server) updateCreator(w http.ResponseWriter, r *http.Request) {
	publicID := strings.TrimSpace(strings.ToLower(r.PathValue("publicId")))
	var snapshot creatorSnapshot
	if err := decodeJSON(r, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid creator payload")
		return
	}
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update creator")
		return
	}
	defer tx.Rollback(r.Context())
	var id int64
	var kind, previousDescription string
	var createdBy, claimedBy, baseRevisionID, currentAvatarFileID *int64
	err = tx.QueryRow(r.Context(), `select id,kind,description_markdown,created_by,claimed_by,published_revision_id,avatar_file_id
		from creators where public_id=$1 for update`, publicID).
		Scan(&id, &kind, &previousDescription, &createdBy, &claimedBy, &baseRevisionID, &currentAvatarFileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	canEdit := hasPermission(claims.Permissions, "admin.*") || hasPermission(claims.Permissions, "creator.edit") ||
		(createdBy != nil && *createdBy == claims.Subject) || (claimedBy != nil && *claimedBy == claims.Subject)
	if !canEdit {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	if snapshot.Kind != kind {
		writeError(w, http.StatusBadRequest, "creator kind cannot be changed")
		return
	}
	if err = s.resolveCreatorAvatarTx(r.Context(), tx, claims.Subject, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if snapshot.AvatarFileID == nil && snapshot.AvatarURL != "" {
		snapshot.AvatarFileID = currentAvatarFileID
	}
	if err = withdrawPendingContentRequestsTx(r.Context(), tx, "creator", publicID, claims.Subject, r); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to replace pending creator revision")
		return
	}
	status := "approved"
	if creatorReviewRequired(loadReviewConfig(r.Context(), s.db), kind, "edit") &&
		!hasPermission(claims.Permissions, "admin.*") {
		status = "pending"
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		AggregateType: "creator", AggregateKey: publicID, BaseRevision: baseRevisionID,
		Snapshot: raw, Reason: "Update creator profile", ActorID: claims.Subject,
		Source: "user", Status: status, Metadata: map[string]any{"creatorId": id, "kind": kind}, Request: r,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create creator revision")
		return
	}
	if status == "approved" {
		if err = applyCreatorSnapshotTx(r.Context(), tx, id, created.RevisionID, snapshot); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to publish creator revision")
			return
		}
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record creator approval")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit creator revision")
		return
	}
	addedBytes := activity.AddedMarkdownBytes(previousDescription, snapshot.DescriptionMarkdown)
	annotateActivity(r, activity.ActionEdit, creatorObjectType(kind), publicID, addedBytes, map[string]any{"revisionId": created.RevisionID})
	writeJSON(w, http.StatusOK, map[string]any{"revisionId": created.RevisionID, "reviewStatus": status})
}

func (s *Server) claimCreator(w http.ResponseWriter, r *http.Request) {
	publicID := strings.TrimSpace(strings.ToLower(r.PathValue("publicId")))
	var request struct {
		ProofMarkdown string `json:"proofMarkdown"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid claim payload")
		return
	}
	request.ProofMarkdown = strings.TrimSpace(request.ProofMarkdown)
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create claim")
		return
	}
	defer tx.Rollback(r.Context())
	var creatorID int64
	var kind, name string
	var claimedBy *int64
	err = tx.QueryRow(r.Context(), `select id,kind,name,claimed_by from creators where public_id=$1 and review_status='approved' for update`, publicID).
		Scan(&creatorID, &kind, &name, &claimedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	if claimedBy != nil {
		writeError(w, http.StatusConflict, "creator has already been claimed")
		return
	}
	status := "pending"
	if !creatorReviewRequired(loadReviewConfig(r.Context(), s.db), kind, "claim") ||
		hasPermission(claims.Permissions, "admin.*") {
		status = "approved"
	}
	var claimID int64
	err = tx.QueryRow(r.Context(), `insert into creator_claims(creator_id,user_id,proof_markdown,status,reviewed_by,reviewed_at)
		values($1,$2,$3,$4,case when $4='approved' then $2 else null end,case when $4='approved' then now() else null end)
		returning id`, creatorID, claims.Subject, request.ProofMarkdown, status).Scan(&claimID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a claim is already pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create claim")
		return
	}
	if status == "approved" {
		if err = s.approveCreatorClaimTx(r.Context(), tx, creatorID, claims.Subject, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to apply creator ownership")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit claim")
		return
	}
	annotateActivity(r, activity.ActionClaim, creatorObjectType(kind), publicID, len(request.ProofMarkdown), map[string]any{"claimId": claimID})
	writeJSON(w, http.StatusCreated, map[string]any{"id": claimID, "status": status, "name": name})
}

func (s *Server) adminCreatorClaims(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		select claim.id,creator.public_id,creator.kind,creator.name,claim.user_id,
		       account.username,account.display_name,claim.proof_markdown,claim.status,claim.created_at
		from creator_claims claim
		join creators creator on creator.id=claim.creator_id
		join users account on account.id=claim.user_id
		where claim.status='pending'
		order by claim.created_at,claim.id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator claims")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, userID int64
		var publicID, kind, name, username, displayName, proof, status string
		var createdAt time.Time
		if err = rows.Scan(&id, &publicID, &kind, &name, &userID, &username, &displayName, &proof, &status, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creator claims")
			return
		}
		items = append(items, map[string]any{
			"id": id, "creatorId": publicID, "kind": kind, "name": name,
			"userId": userID, "username": username, "displayName": displayName,
			"proofMarkdown": proof, "status": status, "createdAt": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) reviewCreatorClaim(w http.ResponseWriter, r *http.Request) {
	claimID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid claim")
		return
	}
	var request struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if decodeJSON(r, &request) != nil || (request.Status != "approved" && request.Status != "rejected") {
		writeError(w, http.StatusBadRequest, "invalid review result")
		return
	}
	claims := currentClaims(r)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review creator claim")
		return
	}
	defer tx.Rollback(r.Context())
	var creatorID, userID int64
	var publicID, kind, name, status string
	err = tx.QueryRow(r.Context(), `select creator.id,claim.user_id,creator.public_id,creator.kind,creator.name,claim.status
		from creator_claims claim join creators creator on creator.id=claim.creator_id
		where claim.id=$1 for update of claim,creator`, claimID).
		Scan(&creatorID, &userID, &publicID, &kind, &name, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim not found")
		return
	}
	if err != nil || status != "pending" {
		writeError(w, http.StatusConflict, "claim cannot be reviewed")
		return
	}
	if request.Status == "approved" {
		if err = s.approveCreatorClaimTx(r.Context(), tx, creatorID, userID, claims.Subject); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to apply creator ownership")
			return
		}
	}
	if _, err = tx.Exec(r.Context(), `update creator_claims set status=$2,reviewed_by=$3,review_note=$4,reviewed_at=now() where id=$1`,
		claimID, request.Status, claims.Subject, strings.TrimSpace(request.Note)); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save claim review")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit claim review")
		return
	}
	code := "creator_claim_approved"
	if request.Status == "rejected" {
		code = "creator_claim_rejected"
	}
	s.sendTemplatedNotification(r.Context(), userID, code, map[string]string{"name": name, "reason": request.Note}, map[string]any{
		"creatorId": publicID, "url": creatorPath(kind, publicID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": claimID, "status": request.Status})
}

func (s *Server) creatorRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `select id,code,name,description,translations,is_custom
		from creator_role_definitions order by is_custom,id`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator roles")
		return
	}
	defer rows.Close()
	items := make([]creatorRoleResponse, 0)
	for rows.Next() {
		var item creatorRoleResponse
		var translationsRaw []byte
		if err = rows.Scan(&item.ID, &item.Code, &item.Name, &item.Description, &translationsRaw, &item.Custom); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creator roles")
			return
		}
		_ = json.Unmarshal(translationsRaw, &item.Translations)
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createCreatorRole(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Code         string         `json:"code"`
		Name         string         `json:"name"`
		Description  string         `json:"description"`
		Translations map[string]any `json:"translations"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid creator role")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		writeError(w, http.StatusBadRequest, "role name is required")
		return
	}
	request.Code = normalizeCode(request.Code)
	if request.Code == "" {
		request.Code = "custom." + strings.ReplaceAll(normalizeCreatorName(request.Name), " ", "_")
	}
	raw, _ := json.Marshal(request.Translations)
	var id int64
	err := s.db.QueryRow(r.Context(), `insert into creator_role_definitions(
		code,name,description,translations,is_custom,created_by
	) values($1,$2,$3,$4,true,$5) returning id`,
		request.Code, request.Name, strings.TrimSpace(request.Description), raw, currentClaims(r).Subject,
	).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "role code already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create creator role")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "code": request.Code, "name": request.Name})
}

func (s *Server) createCreatorTx(ctx context.Context, tx pgx.Tx, snapshot creatorSnapshot, actorID int64, status string, r *http.Request) (int64, string, int64, error) {
	var id int64
	var publicID string
	err := tx.QueryRow(ctx, `insert into creators(
		kind,name,normalized_name,description_markdown,avatar_url,avatar_file_id,created_by,review_status
	) values($1,$2,$3,$4,$5,$6,$7,$8) returning id,public_id`,
		snapshot.Kind, snapshot.Name, normalizeCreatorName(snapshot.Name), snapshot.DescriptionMarkdown,
		snapshot.AvatarURL, snapshot.AvatarFileID, actorID, status,
	).Scan(&id, &publicID)
	if err != nil {
		return 0, "", 0, err
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		AggregateType: "creator", AggregateKey: publicID, Snapshot: raw,
		Reason: "Create creator profile", ActorID: actorID, Source: "user", Status: status,
		Metadata: map[string]any{"creatorId": id, "kind": snapshot.Kind}, Request: r,
	})
	if err != nil {
		return 0, "", 0, err
	}
	if err = applyCreatorRelationsTx(ctx, tx, id, snapshot); err != nil {
		return 0, "", 0, err
	}
	if status == "approved" {
		if _, err = tx.Exec(ctx, `update creators set published_revision_id=$2,review_status='approved' where id=$1`, id, created.RevisionID); err != nil {
			return 0, "", 0, err
		}
		if err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", actorID, "automatic approval", r); err != nil {
			return 0, "", 0, err
		}
	}
	return id, publicID, created.RevisionID, nil
}

func applyCreatorSnapshotTx(ctx context.Context, tx pgx.Tx, creatorID, revisionID int64, snapshot creatorSnapshot) error {
	if _, err := tx.Exec(ctx, `update creators set
		name=$2,normalized_name=$3,description_markdown=$4,avatar_url=$5,avatar_file_id=$6,
		review_status='approved',published_revision_id=$7,updated_at=now()
		where id=$1`, creatorID, snapshot.Name, normalizeCreatorName(snapshot.Name),
		snapshot.DescriptionMarkdown, snapshot.AvatarURL, snapshot.AvatarFileID, revisionID); err != nil {
		return err
	}
	return applyCreatorRelationsTx(ctx, tx, creatorID, snapshot)
}

func (s *Server) resolveCreatorAvatarTx(
	ctx context.Context,
	tx pgx.Tx,
	actorID int64,
	snapshot *creatorSnapshot,
) error {
	if snapshot.AvatarFileID == nil {
		return nil
	}
	if *snapshot.AvatarFileID <= 0 {
		snapshot.AvatarFileID = nil
		snapshot.AvatarURL = ""
		return nil
	}
	var objectKey, contentType string
	err := tx.QueryRow(ctx, `select object_key,content_type from oss_files
		where id=$1 and uploader_id=$2 and status='active'`,
		*snapshot.AvatarFileID, actorID,
	).Scan(&objectKey, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return errors.New("creator avatar file was not found")
	}
	if err != nil {
		return errors.New("failed to load creator avatar file")
	}
	if !strings.HasPrefix(strings.ToLower(contentType), "image/") {
		return errors.New("creator avatar must be an image")
	}
	snapshot.AvatarURL = buildPublicOSSURL(s.ossConfigFromSettings(ctx), objectKey)
	return nil
}

func applyCreatorRelationsTx(ctx context.Context, tx pgx.Tx, creatorID int64, snapshot creatorSnapshot) error {
	for _, statement := range []string{
		`delete from creator_links where creator_id=$1`,
		`delete from creator_collaborations where creator_id=$1`,
		`delete from creator_team_members where team_id=$1`,
	} {
		if _, err := tx.Exec(ctx, statement, creatorID); err != nil {
			return err
		}
	}
	for index, link := range snapshot.Links {
		if _, err := tx.Exec(ctx, `insert into creator_links(creator_id,link_type,url,label,display_order)
			values($1,$2,$3,$4,$5)`, creatorID, link.Type, link.URL, link.Label, index); err != nil {
			return err
		}
	}
	for _, publicID := range snapshot.CollaboratorIDs {
		var collaboratorID int64
		if err := tx.QueryRow(ctx, `select id from creators where public_id=$1`, publicID).Scan(&collaboratorID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into creator_collaborations(creator_id,collaborator_id)
			values($1,$2) on conflict do nothing`, creatorID, collaboratorID); err != nil {
			return err
		}
	}
	if snapshot.Kind != "team" {
		return nil
	}
	for index, member := range snapshot.Members {
		var memberID int64
		if err := tx.QueryRow(ctx, `select id from creators where public_id=$1 and kind='author'`, member.CreatorID).Scan(&memberID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into creator_team_members(
			team_id,member_creator_id,role_id,title,display_order
		) values($1,$2,$3,$4,$5)`, creatorID, memberID, member.RoleID, member.Title, index); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) approveCreatorClaimTx(ctx context.Context, tx pgx.Tx, creatorID, userID, reviewerID int64) error {
	var existing *int64
	if err := tx.QueryRow(ctx, `select claimed_by from creators where id=$1 for update`, creatorID).Scan(&existing); err != nil {
		return err
	}
	if existing != nil && *existing != userID {
		return errors.New("creator already claimed")
	}
	if _, err := tx.Exec(ctx, `update creators set claimed_by=$2,updated_at=now() where id=$1`, creatorID, userID); err != nil {
		return err
	}
	defaults := s.permissionDefaultsFromSettings(ctx)
	rows, err := tx.Query(ctx, `select distinct mod.id,mod.project_code
		from content_creator_bindings binding
		join mods mod on mod.project_code=binding.subject_public_id
		where binding.creator_id=$1 and binding.subject_type='mod'`, creatorID)
	if err != nil {
		return err
	}
	type ownedMod struct {
		id     int64
		unique string
	}
	mods := make([]ownedMod, 0)
	for rows.Next() {
		var item ownedMod
		if err = rows.Scan(&item.id, &item.unique); err != nil {
			rows.Close()
			return err
		}
		mods = append(mods, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, mod := range mods {
		if defaults.DeveloperRole != "" {
			if err = s.bindProjectRoleTx(ctx, tx, userID, defaults.DeveloperRole, mod.unique); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(ctx, `insert into mod_memberships(mod_id,user_id,role,granted_by)
			values($1,$2,'developer',$3) on conflict do nothing`, mod.id, userID, reviewerID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) creatorLinks(ctx context.Context, creatorID int64) ([]creatorLinkPayload, error) {
	rows, err := s.db.Query(ctx, `select link_type,url,label from creator_links where creator_id=$1 order by display_order,id`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]creatorLinkPayload, 0)
	for rows.Next() {
		var item creatorLinkPayload
		if err = rows.Scan(&item.Type, &item.URL, &item.Label); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) creatorCollaborators(ctx context.Context, creatorID int64) ([]creatorSummary, error) {
	rows, err := s.db.Query(ctx, `
		select distinct collaborator.public_id,collaborator.kind,collaborator.name,collaborator.avatar_url,
		       collaborator.review_status,collaborator.claimed_by is not null,
		       (select count(distinct mod.project_code) from content_creator_bindings binding
		        join mods mod on mod.project_code=binding.subject_public_id
		        where binding.creator_id=collaborator.id and binding.subject_type='mod' and mod.review_status='approved')
		from (
			select collaborator_id id from creator_collaborations where creator_id=$1
			union select creator_id from creator_collaborations where collaborator_id=$1
		) relation join creators collaborator on collaborator.id=relation.id
		where collaborator.review_status='approved'
		order by collaborator.name`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]creatorSummary, 0)
	for rows.Next() {
		var item creatorSummary
		if err = rows.Scan(&item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &item.ReviewStatus, &item.Claimed, &item.WorkCount); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) creatorMembers(ctx context.Context, creatorID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select member.public_id,member.name,member.avatar_url,role.id,role.code,role.name,relation.title
		from creator_team_members relation
		join creators member on member.id=relation.member_creator_id
		join creator_role_definitions role on role.id=relation.role_id
		where relation.team_id=$1
		order by relation.display_order,relation.created_at`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var publicID, name, avatarURL, roleCode, roleName, title string
		var roleID int64
		if err = rows.Scan(&publicID, &name, &avatarURL, &roleID, &roleCode, &roleName, &title); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"creatorId": publicID, "name": name, "avatarUrl": avatarURL,
			"role": map[string]any{"id": roleID, "code": roleCode, "name": roleName}, "title": title,
		})
	}
	return result, rows.Err()
}

func (s *Server) creatorWorks(ctx context.Context, creatorID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select distinct mod.project_code,mod.slug,mod.primary_name,mod.secondary_name,mod.summary,mod.icon_url
		from content_creator_bindings binding join mods mod on mod.project_code=binding.subject_public_id
		where binding.creator_id=$1 and binding.subject_type='mod' and mod.review_status='approved'
		order by mod.primary_name`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var uniqueID, siteID, primaryName, secondaryName, summary, iconURL string
		if err = rows.Scan(&uniqueID, &siteID, &primaryName, &secondaryName, &summary, &iconURL); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"uniqueId": uniqueID, "siteId": siteID, "primaryName": primaryName,
			"secondaryName": secondaryName, "summary": summary, "iconUrl": iconURL,
		})
	}
	return result, rows.Err()
}

func normalizeCreatorSnapshot(snapshot *creatorSnapshot) error {
	snapshot.Kind = strings.ToLower(strings.TrimSpace(snapshot.Kind))
	if snapshot.Kind != "author" && snapshot.Kind != "team" {
		return errors.New("creator kind must be author or team")
	}
	snapshot.Name = strings.TrimSpace(snapshot.Name)
	if snapshot.Name == "" || len([]byte(snapshot.Name)) > 160 {
		return errors.New("creator name is required and must not exceed 160 bytes")
	}
	snapshot.DescriptionMarkdown = strings.TrimSpace(snapshot.DescriptionMarkdown)
	snapshot.AvatarURL = strings.TrimSpace(snapshot.AvatarURL)
	if snapshot.AvatarURL != "" {
		parsed, err := url.ParseRequestURI(snapshot.AvatarURL)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return errors.New("avatar URL must use HTTP or HTTPS")
		}
	}
	seenLinks := map[string]struct{}{}
	cleanLinks := make([]creatorLinkPayload, 0, len(snapshot.Links))
	for _, link := range snapshot.Links {
		link.Type = normalizeCode(link.Type)
		link.URL = strings.TrimSpace(link.URL)
		link.Label = strings.TrimSpace(link.Label)
		parsed, err := url.ParseRequestURI(link.URL)
		if link.Type == "" || err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return errors.New("creator links must have a type and HTTP or HTTPS URL")
		}
		key := link.Type + "\x00" + link.URL
		if _, exists := seenLinks[key]; exists {
			continue
		}
		seenLinks[key] = struct{}{}
		cleanLinks = append(cleanLinks, link)
	}
	snapshot.Links = cleanLinks
	snapshot.CollaboratorIDs = uniquePublicIDs(snapshot.CollaboratorIDs)
	if snapshot.Kind != "team" {
		snapshot.Members = nil
	}
	return nil
}

func normalizeCreatorName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), " "))
}

func uniquePublicIDs(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if len(value) != 9 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func creatorObjectType(kind string) int16 {
	if kind == "team" {
		return activity.ObjectTeam
	}
	return activity.ObjectAuthor
}

func creatorPath(kind, publicID string) string {
	if kind == "team" {
		return "/teams/" + publicID
	}
	return "/authors/" + publicID
}

func boundedInt(value string, fallback, minimum, maximum int) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	if parsed < minimum {
		return minimum
	}
	if parsed > maximum {
		return maximum
	}
	return parsed
}

func creatorNameTx(ctx context.Context, tx pgx.Tx, creatorID int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `select name from creators where id=$1`, creatorID).Scan(&name)
	return name, err
}

func creatorRoleNameTx(ctx context.Context, tx pgx.Tx, roleID *int64) (string, error) {
	if roleID == nil {
		return "", nil
	}
	var name string
	err := tx.QueryRow(ctx, `select name from creator_role_definitions where id=$1`, *roleID).Scan(&name)
	return name, err
}
