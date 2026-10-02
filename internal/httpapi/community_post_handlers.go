package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"mcmods-cn-backend/internal/security"
)

const communityPostAggregate = "community_post"

const maximumCommunityPostProjectReferences = 32
const maximumCommunityPostResourceReferences = 64
const maximumCommunityPostResourceIdentifierBytes = 256

var errCommunityPostProjectReferenceNotVisible = errors.New("selected project was not found or is not visible")

func logCommunityPostDataFailure(stage, identity string, err error) {
	slog.Error("community post data failure",
		"module", "community_post",
		"stage", stage,
		"identity", identity,
		"error", err,
	)
}

type communityPostReference struct {
	PublicID    string            `json:"publicId,omitempty"`
	Type        string            `json:"type,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Identifier  string            `json:"identifier"`
	Name        string            `json:"name,omitempty"`
	SiteID      string            `json:"siteId,omitempty"`
	Names       map[string]string `json:"names,omitempty"`
	IconURL     string            `json:"iconUrl,omitempty"`
	VersionID   string            `json:"versionId,omitempty"`
	RevisionID  string            `json:"revisionId,omitempty"`
	IconPath    string            `json:"iconPath,omitempty"`
	Unresolved  bool              `json:"unresolved,omitempty"`
	Unavailable bool              `json:"unavailable,omitempty"`
	InternalID  int64             `json:"-"`
}

type communityPostSnapshot struct {
	Kind              string                   `json:"kind"`
	Category          string                   `json:"category"`
	Title             string                   `json:"title"`
	SourceLocale      string                   `json:"sourceLocale"`
	BodyMarkdown      string                   `json:"bodyMarkdown"`
	MinecraftVersions []string                 `json:"minecraftVersions"`
	ModVersionMin     string                   `json:"modVersionMin"`
	ModVersionMax     string                   `json:"modVersionMax"`
	Severity          string                   `json:"severity"`
	HasFix            bool                     `json:"hasFix"`
	IssueURL          string                   `json:"issueUrl"`
	CoverFileID       string                   `json:"coverFileId,omitempty"`
	CoverInternalID   *int64                   `json:"-"`
	BountyCurrency    string                   `json:"bountyCurrency,omitempty"`
	BountyAmount      int64                    `json:"bountyAmount,omitempty"`
	Projects          []communityPostReference `json:"projects"`
	Resources         []communityPostReference `json:"resources"`
}

type communityPostMutationRequest struct {
	communityPostSnapshot
	BaseRevisionID *string `json:"baseRevisionId,omitempty"`
}

type communityPostEditConflict struct {
	CurrentRevisionID string `json:"currentRevisionId,omitempty"`
}

type communityPostEditConflictError struct {
	Current communityPostEditConflict
}

func (err *communityPostEditConflictError) Error() string {
	return "community post changed after the editor baseline"
}

type communityPostBounty struct {
	Currency     string         `json:"currency"`
	CurrencyName string         `json:"currencyName"`
	CurrencyIcon string         `json:"currencyIcon"`
	Translations map[string]any `json:"translations"`
	Amount       int64          `json:"amount"`
	Status       string         `json:"status"`
	TaxAmount    int64          `json:"taxAmount,omitempty"`
	NetAmount    int64          `json:"netAmount,omitempty"`
}

type communityPostResponse struct {
	ID                  string                   `json:"id"`
	Kind                string                   `json:"kind"`
	Category            string                   `json:"category"`
	Title               string                   `json:"title"`
	SourceLocale        string                   `json:"sourceLocale"`
	BodyMarkdown        string                   `json:"bodyMarkdown,omitempty"`
	Summary             string                   `json:"summary,omitempty"`
	MinecraftVersions   []string                 `json:"minecraftVersions"`
	ModVersionMin       string                   `json:"modVersionMin,omitempty"`
	ModVersionMax       string                   `json:"modVersionMax,omitempty"`
	Severity            string                   `json:"severity,omitempty"`
	HasFix              bool                     `json:"hasFix,omitempty"`
	IssueURL            string                   `json:"issueUrl,omitempty"`
	CoverURL            string                   `json:"coverUrl,omitempty"`
	CoverFileID         string                   `json:"coverFileId,omitempty"`
	ResolutionStatus    string                   `json:"resolutionStatus,omitempty"`
	AcceptedCommentID   string                   `json:"acceptedCommentId,omitempty"`
	ResolvedAt          *time.Time               `json:"resolvedAt,omitempty"`
	Bounty              *communityPostBounty     `json:"bounty,omitempty"`
	AuthorID            string                   `json:"authorId"`
	AuthorName          string                   `json:"authorName"`
	ReviewStatus        string                   `json:"reviewStatus"`
	PublishedRevisionID string                   `json:"publishedRevisionId,omitempty"`
	Projects            []communityPostReference `json:"projects"`
	Resources           []communityPostReference `json:"resources"`
	CreatedAt           time.Time                `json:"createdAt"`
	UpdatedAt           time.Time                `json:"updatedAt"`
	CanEdit             bool                     `json:"canEdit"`
	CanResolve          bool                     `json:"canResolve"`
}

type communityPostPageResponse struct {
	Items      []communityPostResponse `json:"items"`
	Limit      int                     `json:"limit"`
	HasMore    bool                    `json:"hasMore"`
	NextCursor string                  `json:"nextCursor"`
	Categories []string                `json:"categories"`
}

func (s *Server) communityPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.createCommunityPost(w, r)
		return
	}
	claims := currentClaims(r)
	page, err := parseCommunityPostPageRequest(r.URL.Query(), claims)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query, arguments := communityPostPageSQL(page)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		logCommunityPostDataFailure("directory_query", page.Kind, err)
		writeError(w, http.StatusInternalServerError, "failed to load community posts")
		return
	}
	defer rows.Close()
	pageRows := make([]communityPostPageRow, 0, page.Limit+1)
	for rows.Next() {
		var row communityPostPageRow
		if err = rows.Scan(&row.ID, &row.Item.ID, &row.Item.Kind, &row.Item.Category, &row.Item.Title,
			&row.Item.SourceLocale, &row.Item.Summary, &row.Item.MinecraftVersions, &row.Item.ModVersionMin,
			&row.Item.ModVersionMax, &row.Item.Severity, &row.Item.HasFix, &row.Item.IssueURL,
			&row.Item.CoverFileID, &row.CoverKey, &row.Item.AuthorID, &row.Item.AuthorName,
			&row.Item.ReviewStatus, &row.Item.CreatedAt, &row.Item.UpdatedAt, &row.AuthorInternalID,
			&row.Item.ResolutionStatus, &row.Item.AcceptedCommentID, &row.Item.ResolvedAt,
			&row.PublishedAt, &row.SortName, &row.Heat, &row.Downloads, &row.Favorites, &row.Rating,
			&row.RatingCount, &row.Views, &row.Comments, &row.Relevance); err != nil {
			logCommunityPostDataFailure("directory_scan", page.Kind, err)
			writeError(w, http.StatusInternalServerError, "failed to decode community post")
			return
		}
		row.UpdatedAt = row.Item.UpdatedAt
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		logCommunityPostDataFailure("directory_cursor", page.Kind, err)
		writeError(w, http.StatusInternalServerError, "failed to load community posts")
		return
	}
	rows.Close()
	hasMore := len(pageRows) > page.Limit
	if hasMore {
		pageRows = pageRows[:page.Limit]
	}
	items := make([]communityPostResponse, 0, len(pageRows))
	internalIDs := make([]int64, 0, len(pageRows))
	ossCfg := s.ossConfigFromSettings(r.Context())
	for index := range pageRows {
		row := &pageRows[index]
		if row.CoverKey != "" {
			access, accessErr := s.resolveOSSObjectAccessWithConfig(r.Context(), ossCfg, row.CoverKey, ossObjectAccessOptions{})
			if accessErr != nil {
				writeError(w, http.StatusBadGateway, "failed to generate community post cover URL")
				return
			}
			row.Item.CoverURL = access.URL
		}
		row.Item.CanEdit = page.ViewerID > 0 && (page.ViewerID == row.AuthorInternalID || claimsAllow(claims, "community.edit") || claimsAllow(claims, "admin.*"))
		row.Item.CanResolve = row.Item.Kind == "discussion" && row.Item.ResolutionStatus == "open" &&
			row.Item.ReviewStatus == "approved" && page.ViewerID == row.AuthorInternalID
		items = append(items, row.Item)
		internalIDs = append(internalIDs, row.ID)
	}
	bounties, err := s.loadCommunityPostBounties(r.Context(), internalIDs)
	if err != nil {
		logCommunityPostDataFailure("directory_bounties", page.Kind, err)
		writeError(w, http.StatusInternalServerError, "failed to load community post bounties")
		return
	}
	projects, resources, err := s.communityPostReferencesBatch(r.Context(), internalIDs, claims)
	if err != nil {
		logCommunityPostDataFailure("directory_references", page.Kind, err)
		writeError(w, http.StatusInternalServerError, "failed to load community post references")
		return
	}
	for index, internalID := range internalIDs {
		items[index].Bounty = bounties[internalID]
		items[index].Projects = projects[internalID]
		items[index].Resources = resources[internalID]
	}
	nextCursor := ""
	if hasMore && len(pageRows) > 0 {
		nextCursor = communityPostNextCursor(page, pageRows[len(pageRows)-1])
	}
	writeJSON(w, http.StatusOK, communityPostPageResponse{
		Items: items, Limit: page.Limit, HasMore: hasMore, NextCursor: nextCursor,
		Categories: communityPostCategories(page.Kind),
	})
}

func (s *Server) communityPostItem(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if r.Method == http.MethodPut {
		s.updateCommunityPost(w, r, publicID)
		return
	}
	item, err := s.loadCommunityPost(r.Context(), publicID, currentClaims(r))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community post")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) createCommunityPost(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var request communityPostMutationRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid community post")
		return
	}
	snapshot := request.communityPostSnapshot
	if err := normalizeCommunityPostSnapshot(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	permission := "community." + snapshot.Kind + ".create"
	if !claimsAllow(claims, permission) && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "community post permission is required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start community post creation")
		return
	}
	defer tx.Rollback(r.Context())
	if err = s.resolveCommunityPostSnapshot(r.Context(), tx, claims, 0, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reviewConfig := loadReviewConfig(r.Context(), tx)
	reviewRequired := communityPostReviewRequired(reviewConfig, snapshot.Kind, false)
	if claimsAllow(claims, "community.no-review") || claimsAllow(claims, "admin.*") {
		reviewRequired = false
	}
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	status := "approved"
	if reviewRequired {
		status = "pending"
	}
	var internalID int64
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into community_posts(kind,category,author_id,title,source_locale,body_markdown,minecraft_versions,
		mod_version_min,mod_version_max,severity,has_fix,issue_url,cover_file_id,review_status)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) returning id,public_id`, snapshot.Kind, snapshot.Category, claims.Subject,
		snapshot.Title, snapshot.SourceLocale, snapshot.BodyMarkdown, snapshot.MinecraftVersions, snapshot.ModVersionMin,
		snapshot.ModVersionMax, snapshot.Severity, snapshot.HasFix, snapshot.IssueURL, snapshot.CoverInternalID, status).
		Scan(&internalID, &publicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create community post")
		return
	}
	if err = holdCommunityPostBountyTx(r.Context(), tx, internalID, publicID, claims.Subject, snapshot); err != nil {
		if errors.Is(err, errInsufficientBalance) {
			writeError(w, http.StatusConflict, "insufficient balance for question bounty")
		} else if isCommunityPostBountyInputError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			logCommunityPostDataFailure("bounty_hold", publicID, err)
			writeError(w, http.StatusInternalServerError, "failed to reserve question bounty")
		}
		return
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{
		EntityType: "community_post", EntityID: internalID, AggregateType: communityPostAggregate, AggregateKey: publicID,
		Snapshot: raw, Reason: "Create " + snapshot.Kind, ActorID: claims.Subject, Source: "user", Status: status,
		Metadata: map[string]any{"kind": snapshot.Kind, "title": snapshot.Title}, Request: r,
	})
	if err != nil {
		logCommunityPostDataFailure("create_revision", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to save community post revision")
		return
	}
	if status == "approved" {
		err = applyCommunityPostSnapshotTx(r.Context(), tx, internalID, created.RevisionID, snapshot, claims)
	} else {
		err = replaceCommunityPostReferencesTx(r.Context(), tx, internalID, snapshot, claims)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save community post relations")
		return
	}
	if status == "approved" {
		if err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to approve community post")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit community post")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": publicID, "reviewStatus": status, "revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID})
}

func (s *Server) updateCommunityPost(w http.ResponseWriter, r *http.Request, publicID string) {
	claims := currentClaims(r)
	var postID, authorID int64
	var kind string
	err := s.db.QueryRow(r.Context(), `select id,author_id,kind from community_posts where public_id=$1 and status='active'`, publicID).
		Scan(&postID, &authorID, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if err != nil {
		logCommunityPostDataFailure("update_lookup", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to load community post")
		return
	}
	if claims.Subject != authorID && !claimsAllow(claims, "community.edit") && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "cannot edit this community post")
		return
	}
	var request communityPostMutationRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid community post")
		return
	}
	snapshot := request.communityPostSnapshot
	if request.BaseRevisionID != nil {
		baseRevisionID := strings.ToLower(strings.TrimSpace(*request.BaseRevisionID))
		request.BaseRevisionID = &baseRevisionID
		if baseRevisionID != "" && !validCatalogPublicID(baseRevisionID) {
			writeError(w, http.StatusBadRequest, "community post base revision is invalid")
			return
		}
	}
	snapshot.Kind = kind
	if err := normalizeCommunityPostSnapshot(&snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := s.db.BeginTx(r.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start community post update")
		return
	}
	defer tx.Rollback(r.Context())
	baseRevision, err := lockCommunityPostRevisionBaseTx(r.Context(), tx, postID, publicID, request.BaseRevisionID)
	var conflict *communityPostEditConflictError
	if errors.As(err, &conflict) {
		writeAPIError(w, http.StatusConflict, "COMMUNITY_POST_EDIT_CONFLICT", conflict.Error(), 0, conflict.Current)
		return
	}
	if err != nil {
		var serialization *pgconn.PgError
		if errors.As(err, &serialization) && serialization.Code == "40001" {
			// The repeatable-read snapshot may predate a waiter ahead of us.
			// Release the aborted transaction before reading its new baseline;
			// do not retry a mutation or consume an additional pooled connection.
			if rollbackErr := tx.Rollback(r.Context()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				logCommunityPostDataFailure("update_conflict_rollback", publicID, rollbackErr)
				writeError(w, http.StatusInternalServerError, "failed to release community post conflict")
				return
			}
			var currentPublicID string
			if readErr := s.db.QueryRow(r.Context(), `select coalesce(revision.public_id,'') from community_posts post
				left join content_revisions revision on revision.id=post.published_revision_id
				where post.id=$1 and post.public_id=$2 and post.status='active'`, postID, publicID).Scan(&currentPublicID); readErr != nil {
				logCommunityPostDataFailure("update_conflict_baseline", publicID, readErr)
				writeError(w, http.StatusInternalServerError, "failed to load community post conflict")
				return
			}
			current := communityPostEditConflict{CurrentRevisionID: currentPublicID}
			writeAPIError(w, http.StatusConflict, "COMMUNITY_POST_EDIT_CONFLICT", "community post changed after the editor baseline", 0, current)
			return
		}
		logCommunityPostDataFailure("update_base", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to validate community post revision")
		return
	}
	if err = s.resolveCommunityPostSnapshot(r.Context(), tx, claims, postID, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err = validateCommunityPostBountyEditTx(r.Context(), tx, postID, publicID, authorID, snapshot); err != nil {
		if errors.Is(err, errInsufficientBalance) {
			writeError(w, http.StatusConflict, "insufficient balance for question bounty")
		} else if isCommunityPostBountyInputError(err) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			logCommunityPostDataFailure("bounty_edit", publicID, err)
			writeError(w, http.StatusInternalServerError, "failed to update question bounty")
		}
		return
	}
	reviewConfig := loadReviewConfig(r.Context(), tx)
	// Until a first revision is published, a resubmission still belongs to the
	// create policy; an exempt edit policy must not bypass a rejected creation.
	reviewRequired := communityPostReviewRequired(reviewConfig, kind, baseRevision != nil)
	if claimsAllow(claims, "community.no-review") || claimsAllow(claims, "admin.*") {
		reviewRequired = false
	}
	if antiAbuseModerationRequired(r) {
		reviewRequired = true
	}
	status := "approved"
	if reviewRequired {
		status = "pending"
	}
	raw, _ := json.Marshal(snapshot)
	created, err := createContentRevisionTx(r.Context(), tx, createContentRevisionParams{EntityType: "community_post", EntityID: postID,
		AggregateType: communityPostAggregate, AggregateKey: publicID, BaseRevision: baseRevision, Snapshot: raw,
		Reason: "Edit " + kind, ActorID: claims.Subject, Source: "user", Status: status,
		Metadata: map[string]any{"kind": kind, "title": snapshot.Title}, Request: r})
	if err != nil {
		if errors.Is(err, errReviewInProgress) {
			writeError(w, http.StatusConflict, errReviewInProgress.Error())
			return
		}
		logCommunityPostDataFailure("update_revision", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to save community post revision")
		return
	}
	if status == "approved" {
		if err = applyCommunityPostSnapshotTx(r.Context(), tx, postID, created.RevisionID, snapshot, claims); err == nil {
			err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r)
		}
	} else {
		_, err = tx.Exec(r.Context(), `update community_posts set
			review_status=case when published_revision_id is null then 'pending' else 'approved' end,
			updated_at=now() where id=$1`, postID)
	}
	if err != nil {
		logCommunityPostDataFailure("update_apply", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to save community post")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		logCommunityPostDataFailure("update_commit", publicID, err)
		writeError(w, http.StatusInternalServerError, "failed to save community post")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "reviewStatus": status, "revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID})
}

func lockCommunityPostRevisionBaseTx(ctx context.Context, tx pgx.Tx, postID int64, publicID string, expectedRevisionID *string) (*int64, error) {
	lockKey := communityPostAggregate + ":" + publicID
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return nil, err
	}
	var currentRevision *int64
	var currentPublicID string
	if err := tx.QueryRow(ctx, `select post.published_revision_id,coalesce(revision.public_id,'')
		from community_posts post left join content_revisions revision on revision.id=post.published_revision_id
		where post.id=$1 and post.public_id=$2 and post.status='active' for update of post`, postID, publicID).
		Scan(&currentRevision, &currentPublicID); err != nil {
		return nil, err
	}
	expectedPublicID := ""
	if expectedRevisionID != nil {
		expectedPublicID = *expectedRevisionID
	}
	if currentPublicID != expectedPublicID {
		return nil, &communityPostEditConflictError{Current: communityPostEditConflict{CurrentRevisionID: currentPublicID}}
	}
	return currentRevision, nil
}

func normalizeCommunityPostKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tutorial", "issue", "news", "discussion":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
}

var communityPostProjectTypes = stringSet("mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon")

func communityPostProjectTypeAllowed(value string) bool {
	return communityPostProjectTypes[value]
}

func parseCommunityProjectFilters(value string) ([]string, bool) {
	filters, valid := parseCatalogList(value, 20)
	if !valid {
		return nil, false
	}
	for index, filter := range filters {
		separator := strings.IndexByte(filter, ':')
		if separator <= 0 || separator == len(filter)-1 {
			return nil, false
		}
		projectType := strings.ToLower(strings.TrimSpace(filter[:separator]))
		identity := strings.ToLower(strings.TrimSpace(filter[separator+1:]))
		if !communityPostProjectTypeAllowed(projectType) || !validModIdentifier(identity) {
			return nil, false
		}
		filters[index] = projectType + ":" + identity
	}
	return filters, true
}

func communityPostCategories(kind string) []string {
	switch normalizeCommunityPostKind(kind) {
	case "tutorial":
		return []string{"general", "beginner", "technical", "modding", "server"}
	case "issue":
		return []string{"client", "gameplay", "compatibility", "performance", "crash"}
	case "news":
		return []string{"site", "minecraft", "modding", "community", "release"}
	case "discussion":
		return []string{"help", "recommendation", "technical", "gameplay", "server"}
	default:
		return []string{}
	}
}

func defaultCommunityPostCategory(kind string) string {
	items := communityPostCategories(kind)
	if len(items) == 0 {
		return ""
	}
	return items[0]
}

func communityPostCategoryAllowed(kind, category string) bool {
	for _, allowed := range communityPostCategories(kind) {
		if category == allowed {
			return true
		}
	}
	return false
}

func (s *Server) communityPostCategoryOptions(w http.ResponseWriter, r *http.Request) {
	kind := normalizeCommunityPostKind(r.URL.Query().Get("kind"))
	if kind == "" {
		writeError(w, http.StatusBadRequest, "invalid community post kind")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "items": communityPostCategories(kind)})
}

func communityPostPath(kind, publicID string) string {
	switch normalizeCommunityPostKind(kind) {
	case "tutorial":
		return "/tutorials/" + publicID
	case "issue":
		return "/issues/" + publicID
	case "news":
		return "/news/" + publicID
	default:
		return "/discussions/" + publicID
	}
}

func normalizeCommunityPostSnapshot(snapshot *communityPostSnapshot) error {
	snapshot.Kind = normalizeCommunityPostKind(snapshot.Kind)
	snapshot.Category = strings.ToLower(strings.TrimSpace(snapshot.Category))
	if snapshot.Category == "" {
		snapshot.Category = defaultCommunityPostCategory(snapshot.Kind)
	}
	snapshot.Title = strings.TrimSpace(snapshot.Title)
	snapshot.BodyMarkdown = strings.TrimSpace(snapshot.BodyMarkdown)
	if snapshot.Kind == "" || !communityPostCategoryAllowed(snapshot.Kind, snapshot.Category) || snapshot.Title == "" || len([]rune(snapshot.Title)) > 160 || len(snapshot.BodyMarkdown) > 1<<20 {
		return errors.New("invalid community post")
	}
	if sourceLocale := strings.TrimSpace(snapshot.SourceLocale); sourceLocale != "" {
		snapshot.SourceLocale = normalizeContentLocale(sourceLocale)
		if !isEditableContentLocale(snapshot.SourceLocale) {
			return errors.New("community post source locale is invalid")
		}
	} else {
		snapshot.SourceLocale = detectCommunityPostLocale(snapshot.Title + "\n" + snapshot.BodyMarkdown)
		if snapshot.SourceLocale == "" {
			return errors.New("community post source locale is required")
		}
	}
	snapshot.MinecraftVersions = uniqueNonEmpty(snapshot.MinecraftVersions)
	snapshot.ModVersionMin = strings.TrimSpace(snapshot.ModVersionMin)
	snapshot.ModVersionMax = strings.TrimSpace(snapshot.ModVersionMax)
	snapshot.IssueURL = strings.TrimSpace(snapshot.IssueURL)
	snapshot.BountyCurrency = normalizeCode(snapshot.BountyCurrency)
	if err := normalizeCommunityPostReferences(snapshot); err != nil {
		return err
	}
	if snapshot.IssueURL != "" {
		parsed, err := url.ParseRequestURI(snapshot.IssueURL)
		if err != nil || parsed.Scheme != "https" && parsed.Scheme != "http" {
			return errors.New("issue URL is invalid")
		}
	}
	if snapshot.Kind == "tutorial" {
		snapshot.Severity, snapshot.ModVersionMin, snapshot.ModVersionMax, snapshot.IssueURL = "", "", "", ""
		snapshot.HasFix = false
		snapshot.BountyCurrency, snapshot.BountyAmount = "", 0
	} else if snapshot.Kind == "issue" {
		switch snapshot.Severity {
		case "client", "harmless", "minor", "harmful", "severe", "fatal":
		default:
			return errors.New("issue severity is invalid")
		}
		if len(snapshot.Projects) == 0 {
			return errors.New("issues require at least one project")
		}
		if len(snapshot.MinecraftVersions) == 0 || snapshot.ModVersionMin == "" && snapshot.ModVersionMax == "" {
			return errors.New("issues require a Minecraft version and a mod version or version range")
		}
		snapshot.BountyCurrency, snapshot.BountyAmount = "", 0
	} else {
		snapshot.Severity, snapshot.ModVersionMin, snapshot.ModVersionMax, snapshot.IssueURL = "", "", "", ""
		snapshot.HasFix = false
		if snapshot.Kind == "news" {
			snapshot.MinecraftVersions = []string{}
			snapshot.BountyCurrency, snapshot.BountyAmount = "", 0
		} else if (snapshot.BountyCurrency == "") != (snapshot.BountyAmount == 0) || snapshot.BountyAmount < 0 || snapshot.BountyAmount > 1_000_000_000_000 {
			return errors.New("question bounty requires an active currency and a positive amount")
		}
	}
	return nil
}

func communityPostReviewRequired(config reviewConfig, kind string, edit bool) bool {
	switch kind {
	case "tutorial":
		return config.TutorialCreate && !edit || config.TutorialEdit && edit
	case "issue":
		return config.IssueCreate && !edit || config.IssueEdit && edit
	case "news":
		return config.NewsCreate && !edit || config.NewsEdit && edit
	case "discussion":
		return config.DiscussionCreate && !edit || config.DiscussionEdit && edit
	default:
		return true
	}
}

func detectCommunityPostLocale(value string) string {
	for _, char := range value {
		switch {
		case unicode.Is(unicode.Hiragana, char) || unicode.Is(unicode.Katakana, char):
			return "ja-JP"
		case unicode.Is(unicode.Cyrillic, char):
			return "ru-RU"
		}
	}
	if strings.ContainsAny(value, "體臺灣萬與為這個來時會學國書門後發點麼說讓種還區") {
		return "zh-TW"
	}
	for _, char := range value {
		if unicode.Is(unicode.Han, char) {
			return "zh-CN"
		}
	}
	words := strings.FieldsFunc(strings.ToLower(value), func(char rune) bool {
		return !unicode.IsLetter(char)
	})
	stopWords := map[string]map[string]struct{}{
		"en-US": wordSet("the", "this", "that", "and", "is", "with", "for", "not", "a", "an", "to", "how"),
		"de-DE": wordSet("der", "die", "das", "und", "ist", "mit", "für", "nicht", "eine", "einen"),
		"es-ES": wordSet("el", "la", "los", "las", "una", "para", "con", "que", "del", "este"),
		"fr-FR": wordSet("le", "la", "les", "une", "des", "pour", "avec", "est", "dans", "cette"),
	}
	bestLocale, bestScore := "", 0
	tied := false
	for locale, candidates := range stopWords {
		score := 0
		for _, word := range words {
			if _, ok := candidates[word]; ok {
				score++
			}
		}
		if score > bestScore {
			bestLocale, bestScore, tied = locale, score, false
		} else if score > 0 && score == bestScore {
			tied = true
		}
	}
	if bestScore >= 2 && !tied {
		return bestLocale
	}
	return ""
}

func wordSet(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

func uniqueNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
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

func normalizeCommunityPostReferences(snapshot *communityPostSnapshot) error {
	if len(snapshot.Projects) > maximumCommunityPostProjectReferences {
		return fmt.Errorf("community post supports at most %d project references", maximumCommunityPostProjectReferences)
	}
	if len(snapshot.Resources) > maximumCommunityPostResourceReferences {
		return fmt.Errorf("community post supports at most %d resource references", maximumCommunityPostResourceReferences)
	}
	projectKeys := make(map[string]struct{}, len(snapshot.Projects))
	for index := range snapshot.Projects {
		ref := &snapshot.Projects[index]
		ref.Type = strings.ToLower(strings.TrimSpace(ref.Type))
		if ref.Type == "" {
			ref.Type = "mod"
		}
		if !communityPostProjectTypeAllowed(ref.Type) {
			return errors.New("project reference type is invalid")
		}
		ref.PublicID = strings.ToLower(strings.TrimSpace(ref.PublicID))
		ref.Identifier = strings.TrimSpace(ref.Identifier)
		if strings.HasPrefix(ref.PublicID, "unresolved:") {
			ref.PublicID = ""
		}
		key := ref.Type + "\x00"
		if ref.PublicID != "" {
			if !validCatalogPublicID(ref.PublicID) {
				return errors.New("project reference public ID is invalid")
			}
			ref.Identifier = ""
			key += "public:" + ref.PublicID
		} else {
			if !validModIdentifier(ref.Identifier) {
				return errors.New("project reference identifier is invalid")
			}
			key += "raw:" + strings.ToLower(ref.Identifier)
		}
		if _, duplicate := projectKeys[key]; duplicate {
			return errors.New("duplicate project reference")
		}
		projectKeys[key] = struct{}{}
		clearCommunityPostReferencePresentation(ref)
	}
	resourceKeys := make(map[string]struct{}, len(snapshot.Resources))
	for index := range snapshot.Resources {
		ref := &snapshot.Resources[index]
		ref.PublicID = strings.ToLower(strings.TrimSpace(ref.PublicID))
		ref.Kind = strings.ToLower(strings.TrimSpace(ref.Kind))
		ref.Identifier = strings.TrimSpace(ref.Identifier)
		if strings.HasPrefix(ref.PublicID, "unresolved:") {
			ref.PublicID = ""
		}
		if len(ref.Kind) > 64 {
			return errors.New("resource reference kind is invalid")
		}
		key := ""
		if ref.PublicID != "" {
			if !validCatalogPublicID(ref.PublicID) {
				return errors.New("resource reference public ID is invalid")
			}
			ref.Identifier = ""
			key = "public:" + ref.PublicID
		} else {
			if ref.Kind == "" || !validCommunityPostResourceIdentifier(ref.Identifier) {
				return errors.New("resource reference is incomplete or invalid")
			}
			key = ref.Kind + "\x00raw:" + strings.ToLower(ref.Identifier)
		}
		if _, duplicate := resourceKeys[key]; duplicate {
			return errors.New("duplicate resource reference")
		}
		resourceKeys[key] = struct{}{}
		clearCommunityPostReferencePresentation(ref)
	}
	return nil
}

func validCommunityPostResourceIdentifier(value string) bool {
	if value == "" || len(value) > maximumCommunityPostResourceIdentifierBytes || !utf8.ValidString(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) {
			return false
		}
	}
	return true
}

func clearCommunityPostReferencePresentation(ref *communityPostReference) {
	ref.Name = ""
	ref.SiteID = ""
	ref.Names = nil
	ref.IconURL = ""
	ref.VersionID = ""
	ref.RevisionID = ""
	ref.IconPath = ""
	ref.Unresolved = false
	ref.Unavailable = false
	ref.InternalID = 0
}

func (s *Server) resolveCommunityPostSnapshot(ctx context.Context, tx pgx.Tx, claims security.Claims, postID int64, snapshot *communityPostSnapshot) error {
	actorID := claims.Subject
	if snapshot.CoverFileID != "" {
		if postID > 0 {
			var existingID int64
			if tx.QueryRow(ctx, `select file.id from community_posts post join oss_files file on file.id=post.cover_file_id
				where post.id=$1 and file.public_id=$2 and file.status='active'`, postID, snapshot.CoverFileID).Scan(&existingID) == nil {
				snapshot.CoverInternalID = &existingID
			}
		}
		if snapshot.CoverInternalID == nil {
			file, err := resolveTrustedRasterOSSFilePublicID(ctx, tx, snapshot.CoverFileID, ossRasterBindingScope{UploaderID: actorID})
			if err != nil {
				return errors.New("cover image was not found")
			}
			snapshot.CoverInternalID = &file.ID
		}
	}
	return resolveCommunityPostReferences(ctx, tx, claims, snapshot)
}

func resolveCommunityPostReferences(ctx context.Context, tx pgx.Tx, claims security.Claims, snapshot *communityPostSnapshot) error {
	publicIDs := make([]string, 0, len(snapshot.Projects))
	for index := range snapshot.Projects {
		ref := &snapshot.Projects[index]
		if ref.PublicID != "" {
			publicIDs = append(publicIDs, ref.PublicID)
		}
	}
	visibleProjects, err := loadFollowProjectTargetsWithQueryer(ctx, tx, publicIDs, claims)
	if err != nil {
		return err
	}
	for index := range snapshot.Projects {
		ref := &snapshot.Projects[index]
		if ref.PublicID == "" {
			continue
		}
		target, visible := visibleProjects[ref.PublicID]
		if !visible || target.Type != ref.Type {
			return errCommunityPostProjectReferenceNotVisible
		}
		ref.InternalID = target.InternalID
	}

	resourcePublicIDs := make([]string, 0, len(snapshot.Resources))
	unresolvedKinds := make([]string, 0, len(snapshot.Resources))
	seenKinds := make(map[string]struct{}, len(snapshot.Resources))
	for index := range snapshot.Resources {
		ref := &snapshot.Resources[index]
		if ref.PublicID != "" {
			resourcePublicIDs = append(resourcePublicIDs, ref.PublicID)
		} else if _, exists := seenKinds[ref.Kind]; !exists {
			seenKinds[ref.Kind] = struct{}{}
			unresolvedKinds = append(unresolvedKinds, ref.Kind)
		}
	}
	type resolvedResource struct {
		ID   int64
		Kind string
	}
	resourcesByPublicID := make(map[string]resolvedResource, len(resourcePublicIDs))
	if len(resourcePublicIDs) > 0 {
		rows, queryErr := tx.Query(ctx, `select entity.public_id,entity.id,resource.kind_code
			from catalog_entities entity join game_resources resource on resource.entity_id=entity.id
			where entity.public_id=any($1::text[]) and entity.status='active' and entity.archived_at is null
			and `+publicCatalogEntitySQL("entity", "resource"), resourcePublicIDs)
		if queryErr != nil {
			return errors.New("selected resource was not found")
		}
		for rows.Next() {
			var publicID string
			var resource resolvedResource
			if queryErr = rows.Scan(&publicID, &resource.ID, &resource.Kind); queryErr != nil {
				rows.Close()
				return errors.New("selected resource was not found")
			}
			resourcesByPublicID[publicID] = resource
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return errors.New("selected resource was not found")
		}
	}
	validKinds := make(map[string]struct{}, len(unresolvedKinds))
	if len(unresolvedKinds) > 0 {
		rows, queryErr := tx.Query(ctx, `select code from resource_kinds where code=any($1::text[])`, unresolvedKinds)
		if queryErr != nil {
			return errors.New("resource reference kind is invalid")
		}
		for rows.Next() {
			var code string
			if queryErr = rows.Scan(&code); queryErr != nil {
				rows.Close()
				return errors.New("resource reference kind is invalid")
			}
			validKinds[code] = struct{}{}
		}
		queryErr = rows.Err()
		rows.Close()
		if queryErr != nil {
			return errors.New("resource reference kind is invalid")
		}
	}
	for index := range snapshot.Resources {
		ref := &snapshot.Resources[index]
		if ref.PublicID != "" {
			resource, exists := resourcesByPublicID[ref.PublicID]
			if !exists {
				return errors.New("selected resource was not found")
			}
			ref.InternalID = resource.ID
			ref.Kind = resource.Kind
		} else if _, exists := validKinds[ref.Kind]; !exists {
			return errors.New("resource reference kind is invalid")
		}
	}
	return nil
}

func applyCommunityPostSnapshotTx(ctx context.Context, tx pgx.Tx, postID, revisionID int64, snapshot communityPostSnapshot, claims security.Claims) error {
	if _, err := tx.Exec(ctx, `update community_posts set category=$2,title=$3,source_locale=$4,body_markdown=$5,minecraft_versions=$6,
		mod_version_min=$7,mod_version_max=$8,severity=$9,has_fix=$10,issue_url=$11,cover_file_id=$12,
		review_status='approved',published_revision_id=$13,published_at=coalesce(published_at,now()),updated_at=now() where id=$1`,
		postID, snapshot.Category, snapshot.Title, snapshot.SourceLocale, snapshot.BodyMarkdown, snapshot.MinecraftVersions, snapshot.ModVersionMin,
		snapshot.ModVersionMax, snapshot.Severity, snapshot.HasFix, snapshot.IssueURL, snapshot.CoverInternalID, revisionID); err != nil {
		return err
	}
	if err := replaceCommunityPostReferencesTx(ctx, tx, postID, snapshot, claims); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `delete from community_post_translations where post_id=$1 and source_revision_id<>$2`, postID, revisionID)
	return err
}

func replaceCommunityPostReferencesTx(ctx context.Context, tx pgx.Tx, postID int64, snapshot communityPostSnapshot, claims security.Claims) error {
	if communityPostReferencesNeedResolution(snapshot) {
		if err := resolveCommunityPostReferences(ctx, tx, claims, &snapshot); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `delete from community_post_project_refs where post_id=$1`, postID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from community_post_resource_refs where post_id=$1`, postID); err != nil {
		return err
	}
	if err := insertCommunityPostProjectReferencesTx(ctx, tx, postID, snapshot.Projects); err != nil {
		return err
	}
	return insertCommunityPostResourceReferencesTx(ctx, tx, postID, snapshot.Resources)
}

func communityPostReferencesNeedResolution(snapshot communityPostSnapshot) bool {
	for _, ref := range snapshot.Projects {
		if ref.PublicID != "" && ref.InternalID <= 0 {
			return true
		}
	}
	for _, ref := range snapshot.Resources {
		if ref.PublicID != "" && ref.InternalID <= 0 {
			return true
		}
	}
	return false
}

func insertCommunityPostProjectReferencesTx(ctx context.Context, tx pgx.Tx, postID int64, references []communityPostReference) error {
	if len(references) == 0 {
		return nil
	}
	types := make([]string, len(references))
	targetIDs := make([]int64, len(references))
	rawIdentifiers := make([]string, len(references))
	displayOrders := make([]int32, len(references))
	for index, ref := range references {
		types[index] = ref.Type
		targetIDs[index] = ref.InternalID
		rawIdentifiers[index] = ref.Identifier
		displayOrders[index] = int32(index)
	}
	rows, err := tx.Query(ctx, `insert into community_post_project_refs(
		post_id,target_type,target_id,raw_identifier,display_order)
		select $1,value.target_type,nullif(value.target_id,0),value.raw_identifier,value.display_order
		from unnest($2::text[],$3::bigint[],$4::text[],$5::integer[])
			as value(target_type,target_id,raw_identifier,display_order)
		returning id,display_order`, postID, types, targetIDs, rawIdentifiers, displayOrders)
	if err != nil {
		return err
	}
	referenceIDs := make(map[int]int64, len(references))
	for rows.Next() {
		var referenceID int64
		var displayOrder int
		if err = rows.Scan(&referenceID, &displayOrder); err != nil {
			rows.Close()
			return err
		}
		referenceIDs[displayOrder] = referenceID
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(referenceIDs) != len(references) {
		return errors.New("project reference batch insert was incomplete")
	}
	unresolvedIDs := make([]int64, 0, len(references))
	unresolvedTypes := make([]string, 0, len(references))
	unresolvedRaw := make([]string, 0, len(references))
	for index, ref := range references {
		if ref.InternalID > 0 {
			continue
		}
		unresolvedIDs = append(unresolvedIDs, referenceIDs[index])
		unresolvedTypes = append(unresolvedTypes, ref.Type)
		unresolvedRaw = append(unresolvedRaw, ref.Identifier)
	}
	return upsertCommunityPostUnresolvedReferencesTx(ctx, tx, "community_post_project", "projects",
		postID, unresolvedIDs, unresolvedTypes, unresolvedRaw)
}

func insertCommunityPostResourceReferencesTx(ctx context.Context, tx pgx.Tx, postID int64, references []communityPostReference) error {
	if len(references) == 0 {
		return nil
	}
	resourceIDs := make([]int64, len(references))
	kinds := make([]string, len(references))
	rawIdentifiers := make([]string, len(references))
	displayOrders := make([]int32, len(references))
	for index, ref := range references {
		resourceIDs[index] = ref.InternalID
		kinds[index] = ref.Kind
		rawIdentifiers[index] = ref.Identifier
		displayOrders[index] = int32(index)
	}
	rows, err := tx.Query(ctx, `insert into community_post_resource_refs(
		post_id,resource_id,kind_code,raw_resource_id,display_order)
		select $1,nullif(value.resource_id,0),value.kind_code,value.raw_identifier,value.display_order
		from unnest($2::bigint[],$3::text[],$4::text[],$5::integer[])
			as value(resource_id,kind_code,raw_identifier,display_order)
		returning id,display_order`, postID, resourceIDs, kinds, rawIdentifiers, displayOrders)
	if err != nil {
		return err
	}
	referenceIDs := make(map[int]int64, len(references))
	for rows.Next() {
		var referenceID int64
		var displayOrder int
		if err = rows.Scan(&referenceID, &displayOrder); err != nil {
			rows.Close()
			return err
		}
		referenceIDs[displayOrder] = referenceID
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(referenceIDs) != len(references) {
		return errors.New("resource reference batch insert was incomplete")
	}
	unresolvedIDs := make([]int64, 0, len(references))
	unresolvedKinds := make([]string, 0, len(references))
	unresolvedRaw := make([]string, 0, len(references))
	for index, ref := range references {
		if ref.InternalID > 0 {
			continue
		}
		unresolvedIDs = append(unresolvedIDs, referenceIDs[index])
		unresolvedKinds = append(unresolvedKinds, ref.Kind)
		unresolvedRaw = append(unresolvedRaw, ref.Identifier)
	}
	return upsertCommunityPostUnresolvedReferencesTx(ctx, tx, "community_post_resource", "resources",
		postID, unresolvedIDs, unresolvedKinds, unresolvedRaw)
}

func upsertCommunityPostUnresolvedReferencesTx(ctx context.Context, tx pgx.Tx, sourceType, fieldPath string,
	postID int64, sourceIDs []int64, referenceTypes, rawIdentifiers []string,
) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `insert into unresolved_references(
		source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata)
		select $1,value.source_id,$2,value.reference_type,value.raw_identifier,lower(value.raw_identifier),
			jsonb_build_object('postId',$3::bigint)
		from unnest($4::bigint[],$5::text[],$6::text[]) as value(source_id,reference_type,raw_identifier)
		on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
		set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
			resolved_at=null,metadata=excluded.metadata,updated_at=now()`, sourceType, fieldPath, postID,
		sourceIDs, referenceTypes, rawIdentifiers)
	return err
}

func (s *Server) loadCommunityPost(ctx context.Context, publicID string, claims security.Claims) (communityPostResponse, error) {
	var item communityPostResponse
	var internalID, authorInternalID int64
	var coverKey string
	moderator := claimsAllow(claims, "content.review") || claimsAllow(claims, "admin.*")
	err := s.db.QueryRow(ctx, `select post.id,post.public_id,post.kind,post.category,post.title,post.source_locale,post.body_markdown,
		post.minecraft_versions,post.mod_version_min,post.mod_version_max,post.severity,post.has_fix,post.issue_url,
		coalesce(file.public_id,''),coalesce(file.object_key,''),author.public_id,author.username,post.review_status,
		coalesce(revision.public_id,''),post.created_at,post.updated_at,post.author_id,
		post.resolution_status,coalesce(accepted.public_id,''),post.resolved_at
		from community_posts post join users author on author.id=post.author_id
		left join oss_files file on file.id=post.cover_file_id and file.status='active'
		left join content_revisions revision on revision.id=post.published_revision_id
		left join comments accepted on accepted.id=post.accepted_comment_id
		where post.public_id=$1 and post.status='active' and (post.review_status='approved' or post.author_id=$2 or $3)`,
		publicID, claims.Subject, moderator).Scan(&internalID, &item.ID, &item.Kind, &item.Category, &item.Title, &item.SourceLocale,
		&item.BodyMarkdown, &item.MinecraftVersions, &item.ModVersionMin, &item.ModVersionMax, &item.Severity, &item.HasFix,
		&item.IssueURL, &item.CoverFileID, &coverKey, &item.AuthorID, &item.AuthorName, &item.ReviewStatus, &item.PublishedRevisionID,
		&item.CreatedAt, &item.UpdatedAt, &authorInternalID,
		&item.ResolutionStatus, &item.AcceptedCommentID, &item.ResolvedAt)
	if err != nil {
		return item, err
	}
	if coverKey != "" {
		access, accessErr := s.resolveOSSObjectAccess(ctx, coverKey, ossObjectAccessOptions{})
		if accessErr != nil {
			return item, accessErr
		}
		item.CoverURL = access.URL
	}
	item.CanEdit = claims.Subject > 0 && (claims.Subject == authorInternalID || claimsAllow(claims, "community.edit") || claimsAllow(claims, "admin.*"))
	item.CanResolve = item.Kind == "discussion" && item.ResolutionStatus == "open" && item.ReviewStatus == "approved" && claims.Subject == authorInternalID
	item.Bounty, err = s.loadCommunityPostBounty(ctx, internalID)
	if err != nil {
		return item, err
	}
	item.Projects, item.Resources, err = s.communityPostReferences(ctx, internalID, claims)
	return item, err
}

func (s *Server) communityPostReferences(ctx context.Context, postID int64, claims security.Claims) ([]communityPostReference, []communityPostReference, error) {
	projects, resources, err := s.communityPostReferencesBatch(ctx, []int64{postID}, claims)
	return projects[postID], resources[postID], err
}

func loadCommunityPostProjectReferencesWithQueryer(ctx context.Context, queryer followProjectBatchQueryer, postIDs []int64, claims security.Claims) (map[int64][]communityPostReference, error) {
	projects := make(map[int64][]communityPostReference, len(postIDs))
	for _, postID := range postIDs {
		projects[postID] = make([]communityPostReference, 0)
	}
	if len(postIDs) == 0 {
		return projects, nil
	}
	type storedReference struct {
		postID                    int64
		publicID, targetType, raw string
		unresolved                bool
	}
	rows, err := queryer.Query(ctx, `select ref.post_id,coalesce(route.public_id,''),ref.target_type,ref.raw_identifier,ref.target_id is null
		from community_post_project_refs ref
		left join public_routes route on route.entity_type=ref.target_type and route.internal_id=ref.target_id
		where ref.post_id=any($1::bigint[]) order by ref.post_id,ref.display_order,ref.id`, postIDs)
	if err != nil {
		return nil, err
	}
	stored := make([]storedReference, 0)
	publicIDs := make([]string, 0)
	for rows.Next() {
		var ref storedReference
		if err = rows.Scan(&ref.postID, &ref.publicID, &ref.targetType, &ref.raw, &ref.unresolved); err != nil {
			rows.Close()
			return nil, err
		}
		stored = append(stored, ref)
		if !ref.unresolved && ref.publicID != "" {
			publicIDs = append(publicIDs, ref.publicID)
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	visible, err := loadFollowProjectTargetsWithQueryer(ctx, queryer, publicIDs, claims)
	if err != nil {
		return nil, err
	}
	for _, storedRef := range stored {
		ref := communityPostReference{Type: storedRef.targetType}
		switch {
		case storedRef.unresolved:
			ref.Identifier = storedRef.raw
			ref.Name = storedRef.raw
			ref.Unresolved = true
		case visible[storedRef.publicID].Type == storedRef.targetType:
			target := visible[storedRef.publicID]
			ref.PublicID = target.PublicID
			ref.Name = target.Name
			ref.SiteID = target.URL
			if separator := strings.LastIndexByte(ref.SiteID, '/'); separator >= 0 {
				ref.SiteID = ref.SiteID[separator+1:]
			}
		default:
			ref.Unavailable = true
		}
		projects[storedRef.postID] = append(projects[storedRef.postID], ref)
	}
	return projects, nil
}

func (s *Server) communityPostReferencesBatch(ctx context.Context, postIDs []int64, claims security.Claims) (map[int64][]communityPostReference, map[int64][]communityPostReference, error) {
	projects, err := loadCommunityPostProjectReferencesWithQueryer(ctx, s.db, postIDs, claims)
	if err != nil {
		return nil, nil, err
	}
	resources, err := loadCommunityPostResourceReferencesWithQueryer(ctx, s.db, postIDs)
	if err != nil {
		return nil, nil, err
	}
	return projects, resources, nil
}

func loadCommunityPostResourceReferencesWithQueryer(ctx context.Context, queryer followProjectBatchQueryer, postIDs []int64) (map[int64][]communityPostReference, error) {
	resources := make(map[int64][]communityPostReference, len(postIDs))
	for _, postID := range postIDs {
		resources[postID] = make([]communityPostReference, 0)
	}
	if len(postIDs) == 0 {
		return resources, nil
	}
	rows, err := queryer.Query(ctx, `select ref.post_id,coalesce(entity.public_id,''),ref.kind_code,ref.raw_resource_id,
		coalesce(resource.canonical_id,ref.raw_resource_id),coalesce(detail.version_id,0),coalesce(version.public_id,''),
		coalesce(detail.icon_file_id,0),coalesce(snapshot.revision_id,''),coalesce(snapshot.icon_path,''),
		coalesce(localized.names,snapshot.names,'{}'::jsonb),ref.resource_id is not null
		from community_post_resource_refs ref left join catalog_entities entity on entity.id=ref.resource_id
		 and entity.status='active' and entity.archived_at is null and `+publicCatalogEntitySQL("entity", "resource")+`
		left join game_resources resource on resource.entity_id=entity.id
		left join lateral (select value.version_id,coalesce(value.icon_small_file_id,value.icon_file_id) icon_file_id
			from mod_resource_version_details value
			join mod_content_versions public_version on public_version.id=value.version_id and public_version.status='active'
			join mods public_mod on public_mod.id=public_version.mod_id and public_mod.review_status='approved'
			where value.resource_id=entity.id and value.status='active'
			order by value.updated_at desc limit 1) detail on true
		left join mod_content_versions version on version.id=detail.version_id
		left join lateral (select imported.revision_id,imported.icon_path,imported.names from resource_import_snapshots imported
			join catalog_import_revisions import_revision on import_revision.id=imported.revision_id
			join mods public_mod on public_mod.id=import_revision.mod_id and public_mod.review_status='approved'
			where imported.resource_id=entity.id and import_revision.is_active and import_revision.status in ('ready','partial')
			order by (import_revision.target_version_id=detail.version_id) desc,
			(imported.icon_path<>'') desc,imported.created_at desc limit 1) snapshot on true
		left join lateral (select jsonb_object_agg(value.locale,value.name) names from content_localizations value
			where value.catalog_entity_id=entity.id and value.name<>'') localized on true
		where ref.post_id=any($1::bigint[]) order by ref.post_id,ref.display_order,ref.id`, postIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var postID int64
		var ref communityPostReference
		var versionInternalID, iconFileID int64
		var namesRaw []byte
		var resolvedReference bool
		if err = rows.Scan(&postID, &ref.PublicID, &ref.Kind, &ref.Identifier, &ref.Name, &versionInternalID, &ref.VersionID,
			&iconFileID, &ref.RevisionID, &ref.IconPath, &namesRaw, &resolvedReference); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(namesRaw, &ref.Names); err != nil {
			return nil, fmt.Errorf("decode community resource names for post %d resource %q: %w", postID, ref.Identifier, err)
		}
		ref.Unavailable = resolvedReference && ref.PublicID == ""
		ref.Unresolved = !resolvedReference
		if iconFileID > 0 && ref.PublicID != "" && ref.VersionID != "" {
			ref.IconURL = "/api/v1/catalog/resources/" + ref.PublicID + "/versions/" + ref.VersionID + "/assets/icon-small"
		}
		resources[postID] = append(resources[postID], ref)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return resources, nil
}
