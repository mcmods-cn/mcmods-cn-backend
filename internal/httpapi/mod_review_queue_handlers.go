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
			where revision.aggregate_type='catalog_resource' and request.status='pending'
			union all
			select revision.id::text,'catalog'::text,''::text,'Global catalog'::text,request.submitted_by,
			       case revision.aggregate_type when 'catalog_tag' then 'Tag: ' when 'catalog_recipe_type' then 'Recipe type: ' else 'Recipe: ' end || revision.aggregate_key,
			       request.reason,revision.created_at
			from content_revisions revision
			join change_requests request on request.proposed_revision_id=revision.id
			where revision.aggregate_type in ('catalog_tag','catalog_recipe_type','catalog_recipe') and request.status='pending'
			union all
			select revision.id::text,'blueprint'::text,blueprint.public_id,blueprint.title,request.submitted_by,
			       'Blueprint: ' || blueprint.title,
			       coalesce((select string_agg(change.path || ': ' || coalesce(change.before_value::text,'∅') || ' → ' || coalesce(change.after_value::text,'∅'), E'\n')
			                 from content_change_items change where change.revision_id=revision.id),request.reason),revision.created_at
			from content_revisions revision
			join change_requests request on request.proposed_revision_id=revision.id
			join blueprints blueprint on blueprint.public_id=revision.aggregate_key
			where revision.aggregate_type='blueprint' and request.status='pending'
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
		} else if item.Source == "entry" || item.Source == "catalog" || item.Source == "blueprint" {
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
