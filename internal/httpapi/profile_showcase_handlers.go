package httpapi

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"strings"
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

type userContributionActivity struct {
	ID         string    `json:"id"`
	EntityType string    `json:"entityType"`
	Action     string    `json:"action"`
	Name       string    `json:"name"`
	Href       string    `json:"href"`
	OccurredAt time.Time `json:"occurredAt"`
}

type userContributionsPayload struct {
	Year                    int                        `json:"year"`
	From                    string                     `json:"from"`
	To                      string                     `json:"to"`
	Total                   int                        `json:"total"`
	Days                    []userContributionDay      `json:"days"`
	Years                   []int                      `json:"years"`
	RecentActivity          []userContributionActivity `json:"recentActivity"`
	RecentActivityTruncated bool                       `json:"recentActivityTruncated"`
}

func userContributionRecentActivityQuery() string {
	return `select request.public_id,route.entity_type,
		case when request.base_revision_id is null then 'created' else 'edited' end,
		coalesce(` + followProjectTargetNameSQL("route") + `,route.public_id),
		route.canonical_path,coalesce(request.resolved_at,request.submitted_at)
		from change_requests request
		join public_routes route on route.entity_type=request.entity_type and route.internal_id=request.entity_id
		where request.submitted_by=$1 and request.status='approved' and request.entity_type is not null
		  and coalesce(request.resolved_at,request.submitted_at)>=$2
		  and ` + publicContributionTargetVisibilitySQL("route") + `
		order by coalesce(request.resolved_at,request.submitted_at) desc,request.id desc
		limit 101`
}

func publicContributionTargetVisibilitySQL(route string) string {
	return strings.Replace(
		followProjectTargetVisibilitySQL(route, "0", "false"),
		"visibility in ('public','unlisted')",
		"visibility='public'",
		1,
	)
}

const userShowcaseProjectsQuery = `with qualified_access as materialized (
	select access.project_type,access.project_id,
		bool_or(access.access_level='developer') is_developer,
		bool_or(access.access_level='editor') is_editor
	from effective_project_access access
	where access.user_id=$1 and access.access_level in ('developer','editor')
	group by access.project_type,access.project_id
), projects as (
	select access.project_id id,access.project_type entity_type,mod.primary_name name,mod.summary,mod.icon_url,mod.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join mods mod
		on access.project_type='mod' and mod.id=access.project_id
	where mod.review_status='approved'
	union all
	select access.project_id,access.project_type,pack.primary_name,pack.summary,pack.icon_url,pack.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join modpacks pack
		on access.project_type='modpack' and pack.id=access.project_id
	where pack.review_status='approved'
	union all
	select access.project_id,access.project_type,project.primary_name,project.summary,project.icon_url,project.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join simple_projects project
		on project.project_type=access.project_type and project.id=access.project_id
	where project.review_status='approved'
	union all
	select access.project_id,access.project_type,server.name,server.body_markdown,'',server.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join minecraft_servers server
		on access.project_type='minecraft_server' and server.id=access.project_id
	where server.review_status='approved'
	union all
	select access.project_id,access.project_type,blueprint.title,blueprint.description_markdown,blueprint.cover_object_key,blueprint.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join blueprints blueprint
		on access.project_type='blueprint' and blueprint.id=access.project_id
	where blueprint.status in ('ready','partial') and blueprint.review_status in ('not_required','approved')
	union all
	select access.project_id,access.project_type,asset.display_name,asset.description,blob.object_key,asset.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join skin_assets asset
		on access.project_type='skin' and asset.id=access.project_id
	join skin_texture_blobs blob on blob.hash=asset.blob_hash
	where asset.status='active' and asset.visibility='public' and asset.review_status='approved'
	union all
	select access.project_id,access.project_type,post.title,post.body_markdown,'',post.updated_at,
		access.is_developer,access.is_editor
	from qualified_access access join community_posts post
		on access.project_type='community_post' and post.id=access.project_id
	where post.status='active' and post.review_status='approved'
)
select project.entity_type,route.public_id,project.name,project.summary,project.icon_url,
	route.canonical_path,project.is_developer,project.is_editor,project.updated_at
from projects project
join public_routes route on route.entity_type=project.entity_type and route.internal_id=project.id
order by project.updated_at desc,project.id desc
limit 100`

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
	claimedAuthors, err := s.loadUserClaimedAuthors(r.Context(), identity.InternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load claimed authors")
		return
	}
	developerProjects := make([]userShowcaseItem, 0, len(projects))
	editorProjects := make([]userShowcaseItem, 0, len(projects))
	for _, project := range projects {
		isDeveloper := false
		isEditor := false
		for _, role := range project.Roles {
			switch role {
			case "developer":
				isDeveloper = true
			case "editor":
				isEditor = true
			}
		}
		if isDeveloper {
			project.Roles = []string{"developer"}
			developerProjects = append(developerProjects, project)
		} else if isEditor {
			project.Roles = []string{"editor"}
			editorProjects = append(editorProjects, project)
		}
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
	contributions, err := s.loadUserContributions(r.Context(), identity.InternalID, time.Now().UTC().Year())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user contributions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"claimedAuthors":    claimedAuthors,
		"developerProjects": developerProjects,
		"editorProjects":    editorProjects,
		"uploads":           uploads,
		"posts":             posts,
		"contributions":     contributions,
	})
}

func (s *Server) loadUserClaimedAuthors(ctx context.Context, userID int64) ([]userShowcaseItem, error) {
	rows, err := s.db.Query(ctx, `select author.public_id,author.name,author.description_markdown,
		author.avatar_url,route.canonical_path,author.updated_at
		from creator_claims claim
		join creators author on author.id=claim.creator_id and author.kind='author' and author.review_status='approved'
		join public_routes route on route.entity_type='author' and route.internal_id=author.id
		where claim.user_id=$1 and claim.status='approved'
		order by author.updated_at desc,author.id desc`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userShowcaseItem, 0)
	for rows.Next() {
		var item userShowcaseItem
		item.EntityType = "author"
		if err = rows.Scan(&item.PublicID, &item.Name, &item.Summary, &item.IconURL, &item.Href, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Roles = []string{"claimed_author"}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	ossCfg := s.ossConfigFromSettings(ctx)
	urls := make([]string, len(items))
	for i := range items {
		urls[i] = items[i].IconURL
	}
	urls, err = s.resolveStoredOSSImageURLsWithConfig(ctx, ossCfg, urls)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].IconURL = urls[i]
	}
	return items, nil
}

func (s *Server) userContributions(w http.ResponseWriter, r *http.Request) {
	identity, ok := s.pathUserIdentity(w, r)
	if !ok {
		return
	}
	year, ok := requestedContributionYear(r, time.Now().UTC().Year())
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid contribution year")
		return
	}
	contributions, err := s.loadUserContributions(r.Context(), identity.InternalID, year)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user contributions")
		return
	}
	writeJSON(w, http.StatusOK, contributions)
}

func (s *Server) loadUserShowcaseProjects(ctx context.Context, userID int64) ([]userShowcaseItem, error) {
	rows, err := s.db.Query(ctx, userShowcaseProjectsQuery, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]userShowcaseItem, 0)
	for rows.Next() {
		var item userShowcaseItem
		var isDeveloper, isEditor bool
		if err = rows.Scan(&item.EntityType, &item.PublicID, &item.Name, &item.Summary, &item.IconURL,
			&item.Href, &isDeveloper, &isEditor, &item.UpdatedAt); err != nil {
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
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	ossCfg := s.ossConfigFromSettings(ctx)
	urls := make([]string, len(items))
	for i := range items {
		urls[i] = items[i].IconURL
	}
	urls, err = s.resolveStoredOSSImageURLsWithConfig(ctx, ossCfg, urls)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].IconURL = urls[i]
	}
	return items, nil
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
	for rows.Next() {
		var item userShowcaseItem
		if err = rows.Scan(&item.EntityType, &item.PublicID, &item.Name, &item.Summary, &item.IconURL, &item.Href, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.Roles = []string{"uploader"}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	ossCfg := s.ossConfigFromSettings(ctx)
	urls := make([]string, len(items))
	for i := range items {
		urls[i] = items[i].IconURL
	}
	urls, err = s.resolveStoredOSSImageURLsWithConfig(ctx, ossCfg, urls)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].IconURL = urls[i]
	}
	return items, nil
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

func (s *Server) loadUserContributions(ctx context.Context, userID int64, year int) (userContributionsPayload, error) {
	now := time.Now().UTC()
	from, to := contributionDateRange(year, now)
	result := userContributionsPayload{
		Year: year, From: from.Format("2006-01-02"), To: to.Format("2006-01-02"),
		Days: make([]userContributionDay, 0), Years: make([]int, 0), RecentActivity: make([]userContributionActivity, 0),
	}
	rows, err := s.db.Query(ctx, `select contribution_date::text,contribution_count
		from user_daily_contributions
		where user_id=$1 and contribution_date between $2::date and $3::date
		order by contribution_date`, userID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return userContributionsPayload{}, err
	}
	for rows.Next() {
		var item userContributionDay
		if err = rows.Scan(&item.Date, &item.Count); err != nil {
			rows.Close()
			return userContributionsPayload{}, err
		}
		result.Total += item.Count
		result.Days = append(result.Days, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return userContributionsPayload{}, err
	}
	rows.Close()

	yearRows, err := s.db.Query(ctx, `select distinct extract(year from contribution_date)::integer
		from user_daily_contributions where user_id=$1 order by 1 desc`, userID)
	if err != nil {
		return userContributionsPayload{}, err
	}
	for yearRows.Next() {
		var availableYear int
		if err = yearRows.Scan(&availableYear); err != nil {
			yearRows.Close()
			return userContributionsPayload{}, err
		}
		result.Years = appendContributionYear(result.Years, availableYear)
	}
	if err = yearRows.Err(); err != nil {
		yearRows.Close()
		return userContributionsPayload{}, err
	}
	yearRows.Close()
	result.Years = appendContributionYear(result.Years, now.Year())
	result.Years = appendContributionYear(result.Years, year)
	sort.Sort(sort.Reverse(sort.IntSlice(result.Years)))

	activityRows, err := s.db.Query(ctx, userContributionRecentActivityQuery(), userID, now.AddDate(0, -1, 0))
	if err != nil {
		return userContributionsPayload{}, err
	}
	for activityRows.Next() {
		var item userContributionActivity
		if err = activityRows.Scan(&item.ID, &item.EntityType, &item.Action, &item.Name, &item.Href, &item.OccurredAt); err != nil {
			activityRows.Close()
			return userContributionsPayload{}, err
		}
		if len(result.RecentActivity) < 100 {
			result.RecentActivity = append(result.RecentActivity, item)
		} else {
			result.RecentActivityTruncated = true
		}
	}
	if err = activityRows.Err(); err != nil {
		activityRows.Close()
		return userContributionsPayload{}, err
	}
	activityRows.Close()
	return result, nil
}

func requestedContributionYear(r *http.Request, currentYear int) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("year"))
	if raw == "" {
		return currentYear, true
	}
	year, err := strconv.Atoi(raw)
	return year, err == nil && year >= 1970 && year <= currentYear
}

func contributionDateRange(year int, now time.Time) (time.Time, time.Time) {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if year == today.Year() {
		return today.AddDate(-1, 0, 1), today
	}
	return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC), time.Date(year, time.December, 31, 0, 0, 0, 0, time.UTC)
}

func appendContributionYear(years []int, year int) []int {
	for _, existing := range years {
		if existing == year {
			return years
		}
	}
	return append(years, year)
}
