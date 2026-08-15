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
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

const communityPostAggregate = "community_post"

type communityPostReference struct {
	PublicID   string            `json:"publicId,omitempty"`
	Type       string            `json:"type,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Identifier string            `json:"identifier"`
	Name       string            `json:"name,omitempty"`
	SiteID     string            `json:"siteId,omitempty"`
	Names      map[string]string `json:"names,omitempty"`
	IconURL    string            `json:"iconUrl,omitempty"`
	VersionID  string            `json:"versionId,omitempty"`
	RevisionID string            `json:"revisionId,omitempty"`
	IconPath   string            `json:"iconPath,omitempty"`
	Unresolved bool              `json:"unresolved,omitempty"`
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
	ID                string                   `json:"id"`
	Kind              string                   `json:"kind"`
	Category          string                   `json:"category"`
	Title             string                   `json:"title"`
	SourceLocale      string                   `json:"sourceLocale"`
	BodyMarkdown      string                   `json:"bodyMarkdown,omitempty"`
	MinecraftVersions []string                 `json:"minecraftVersions"`
	ModVersionMin     string                   `json:"modVersionMin,omitempty"`
	ModVersionMax     string                   `json:"modVersionMax,omitempty"`
	Severity          string                   `json:"severity,omitempty"`
	HasFix            bool                     `json:"hasFix,omitempty"`
	IssueURL          string                   `json:"issueUrl,omitempty"`
	CoverURL          string                   `json:"coverUrl,omitempty"`
	CoverFileID       string                   `json:"coverFileId,omitempty"`
	ResolutionStatus  string                   `json:"resolutionStatus,omitempty"`
	AcceptedCommentID string                   `json:"acceptedCommentId,omitempty"`
	ResolvedAt        *time.Time               `json:"resolvedAt,omitempty"`
	Bounty            *communityPostBounty     `json:"bounty,omitempty"`
	AuthorID          string                   `json:"authorId"`
	AuthorName        string                   `json:"authorName"`
	ReviewStatus      string                   `json:"reviewStatus"`
	Projects          []communityPostReference `json:"projects"`
	Resources         []communityPostReference `json:"resources"`
	CreatedAt         time.Time                `json:"createdAt"`
	UpdatedAt         time.Time                `json:"updatedAt"`
	CanEdit           bool                     `json:"canEdit"`
	CanResolve        bool                     `json:"canResolve"`
}

func (s *Server) communityPosts(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.createCommunityPost(w, r)
		return
	}
	kind := normalizeCommunityPostKind(r.URL.Query().Get("kind"))
	if kind == "" {
		writeError(w, http.StatusBadRequest, "invalid community post kind")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 24, 100)
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	query, validQuery := parseCatalogQuery(r.URL.Query().Get("q"))
	if !validQuery {
		writeError(w, http.StatusBadRequest, "invalid community post query")
		return
	}
	category := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	if category != "" && !communityPostCategoryAllowed(kind, category) {
		writeError(w, http.StatusBadRequest, "invalid community post category")
		return
	}
	versions, validVersions := parseCatalogList(r.URL.Query().Get("version"), 20)
	if !validVersions {
		writeError(w, http.StatusBadRequest, "invalid Minecraft version filter")
		return
	}
	sort := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sort == "" {
		sort = "latest"
	}
	if sort != "latest" && sort != "oldest" && sort != "updated" {
		writeError(w, http.StatusBadRequest, "invalid community post sort")
		return
	}
	modID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("modId")))
	resourceID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("resourceId")))
	viewerID := currentClaims(r).Subject
	moderator := claimsAllow(currentClaims(r), "content.review") || claimsAllow(currentClaims(r), "admin.*")
	indexed := indexedSearchPage{}
	if category == "" && len(versions) == 0 && sort == "latest" {
		indexed = s.searchCommunityPage(r.Context(), query, kind, modID, resourceID, currentClaims(r), moderator, limit, offset)
	}
	databaseOffset := offset
	if indexed.Used {
		databaseOffset = 0
	}
	orderSQL := "post.published_at desc nulls last,post.created_at desc,post.id desc"
	if indexed.Used {
		orderSQL = "array_position($8::bigint[],post.id),post.updated_at desc,post.id desc"
	} else if sort == "oldest" {
		orderSQL = "post.created_at asc,post.id asc"
	} else if sort == "updated" {
		orderSQL = "post.updated_at desc,post.id desc"
	}
	rows, err := s.db.Query(r.Context(), `select post.id,post.public_id,post.kind,post.category,post.title,post.source_locale,post.body_markdown,
		post.minecraft_versions,post.mod_version_min,post.mod_version_max,post.severity,post.has_fix,post.issue_url,
		coalesce(file.public_id,''),coalesce(file.object_key,''),author.public_id,author.username,post.review_status,post.created_at,post.updated_at,post.author_id,
		post.resolution_status,coalesce(accepted.public_id,''),post.resolved_at
		from community_posts post join users author on author.id=post.author_id
		left join oss_files file on file.id=post.cover_file_id and file.status='active'
		left join comments accepted on accepted.id=post.accepted_comment_id
		where post.kind=$1 and post.status='active' and (post.review_status='approved' or post.author_id=$2 or $3)
		and (($7 and post.id=any($8::bigint[])) or (not $7 and ($4='' or lower(post.title) like '%%'||lower($4)||'%%' or lower(post.body_markdown) like '%%'||lower($4)||'%%')))
		and ($5='' or exists(select 1 from community_post_project_refs ref join public_routes route on route.internal_id=ref.target_id and route.entity_type=ref.target_type where ref.post_id=post.id and route.public_id=$5))
		and ($6='' or exists(select 1 from community_post_resource_refs ref join catalog_entities entity on entity.id=ref.resource_id where ref.post_id=post.id and entity.public_id=$6))
		and ($11='' or post.category=$11)
		and (cardinality($12::text[])=0 or post.minecraft_versions && $12::text[])
		order by `+orderSQL+`
		limit $9 offset $10`, kind, viewerID, moderator, query, modID, resourceID, indexed.Used, indexed.IDs, limit, databaseOffset, category, versions)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community posts")
		return
	}
	defer rows.Close()
	items := make([]communityPostResponse, 0)
	internalIDs := make([]int64, 0, limit)
	ossCfg := s.ossConfigFromSettings(r.Context())
	for rows.Next() {
		var item communityPostResponse
		var internalID, authorInternalID int64
		var coverKey string
		if err = rows.Scan(&internalID, &item.ID, &item.Kind, &item.Category, &item.Title, &item.SourceLocale, &item.BodyMarkdown,
			&item.MinecraftVersions, &item.ModVersionMin, &item.ModVersionMax, &item.Severity, &item.HasFix, &item.IssueURL,
			&item.CoverFileID, &coverKey, &item.AuthorID, &item.AuthorName, &item.ReviewStatus, &item.CreatedAt, &item.UpdatedAt, &authorInternalID,
			&item.ResolutionStatus, &item.AcceptedCommentID, &item.ResolvedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode community post")
			return
		}
		if coverKey != "" {
			access, accessErr := s.resolveOSSObjectAccessWithConfig(r.Context(), ossCfg, coverKey, ossObjectAccessOptions{})
			if accessErr != nil {
				writeError(w, http.StatusBadGateway, "failed to generate community post cover URL")
				return
			}
			item.CoverURL = access.URL
		}
		item.CanEdit = viewerID > 0 && (viewerID == authorInternalID || claimsAllow(currentClaims(r), "community.edit") || moderator)
		item.CanResolve = item.Kind == "discussion" && item.ResolutionStatus == "open" && item.ReviewStatus == "approved" && viewerID == authorInternalID
		items = append(items, item)
		internalIDs = append(internalIDs, internalID)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community posts")
		return
	}
	bounties, err := s.loadCommunityPostBounties(r.Context(), internalIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community post bounties")
		return
	}
	projects, resources, err := s.communityPostReferencesBatch(r.Context(), internalIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community post references")
		return
	}
	for index, internalID := range internalIDs {
		items[index].Bounty = bounties[internalID]
		items[index].Projects = projects[internalID]
		items[index].Resources = resources[internalID]
	}
	var total int
	if indexed.Used {
		total = indexed.Total
	} else {
		_ = s.db.QueryRow(r.Context(), `select count(*) from community_posts post where post.kind=$1 and post.status='active'
		and (post.review_status='approved' or post.author_id=$2 or $3)
		and ($4='' or lower(post.title) like '%%'||lower($4)||'%%' or lower(post.body_markdown) like '%%'||lower($4)||'%%')
		and ($5='' or exists(select 1 from community_post_project_refs ref join public_routes route on route.internal_id=ref.target_id and route.entity_type=ref.target_type where ref.post_id=post.id and route.public_id=$5))
		and ($6='' or exists(select 1 from community_post_resource_refs ref join catalog_entities entity on entity.id=ref.resource_id where ref.post_id=post.id and entity.public_id=$6))
		and ($7='' or post.category=$7)
		and (cardinality($8::text[])=0 or post.minecraft_versions && $8::text[])`,
			kind, viewerID, moderator, query, modID, resourceID, category, versions).Scan(&total)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset, "categories": communityPostCategories(kind)})
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
	var snapshot communityPostSnapshot
	if decodeJSON(r, &snapshot) != nil || normalizeCommunityPostSnapshot(&snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid community post")
		return
	}
	permission := "community." + snapshot.Kind + ".create"
	if !claimsAllow(claims, permission) && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "community post permission is required")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start community post creation")
		return
	}
	defer tx.Rollback(r.Context())
	if err = s.resolveCommunityPostSnapshot(r.Context(), tx, claims.Subject, 0, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	reviewConfig := loadReviewConfig(r.Context(), s.db)
	reviewRequired := communityPostReviewRequired(reviewConfig, snapshot.Kind, false)
	if claimsAllow(claims, "community.no-review") || claimsAllow(claims, "admin.*") {
		reviewRequired = false
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
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status == "approved" {
		err = applyCommunityPostSnapshotTx(r.Context(), tx, internalID, created.RevisionID, snapshot)
	} else {
		err = replaceCommunityPostReferencesTx(r.Context(), tx, internalID, snapshot)
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
	var baseRevision *int64
	if err := s.db.QueryRow(r.Context(), `select id,author_id,kind,published_revision_id from community_posts where public_id=$1 and status='active'`, publicID).
		Scan(&postID, &authorID, &kind, &baseRevision); err != nil {
		writeError(w, http.StatusNotFound, "community post not found")
		return
	}
	if claims.Subject != authorID && !claimsAllow(claims, "community.edit") && !claimsAllow(claims, "admin.*") {
		writeError(w, http.StatusForbidden, "cannot edit this community post")
		return
	}
	var snapshot communityPostSnapshot
	if decodeJSON(r, &snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid community post")
		return
	}
	snapshot.Kind = kind
	if normalizeCommunityPostSnapshot(&snapshot) != nil {
		writeError(w, http.StatusBadRequest, "invalid community post")
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start community post update")
		return
	}
	defer tx.Rollback(r.Context())
	if err = s.resolveCommunityPostSnapshot(r.Context(), tx, claims.Subject, postID, &snapshot); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err = validateCommunityPostBountyEditTx(r.Context(), tx, postID, publicID, authorID, snapshot); err != nil {
		if errors.Is(err, errInsufficientBalance) {
			writeError(w, http.StatusConflict, "insufficient balance for question bounty")
		} else {
			writeError(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	reviewConfig := loadReviewConfig(r.Context(), s.db)
	reviewRequired := communityPostReviewRequired(reviewConfig, kind, true)
	if claimsAllow(claims, "community.no-review") || claimsAllow(claims, "admin.*") {
		reviewRequired = false
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
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if status == "approved" {
		if err = applyCommunityPostSnapshotTx(r.Context(), tx, postID, created.RevisionID, snapshot); err == nil {
			err = appendReviewResolutionTx(r.Context(), tx, created.ChangeRequestID, "approved", claims.Subject, "automatic approval", r)
		}
	} else {
		_, err = tx.Exec(r.Context(), `update community_posts set
			review_status=case when published_revision_id is null then 'pending' else 'approved' end,
			updated_at=now() where id=$1`, postID)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to save community post")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": publicID, "reviewStatus": status, "revisionId": created.RevisionPublicID, "changeRequestId": created.ChangeRequestPublicID})
}

func normalizeCommunityPostKind(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "tutorial", "issue", "news", "discussion":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return ""
	}
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
	snapshot.SourceLocale = detectCommunityPostLocale(snapshot.Title + "\n" + snapshot.BodyMarkdown)
	snapshot.MinecraftVersions = uniqueNonEmpty(snapshot.MinecraftVersions)
	snapshot.ModVersionMin = strings.TrimSpace(snapshot.ModVersionMin)
	snapshot.ModVersionMax = strings.TrimSpace(snapshot.ModVersionMax)
	snapshot.IssueURL = strings.TrimSpace(snapshot.IssueURL)
	snapshot.BountyCurrency = normalizeCode(snapshot.BountyCurrency)
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
		if len(snapshot.Projects) == 0 || len(snapshot.Resources) == 0 {
			return errors.New("issues require at least one project and one resource")
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
		"de-DE": wordSet("der", "die", "das", "und", "ist", "mit", "für", "nicht", "eine", "einen"),
		"es-ES": wordSet("el", "la", "los", "las", "una", "para", "con", "que", "del", "este"),
		"fr-FR": wordSet("le", "la", "les", "une", "des", "pour", "avec", "est", "dans", "cette"),
	}
	bestLocale, bestScore := "en-US", 0
	for locale, candidates := range stopWords {
		score := 0
		for _, word := range words {
			if _, ok := candidates[word]; ok {
				score++
			}
		}
		if score > bestScore {
			bestLocale, bestScore = locale, score
		}
	}
	if bestScore >= 2 {
		return bestLocale
	}
	return "en-US"
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

func (s *Server) resolveCommunityPostSnapshot(ctx context.Context, tx pgx.Tx, actorID, postID int64, snapshot *communityPostSnapshot) error {
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
	for index := range snapshot.Projects {
		ref := &snapshot.Projects[index]
		ref.Type = strings.ToLower(strings.TrimSpace(ref.Type))
		if ref.Type == "" {
			ref.Type = "mod"
		}
		switch ref.Type {
		case "mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		default:
			return errors.New("project reference type is invalid")
		}
		ref.PublicID = strings.ToLower(strings.TrimSpace(ref.PublicID))
		ref.Identifier = strings.TrimSpace(ref.Identifier)
		if ref.PublicID != "" && !strings.HasPrefix(ref.PublicID, "unresolved:") {
			var targetID int64
			if tx.QueryRow(ctx, `select internal_id from public_routes where public_id=$1 and entity_type=$2`, ref.PublicID, ref.Type).Scan(&targetID) != nil {
				return errors.New("selected project was not found")
			}
			ref.Identifier = ""
		} else if ref.Identifier == "" {
			return errors.New("project reference is incomplete")
		}
	}
	for index := range snapshot.Resources {
		ref := &snapshot.Resources[index]
		ref.PublicID = strings.ToLower(strings.TrimSpace(ref.PublicID))
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.Identifier = strings.TrimSpace(ref.Identifier)
		if ref.PublicID != "" && !strings.HasPrefix(ref.PublicID, "unresolved:") {
			var resourceID int64
			if tx.QueryRow(ctx, `select entity.id,resource.kind_code from catalog_entities entity
				join game_resources resource on resource.entity_id=entity.id
				where entity.public_id=$1 and entity.status='active'`, ref.PublicID).Scan(&resourceID, &ref.Kind) != nil {
				return errors.New("selected resource was not found")
			}
			ref.Identifier = ""
		} else if ref.Identifier == "" || ref.Kind == "" {
			return errors.New("resource reference is incomplete")
		} else {
			var exists bool
			if tx.QueryRow(ctx, `select exists(select 1 from resource_kinds where code=$1)`, ref.Kind).Scan(&exists) != nil || !exists {
				return errors.New("resource reference kind is invalid")
			}
		}
	}
	return nil
}

func applyCommunityPostSnapshotTx(ctx context.Context, tx pgx.Tx, postID, revisionID int64, snapshot communityPostSnapshot) error {
	if _, err := tx.Exec(ctx, `update community_posts set category=$2,title=$3,source_locale=$4,body_markdown=$5,minecraft_versions=$6,
		mod_version_min=$7,mod_version_max=$8,severity=$9,has_fix=$10,issue_url=$11,cover_file_id=$12,
		review_status='approved',published_revision_id=$13,published_at=coalesce(published_at,now()),updated_at=now() where id=$1`,
		postID, snapshot.Category, snapshot.Title, snapshot.SourceLocale, snapshot.BodyMarkdown, snapshot.MinecraftVersions, snapshot.ModVersionMin,
		snapshot.ModVersionMax, snapshot.Severity, snapshot.HasFix, snapshot.IssueURL, snapshot.CoverInternalID, revisionID); err != nil {
		return err
	}
	if err := replaceCommunityPostReferencesTx(ctx, tx, postID, snapshot); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `delete from community_post_translations where post_id=$1 and source_revision_id<>$2`, postID, revisionID)
	return err
}

func replaceCommunityPostReferencesTx(ctx context.Context, tx pgx.Tx, postID int64, snapshot communityPostSnapshot) error {
	if _, err := tx.Exec(ctx, `delete from community_post_project_refs where post_id=$1`, postID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from community_post_resource_refs where post_id=$1`, postID); err != nil {
		return err
	}
	for index, ref := range snapshot.Projects {
		var targetID *int64
		if ref.PublicID != "" && !strings.HasPrefix(ref.PublicID, "unresolved:") {
			var id int64
			if err := tx.QueryRow(ctx, `select internal_id from public_routes where public_id=$1 and entity_type=$2`, ref.PublicID, ref.Type).Scan(&id); err != nil {
				return err
			}
			targetID = &id
		}
		var referenceID int64
		if err := tx.QueryRow(ctx, `insert into community_post_project_refs(post_id,target_type,target_id,raw_identifier,display_order)
			values($1,$2,$3,$4,$5) returning id`, postID, ref.Type, targetID, ref.Identifier, index).Scan(&referenceID); err != nil {
			return err
		}
		if targetID == nil {
			if _, err := tx.Exec(ctx, `insert into unresolved_references(
				source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
			) values('community_post_project',$1,'projects',$2,$3,lower($3),jsonb_build_object('postId',$4::bigint))
			on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
			set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
				resolved_at=null,metadata=excluded.metadata,updated_at=now()`, referenceID, ref.Type, ref.Identifier, postID); err != nil {
				return err
			}
		}
	}
	for index, ref := range snapshot.Resources {
		var resourceID *int64
		if ref.PublicID != "" && !strings.HasPrefix(ref.PublicID, "unresolved:") {
			var id int64
			if err := tx.QueryRow(ctx, `select id from catalog_entities where public_id=$1`, ref.PublicID).Scan(&id); err != nil {
				return err
			}
			resourceID = &id
		}
		var referenceID int64
		if err := tx.QueryRow(ctx, `insert into community_post_resource_refs(post_id,resource_id,kind_code,raw_resource_id,display_order)
			values($1,$2,$3,$4,$5) returning id`, postID, resourceID, ref.Kind, ref.Identifier, index).Scan(&referenceID); err != nil {
			return err
		}
		if resourceID == nil {
			if _, err := tx.Exec(ctx, `insert into unresolved_references(
				source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier,metadata
			) values('community_post_resource',$1,'resources',$2,$3,lower($3),jsonb_build_object('postId',$4::bigint))
			on conflict(source_type,source_id,field_path,reference_type,normalized_identifier) do update
			set raw_identifier=excluded.raw_identifier,status='pending',resolved_type='',resolved_id=null,
				resolved_at=null,metadata=excluded.metadata,updated_at=now()`, referenceID, ref.Kind, ref.Identifier, postID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) loadCommunityPost(ctx context.Context, publicID string, claims security.Claims) (communityPostResponse, error) {
	var item communityPostResponse
	var internalID, authorInternalID int64
	var coverKey string
	moderator := claimsAllow(claims, "content.review") || claimsAllow(claims, "admin.*")
	err := s.db.QueryRow(ctx, `select post.id,post.public_id,post.kind,post.category,post.title,post.source_locale,post.body_markdown,
		post.minecraft_versions,post.mod_version_min,post.mod_version_max,post.severity,post.has_fix,post.issue_url,
		coalesce(file.public_id,''),coalesce(file.object_key,''),author.public_id,author.username,post.review_status,post.created_at,post.updated_at,post.author_id,
		post.resolution_status,coalesce(accepted.public_id,''),post.resolved_at
		from community_posts post join users author on author.id=post.author_id
		left join oss_files file on file.id=post.cover_file_id and file.status='active'
		left join comments accepted on accepted.id=post.accepted_comment_id
		where post.public_id=$1 and post.status='active' and (post.review_status='approved' or post.author_id=$2 or $3)`,
		publicID, claims.Subject, moderator).Scan(&internalID, &item.ID, &item.Kind, &item.Category, &item.Title, &item.SourceLocale,
		&item.BodyMarkdown, &item.MinecraftVersions, &item.ModVersionMin, &item.ModVersionMax, &item.Severity, &item.HasFix,
		&item.IssueURL, &item.CoverFileID, &coverKey, &item.AuthorID, &item.AuthorName, &item.ReviewStatus, &item.CreatedAt, &item.UpdatedAt, &authorInternalID,
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
	item.CanEdit = claims.Subject > 0 && (claims.Subject == authorInternalID || claimsAllow(claims, "community.edit") || moderator)
	item.CanResolve = item.Kind == "discussion" && item.ResolutionStatus == "open" && item.ReviewStatus == "approved" && claims.Subject == authorInternalID
	item.Bounty, err = s.loadCommunityPostBounty(ctx, internalID)
	if err != nil {
		return item, err
	}
	item.Projects, item.Resources, err = s.communityPostReferences(ctx, internalID)
	return item, err
}

func (s *Server) communityPostReferences(ctx context.Context, postID int64) ([]communityPostReference, []communityPostReference, error) {
	projects, resources, err := s.communityPostReferencesBatch(ctx, []int64{postID})
	return projects[postID], resources[postID], err
}

func (s *Server) communityPostReferencesBatch(ctx context.Context, postIDs []int64) (map[int64][]communityPostReference, map[int64][]communityPostReference, error) {
	projects := make(map[int64][]communityPostReference, len(postIDs))
	resources := make(map[int64][]communityPostReference, len(postIDs))
	for _, postID := range postIDs {
		projects[postID] = make([]communityPostReference, 0)
		resources[postID] = make([]communityPostReference, 0)
	}
	if len(postIDs) == 0 {
		return projects, resources, nil
	}
	rows, err := s.db.Query(ctx, `select ref.post_id,coalesce(route.public_id,''),ref.target_type,ref.raw_identifier,
		coalesce(mod.slug,modpack.slug,project.slug,''),coalesce(mod.primary_name,modpack.primary_name,project.primary_name,ref.raw_identifier)
		from community_post_project_refs ref left join public_routes route on route.entity_type=ref.target_type and route.internal_id=ref.target_id
		left join mods mod on ref.target_type='mod' and mod.id=ref.target_id
		left join modpacks modpack on ref.target_type='modpack' and modpack.id=ref.target_id
		left join simple_projects project on project.project_type=ref.target_type and project.id=ref.target_id
		where ref.post_id=any($1::bigint[]) order by ref.post_id,ref.display_order,ref.id`, postIDs)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var postID int64
		var ref communityPostReference
		if err = rows.Scan(&postID, &ref.PublicID, &ref.Type, &ref.Identifier, &ref.SiteID, &ref.Name); err != nil {
			rows.Close()
			return nil, nil, err
		}
		ref.Unresolved = ref.PublicID == ""
		projects[postID] = append(projects[postID], ref)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()
	rows, err = s.db.Query(ctx, `select ref.post_id,coalesce(entity.public_id,''),ref.kind_code,ref.raw_resource_id,
		coalesce(resource.canonical_id,ref.raw_resource_id),coalesce(detail.version_id,0),coalesce(version.public_id,''),
		coalesce(detail.icon_file_id,0),coalesce(snapshot.revision_id,''),coalesce(snapshot.icon_path,''),
		coalesce(localized.names,snapshot.names,'{}'::jsonb)
		from community_post_resource_refs ref left join catalog_entities entity on entity.id=ref.resource_id
		left join game_resources resource on resource.entity_id=ref.resource_id
		left join lateral (select value.version_id,coalesce(value.icon_small_file_id,value.icon_file_id) icon_file_id
			from mod_resource_version_details value where value.resource_id=ref.resource_id and value.status='active'
			order by value.updated_at desc limit 1) detail on true
		left join mod_content_versions version on version.id=detail.version_id
		left join lateral (select imported.revision_id,imported.icon_path,imported.names from resource_import_snapshots imported
			join catalog_import_revisions import_revision on import_revision.id=imported.revision_id
			where imported.resource_id=ref.resource_id order by (import_revision.target_version_id=detail.version_id) desc,
			(imported.icon_path<>'') desc,imported.created_at desc limit 1) snapshot on true
		left join lateral (select jsonb_object_agg(value.locale,value.name) names from content_localizations value
			where value.catalog_entity_id=ref.resource_id and value.name<>'') localized on true
		where ref.post_id=any($1::bigint[]) order by ref.post_id,ref.display_order,ref.id`, postIDs)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var postID int64
		var ref communityPostReference
		var versionInternalID, iconFileID int64
		var namesRaw []byte
		if err = rows.Scan(&postID, &ref.PublicID, &ref.Kind, &ref.Identifier, &ref.Name, &versionInternalID, &ref.VersionID,
			&iconFileID, &ref.RevisionID, &ref.IconPath, &namesRaw); err != nil {
			return nil, nil, err
		}
		_ = json.Unmarshal(namesRaw, &ref.Names)
		ref.Unresolved = ref.PublicID == ""
		if iconFileID > 0 && ref.PublicID != "" && ref.VersionID != "" {
			ref.IconURL = "/api/v1/catalog/resources/" + ref.PublicID + "/versions/" + ref.VersionID + "/assets/icon-small"
		}
		resources[postID] = append(resources[postID], ref)
	}
	return projects, resources, rows.Err()
}

func (s *Server) requestCommunityPostTranslation(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var request struct {
		TargetLocale string `json:"targetLocale"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "invalid translation request")
		return
	}
	targetLocale := normalizeContentLocale(request.TargetLocale)
	var postID, revisionID int64
	var sourceLocale, title, body string
	err := s.db.QueryRow(r.Context(), `select id,published_revision_id,source_locale,title,body_markdown from community_posts
		where public_id=$1 and status='active' and review_status='approved'`, publicID).Scan(&postID, &revisionID, &sourceLocale, &title, &body)
	if err != nil || targetLocale == "" || targetLocale == sourceLocale {
		writeError(w, http.StatusBadRequest, "translation request is invalid")
		return
	}
	var cachedTitle, cachedBody string
	if s.db.QueryRow(r.Context(), `select title,body_markdown from community_post_translations where post_id=$1 and locale=$2 and source_revision_id=$3`, postID, targetLocale, revisionID).Scan(&cachedTitle, &cachedBody) == nil {
		writeJSON(w, http.StatusOK, map[string]any{"cached": true, "translation": map[string]string{"title": cachedTitle, "bodyMarkdown": cachedBody}})
		return
	}
	limit := int64(claimsNumericPermissionValue(claims, "user.ai.daily_token_limit"))
	if limit <= 0 {
		writeError(w, http.StatusForbidden, "no daily AI token allowance is available")
		return
	}
	task, err := s.enqueueCommunityPostTranslation(r.Context(), publicID, postID, revisionID, sourceLocale, targetLocale, title, body, claims.Subject, limit)
	if errors.Is(err, errAIQuotaExceeded) {
		writeError(w, http.StatusTooManyRequests, "daily AI token allowance is insufficient")
		return
	}
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if task.Created && !s.publishContentTranslationTask(r.Context(), task) {
		writeError(w, http.StatusServiceUnavailable, "translation queue is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": task.TaskUID, "status": task.Status, "countsTowardDailyTokenQuota": true})
}

func (s *Server) enqueueCommunityPostTranslation(ctx context.Context, publicID string, postID, revisionID int64, sourceLocale, targetLocale, title, body string, actorID, tokenLimit int64) (enqueuedContentTranslation, error) {
	cfg := s.aiConfigFromSettings(ctx)
	binding, ok := findAITaskModel(cfg.TaskModels, aiTaskContentTranslation)
	if !ok {
		return enqueuedContentTranslation{}, errors.New("content translation is not configured")
	}
	provider, model, ok := resolveAIModel(cfg, binding.ModelKey)
	if !ok {
		return enqueuedContentTranslation{}, errors.New("content translation model is unavailable")
	}
	payload := map[string]any{"scope": "community_post", "postId": publicID, "postInternalId": postID,
		"sourceRevisionId": revisionID, "sourceLocale": sourceLocale, "targetLocale": targetLocale,
		"quotaBacked": true, "items": []map[string]string{{"key": "title", "text": title}, {"key": "bodyMarkdown", "text": body}}}
	raw, _ := json.Marshal(payload)
	reserved := int64(utf8.RuneCountInString(title+body)*2 + 256)
	concurrencyKey := "community-post:" + publicID + ":" + targetLocale + ":" + strconv.FormatInt(revisionID, 10) + ":actor:" + strconv.FormatInt(actorID, 10)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return enqueuedContentTranslation{}, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtext($1))`, concurrencyKey); err != nil {
		return enqueuedContentTranslation{}, err
	}
	var existing enqueuedContentTranslation
	err = tx.QueryRow(ctx, `select id,task_uid,status from ai_tasks where task_type=$1 and concurrency_key=$2
		and status in ('queued','running','retrying') order by created_at desc limit 1`, aiTaskContentTranslation, concurrencyKey).
		Scan(&existing.TaskID, &existing.TaskUID, &existing.Status)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return enqueuedContentTranslation{}, err
		}
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return enqueuedContentTranslation{}, err
	}
	if err = reserveAITaskQuotaTx(ctx, tx, actorID, tokenLimit, reserved); err != nil {
		return enqueuedContentTranslation{}, err
	}
	taskUID := "ai_" + randomHex(16)
	var taskID int64
	err = tx.QueryRow(ctx, `insert into ai_tasks(task_uid,task_type,provider,model,status,priority,concurrency_key,payload,created_by,queued_at,quota_reserved_tokens)
		values($1,$2,$3,$4,'queued',0,$5,$6::jsonb,$7,now(),$8) returning id`, taskUID, aiTaskContentTranslation,
		provider.Code, model.Model, concurrencyKey, string(raw), actorID, reserved).Scan(&taskID)
	if err != nil {
		return enqueuedContentTranslation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return enqueuedContentTranslation{}, err
	}
	s.writeAITaskLog(ctx, taskID, "info", "task_queued", "Community post translation queued", payload)
	return enqueuedContentTranslation{TaskID: taskID, TaskUID: taskUID, Status: "queued", Created: true}, nil
}

func (s *Server) communityPostTranslationResult(w http.ResponseWriter, r *http.Request) {
	taskUID := strings.ToLower(strings.TrimSpace(r.PathValue("taskId")))
	var status, errorText string
	var createdBy int64
	var payloadRaw []byte
	if s.db.QueryRow(r.Context(), `select status,error,coalesce(created_by,0),payload from ai_tasks where task_uid=$1 and task_type=$2`, taskUID, aiTaskContentTranslation).
		Scan(&status, &errorText, &createdBy, &payloadRaw) != nil || createdBy != currentClaims(r).Subject {
		writeError(w, http.StatusNotFound, "translation task not found")
		return
	}
	response := map[string]any{"taskId": taskUID, "status": status, "error": errorText}
	if status == "completed" {
		var payload struct {
			PostInternalID int64  `json:"postInternalId"`
			TargetLocale   string `json:"targetLocale"`
			SourceRevision int64  `json:"sourceRevisionId"`
		}
		_ = json.Unmarshal(payloadRaw, &payload)
		var title, body string
		if s.db.QueryRow(r.Context(), `select title,body_markdown from community_post_translations where post_id=$1 and locale=$2 and source_revision_id=$3`, payload.PostInternalID, payload.TargetLocale, payload.SourceRevision).Scan(&title, &body) == nil {
			response["translation"] = map[string]string{"title": title, "bodyMarkdown": body}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (worker *AIWorker) persistCommunityPostTranslation(ctx context.Context, taskID int64, rawPayload []byte, result map[string]any) error {
	var payload struct {
		PostInternalID int64  `json:"postInternalId"`
		TargetLocale   string `json:"targetLocale"`
		SourceRevision int64  `json:"sourceRevisionId"`
	}
	if json.Unmarshal(rawPayload, &payload) != nil || payload.PostInternalID <= 0 || payload.SourceRevision <= 0 {
		return errors.New("community post translation payload is invalid")
	}
	translated := translationItemsToMap(result)
	if strings.TrimSpace(translated["title"]) == "" || strings.TrimSpace(translated["bodyMarkdown"]) == "" {
		return errors.New("community post translation result is incomplete")
	}
	var currentRevision int64
	if worker.db.QueryRow(ctx, `select published_revision_id from community_posts where id=$1 and review_status='approved'`, payload.PostInternalID).Scan(&currentRevision) != nil || currentRevision != payload.SourceRevision {
		return errors.New("community post changed while translation was running")
	}
	_, err := worker.db.Exec(ctx, `insert into community_post_translations(post_id,locale,title,body_markdown,source_revision_id,ai_task_id)
		values($1,$2,$3,$4,$5,$6) on conflict(post_id,locale) do update set title=excluded.title,
		body_markdown=excluded.body_markdown,source_revision_id=excluded.source_revision_id,ai_task_id=excluded.ai_task_id,updated_at=now()`,
		payload.PostInternalID, normalizeContentLocale(payload.TargetLocale), translated["title"], translated["bodyMarkdown"], payload.SourceRevision, taskID)
	return err
}
