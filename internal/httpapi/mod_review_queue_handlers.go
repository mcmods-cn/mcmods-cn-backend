package httpapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"mcmods-cn-backend/internal/security"
)

type modContentReviewItem struct {
	ID             string    `json:"id"`
	Source         string    `json:"source"`
	Category       string    `json:"category"`
	Operation      string    `json:"operation"`
	AggregateType  string    `json:"aggregateType"`
	ProjectType    string    `json:"projectType,omitempty"`
	ProjectID      string    `json:"projectId,omitempty"`
	ModSiteID      string    `json:"modSiteId"`
	ModName        string    `json:"modName"`
	UserID         *string   `json:"userId,omitempty"`
	Username       string    `json:"username"`
	Title          string    `json:"title"`
	Summary        string    `json:"summary"`
	CreatedAt      time.Time `json:"createdAt"`
	ReviewURL      string    `json:"reviewUrl"`
	ReviewerScope  string    `json:"reviewerScope"`
	SubmittedBy    int64     `json:"-"`
	RequiresGlobal bool      `json:"-"`
}

type reviewQueueFacet struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

const contentReviewQueueQuery = `
	select queue.id,queue.source,queue.category,queue.operation,queue.aggregate_type,
	       queue.project_type,queue.project_id,queue.slug,queue.project_name,coalesce(queue.user_id,0),
	       user_account.public_id,coalesce(user_account.username,''),queue.title,queue.summary,
	       queue.created_at,queue.requires_global
	from (
		select revision.public_id id,'revision'::text source,'mod'::text category,
		       case when request.base_revision_id is null then 'create' else 'edit' end operation,
		       revision.aggregate_type,'mod'::text project_type,mod.project_code project_id,
		       mod.slug,mod.primary_name project_name,request.submitted_by user_id,
		       'Mod revision #' || revision.revision_no title,request.reason summary,revision.created_at,false requires_global
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join mods mod on mod.id=revision.entity_id
		where revision.aggregate_type='mod' and request.status='pending'
		union all
		select revision.public_id,'modpack','modpack',case when request.base_revision_id is null then 'create' else 'edit' end,
		       revision.aggregate_type,'modpack',pack.public_id,pack.slug,pack.primary_name,request.submitted_by,
		       'Modpack revision #' || revision.revision_no,request.reason,revision.created_at,false
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join modpacks pack on pack.id=revision.entity_id
		where revision.aggregate_type='modpack' and request.status='pending'
		union all
		select revision.public_id,project.project_type,project.project_type,
		       case when request.base_revision_id is null then 'create' else 'edit' end,
		       revision.aggregate_type,project.project_type,project.public_id,project.slug,project.primary_name,request.submitted_by,
		       initcap(replace(project.project_type,'_',' ')) || ' revision #' || revision.revision_no,
		       request.reason,revision.created_at,false
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join simple_projects project on project.id=revision.entity_id and project.project_type=revision.entity_type
		where revision.aggregate_type='simple_project' and request.status='pending'
		union all
		select revision.public_id,'mod_content',
		       case coalesce(request.metadata->>'kind','resource')
		         when 'version' then 'mod_content_version'
		         when 'template' then 'mod_content_template'
		         when 'section' then 'mod_content_section'
		         when 'layout' then 'mod_content_layout'
		         else 'mod_content_resource' end,
		       coalesce(nullif(request.metadata->>'operation',''),case when request.base_revision_id is null then 'create' else 'edit' end),
		       revision.aggregate_type,'mod',mod.project_code,mod.slug,mod.primary_name,request.submitted_by,
		       case coalesce(request.metadata->>'kind','resource')
		         when 'version' then 'Mod data version'
		         when 'template' then 'Mod data template'
		         when 'section' then 'Mod data section'
		         when 'layout' then 'Arrange mod data'
		         else 'Mod data entry' end || ': ' || coalesce(request.metadata->>'publicId',revision.aggregate_key),
		       request.reason,revision.created_at,false
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join mods mod on mod.project_code=request.metadata->>'modId'
		where revision.aggregate_type in ('mod_content_version','mod_content_template','mod_content_section','mod_content_resource')
		  and request.status='pending'
		union all
		select revision.public_id,'entry','mod_content_entry','edit',revision.aggregate_type,'mod',mod.project_code,
		       mod.slug,mod.primary_name,request.submitted_by,
		       'Entry introduction: ' || coalesce(request.metadata->>'objectId',revision.aggregate_key),request.reason,revision.created_at,false
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join mods mod on mod.slug=request.metadata->>'siteId'
		where revision.aggregate_type='catalog_resource' and request.status='pending'
		union all
		select revision.public_id,'catalog','global_catalog',coalesce(nullif(request.metadata->>'operation',''),'edit'),
		       revision.aggregate_type,''::text,''::text,''::text,'Global catalog'::text,request.submitted_by,
		       case revision.aggregate_type
		         when 'catalog_editor_resource' then 'Resource: '
		         when 'catalog_editor_tag' then 'Tag: '
		         when 'catalog_editor_recipe_type' then 'Recipe type: '
		         when 'catalog_editor_recipe_template' then 'Recipe template: '
		         else 'Recipe: ' end || revision.aggregate_key,
		       request.reason,revision.created_at,true
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		where revision.aggregate_type in ('catalog_editor_resource','catalog_editor_tag','catalog_editor_recipe_type','catalog_editor_recipe_template','catalog_editor_recipe')
		  and request.status='pending'
		union all
		select revision.public_id,'localization','content_localization',coalesce(nullif(request.metadata->>'operation',''),'edit'),
		       revision.aggregate_type,''::text,''::text,''::text,'Content localization'::text,request.submitted_by,
		       'Localization: ' || revision.aggregate_key,request.reason,revision.created_at,true
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		where revision.aggregate_type='catalog_localization' and request.status='pending'
		union all
		select revision.public_id,'blueprint','blueprint',coalesce(nullif(request.metadata->>'operation',''),case when request.base_revision_id is null then 'create' else 'edit' end),
		       revision.aggregate_type,''::text,''::text,blueprint.public_id,blueprint.title,request.submitted_by,
		       'Blueprint: ' || blueprint.title,
		       coalesce((select string_agg(change.path || ': ' || coalesce(change.before_value::text,'empty') || ' -> ' || coalesce(change.after_value::text,'empty'), E'\n')
		                 from content_change_items change where change.revision_id=revision.id),request.reason),revision.created_at,true
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join blueprints blueprint on blueprint.public_id=revision.aggregate_key
		where revision.aggregate_type='blueprint' and request.status='pending'
		union all
		select revision.public_id,'skin','skin',coalesce(nullif(request.metadata->>'operation',''),case when request.base_revision_id is null then 'create' else 'edit' end),
		       revision.aggregate_type,''::text,''::text,skin.public_id,skin.display_name,request.submitted_by,
		       'Skin: ' || skin.display_name,request.reason,revision.created_at,true
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join skin_assets skin on skin.public_id=revision.aggregate_key
		where revision.aggregate_type='skin' and request.status='pending'
		union all
		select revision.public_id,'creator','creator',case when request.base_revision_id is null then 'create' else 'edit' end,
		       revision.aggregate_type,''::text,''::text,creator.public_id,creator.name,request.submitted_by,
		       case creator.kind when 'team' then 'Team: ' else 'Author: ' end || creator.name,
		       coalesce((select string_agg(change.path || ': ' || coalesce(change.before_value::text,'empty') || ' -> ' || coalesce(change.after_value::text,'empty'), E'\n')
		                 from content_change_items change where change.revision_id=revision.id),request.reason),revision.created_at,true
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join creators creator on creator.public_id=revision.aggregate_key
		where revision.aggregate_type='creator' and request.status='pending'
		union all
		select revision.public_id,post.kind,'community_' || post.kind,
		       case when request.base_revision_id is null then 'create' else 'edit' end,
		       revision.aggregate_type,''::text,''::text,post.public_id,post.title,request.submitted_by,
		       case post.kind when 'tutorial' then 'Tutorial: ' when 'issue' then 'BUG / feature: '
		            when 'news' then 'News: ' else 'Question: ' end || post.title,
		       request.reason,revision.created_at,true
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join community_posts post on post.public_id=revision.aggregate_key
		where revision.aggregate_type='community_post' and request.status='pending'
		union all
		select revision.public_id,'changelog','project_changelog',
		       case when request.base_revision_id is null then 'create' else 'edit' end,
		       revision.aggregate_type,target.entity_type,target.public_id,coalesce(nullif(target.canonical_path,''),entry.public_id),
		       coalesce(mod.primary_name,pack.primary_name,project.primary_name,server.name,target.public_id),request.submitted_by,
		       'Changelog: ' || entry.project_version,request.reason,revision.created_at,false
		from content_revisions revision
		join change_requests request on request.proposed_revision_id=revision.id
		join project_changelogs entry on entry.public_id=revision.aggregate_key
		join public_routes target on target.id=entry.object_route_id
		left join mods mod on target.entity_type='mod' and mod.id=target.internal_id
		left join modpacks pack on target.entity_type='modpack' and pack.id=target.internal_id
		left join simple_projects project on target.entity_type=project.project_type and project.id=target.internal_id
		left join minecraft_servers server on target.entity_type='minecraft_server' and server.id=target.internal_id
		where revision.aggregate_type='project_changelog' and request.status='pending'
		union all
		select export_revision.id,'export','catalog_import','import','catalog_import','mod',mod.project_code,
		       mod.slug,mod.primary_name,export_revision.submitted_by,
		       'mcmods_exporter ' || export_revision.minecraft_version || ' / ' || export_revision.loader,
		       export_revision.source_namespace || ' revision ' || export_revision.revision_no,
		       export_revision.created_at,true
		from catalog_import_revisions export_revision
		join mods mod on mod.id=export_revision.mod_id
		where export_revision.status in ('ready','partial') and not export_revision.is_active
	) queue
	left join users user_account on user_account.id=queue.user_id
	where ($2::boolean and queue.requires_global)
	   or (not queue.requires_global and ($3::boolean or (
	       queue.project_id=any($1::text[]) and coalesce(queue.user_id,0)>0 and coalesce(queue.user_id,0)<>$4::bigint
	   )))
	order by queue.created_at asc
	limit 2000`

func (s *Server) adminModContentReviews(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if !canAccessAnyReviewQueue(claims) {
		writeError(w, http.StatusForbidden, "permission denied")
		return
	}
	rows, err := s.db.Query(
		r.Context(), contentReviewQueueQuery, projectReviewIDs(claims), canReviewAllContent(claims), canReviewAllProjects(claims), claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pending reviews")
		return
	}
	defer rows.Close()
	accessible := make([]modContentReviewItem, 0)
	for rows.Next() {
		var item modContentReviewItem
		if err = rows.Scan(
			&item.ID, &item.Source, &item.Category, &item.Operation, &item.AggregateType,
			&item.ProjectType, &item.ProjectID, &item.ModSiteID, &item.ModName, &item.SubmittedBy,
			&item.UserID, &item.Username, &item.Title, &item.Summary, &item.CreatedAt, &item.RequiresGlobal,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode pending reviews")
			return
		}
		if !reviewQueueItemAllowed(claims, item) {
			continue
		}
		item.ReviewerScope = "project"
		if item.RequiresGlobal || canReviewAllProjects(claims) {
			item.ReviewerScope = "global"
		}
		item.ReviewURL = contentReviewURL(item)
		accessible = append(accessible, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load pending reviews")
		return
	}

	facets := map[string][]reviewQueueFacet{
		"categories":   reviewQueueFacets(accessible, func(item modContentReviewItem) string { return item.Category }),
		"operations":   reviewQueueFacets(accessible, func(item modContentReviewItem) string { return item.Operation }),
		"projectTypes": reviewQueueFacets(accessible, func(item modContentReviewItem) string { return item.ProjectType }),
	}
	filtered := filterReviewQueue(accessible, r)
	total := len(filtered)
	limit := boundedReviewQueueInt(r.URL.Query().Get("limit"), 50, 1, 100)
	offset := boundedReviewQueueInt(r.URL.Query().Get("offset"), 0, 0, max(total, 0))
	if offset > total {
		offset = total
	}
	end := min(total, offset+limit)
	writeJSON(w, http.StatusOK, map[string]any{"items": filtered[offset:end], "total": total, "facets": facets})
}

func reviewQueueItemAllowed(claims security.Claims, item modContentReviewItem) bool {
	if item.RequiresGlobal {
		return canReviewAllContent(claims)
	}
	return canReviewProjectSubmission(claims, item.ProjectID, item.SubmittedBy)
}

func contentReviewURL(item modContentReviewItem) string {
	switch item.Source {
	case "revision":
		return "/api/v1/mods/" + item.ModSiteID + "/revisions/" + item.ID
	case "export":
		return "/api/v1/admin/export-revisions/" + item.ID + "/activate"
	default:
		return "/api/v1/content-revisions/" + item.ID
	}
}

func filterReviewQueue(items []modContentReviewItem, r *http.Request) []modContentReviewItem {
	category := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("category")))
	operation := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("operation")))
	projectType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("projectType")))
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len(query) > 100 {
		query = query[:100]
	}
	filtered := make([]modContentReviewItem, 0, len(items))
	for _, item := range items {
		if category != "" && item.Category != category || operation != "" && item.Operation != operation || projectType != "" && item.ProjectType != projectType {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(item.ModName+" "+item.Title+" "+item.Username+" "+item.ProjectID), query) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func reviewQueueFacets(items []modContentReviewItem, value func(modContentReviewItem) string) []reviewQueueFacet {
	counts := make(map[string]int)
	for _, item := range items {
		if key := strings.TrimSpace(value(item)); key != "" {
			counts[key]++
		}
	}
	result := make([]reviewQueueFacet, 0, len(counts))
	for key, count := range counts {
		result = append(result, reviewQueueFacet{Value: key, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Value < result[j].Value })
	return result
}

func boundedReviewQueueInt(raw string, fallback, minimum, maximum int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return fallback
	}
	return min(maximum, max(minimum, value))
}
