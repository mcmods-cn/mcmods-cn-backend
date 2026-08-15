package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	RoleID    string `json:"roleId"`
	Title     string `json:"title"`
}

type creatorSnapshot struct {
	Kind                string                    `json:"kind"`
	Name                string                    `json:"name"`
	DescriptionMarkdown string                    `json:"descriptionMarkdown"`
	DefaultLocale       string                    `json:"defaultLocale"`
	Localizations       []creatorLocalizationEdit `json:"localizations"`
	AvatarURL           string                    `json:"avatarUrl"`
	AvatarFileID        *string                   `json:"avatarFileId,omitempty"`
	AvatarInternalID    *int64                    `json:"-"`
	Links               []creatorLinkPayload      `json:"links"`
	Members             []creatorMemberPayload    `json:"members"`
}

type creatorLocalizationEdit struct {
	Locale          string `json:"locale"`
	ContentMarkdown string `json:"contentMarkdown"`
}

type creatorClaimAttachment struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
}

const (
	creatorClaimMaxFiles      = 5
	creatorClaimMaxTotalBytes = int64(10 << 20)
)

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
	ID           string         `json:"id"`
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
	admin := claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.review")
	indexed := s.searchCreatorPage(r.Context(), query, kind, claims, admin, limit)
	indexedCounts := s.searchCreatorCounts(r.Context(), query, claims, admin)
	var authorCount, teamCount int
	if indexedCounts.Used {
		authorCount, teamCount = indexedCounts.Author, indexedCounts.Team
	} else if err := s.db.QueryRow(r.Context(), `
		select count(*) filter(where creator.kind='author'),
		       count(*) filter(where creator.kind='team')
		from creators creator
		where ($1='' or creator.name ilike '%' || $1 || '%')
		  and (creator.review_status='approved' or creator.created_by=$2 or creator.claimed_by=$2 or $3)`,
		query, claims.Subject, admin).Scan(&authorCount, &teamCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count creators")
		return
	}
	rows, err := s.db.Query(r.Context(), `
		select creator.public_id,creator.kind,creator.name,creator.avatar_url,creator.review_status,
		       creator.claimed_by is not null,count(distinct mod.project_code)
		from creators creator
		left join content_creator_bindings binding on binding.creator_id=creator.id and binding.subject_type='mod'
		left join mods mod on mod.id=binding.subject_id and mod.review_status='approved'
		where ($1='' or creator.kind=$1)
		  and (($5 and creator.id=any($6::bigint[])) or (not $5 and ($2='' or creator.name ilike '%' || $2 || '%')))
		  and (creator.review_status='approved' or creator.created_by=$3 or creator.claimed_by=$3 or $4)
		group by creator.id
		order by case when $5 then array_position($6::bigint[],creator.id) end,
			creator.review_status='approved' desc,lower(creator.name),creator.id
		limit $7`, kind, query, claims.Subject, admin, indexed.Used, indexed.IDs, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creators")
		return
	}
	defer rows.Close()
	items := make([]creatorSummary, 0)
	ossCfg := s.ossConfigFromSettings(r.Context())
	for rows.Next() {
		var item creatorSummary
		if err = rows.Scan(&item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &item.ReviewStatus, &item.Claimed, &item.WorkCount); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creators")
			return
		}
		item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.AvatarURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate creator avatar URL")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creators")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"counts": map[string]int{"author": authorCount, "team": teamCount},
	})
}

func (s *Server) creatorDetail(w http.ResponseWriter, r *http.Request) {
	publicID := strings.TrimSpace(strings.ToLower(r.PathValue("publicId")))
	claims := currentClaims(r)
	admin := claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.review")
	var id int64
	var item creatorSummary
	var description string
	var avatarFileID *string
	var createdBy, claimedBy *int64
	var publishedRevisionID *int64
	err := s.db.QueryRow(r.Context(), `
		select id,public_id,kind,name,avatar_url,description_markdown,review_status,created_by,claimed_by,published_revision_id,
		       (select public_id from oss_files where id=creators.avatar_file_id)
		from creators
		where public_id=$1 and (review_status='approved' or created_by=$2 or claimed_by=$2 or $3)`,
		publicID, claims.Subject, admin,
	).Scan(&id, &item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &description, &item.ReviewStatus, &createdBy, &claimedBy, &publishedRevisionID, &avatarFileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	item.Claimed = claimedBy != nil
	item.AvatarURL, err = s.resolveStoredOSSObjectAccessURL(r.Context(), item.AvatarURL)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to generate creator avatar URL")
		return
	}
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
	members := make([]map[string]any, 0)
	teams := make([]map[string]any, 0)
	if item.Kind == "team" {
		members, err = s.creatorMembers(r.Context(), id, claims.Subject, admin)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load team members")
			return
		}
	} else {
		teams, err = s.creatorTeams(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load creator teams")
			return
		}
	}
	works, err := s.creatorWorks(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator works")
		return
	}
	defaultLocale, localizations, err := s.creatorLocalizations(r.Context(), id, item.Kind, item.Name, description)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator localizations")
		return
	}
	var claimedUser any
	if claimedBy != nil {
		var userPublicID, username, avatarURL string
		if scanErr := s.db.QueryRow(r.Context(), `select public_id,username,avatar_url from users where id=$1`, *claimedBy).
			Scan(&userPublicID, &username, &avatarURL); scanErr == nil {
			avatarURL, _ = s.resolveStoredOSSObjectAccessURL(r.Context(), avatarURL)
			claimedUser = map[string]any{
				"id": userPublicID, "publicId": userPublicID, "username": username,
				"avatarUrl": avatarURL,
			}
		}
	}
	canEdit := admin || claims.Subject > 0 &&
		((createdBy != nil && *createdBy == claims.Subject) || (claimedBy != nil && *claimedBy == claims.Subject)) ||
		claimsAllow(claims, "creator.edit")
	writeJSON(w, http.StatusOK, map[string]any{
		"creator": item, "descriptionMarkdown": description, "links": links,
		"collaborators": collaborators, "members": members, "teams": teams, "works": works,
		"defaultLocale": defaultLocale, "localizations": localizations,
		"claimedUser": claimedUser, "canEdit": canEdit,
		"avatarFileId":        avatarFileID,
		"canClaim":            claims.Subject > 0 && claimedBy == nil && item.ReviewStatus == "approved",
		"publishedRevisionId": revisionPublicIDValue(r.Context(), s.db, publishedRevisionID),
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
		!claimsAllow(claims, "admin.*")
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
	_, publicID, created, err := createCreatorTx(r.Context(), tx, snapshot, claims.Subject, status, r)
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
	annotateActivity(r, activity.ActionCreate, creatorObjectType(snapshot.Kind), publicID, len(snapshot.DescriptionMarkdown))
	writeJSON(w, http.StatusCreated, map[string]any{"publicId": publicID, "revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID, "reviewStatus": status})
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
	var createdBy, claimedBy, baseRevisionID *int64
	var currentAvatarFileID *string
	err = tx.QueryRow(r.Context(), `select id,kind,description_markdown,created_by,claimed_by,published_revision_id,
		(select public_id from oss_files where id=creators.avatar_file_id)
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
	canEdit := claimsAllow(claims, "admin.*") || claimsAllow(claims, "creator.edit") ||
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
	status := "approved"
	if creatorReviewRequired(loadReviewConfig(r.Context(), s.db), kind, "edit") &&
		!claimsAllow(claims, "admin.*") {
		status = "pending"
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: kind, EntityID: id, AggregateType: "creator", AggregateKey: publicID, BaseRevision: baseRevisionID,
		Snapshot: raw, Reason: "Update creator profile", ActorID: claims.Subject,
		Source: "user", Status: status, Metadata: map[string]any{"creatorId": publicID, "kind": kind}, Request: r,
	})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create creator revision")
		return
	}
	if status == "approved" {
		if err = s.applyCreatorSnapshotTx(r.Context(), tx, id, created.RevisionID, claims.Subject, claims.Subject, snapshot); err != nil {
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
	addedBytes, deletedBytes := activity.MarkdownDeltaBytes(previousDescription, snapshot.DescriptionMarkdown)
	annotateActivityDelta(r, activity.ActionEdit, creatorObjectType(kind), publicID, addedBytes, deletedBytes)
	writeJSON(w, http.StatusOK, map[string]any{"revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID, "reviewStatus": status})
}

func (s *Server) claimCreator(w http.ResponseWriter, r *http.Request) {
	publicID := strings.TrimSpace(strings.ToLower(r.PathValue("publicId")))
	var request struct {
		ProofMarkdown string   `json:"proofMarkdown"`
		ProofFileIDs  []string `json:"proofFileIds"`
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
	request.ProofFileIDs = uniquePublicIDs(request.ProofFileIDs)
	proofFiles, err := resolveCreatorClaimProofFiles(r.Context(), tx, claims.Subject, request.ProofFileIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	status := "pending"
	if !creatorReviewRequired(loadReviewConfig(r.Context(), s.db), kind, "claim") ||
		claimsAllow(claims, "admin.*") {
		status = "approved"
	}
	var claimID int64
	var claimPublicID string
	err = tx.QueryRow(r.Context(), `insert into creator_claims(creator_id,user_id,proof_markdown,status,reviewed_by,reviewed_at)
		values($1,$2,$3,$4,case when $4='approved' then $2 else null end,case when $4='approved' then now() else null end)
		returning id,public_id`, creatorID, claims.Subject, request.ProofMarkdown, status).Scan(&claimID, &claimPublicID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a claim is already pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create claim")
		return
	}
	for index, file := range proofFiles {
		if _, err = tx.Exec(r.Context(), `insert into creator_claim_attachments(claim_id,oss_file_id,display_order)
			values($1,$2,$3)`, claimID, file.InternalID, index); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save claim attachments")
			return
		}
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
	annotateActivity(r, activity.ActionClaim, creatorObjectType(kind), publicID, len(request.ProofMarkdown))
	writeJSON(w, http.StatusCreated, map[string]any{"id": claimPublicID, "status": status, "name": name})
}

func (s *Server) adminCreatorClaims(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		select claim.public_id,creator.public_id,creator.kind,creator.name,account.public_id,
			account.username,claim.proof_markdown,claim.status,claim.created_at
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
		var id, publicID, kind, name, userID, username, proof, status string
		var createdAt time.Time
		if err = rows.Scan(&id, &publicID, &kind, &name, &userID, &username, &proof, &status, &createdAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creator claims")
			return
		}
		attachments, attachmentErr := s.creatorClaimAttachments(r.Context(), id)
		if attachmentErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to load creator claim attachments")
			return
		}
		items = append(items, map[string]any{
			"id": id, "creatorId": publicID, "kind": kind, "name": name,
			"userId": userID, "username": username,
			"proofMarkdown": proof, "attachments": attachments, "status": status, "createdAt": createdAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) reviewCreatorClaim(w http.ResponseWriter, r *http.Request) {
	claimPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(claimPublicID) {
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
	var claimID, creatorID, userID int64
	var publicID, kind, name, status string
	err = tx.QueryRow(r.Context(), `select claim.id,creator.id,claim.user_id,creator.public_id,creator.kind,creator.name,claim.status
		from creator_claims claim join creators creator on creator.id=claim.creator_id
		where claim.public_id=$1 for update of claim,creator`, claimPublicID).
		Scan(&claimID, &creatorID, &userID, &publicID, &kind, &name, &status)
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
		"creatorId": publicID, "targetLabel": name, "url": creatorPath(kind, publicID),
	})
	writeJSON(w, http.StatusOK, map[string]any{"id": claimPublicID, "status": request.Status})
}

func (s *Server) creatorRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `select public_id,code,name,description,translations,is_custom
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

func (s *Server) presignCreatorClaimAttachment(w http.ResponseWriter, r *http.Request) {
	claimID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	fileID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	if !validCatalogPublicID(claimID) || !validCatalogPublicID(fileID) {
		writeError(w, http.StatusBadRequest, "invalid claim attachment")
		return
	}
	file, err := lookupReviewAttachment(r.Context(), s.db, reviewAttachmentLookup{
		Kind: reviewAttachmentForCreatorClaim, SubjectPublicID: claimID,
	}, fileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "claim attachment was not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load claim attachment")
		return
	}
	s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: file.ObjectKey})
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
	var id string
	err := s.db.QueryRow(r.Context(), `insert into creator_role_definitions(
		code,name,description,translations,is_custom,created_by
	) values($1,$2,$3,$4,true,$5) returning public_id`,
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

func createCreatorTx(ctx context.Context, tx pgx.Tx, snapshot creatorSnapshot, actorID int64, status string, r *http.Request) (int64, string, createdContentRevision, error) {
	var id int64
	var publicID string
	err := tx.QueryRow(ctx, `insert into creators(
		kind,name,normalized_name,description_markdown,avatar_url,avatar_file_id,created_by,review_status
	) values($1,$2,$3,$4,$5,$6,$7,$8) returning id,public_id`,
		snapshot.Kind, snapshot.Name, normalizeCreatorName(snapshot.Name), snapshot.DescriptionMarkdown,
		snapshot.AvatarURL, snapshot.AvatarInternalID, actorID, status,
	).Scan(&id, &publicID)
	if err != nil {
		return 0, "", createdContentRevision{}, err
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(ctx, tx, createContentRevisionParams{
		EntityType: snapshot.Kind, EntityID: id, AggregateType: "creator", AggregateKey: publicID, Snapshot: raw,
		Reason: "Create creator profile", ActorID: actorID, Source: "user", Status: status,
		Metadata: map[string]any{"creatorId": publicID, "kind": snapshot.Kind}, Request: r,
	})
	if err != nil {
		return 0, "", createdContentRevision{}, err
	}
	if err = applyCreatorRelationsTx(ctx, tx, id, snapshot); err != nil {
		return 0, "", createdContentRevision{}, err
	}
	if status == "approved" {
		if err = publishCreatorLocalizationsTx(ctx, tx, id, snapshot.Kind, snapshot.Name, snapshot.DefaultLocale, snapshot.Localizations, created.RevisionID, actorID); err != nil {
			return 0, "", createdContentRevision{}, err
		}
		if _, err = tx.Exec(ctx, `update creators set published_revision_id=$2,review_status='approved' where id=$1`, id, created.RevisionID); err != nil {
			return 0, "", createdContentRevision{}, err
		}
		if err = appendReviewResolutionTx(ctx, tx, created.ChangeRequestID, "approved", actorID, "automatic approval", r); err != nil {
			return 0, "", createdContentRevision{}, err
		}
	}
	return id, publicID, created, nil
}

func ensureNamedCreatorSnapshotTx(ctx context.Context, tx pgx.Tx, snapshot creatorSnapshot, actorID int64, status string, r *http.Request) (int64, string, bool, error) {
	if snapshot.Kind != "author" && snapshot.Kind != "team" {
		snapshot.Kind = "author"
	}
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		return 0, "", false, err
	}
	var id int64
	var publicID string
	identityType, identityURL := creatorIdentityLink(snapshot.Links)
	err := tx.QueryRow(ctx, `select id,public_id from creators
		where kind=$1 and (
			normalized_name=$2 or ($3<>'' and exists(
				select 1 from creator_links link where link.creator_id=creators.id and link.link_type=$3 and link.url=$4
			))
		order by ($3<>'' and exists(
			select 1 from creator_links link where link.creator_id=creators.id and link.link_type=$3 and link.url=$4
		)) desc,review_status='approved' desc,id limit 1`,
		snapshot.Kind, normalizeCreatorName(snapshot.Name), identityType, identityURL).Scan(&id, &publicID)
	if err == nil {
		return id, publicID, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, err
	}
	id, publicID, _, err = createCreatorTx(ctx, tx, snapshot, actorID, status, r)
	if err != nil {
		return 0, "", false, fmt.Errorf("create imported creator: %w", err)
	}
	return id, publicID, true, nil
}

func creatorIdentityLink(links []creatorLinkPayload) (string, string) {
	for _, link := range links {
		switch link.Type {
		case "modrinth", "curseforge":
			if link.URL != "" {
				return link.Type, link.URL
			}
		}
	}
	return "", ""
}

func (s *Server) applyCreatorSnapshotTx(
	ctx context.Context,
	tx pgx.Tx,
	creatorID, revisionID, actorID, uploaderID int64,
	snapshot creatorSnapshot,
) error {
	if err := normalizeCreatorSnapshot(&snapshot); err != nil {
		return err
	}
	var avatarFileID *int64
	if snapshot.AvatarFileID != nil {
		file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, *snapshot.AvatarFileID, ossRasterBindingScope{UploaderID: uploaderID})
		if err != nil {
			return err
		}
		avatarFileID = &file.ID
		snapshot.AvatarURL = ossStoredObjectURL(s.ossConfigFromSettings(ctx), file.ObjectKey)
	}
	if _, err := tx.Exec(ctx, `update creators set
		name=$2,normalized_name=$3,description_markdown=$4,avatar_url=$5,avatar_file_id=$6,
		review_status='approved',published_revision_id=$7,updated_at=now()
		where id=$1`, creatorID, snapshot.Name, normalizeCreatorName(snapshot.Name),
		snapshot.DescriptionMarkdown, snapshot.AvatarURL, avatarFileID, revisionID); err != nil {
		return err
	}
	if err := publishCreatorLocalizationsTx(ctx, tx, creatorID, snapshot.Kind, snapshot.Name, snapshot.DefaultLocale, snapshot.Localizations, revisionID, actorID); err != nil {
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
	publicID := strings.ToLower(strings.TrimSpace(*snapshot.AvatarFileID))
	if publicID == "" {
		snapshot.AvatarFileID = nil
		snapshot.AvatarInternalID = nil
		snapshot.AvatarURL = ""
		return nil
	}
	file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, publicID, ossRasterBindingScope{UploaderID: actorID})
	if err != nil {
		return errors.New("creator avatar file was not found")
	}
	snapshot.AvatarFileID = &publicID
	snapshot.AvatarInternalID = &file.ID
	snapshot.AvatarURL = ossStoredObjectURL(s.ossConfigFromSettings(ctx), file.ObjectKey)
	return nil
}

func applyCreatorRelationsTx(ctx context.Context, tx pgx.Tx, creatorID int64, snapshot creatorSnapshot) error {
	for _, statement := range []string{
		`delete from creator_links where creator_id=$1`,
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
	if snapshot.Kind != "team" {
		return nil
	}
	for index, member := range snapshot.Members {
		var memberID int64
		if err := tx.QueryRow(ctx, `select id from creators where public_id=$1 and kind='author'`, member.CreatorID).Scan(&memberID); err != nil {
			return err
		}
		var roleID int64
		if err := tx.QueryRow(ctx, `select id from creator_role_definitions where public_id=$1`, member.RoleID).Scan(&roleID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `insert into creator_team_members(
			team_id,member_creator_id,role_id,title,display_order
		) values($1,$2,$3,$4,$5)`, creatorID, memberID, roleID, member.Title, index); err != nil {
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
		join mods mod on mod.id=binding.subject_id
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
		       (select count(distinct mod.id) from content_creator_bindings binding
		        join mods mod on mod.id=binding.subject_id
		        where binding.creator_id=collaborator.id and binding.subject_type='mod' and mod.review_status='approved')
		from content_creator_bindings own_binding
		join content_creator_bindings collaborator_binding
		  on collaborator_binding.subject_type=own_binding.subject_type
		 and collaborator_binding.subject_id=own_binding.subject_id
		 and collaborator_binding.creator_id<>own_binding.creator_id
		join creators collaborator on collaborator.id=collaborator_binding.creator_id
		where own_binding.creator_id=$1
		  and (own_binding.subject_type<>'mod' or exists(
		    select 1 from mods shared_mod where shared_mod.id=own_binding.subject_id and shared_mod.review_status='approved'
		  ))
		  and collaborator.review_status='approved'
		order by collaborator.name`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]creatorSummary, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var item creatorSummary
		if err = rows.Scan(&item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &item.ReviewStatus, &item.Claimed, &item.WorkCount); err != nil {
			return nil, err
		}
		item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, item.AvatarURL)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Server) creatorMembers(ctx context.Context, creatorID, viewerID int64, admin bool) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select member.public_id,member.name,member.avatar_url,role.public_id,role.code,role.name,relation.title
		from creator_team_members relation
		join creators member on member.id=relation.member_creator_id
		join creator_role_definitions role on role.id=relation.role_id
		where relation.team_id=$1
		  and (member.review_status='approved' or member.created_by=$2 or $3)
		order by relation.display_order,relation.created_at`, creatorID, viewerID, admin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var publicID, name, avatarURL, roleID, roleCode, roleName, title string
		if err = rows.Scan(&publicID, &name, &avatarURL, &roleID, &roleCode, &roleName, &title); err != nil {
			return nil, err
		}
		avatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, avatarURL)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"creatorId": publicID, "name": name, "avatarUrl": avatarURL,
			"role": map[string]any{"id": roleID, "code": roleCode, "name": roleName}, "title": title,
		})
	}
	return result, rows.Err()
}

func (s *Server) creatorTeams(ctx context.Context, creatorID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select team.public_id,team.kind,team.name,team.avatar_url,team.review_status,
		       team.claimed_by is not null,
		       (select count(distinct mod.id) from content_creator_bindings binding
		        join mods mod on mod.id=binding.subject_id
		        where binding.creator_id=team.id and binding.subject_type='mod' and mod.review_status='approved'),
		       role.public_id,role.code,role.name,relation.title
		from creator_team_members relation
		join creators team on team.id=relation.team_id
		join creator_role_definitions role on role.id=relation.role_id
		where relation.member_creator_id=$1 and team.review_status='approved'
		order by lower(team.name),team.id,relation.display_order`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var team creatorSummary
		var roleID, roleCode, roleName, title string
		if err = rows.Scan(
			&team.PublicID, &team.Kind, &team.Name, &team.AvatarURL, &team.ReviewStatus,
			&team.Claimed, &team.WorkCount, &roleID, &roleCode, &roleName, &title,
		); err != nil {
			return nil, err
		}
		team.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, team.AvatarURL)
		if err != nil {
			return nil, err
		}
		result = append(result, map[string]any{
			"team":  team,
			"role":  map[string]any{"id": roleID, "code": roleCode, "name": roleName},
			"title": title,
		})
	}
	return result, rows.Err()
}

func (s *Server) creatorWorks(ctx context.Context, creatorID int64) ([]map[string]any, error) {
	rows, err := s.db.Query(ctx, `
		select distinct mod.project_code,mod.slug,mod.primary_name,mod.secondary_name,mod.summary,mod.icon_url
		from content_creator_bindings binding join mods mod on mod.id=binding.subject_id
		where binding.creator_id=$1 and binding.subject_type='mod' and mod.review_status='approved'
		order by mod.primary_name`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var uniqueID, siteID, primaryName, secondaryName, summary, iconURL string
		if err = rows.Scan(&uniqueID, &siteID, &primaryName, &secondaryName, &summary, &iconURL); err != nil {
			return nil, err
		}
		iconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, iconURL)
		if err != nil {
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
	snapshot.DescriptionMarkdown = strings.TrimSpace(snapshot.DescriptionMarkdown)
	if snapshot.Name == "" || len([]byte(snapshot.Name)) > 160 {
		return errors.New("creator name is required and must not exceed 160 bytes")
	}
	if strings.TrimSpace(snapshot.DefaultLocale) == "" {
		snapshot.DefaultLocale = defaultCreatorLocale(snapshot.DescriptionMarkdown, snapshot.Name)
	}
	if len(snapshot.Localizations) == 0 {
		snapshot.Localizations = []creatorLocalizationEdit{{
			Locale: snapshot.DefaultLocale, ContentMarkdown: snapshot.DescriptionMarkdown,
		}}
	}
	defaultLocale, localizations, localizationErr := normalizeCreatorLocalizations(snapshot.DefaultLocale, snapshot.Localizations)
	if localizationErr != nil {
		return errors.New("localized creator content is invalid")
	}
	snapshot.DefaultLocale, snapshot.Localizations = defaultLocale, localizations
	for _, localization := range localizations {
		if localization.Locale != defaultLocale {
			continue
		}
		snapshot.DescriptionMarkdown = localization.ContentMarkdown
		break
	}
	snapshot.AvatarURL = strings.TrimSpace(snapshot.AvatarURL)
	if snapshot.AvatarURL != "" && !validHTTPURL(snapshot.AvatarURL) {
		return errors.New("avatar URL must use HTTP or HTTPS")
	}
	seenLinks := map[string]struct{}{}
	cleanLinks := make([]creatorLinkPayload, 0, len(snapshot.Links))
	for _, link := range snapshot.Links {
		link.Type = normalizeCode(link.Type)
		link.URL = strings.TrimSpace(link.URL)
		link.Label = strings.TrimSpace(link.Label)
		if link.Type == "" || !validHTTPURL(link.URL) || len(link.Label) > 160 {
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
	if snapshot.Kind != "team" {
		snapshot.Members = nil
	}
	return nil
}

func (s *Server) creatorLocalizations(ctx context.Context, creatorID int64, kind, name, description string) (string, []creatorLocalizationEdit, error) {
	defaultLocale := ""
	if err := s.db.QueryRow(ctx, `select default_locale from content_subjects where subject_type=$1 and subject_id=$2`, kind, creatorID).Scan(&defaultLocale); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", nil, err
	}
	rows, err := s.db.Query(ctx, `select locale,content_markdown from content_localizations
		where subject_type=$1 and subject_id=$2 and review_status='approved' order by locale`, kind, creatorID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	localizations := make([]creatorLocalizationEdit, 0)
	for rows.Next() {
		var item creatorLocalizationEdit
		if err = rows.Scan(&item.Locale, &item.ContentMarkdown); err != nil {
			return "", nil, err
		}
		localizations = append(localizations, item)
	}
	if err = rows.Err(); err != nil {
		return "", nil, err
	}
	if len(localizations) == 0 {
		defaultLocale = defaultCreatorLocale(name, description)
		localizations = append(localizations, creatorLocalizationEdit{Locale: defaultLocale, ContentMarkdown: description})
	}
	if defaultLocale == "" {
		defaultLocale = localizations[0].Locale
	}
	return defaultLocale, localizations, nil
}

func publishCreatorLocalizationsTx(ctx context.Context, tx pgx.Tx, creatorID int64, kind, name, defaultLocale string, localizations []creatorLocalizationEdit, revisionID, actorID int64) error {
	if _, err := tx.Exec(ctx, `insert into content_subjects(subject_type,subject_id,default_locale)
		values($1,$2,$3) on conflict(subject_type,subject_id) do update
		set default_locale=excluded.default_locale,updated_at=now()`, kind, creatorID, defaultLocale); err != nil {
		return err
	}
	catalogLocalizations := make([]catalogLocalizationEdit, 0, len(localizations))
	for _, localization := range localizations {
		catalogLocalizations = append(catalogLocalizations, catalogLocalizationEdit{
			Locale: localization.Locale, Name: name, ContentMarkdown: localization.ContentMarkdown,
		})
	}
	if err := publishCatalogLocalizationsTx(ctx, tx, creatorID, "", kind, catalogLocalizations, revisionID, actorID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `update content_localizations set name=$3
		where subject_type=$1 and subject_id=$2 and name<>$3`, kind, creatorID, name)
	return err
}

func normalizeCreatorLocalizations(defaultLocale string, localizations []creatorLocalizationEdit) (string, []creatorLocalizationEdit, error) {
	seen := make(map[string]struct{}, len(localizations))
	for index := range localizations {
		locale, err := normalizeCatalogLocale(localizations[index].Locale)
		if err != nil || !isEditableContentLocale(locale) {
			return "", nil, errCatalogEditorInvalid
		}
		if _, exists := seen[locale]; exists {
			return "", nil, errCatalogEditorInvalid
		}
		if len(localizations[index].ContentMarkdown) > maxModExportEntryMarkdownBytes {
			return "", nil, errCatalogEditorInvalid
		}
		seen[locale] = struct{}{}
		localizations[index].Locale = locale
	}
	if strings.TrimSpace(defaultLocale) == "" {
		defaultLocale = "en-US"
	}
	normalizedDefault, err := normalizeCatalogLocale(defaultLocale)
	if err != nil || !isEditableContentLocale(normalizedDefault) {
		return "", nil, errCatalogEditorInvalid
	}
	if _, exists := seen[normalizedDefault]; !exists {
		return "", nil, errCatalogEditorInvalid
	}
	return normalizedDefault, localizations, nil
}

func defaultCreatorLocale(values ...string) string {
	for _, value := range values {
		for _, char := range value {
			if unicode.Is(unicode.Han, char) {
				return "zh-CN"
			}
		}
	}
	return "en-US"
}

func resolveCreatorClaimProofFiles(ctx context.Context, tx pgx.Tx, userID int64, publicIDs []string) ([]reviewAttachmentFile, error) {
	if len(publicIDs) > creatorClaimMaxFiles {
		return nil, errors.New("claim attachments cannot exceed 5 files")
	}
	files := make([]reviewAttachmentFile, 0, len(publicIDs))
	var total int64
	for _, publicID := range publicIDs {
		file, err := lookupReviewAttachment(ctx, tx, reviewAttachmentLookup{
			Kind: reviewAttachmentByUploader, UploaderID: userID,
		}, publicID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("claim attachment was not found or does not belong to the current user")
		}
		if err != nil {
			return nil, errors.New("failed to load claim attachment")
		}
		total += file.SizeBytes
		files = append(files, file)
	}
	if total > creatorClaimMaxTotalBytes {
		return nil, errors.New("claim attachments cannot exceed 10 MB in total")
	}
	return files, nil
}

func (s *Server) creatorClaimAttachments(ctx context.Context, claimPublicID string) ([]creatorClaimAttachment, error) {
	rows, err := s.db.Query(ctx, `select file.public_id,file.original_name,greatest(file.size_bytes,file.source_size_bytes)
		from creator_claim_attachments attachment
		join creator_claims claim on claim.id=attachment.claim_id
		join oss_files file on file.id=attachment.oss_file_id
		where claim.public_id=$1 and `+safeReviewAttachmentPredicate+`
		order by attachment.display_order,file.id`, claimPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]creatorClaimAttachment, 0)
	for rows.Next() {
		var item creatorClaimAttachment
		if err = rows.Scan(&item.ID, &item.Name, &item.SizeBytes); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
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

func creatorRoleNameTx(ctx context.Context, tx pgx.Tx, roleID *string) (string, error) {
	if roleID == nil {
		return "", nil
	}
	var name string
	err := tx.QueryRow(ctx, `select name from creator_role_definitions where public_id=$1`, *roleID).Scan(&name)
	return name, err
}

func creatorRoleInternalIDTx(ctx context.Context, tx pgx.Tx, roleID *string) (*int64, error) {
	if roleID == nil {
		return nil, nil
	}
	var internalID int64
	if err := tx.QueryRow(ctx, `select id from creator_role_definitions where public_id=$1`, *roleID).Scan(&internalID); err != nil {
		return nil, err
	}
	return &internalID, nil
}
