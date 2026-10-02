package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/security"
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
	Members             []creatorMemberPayload    `json:"members,omitempty"`
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

type creatorClaimReviewResponse struct {
	ID            string                   `json:"id"`
	InternalID    int64                    `json:"-"`
	CreatorID     string                   `json:"creatorId"`
	Kind          string                   `json:"kind"`
	Name          string                   `json:"name"`
	UserID        string                   `json:"userId"`
	Username      string                   `json:"username"`
	ProofMarkdown string                   `json:"proofMarkdown"`
	Attachments   []creatorClaimAttachment `json:"attachments"`
	Status        string                   `json:"status"`
	CreatedAt     time.Time                `json:"createdAt"`
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

type creatorCapabilitySet struct {
	EditProfile   bool
	ManageMembers bool
	CreateRoles   bool
}

func resolveCreatorCapabilities(claims security.Claims, kind, publicID string) creatorCapabilitySet {
	admin := claimsAllow(claims, "admin.*")
	return creatorCapabilitySet{
		EditProfile:   admin || claimsAllow(claims, "creator.edit") || claimsAllow(claims, "creator.edit."+publicID),
		ManageMembers: kind == "team" && (admin || claimsAllow(claims, "team.members.manage")),
		CreateRoles:   admin || claimsAllow(claims, "creator.role.write"),
	}
}

func (s *Server) creators(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	admin := claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.review")
	request, err := parseCreatorPageRequest(r.URL.Query(), claims.Subject, admin)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, query := request.Kind, request.Query
	indexedOffset := 0
	if request.Cursor != nil && request.Cursor.Mode == creatorPageModeIndex {
		indexedOffset = request.Cursor.Offset
	}
	indexed := indexedSearchPage{}
	if catalogSortUsesSearchIndex(request.Sort) && (request.Cursor == nil || request.Cursor.Mode == creatorPageModeIndex) {
		indexed = s.searchCreatorPage(r.Context(), query, kind, claims, admin, request.Limit, indexedOffset)
		if request.Cursor != nil && request.Cursor.Mode == creatorPageModeIndex && !indexed.Used {
			writeError(w, http.StatusServiceUnavailable, "creator search cursor is temporarily unavailable")
			return
		}
	}
	indexedCounts := s.searchCreatorCounts(r.Context(), query, claims, admin)
	var authorCount, teamCount int
	if indexedCounts.Used {
		authorCount, teamCount = indexedCounts.Author, indexedCounts.Team
	} else if err := s.db.QueryRow(r.Context(), `
		select count(*) filter(where creator.kind='author'),
		       count(*) filter(where creator.kind='team')
		from creators creator
		where ($1='' or creator.name ilike '%' || $1 || '%')
		  and (creator.review_status='approved' or creator.created_by=$2 or $3)`,
		query, claims.Subject, admin).Scan(&authorCount, &teamCount); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count creators")
		return
	}
	orderSQL := catalogOrderSQL(request.Sort, request.Direction, indexed.Used, 6,
		"creator.created_at", "creator.updated_at", "creator.id", "creator.name")
	queryArguments := []any{kind, query, claims.Subject, admin, indexed.Used, indexed.IDs}
	cursorPredicate := ""
	if request.Cursor != nil && request.Cursor.Mode == creatorPageModeSQL {
		var cursorArguments []any
		cursorPredicate, cursorArguments = creatorCursorPredicateSQL(request.Cursor, len(queryArguments)+1)
		queryArguments = append(queryArguments, cursorArguments...)
	}
	pageLimit := request.Limit + 1
	if indexed.Used {
		pageLimit = request.Limit
	}
	queryArguments = append(queryArguments, pageLimit)
	limitParameter := len(queryArguments)
	rows, err := s.db.Query(r.Context(), fmt.Sprintf(`
		select creator.public_id,creator.kind,creator.name,creator.avatar_url,creator.review_status,
		       exists(select 1 from creator_claims claim where claim.creator_id=creator.id and claim.status='approved'),
		       count(distinct mod.project_code),creator.id,creator.created_at,creator.updated_at,lower(creator.name),
		       coalesce(popularity.heat_score,0)::text,coalesce(popularity.download_count,0)::text,
		       coalesce(popularity.favorite_count,0)::text,coalesce(popularity.bayesian_rating,0)::text,
		       coalesce(popularity.rating_count,0),coalesce(popularity.view_count,0)::text,
		       coalesce(popularity.comment_count,0)::text
		from creators creator
		left join content_creator_bindings binding on binding.creator_id=creator.id and binding.subject_type='mod' and binding.status='approved'
		left join mods mod on mod.id=binding.subject_id and mod.review_status='approved'
		left join public_routes popularity_route on popularity_route.entity_type=creator.kind and popularity_route.internal_id=creator.id
		left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id
		where ($1='' or creator.kind=$1)
		  and (($5 and creator.id=any($6::bigint[])) or (not $5 and ($2='' or creator.name ilike '%%' || $2 || '%%')))
		  and (creator.review_status='approved' or creator.created_by=$3 or $4)
		  %s
		group by creator.id,popularity.heat_score,popularity.view_count,popularity.download_count,
			popularity.favorite_count,popularity.bayesian_rating,popularity.rating_count,popularity.comment_count
		order by %s
		limit $%d`, cursorPredicate, orderSQL, limitParameter), queryArguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creators")
		return
	}
	defer rows.Close()
	pageRows := make([]creatorPageRow, 0, pageLimit)
	for rows.Next() {
		var row creatorPageRow
		if err = rows.Scan(
			&row.Summary.PublicID, &row.Summary.Kind, &row.Summary.Name, &row.Summary.AvatarURL,
			&row.Summary.ReviewStatus, &row.Summary.Claimed, &row.Summary.WorkCount, &row.InternalID,
			&row.CreatedAt, &row.UpdatedAt, &row.SortName, &row.Heat, &row.Downloads, &row.Favorites,
			&row.Rating, &row.RatingCount, &row.Views, &row.Comments,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode creators")
			return
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creators")
		return
	}
	hasMore := false
	nextCursor := ""
	if indexed.Used {
		hasMore = indexed.Total > indexedOffset+request.Limit
		if hasMore {
			nextCursor = creatorIndexPageCursor(request, indexedOffset+request.Limit)
		}
	} else if len(pageRows) > request.Limit {
		hasMore = true
		pageRows = pageRows[:request.Limit]
		nextCursor = creatorSQLPageCursor(request, pageRows[len(pageRows)-1])
	}
	items := make([]creatorSummary, 0, len(pageRows))
	ossCfg := s.ossConfigFromSettings(r.Context())
	for _, row := range pageRows {
		row.Summary.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, row.Summary.AvatarURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate creator avatar URL")
			return
		}
		items = append(items, row.Summary)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "counts": map[string]int{"author": authorCount, "team": teamCount},
		"hasMore": hasMore, "nextCursor": nextCursor,
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
	var createdBy *int64
	var publishedRevisionID *int64
	err := s.db.QueryRow(r.Context(), `
		select id,public_id,kind,name,avatar_url,description_markdown,review_status,created_by,published_revision_id,
		       (select public_id from oss_files where id=creators.avatar_file_id)
		from creators
		where public_id=$1 and (review_status='approved' or created_by=$2 or $3)`,
		publicID, claims.Subject, admin,
	).Scan(&id, &item.PublicID, &item.Kind, &item.Name, &item.AvatarURL, &description, &item.ReviewStatus, &createdBy, &publishedRevisionID, &avatarFileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	var claimedUserID *int64
	var currentClaimStatus string
	_ = s.db.QueryRow(r.Context(), `select user_id from creator_claims where creator_id=$1 and status='approved'`, id).Scan(&claimedUserID)
	if claims.Subject > 0 {
		_ = s.db.QueryRow(r.Context(), `select status from creator_claims where creator_id=$1 and user_id=$2
			order by created_at desc,id desc limit 1`, id, claims.Subject).Scan(&currentClaimStatus)
	}
	item.Claimed = claimedUserID != nil
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
	if claimedUserID != nil {
		var userPublicID, username, avatarURL string
		if scanErr := s.db.QueryRow(r.Context(), `select public_id,username,avatar_url from users where id=$1`, *claimedUserID).
			Scan(&userPublicID, &username, &avatarURL); scanErr == nil {
			avatarURL, _ = s.resolveStoredOSSObjectAccessURL(r.Context(), avatarURL)
			claimedUser = map[string]any{
				"id": userPublicID, "publicId": userPublicID, "username": username,
				"avatarUrl": avatarURL,
			}
		}
	}
	capabilities := resolveCreatorCapabilities(claims, item.Kind, publicID)
	publishedRevisionPublicID, revisionErr := revisionPublicIDValue(r.Context(), s.db, publishedRevisionID)
	if revisionErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve published revision")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"creator": item, "descriptionMarkdown": description, "links": links,
		"collaborators": collaborators, "members": members, "teams": teams, "works": works,
		"defaultLocale": defaultLocale, "localizations": localizations,
		"claimedUser": claimedUser, "canEditProfile": capabilities.EditProfile, "canManageMembers": capabilities.ManageMembers,
		"canCreateRoles": capabilities.CreateRoles, "claimStatus": currentClaimStatus,
		"avatarFileId":        avatarFileID,
		"canClaim":            claims.Subject > 0 && item.Kind == "author" && claimedUserID == nil && currentClaimStatus != "pending" && item.ReviewStatus == "approved",
		"publishedRevisionId": publishedRevisionPublicID,
	})
}

func (s *Server) createCreator(w http.ResponseWriter, r *http.Request) {
	var snapshot creatorSnapshot
	if err := decodeJSON(r, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, "invalid creator payload")
		return
	}
	if snapshot.Members != nil {
		writeError(w, http.StatusBadRequest, "team members must be updated through the members endpoint")
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
	_, publicID, created, err := createCreatorTx(r.Context(), tx, snapshot, claims.Subject, status, false, r)
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
	if !s.requireSecurityVersionRefresh(w, r, "create_creator", 0, s.refreshProjectACLVersion(r.Context())) {
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
	if snapshot.Members != nil {
		writeError(w, http.StatusBadRequest, "team members must be updated through the members endpoint")
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
	var createdBy, baseRevisionID *int64
	var currentAvatarFileID *string
	err = tx.QueryRow(r.Context(), `select id,kind,description_markdown,created_by,published_revision_id,
		(select public_id from oss_files where id=creators.avatar_file_id)
		from creators where public_id=$1 for update`, publicID).
		Scan(&id, &kind, &previousDescription, &createdBy, &baseRevisionID, &currentAvatarFileID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	canEdit := claimsAllow(claims, "admin.*") || claimsAllow(claims, "creator.edit") ||
		claimsAllow(claims, "creator.edit."+publicID)
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
		if err = s.applyCreatorSnapshotTx(r.Context(), tx, id, created.RevisionID, claims.Subject,
			claims.Subject, false, snapshot); err != nil {
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
	if !s.requireSecurityVersionRefresh(w, r, "update_creator", 0, s.refreshProjectACLVersion(r.Context())) {
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
	err = tx.QueryRow(r.Context(), `select id,kind,name from creators where public_id=$1 and review_status='approved' for update`, publicID).
		Scan(&creatorID, &kind, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "creator not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator")
		return
	}
	if kind != "author" {
		writeError(w, http.StatusConflict, "teams cannot be claimed; claim a personal author identity")
		return
	}
	var alreadyClaimed bool
	if err = tx.QueryRow(r.Context(), `select exists(select 1 from creator_claims where creator_id=$1 and status='approved')`, creatorID).Scan(&alreadyClaimed); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect author claim")
		return
	}
	if alreadyClaimed {
		writeError(w, http.StatusConflict, "author has already been claimed")
		return
	}
	request.ProofFileIDs = uniquePublicIDs(request.ProofFileIDs)
	proofFiles, err := resolveCreatorClaimProofFiles(r.Context(), tx, claims.Subject, request.ProofFileIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Identity claims always require an independent review. Content-review
	// preferences and administrator status must not self-grant derived project
	// developer access.
	status := "pending"
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
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit claim")
		return
	}
	annotateActivity(r, activity.ActionClaim, creatorObjectType(kind), publicID, len(request.ProofMarkdown))
	writeJSON(w, http.StatusCreated, map[string]any{"id": claimPublicID, "status": status, "name": name})
}

func (s *Server) adminCreatorClaims(w http.ResponseWriter, r *http.Request) {
	request, err := parseCreatorClaimPageRequest(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	items, hasMore, err := s.queryCreatorClaimPage(r.Context(), request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load creator claims")
		return
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeCreatorClaimPageCursor(creatorClaimPageCursor{
			Version: creatorClaimCursorVersion,
			Scope:   request.Scope, CreatedAt: last.CreatedAt, ID: last.InternalID,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func (s *Server) queryCreatorClaimPage(ctx context.Context, request creatorClaimPageRequest) ([]creatorClaimReviewResponse, bool, error) {
	var cursorTime *time.Time
	var cursorID int64
	if request.Cursor != nil {
		cursorTime = &request.Cursor.CreatedAt
		cursorID = request.Cursor.ID
	}
	rows, err := s.db.Query(ctx, `select claim.public_id,claim.id,creator.public_id,creator.kind,creator.name,
		account.public_id,account.username,claim.proof_markdown,claim.status,claim.created_at
		from creator_claims claim
		join creators creator on creator.id=claim.creator_id
		join users account on account.id=claim.user_id
		where claim.status='pending' and creator.kind='author'
		  and ($1::timestamptz is null or (claim.created_at,claim.id)>($1,$2))
		order by claim.created_at,claim.id limit $3`, cursorTime, cursorID, request.Limit+1)
	if err != nil {
		return nil, false, err
	}
	items := make([]creatorClaimReviewResponse, 0, request.Limit+1)
	claimIDs := make([]int64, 0, request.Limit+1)
	for rows.Next() {
		var item creatorClaimReviewResponse
		if err = rows.Scan(&item.ID, &item.InternalID, &item.CreatorID, &item.Kind, &item.Name,
			&item.UserID, &item.Username, &item.ProofMarkdown, &item.Status, &item.CreatedAt); err != nil {
			rows.Close()
			return nil, false, err
		}
		item.Attachments = []creatorClaimAttachment{}
		items = append(items, item)
		claimIDs = append(claimIDs, item.InternalID)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, false, err
	}
	rows.Close()
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
		claimIDs = claimIDs[:request.Limit]
	}
	attachments, err := s.creatorClaimAttachments(ctx, claimIDs)
	if err != nil {
		return nil, false, err
	}
	for index := range items {
		if attached := attachments[items[index].InternalID]; attached != nil {
			items[index].Attachments = attached
		}
	}
	return items, hasMore, nil
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
	if kind != "author" {
		writeError(w, http.StatusConflict, "teams cannot be claimed")
		return
	}
	if _, err = tx.Exec(r.Context(), `update creator_claims set status=$2,reviewed_by=$3,review_note=$4,reviewed_at=now() where id=$1`,
		claimID, request.Status, claims.Subject, strings.TrimSpace(request.Note)); err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "author has already been claimed")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to save claim review")
		return
	}
	if request.Status == "approved" {
		if _, err = tx.Exec(r.Context(), `update creator_claims set status='rejected',review_note='another claim was approved',
			reviewed_by=$2,reviewed_at=now() where creator_id=$1 and id<>$3 and status='pending'`, creatorID, claims.Subject, claimID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to close competing claims")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit claim review")
		return
	}
	if request.Status == "approved" {
		if !s.requireSecurityVersionRefresh(w, r, "approve_creator_claim", userID,
			s.refreshPermissionVersion(r.Context(), userID)) {
			return
		}
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

func (s *Server) revokeCreatorClaim(w http.ResponseWriter, r *http.Request) {
	claimPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var request struct {
		Reason string `json:"reason"`
	}
	if !validCatalogPublicID(claimPublicID) || decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid author claim revocation")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke author claim")
		return
	}
	defer tx.Rollback(r.Context())
	var claimID, userID int64
	var authorID, authorName string
	err = tx.QueryRow(r.Context(), `select claim.id,claim.user_id,author.public_id,author.name
		from creator_claims claim join creators author on author.id=claim.creator_id
		where claim.public_id=$1 and claim.status='approved' and author.kind='author' for update of claim`, claimPublicID).
		Scan(&claimID, &userID, &authorID, &authorName)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "approved author claim not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load author claim")
		return
	}
	claims := currentClaims(r)
	request.Reason = strings.TrimSpace(request.Reason)
	if _, err = tx.Exec(r.Context(), `update creator_claims set status='revoked',reviewed_by=$2,
		review_note=$3,revoked_at=now(),reviewed_at=now() where id=$1`, claimID, claims.Subject, request.Reason); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke author claim")
		return
	}
	payload, _ := json.Marshal(map[string]any{"claimId": claimPublicID, "authorId": authorID, "reason": request.Reason})
	_, _ = tx.Exec(r.Context(), `insert into permission_audit_logs(operator_id,target_user_id,action,payload)
		values($1,$2,'author_claim.revoke',$3::jsonb)`, claims.Subject, userID, payload)
	if err = enqueueTemplatedNotificationTx(r.Context(), tx, "creator.claim.revoked", userID, claims.Subject, "creator_claim_revoked",
		map[string]string{"name": authorName, "reason": request.Reason},
		map[string]any{"creatorId": authorID, "claimId": claimPublicID, "url": "/authors/" + authorID},
		r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to queue author claim revocation notification")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit author claim revocation")
		return
	}
	if !s.requireSecurityVersionRefresh(w, r, "revoke_creator_claim", userID,
		s.refreshPermissionVersion(r.Context(), userID)) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked"})
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
