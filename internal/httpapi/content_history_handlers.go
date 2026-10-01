package httpapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
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
	items, err := contentRevisionHistory(r.Context(), s.db, communityPostAggregate, publicID, publishedRevisionID, includeUnpublished)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load community post history")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) modContentResourceHistory(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	identity, ok := s.requireReadableMod(w, r)
	if !ok {
		return
	}
	resourcePublicID := strings.ToLower(strings.TrimSpace(r.PathValue("resourceId")))
	versionPublicID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("version")))
	var resourceID, versionID int64
	var publishedRevisionID *int64
	err := s.db.QueryRow(r.Context(), `select entity.id,version.id,detail.published_revision_id
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
	includeUnpublished := canEditMod(claims, identity) || claimsAllow(claims, "content.review") || claimsAllow(claims, "project.review") || claimsAllow(claims, "admin.*")
	items, err := contentRevisionHistory(r.Context(), s.db, modContentAggregateResource,
		resourcePublicID+":"+versionPublicID, publishedRevisionID, includeUnpublished)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load resource history")
		return
	}
	imports, err := importedResourceHistory(r.Context(), s.db, resourceID, versionID, includeUnpublished)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load imported resource history")
		return
	}
	items = append(items, imports...)
	sort.SliceStable(items, func(left, right int) bool { return items[left].CreatedAt.After(items[right].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func contentRevisionHistory(ctx context.Context, query contentHistoryQuery, aggregateType, aggregateKey string, publishedRevisionID *int64, includeUnpublished bool) ([]contentHistoryItem, error) {
	rows, err := query.Query(ctx, `select revision.public_id,revision.revision_no,request.status,revision.source,request.reason,
		coalesce(account.public_id,''),coalesce(nullif(request.submitted_by_snapshot,''),nullif(revision.created_by_snapshot,''),account.username,'system'),
		revision.created_at,coalesce(revision.id=$3::bigint,false)
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		left join users account on account.id=coalesce(request.submitted_by,revision.created_by)
		where revision.aggregate_type=$1 and revision.aggregate_key=$2 and ($4 or request.status='approved')
		order by revision.revision_no desc`, aggregateType, aggregateKey, publishedRevisionID, includeUnpublished)
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

func importedResourceHistory(ctx context.Context, query contentHistoryQuery, resourceID, versionID int64, includeUnpublished bool) ([]contentHistoryItem, error) {
	rows, err := query.Query(ctx, `select revision.id,revision.revision_no,revision.status,revision.source_kind,revision.source_namespace,
		coalesce(account.public_id,''),coalesce(nullif(revision.submitted_by_snapshot,''),account.username,'system'),
		revision.created_at,revision.is_active
		from resource_import_snapshots snapshot
		join catalog_import_revisions revision on revision.id=snapshot.revision_id
		left join users account on account.id=revision.submitted_by
		where snapshot.resource_id=$1 and revision.target_version_id=$2
		  and ($3 or revision.status in ('ready','partial','superseded'))
		order by revision.created_at desc`, resourceID, versionID, includeUnpublished)
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
