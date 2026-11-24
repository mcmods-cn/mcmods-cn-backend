package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

var errWriteAccountBanned = errors.New("account is banned")

// Read the current session and permission rules inside the transaction after
// waiting for the target lock or completing external work.
func resolveCurrentWriteActorTx(ctx context.Context, tx pgx.Tx, claims security.Claims) (security.Claims, error) {
	if claims.ExpiresAt <= time.Now().Unix() {
		return security.Claims{}, pgx.ErrNoRows
	}
	userID, err := readSessionSubject(ctx, tx, claims, true, nil)
	if err != nil {
		return security.Claims{}, err
	}
	if userID != claims.Subject {
		return security.Claims{}, pgx.ErrNoRows
	}
	resolved, err := resolveUsersRootPermissionsQuery(ctx, tx, []int64{userID})
	if err != nil {
		return security.Claims{}, err
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		return security.Claims{}, pgx.ErrNoRows
	}
	claims.PermissionRules = resolved[userID].Permissions
	if claimsExplicitlyAllow(claims, "account.banned") {
		return security.Claims{}, errWriteAccountBanned
	}
	return claims, nil
}

func requireCurrentWriteActor(w http.ResponseWriter, r *http.Request, tx pgx.Tx) (security.Claims, bool) {
	claims, err := resolveCurrentWriteActorTx(r.Context(), tx, currentClaims(r))
	if err == nil {
		return claims, true
	}
	writeCurrentWriteActorFailure(w, err)
	return security.Claims{}, false
}

func writeCurrentWriteActorFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusUnauthorized, "session is no longer active")
	case errors.Is(err, errWriteAccountBanned):
		writeError(w, http.StatusForbidden, "permission denied")
	default:
		writeError(w, http.StatusServiceUnavailable, "permissions are temporarily unavailable")
	}
}

func canReviewAllContent(claims security.Claims) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.review")
}

func canReviewAllProjects(claims security.Claims) bool {
	return canReviewAllContent(claims) || claimsAllow(claims, "project.review")
}

func canAccessAnyReviewQueue(claims security.Claims) bool {
	if canReviewAllProjects(claims) || claimsAllow(claims, "project.review.*") {
		return true
	}
	for _, rule := range claims.PermissionRules {
		code := strings.ToLower(strings.TrimSpace(rule.Code))
		if strings.HasPrefix(code, "project.review.") && !strings.ContainsAny(code, "<>*[]") && claimsAllow(claims, code) {
			return true
		}
	}
	return false
}

func projectReviewIDs(claims security.Claims) []string {
	projectIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for _, rule := range claims.PermissionRules {
		code := strings.ToLower(strings.TrimSpace(rule.Code))
		if !strings.HasPrefix(code, "project.review.") || strings.ContainsAny(code, "<>*[]") || !claimsAllow(claims, code) {
			continue
		}
		projectID := strings.TrimPrefix(code, "project.review.")
		if projectID == "" {
			continue
		}
		if _, exists := seen[projectID]; exists {
			continue
		}
		seen[projectID] = struct{}{}
		projectIDs = append(projectIDs, projectID)
	}
	sort.Strings(projectIDs)
	return projectIDs
}

// projectReviewDeniedIDs narrows an effective scoped wildcard with the same
// concrete rules evaluated by the decision action. Returning real IDs keeps
// SQL scope arguments distinct from permission syntax. An overridden deny is
// not an exclusion: priority, specificity and expiry remain claimsAllow's job.
func projectReviewDeniedIDs(claims security.Claims) []string {
	denied := make([]string, 0)
	seen := make(map[string]struct{})
	for _, rule := range claims.PermissionRules {
		code := strings.ToLower(strings.TrimSpace(rule.Code))
		if !strings.HasPrefix(code, "project.review.") || strings.ContainsAny(code, "<>*[]") || claimsAllow(claims, code) {
			continue
		}
		id := strings.TrimPrefix(code, "project.review.")
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		denied = append(denied, id)
	}
	sort.Strings(denied)
	return denied
}

// canReviewProjectSubmission deliberately prevents a project-scoped reviewer
// from approving their own submission. Site-wide reviewers retain the
// existing ability to resolve any queue item.
func canReviewProjectSubmission(claims security.Claims, projectID string, submittedBy int64) bool {
	if canReviewAllProjects(claims) {
		return true
	}
	projectID = strings.ToLower(strings.TrimSpace(projectID))
	return projectID != "" && submittedBy > 0 && submittedBy != claims.Subject &&
		claimsAllow(claims, "project.review."+projectID)
}

type pendingReviewVisibility struct {
	includeAll    bool
	includeScoped bool
	reviewerID    int64
}

func projectPendingReviewVisibility(claims security.Claims, projectID string, canEdit bool) pendingReviewVisibility {
	visibility := pendingReviewVisibility{reviewerID: claims.Subject}
	if canEdit || canReviewAllProjects(claims) {
		visibility.includeAll = true
		return visibility
	}
	projectID = strings.ToLower(strings.TrimSpace(projectID))
	visibility.includeScoped = projectID != "" && claimsAllow(claims, "project.review."+projectID)
	return visibility
}

func (visibility pendingReviewVisibility) allows(status string, submittedBy int64) bool {
	if status == "approved" || visibility.includeAll {
		return true
	}
	return visibility.includeScoped && submittedBy > 0 && submittedBy != visibility.reviewerID
}

func canReviewContentSubmission(
	ctx context.Context,
	query databaseQuery,
	claims security.Claims,
	submittedBy int64,
	entityType string,
	entityID int64,
	aggregateType string,
	aggregateKey string,
	snapshotRaw []byte,
	metadataRaw []byte,
) bool {
	if canReviewAllContent(claims) {
		return true
	}
	switch aggregateType {
	case modpackAggregate, simpleProjectAggregate, projectChangelogAggregate, "catalog_resource",
		modContentAggregateVersion, modContentAggregateTemplate, modContentAggregateSection, modContentAggregateResource:
	default:
		return false
	}
	projectID := reviewProjectID(ctx, query, entityType, entityID, aggregateType, aggregateKey, snapshotRaw, metadataRaw)
	return projectID != "" && canReviewProjectSubmission(claims, projectID, submittedBy)
}

func reviewProjectID(
	ctx context.Context,
	query databaseQuery,
	entityType string,
	entityID int64,
	aggregateType string,
	aggregateKey string,
	snapshotRaw []byte,
	metadataRaw []byte,
) string {
	entityType = strings.ToLower(strings.TrimSpace(entityType))
	if projectReviewEntityTypes[entityType] && entityID > 0 {
		var projectID string
		if query.QueryRow(ctx, `select public_id from public_routes where entity_type=$1 and internal_id=$2`, entityType, entityID).Scan(&projectID) == nil {
			return strings.ToLower(strings.TrimSpace(projectID))
		}
	}

	aggregateType = strings.ToLower(strings.TrimSpace(aggregateType))
	switch aggregateType {
	case modContentAggregateVersion, modContentAggregateTemplate, modContentAggregateSection, modContentAggregateResource:
		var snapshot modContentSnapshot
		if json.Unmarshal(snapshotRaw, &snapshot) == nil {
			return strings.ToLower(strings.TrimSpace(snapshot.ModPublicID))
		}
	case modpackAggregate, simpleProjectAggregate:
		return strings.ToLower(strings.TrimSpace(aggregateKey))
	case projectChangelogAggregate:
		var projectID string
		_ = query.QueryRow(ctx, `select target.public_id from project_changelogs entry
			join public_routes target on target.id=entry.object_route_id where entry.public_id=$1`, aggregateKey).Scan(&projectID)
		return strings.ToLower(strings.TrimSpace(projectID))
	case "catalog_resource":
		var snapshot modExportEntryContentSnapshot
		if json.Unmarshal(snapshotRaw, &snapshot) == nil && strings.TrimSpace(snapshot.ModID) != "" {
			return strings.ToLower(strings.TrimSpace(snapshot.ModID))
		}
	}

	var metadata struct {
		ModID  string `json:"modId"`
		SiteID string `json:"siteId"`
	}
	if json.Unmarshal(metadataRaw, &metadata) == nil {
		if projectID := strings.ToLower(strings.TrimSpace(metadata.ModID)); projectID != "" {
			return projectID
		}
		if siteID := strings.ToLower(strings.TrimSpace(metadata.SiteID)); siteID != "" {
			var projectID string
			if query.QueryRow(ctx, `select project_code from mods where slug=$1`, siteID).Scan(&projectID) == nil {
				return strings.ToLower(strings.TrimSpace(projectID))
			}
		}
	}
	return ""
}

var projectReviewEntityTypes = stringSet(
	"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon",
)
