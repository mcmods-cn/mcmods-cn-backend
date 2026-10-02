package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type contentHistoryQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type contentHistoryItem struct {
	ID              string    `json:"id"`
	Version         int64     `json:"version"`
	Status          string    `json:"status"`
	Origin          string    `json:"origin"`
	Source          string    `json:"source"`
	SourceNamespace string    `json:"sourceNamespace,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	SubmittedByID   string    `json:"submittedById,omitempty"`
	SubmittedByName string    `json:"submittedByName"`
	CreatedAt       time.Time `json:"createdAt"`
	Current         bool      `json:"current"`
}

func (s *Server) communityPostHistory(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	pageRequest, err := parseContentHistoryPageRequest(r.URL.Query(), contentHistoryScope("community", publicID))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	claims := currentClaims(r)
	moderator := claimsAllow(claims, "content.review") || claimsAllow(claims, "community.edit") || claimsAllow(claims, "admin.*")
	var authorID int64
	var publishedRevisionID *int64
	if err := s.db.QueryRow(r.Context(), `select author_id,published_revision_id from community_posts
		where public_id=$1 and status='active' and (review_status='approved' or author_id=$2 or $3)`,
		publicID, claims.Subject, moderator).Scan(&authorID, &publishedRevisionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "community post not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load community post history")
		}
		return
	}
	includeUnpublished := moderator || claims.Subject == authorID
	items, err := contentRevisionHistoryPage(r.Context(), s.db, communityPostAggregate, publicID, publishedRevisionID,
		pendingReviewVisibility{includeAll: includeUnpublished}, pageRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community post history")
		return
	}
	writeJSON(w, http.StatusOK, mergeContentHistoryPage(items, nil, pageRequest))
}

func (s *Server) modContentResourceHistory(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	siteID := normalizeModSiteID(r.PathValue("siteId"))
	resourcePublicID := strings.ToLower(strings.TrimSpace(r.PathValue("resourceId")))
	versionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("version")))
	pageRequest, err := parseContentHistoryPageRequest(r.URL.Query(),
		contentHistoryScope("mod-resource", siteID, resourcePublicID, versionPublicID), "version")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	identity, err := s.modIdentity(r.Context(), siteID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "mod not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load resource history")
		return
	}
	var resourceID, versionID int64
	var publishedRevisionID *int64
	err = s.db.QueryRow(r.Context(), `select entity.id,version.id,detail.published_revision_id
		from catalog_entities entity
		join mod_resource_bindings binding on binding.resource_id=entity.id and binding.mod_id=$1
		join mod_content_versions version on version.mod_id=$1 and version.public_id=$3 and version.status='active'
		left join mod_resource_version_details detail on detail.resource_id=entity.id and detail.version_id=version.id
		where entity.public_id=$2 and entity.entity_type='resource' and entity.status='active'`,
		identity.ID, resourcePublicID, versionPublicID).Scan(&resourceID, &versionID, &publishedRevisionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "resource version not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load resource history")
		}
		return
	}
	visibility := projectPendingReviewVisibility(claims, identity.UniqueID, canEditMod(claims, identity))
	items, err := contentRevisionHistoryPage(r.Context(), s.db, modContentAggregateResource,
		resourcePublicID+":"+versionPublicID, publishedRevisionID, visibility, pageRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load resource history")
		return
	}
	imports, err := importedResourceHistoryPage(r.Context(), s.db, resourceID, versionID, visibility.includeAll, pageRequest)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load imported resource history")
		return
	}
	writeJSON(w, http.StatusOK, mergeContentHistoryPage(items, imports, pageRequest))
}

func contentRevisionHistoryPage(
	ctx context.Context,
	query contentHistoryQuery,
	aggregateType string,
	aggregateKey string,
	publishedRevisionID *int64,
	visibility pendingReviewVisibility,
	request contentHistoryPageRequest,
) ([]contentHistoryItem, error) {
	statement, arguments := contentRevisionHistoryPageSQL(aggregateType, aggregateKey, publishedRevisionID, visibility, request)
	rows, err := query.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentHistoryItem, 0)
	for rows.Next() {
		var item contentHistoryItem
		item.Origin = "manual"
		if err = rows.Scan(&item.ID, &item.Version, &item.Status, &item.Source, &item.Reason,
			&item.SubmittedByID, &item.SubmittedByName, &item.CreatedAt, &item.Current); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func importedResourceHistoryPage(ctx context.Context, query contentHistoryQuery, resourceID, versionID int64, includeUnpublished bool, request contentHistoryPageRequest) ([]contentHistoryItem, error) {
	statement, arguments := importedResourceHistoryPageSQL(resourceID, versionID, includeUnpublished, request)
	rows, err := query.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentHistoryItem, 0)
	for rows.Next() {
		var item contentHistoryItem
		item.Origin = "import"
		if err = rows.Scan(&item.ID, &item.Version, &item.Status, &item.Source, &item.SourceNamespace,
			&item.SubmittedByID, &item.SubmittedByName, &item.CreatedAt, &item.Current); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
