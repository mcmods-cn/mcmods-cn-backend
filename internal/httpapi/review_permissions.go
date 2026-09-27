package httpapi

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"mcmods-cn-backend/internal/security"
)

func canReviewAllContent(claims security.Claims) bool {
	return claimsAllow(claims, "admin.*") || claimsAllow(claims, "content.review")
}

func canReviewAllProjects(claims security.Claims) bool {
	return canReviewAllContent(claims) || claimsAllow(claims, "project.review")
}

func canAccessAnyReviewQueue(claims security.Claims) bool {
	if canReviewAllProjects(claims) {
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
	for _, allowedProjectID := range projectReviewIDs(claims) {
		if allowedProjectID == projectID {
			visibility.includeScoped = true
			break
		}
	}
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
	projectID := reviewProjectID(ctx, query, entityType, entityID, aggregateType, aggregateKey, snapshotRaw, metadataRaw)
	return canReviewProjectSubmission(claims, projectID, submittedBy)
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
