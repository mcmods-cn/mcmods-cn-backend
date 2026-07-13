package httpapi

import (
	"net/http"
	"time"
)

type modContentReviewItem struct {
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	ModSiteID   string    `json:"modSiteId"`
	ModName     string    `json:"modName"`
	UserID      *int64    `json:"userId,omitempty"`
	Username    string    `json:"username"`
	DisplayName string    `json:"displayName"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	CreatedAt   time.Time `json:"createdAt"`
	ReviewURL   string    `json:"reviewUrl"`
}

func (s *Server) adminModContentReviews(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		select queue.id,queue.source,queue.slug,queue.mod_name,queue.user_id,
		       coalesce(user_account.username,''),coalesce(user_account.display_name,''),
		       queue.title,queue.summary,queue.created_at
		from (
			select revision.id::text id,'revision'::text source,mod.slug,mod.primary_name mod_name,
			       request.submitted_by user_id,'Mod revision #' || revision.revision_no title,
			       request.reason summary,revision.created_at
			from content_revisions revision
			join change_requests request on request.proposed_revision_id=revision.id
			join mods mod on mod.id=revision.aggregate_key::bigint
			where revision.aggregate_type='mod' and request.status='pending'
			union all
			select revision.id::text,'entry'::text,mod.slug,mod.primary_name,request.submitted_by,
			       'Entry introduction: ' || (request.metadata->>'objectId'),request.reason,revision.created_at
			from content_revisions revision
			join change_requests request on request.proposed_revision_id=revision.id
			join mods mod on mod.id=(request.metadata->>'modId')::bigint
			where revision.aggregate_type='mod_export_entry' and request.status='pending'
			union all
			select revision.id::text,'catalog'::text,''::text,'Global catalog'::text,request.submitted_by,
			       case revision.aggregate_type when 'global_tag' then 'Tag: ' when 'global_recipe_type' then 'Recipe type: ' else 'Recipe: ' end || replace(revision.aggregate_key, chr(10), ' / '),
			       request.reason,revision.created_at
			from content_revisions revision
			join change_requests request on request.proposed_revision_id=revision.id
			where revision.aggregate_type in ('global_tag','global_recipe_type','global_recipe') and request.status='pending'
			union all
			select export_revision.id,'export'::text,mod.slug,mod.primary_name,job.created_by,
			       'mcmods_exporter ' || export_revision.minecraft_version || ' / ' || export_revision.loader,
			       export_revision.source_namespace || ' revision ' || export_revision.revision_no,
			       export_revision.created_at
			from mod_export_revisions export_revision
			join mods mod on mod.id=export_revision.mod_id
			left join mod_export_jobs job on job.mod_id=export_revision.mod_id and job.package_id=export_revision.package_id
			where export_revision.status in ('ready','partial') and not export_revision.is_active
		) queue
		left join users user_account on user_account.id=queue.user_id
		order by queue.created_at asc`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pending reviews")
		return
	}
	defer rows.Close()
	items := make([]modContentReviewItem, 0)
	for rows.Next() {
		var item modContentReviewItem
		if err = rows.Scan(
			&item.ID, &item.Source, &item.ModSiteID, &item.ModName, &item.UserID,
			&item.Username, &item.DisplayName, &item.Title, &item.Summary, &item.CreatedAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode pending reviews")
			return
		}
		if item.Source == "revision" {
			item.ReviewURL = "/api/v1/mods/" + item.ModSiteID + "/revisions/" + item.ID
		} else if item.Source == "entry" || item.Source == "catalog" {
			item.ReviewURL = "/api/v1/content-revisions/" + item.ID
		} else {
			item.ReviewURL = "/api/v1/admin/export-revisions/" + item.ID + "/activate"
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pending reviews")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
