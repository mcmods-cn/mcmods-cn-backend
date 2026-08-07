package httpapi

import (
	"context"
	"net/http"
	"time"
)

type userShowcaseItem struct {
	EntityType string    `json:"entityType"`
	PublicID   string    `json:"publicId"`
	Name       string    `json:"name"`
	Summary    string    `json:"summary"`
	IconURL    string    `json:"iconUrl"`
	Href       string    `json:"href"`
	Roles      []string  `json:"roles"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type userContributionDay struct {
	Date  string `json:"date"`
	Count int    `json:"count"`
}

func (s *Server) userShowcase(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	projects, err := s.loadUserShowcaseProjects(r.Context(), identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user projects")
		return
	}
	uploads, err := s.loadUserShowcaseUploads(r.Context(), identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user uploads")
		return
	}
	posts, err := s.loadUserShowcasePosts(r.Context(), identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user posts")
		return
	}
	contributions, from, to, total, err := s.loadUserContributions(r.Context(), identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user contributions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"projects": projects,
		"uploads":  uploads,
		"posts":    posts,
		"contributions": map[string]any{
			"from": from, "to": to, "total": total, "days": contributions,
		},
	})
}

func (s *Server) loadUserShowcaseProjects(ctx context.Context, userID int64) ([]userShowcaseItem, error) {
	rows, err := s.db.Query(ctx, `with projects as (
		select mod.id,'mod'::text entity_type,mod.primary_name name,mod.summary,mod.icon_url,mod.updated_at
		from mods mod where mod.review_status='approved'
		union all
		select pack.id,'modpack',pack.primary_name,pack.summary,pack.icon_url,pack.updated_at
		from modpacks pack where pack.review_status='approved'
		union all
		select project.id,project.project_type,project.primary_name,project.summary,project.icon_url,project.updated_at
		from simple_projects project where project.review_status='approved'
	), qualified as (
		select project.*,
			exists(
				select 1 from content_creator_bindings binding
				join creators creator on creator.id=binding.creator_id
				where binding.subject_type=project.entity_type and binding.subject_id=project.id
				  and creator.review_status='approved' and creator.claimed_by=$1
			) or exists(
				select 1 from content_creator_bindings binding
				join creator_team_members membership on membership.team_id=binding.creator_id
				join creators member on member.id=membership.member_creator_id
				where binding.subject_type=project.entity_type and binding.subject_id=project.id
				  and member.review_status='approved' and member.claimed_by=$1
			) is_developer,
			exists(
				select 1 from change_requests request
				where request.entity_type=project.entity_type and request.entity_id=project.id
				  and request.submitted_by=$1 and request.status='approved'
			) is_editor
		from projects project
	)
	select qualified.entity_type,route.public_id,qualified.name,qualified.summary,qualified.icon_url,
		route.canonical_path,qualified.is_developer,qualified.is_editor,qualified.updated_at
	from qualified
	join public_routes route on route.entity_type=qualified.entity_type and route.internal_id=qualified.id
	where qualified.is_developer or qualified.is_editor
	order by qualified.updated_at desc,qualified.id desc
	limit 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userShowcaseItem, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var item userShowcaseItem
		var isDeveloper, isEditor bool
		if err = rows.Scan(&item.EntityType, &item.PublicID, &item.Name, &item.Summary, &item.IconURL,
			&item.Href, &isDeveloper, &isEditor, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, item.IconURL)
		if err != nil {
			return nil, err
		}
		item.Roles = make([]string, 0, 2)
		if isDeveloper {
			item.Roles = append(item.Roles, "developer")
		}
		if isEditor {
			item.Roles = append(item.Roles, "editor")
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadUserShowcaseUploads(ctx context.Context, userID int64) ([]userShowcaseItem, error) {
	rows, err := s.db.Query(ctx, `select upload.entity_type,route.public_id,upload.name,upload.summary,upload.icon_url,route.canonical_path,upload.updated_at
		from (
			select blueprint.id,'blueprint'::text entity_type,blueprint.title name,
				blueprint.description_markdown summary,blueprint.cover_object_key icon_url,blueprint.updated_at
			from blueprints blueprint
			where blueprint.owner_id=$1 and blueprint.status in ('ready','partial')
			  and blueprint.review_status in ('not_required','approved')
			union all
			select asset.id,'skin',asset.display_name,asset.description,blob.object_key,asset.updated_at
			from skin_assets asset join skin_texture_blobs blob on blob.hash=asset.blob_hash
			where asset.owner_id=$1 and asset.status='active' and asset.review_status='approved'
			  and asset.visibility='public'
		) upload
		join public_routes route on route.entity_type=upload.entity_type and route.internal_id=upload.id
		order by upload.updated_at desc
		limit 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userShowcaseItem, 0)
	ossCfg := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var item userShowcaseItem
		if err = rows.Scan(&item.EntityType, &item.PublicID, &item.Name, &item.Summary, &item.IconURL, &item.Href, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.IconURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, ossCfg, item.IconURL)
		if err != nil {
			return nil, err
		}
		item.Roles = []string{"uploader"}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadUserShowcasePosts(ctx context.Context, userID int64) ([]userShowcaseItem, error) {
	rows, err := s.db.Query(ctx, `select post.kind,route.public_id,post.title,route.canonical_path,post.updated_at
		from community_posts post
		join public_routes route on route.entity_type='community_post' and route.internal_id=post.id
		where post.author_id=$1 and post.status='active' and post.review_status='approved'
		order by post.published_at desc nulls last,post.updated_at desc,post.id desc
		limit 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userShowcaseItem, 0)
	for rows.Next() {
		var item userShowcaseItem
		if err = rows.Scan(&item.EntityType, &item.PublicID, &item.Name, &item.Href, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Roles = []string{"author"}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadUserContributions(ctx context.Context, userID int64) ([]userContributionDay, string, string, int, error) {
	now := time.Now().UTC()
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := to.AddDate(-1, 0, 1)
	rows, err := s.db.Query(ctx, `select contribution_date::text,contribution_count
		from user_daily_contributions
		where user_id=$1 and contribution_date between $2::date and $3::date
		order by contribution_date`, userID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, "", "", 0, err
	}
	defer rows.Close()
	days := make([]userContributionDay, 0)
	total := 0
	for rows.Next() {
		var item userContributionDay
		if err = rows.Scan(&item.Date, &item.Count); err != nil {
			return nil, "", "", 0, err
		}
		total += item.Count
		days = append(days, item)
	}
	if err = rows.Err(); err != nil {
		return nil, "", "", 0, err
	}
	return days, from.Format("2006-01-02"), to.Format("2006-01-02"), total, nil
}
