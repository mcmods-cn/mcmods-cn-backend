package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type metricTarget struct {
	RouteID    int64
	InternalID int64
	PublicID   string
	Type       string
}

type contentMetricActor struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	AvatarURL  string    `json:"avatarUrl,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
	Count      int64     `json:"count,omitempty"`
	URL        string    `json:"url"`
}

type contentMetricDeveloper struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Role      string `json:"role,omitempty"`
	URL       string `json:"url"`
}

type contentMetricEditor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatarUrl,omitempty"`
	Role      string `json:"role"`
	URL       string `json:"url"`
}

type contentMetricReference struct {
	ID          string    `json:"id"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"publishedAt"`
}

type contentMetricsResponse struct {
	ID               string                   `json:"id"`
	Type             string                   `json:"type"`
	CreatedAt        time.Time                `json:"createdAt"`
	LastEditedAt     *time.Time               `json:"lastEditedAt"`
	EditCount        int64                    `json:"editCount"`
	DirectViews      int64                    `json:"directViews"`
	ChildViews       int64                    `json:"childViews"`
	TotalViews       int64                    `json:"totalViews"`
	HeatScore        *float64                 `json:"heatScore,omitempty"`
	RecentEditors    []contentMetricActor     `json:"recentEditors"`
	RecentViewers    []contentMetricActor     `json:"recentViewers"`
	Editors          []contentMetricEditor    `json:"editors"`
	Developers       []contentMetricDeveloper `json:"developers"`
	Tutorials        []contentMetricReference `json:"tutorials"`
	Issues           []contentMetricReference `json:"issues"`
	News             []contentMetricReference `json:"news"`
	Discussions      []contentMetricReference `json:"discussions"`
	StatisticsAsOf   time.Time                `json:"statisticsAsOf"`
	IncludesChildren bool                     `json:"includesChildren"`
}

var metricProjectTypes = map[string]bool{
	"mod": true, "modpack": true, "plugin": true, "map": true,
	"resource_pack": true, "shader_pack": true, "datapack": true,
	"addon": true, "minecraft_server": true,
}

func (s *Server) contentMetrics(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveMetricTarget(r.Context(), r.PathValue("publicId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "content statistics target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve content statistics target")
		return
	}
	response := contentMetricsResponse{
		ID: target.PublicID, Type: target.Type, RecentEditors: []contentMetricActor{}, RecentViewers: []contentMetricActor{},
		Editors: []contentMetricEditor{}, Developers: []contentMetricDeveloper{}, Tutorials: []contentMetricReference{}, Issues: []contentMetricReference{},
		News: []contentMetricReference{}, Discussions: []contentMetricReference{}, IncludesChildren: target.Type == "mod",
	}
	var lastEditedAt *time.Time
	var hasMetrics bool
	err = s.db.QueryRow(r.Context(), `select route.created_at,metrics.last_edited_at,
		coalesce(metrics.edit_count,0),coalesce(metrics.direct_view_count,0),coalesce(metrics.child_view_count,0),
		coalesce(metrics.total_view_count,0),coalesce(metrics.updated_at,route.created_at),metrics.object_route_id is not null
		from public_routes route left join content_route_metrics metrics on metrics.object_route_id=route.id
		where route.id=$1`, target.RouteID).Scan(&response.CreatedAt, &lastEditedAt, &response.EditCount,
		&response.DirectViews, &response.ChildViews, &response.TotalViews, &response.StatisticsAsOf, &hasMetrics)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load content statistics")
		return
	}
	response.LastEditedAt = lastEditedAt
	if metricProjectTypes[target.Type] {
		var heat float64
		if scanErr := s.db.QueryRow(r.Context(), `select coalesce(heat_score,0) from content_popularity_stats
			where object_route_id=$1`, target.RouteID).Scan(&heat); scanErr == nil {
			response.HeatScore = &heat
		} else if !errors.Is(scanErr, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load content heat")
			return
		}
	}
	if response.RecentEditors, err = s.loadRecentMetricEditors(r.Context(), target); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load recent content editors")
		return
	}
	if response.RecentViewers, err = s.loadRecentMetricViewers(r.Context(), target.RouteID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load recent content viewers")
		return
	}
	if metricProjectTypes[target.Type] {
		if response.Editors, err = s.loadMetricProjectEditors(r.Context(), target); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load content editors")
			return
		}
		if response.Developers, err = s.loadMetricDevelopers(r.Context(), target); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load content developers")
			return
		}
	}
	locale := normalizeContentLocale(strings.TrimSpace(r.URL.Query().Get("locale")))
	if locale == "" {
		locale = "zh-CN"
	}
	references, err := s.loadMetricReferences(r.Context(), target, locale)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load related community content")
		return
	}
	for _, reference := range references {
		switch reference.Kind {
		case "tutorial":
			response.Tutorials = append(response.Tutorials, reference)
		case "issue":
			response.Issues = append(response.Issues, reference)
		case "news":
			response.News = append(response.News, reference)
		case "discussion":
			response.Discussions = append(response.Discussions, reference)
		}
	}
	if !hasMetrics || response.StatisticsAsOf.Before(time.Now().Add(-5*time.Minute)) {
		_, _ = s.db.Exec(r.Context(), `select enqueue_content_stats_refresh($1,true,$2)`, target.RouteID, metricProjectTypes[target.Type])
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) loadMetricProjectEditors(ctx context.Context, target metricTarget) ([]contentMetricEditor, error) {
	rows, err := s.db.Query(ctx, `with candidates as (
		select access.user_id,access.access_level role_code,
			case when access.access_level='developer' then 0 else 1 end priority
		from effective_project_access access
		where access.project_type=$1 and access.project_id=$2
		union all
		select content_target_owner_id($3),'developer',0
	), selected as (
		select distinct on(user_id) user_id,role_code from candidates where user_id is not null
		order by user_id,priority
	)
	select account.public_id,account.username,account.avatar_url,selected.role_code
	from selected join users account on account.id=selected.user_id and account.status='active'
	order by selected.role_code,lower(account.username),account.id`, target.Type, target.InternalID, target.RouteID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentMetricEditor, 0)
	config := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var item contentMetricEditor
		if err = rows.Scan(&item.ID, &item.Name, &item.AvatarURL, &item.Role); err != nil {
			return nil, err
		}
		item.URL = "/" + item.ID
		if item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, config, item.AvatarURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) recordMetricView(w http.ResponseWriter, r *http.Request) {
	target, err := s.resolveMetricTarget(r.Context(), r.PathValue("publicId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "content statistics target not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve content statistics target")
		return
	}
	if err = s.recordContentRouteView(r.Context(), r, target.RouteID, metricProjectTypes[target.Type]); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record content view")
		return
	}
	skipRequestActivity(r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) resolveMetricTarget(ctx context.Context, publicID string) (metricTarget, error) {
	publicID = strings.ToLower(strings.TrimSpace(publicID))
	if !validCatalogPublicID(publicID) {
		return metricTarget{}, pgx.ErrNoRows
	}
	var target metricTarget
	target.PublicID = publicID
	if err := s.db.QueryRow(ctx, `select id,entity_type,internal_id from public_routes where public_id=$1
		and entity_type in ('resource','mod','modpack','plugin','map','resource_pack','shader_pack','datapack','addon','minecraft_server')`,
		publicID).Scan(&target.RouteID, &target.Type, &target.InternalID); err != nil {
		return metricTarget{}, err
	}
	var visible bool
	var err error
	switch target.Type {
	case "resource":
		err = s.db.QueryRow(ctx, `select status in ('active','placeholder') from catalog_entities where id=$1`, target.InternalID).Scan(&visible)
	case "mod":
		err = s.db.QueryRow(ctx, `select review_status='approved' from mods where id=$1`, target.InternalID).Scan(&visible)
	case "modpack":
		err = s.db.QueryRow(ctx, `select review_status='approved' from modpacks where id=$1`, target.InternalID).Scan(&visible)
	case "minecraft_server":
		err = s.db.QueryRow(ctx, `select review_status='approved' from minecraft_servers where id=$1`, target.InternalID).Scan(&visible)
	default:
		err = s.db.QueryRow(ctx, `select review_status='approved' from simple_projects where id=$1 and project_type=$2`,
			target.InternalID, target.Type).Scan(&visible)
	}
	if err != nil {
		return metricTarget{}, err
	}
	if !visible {
		return metricTarget{}, pgx.ErrNoRows
	}
	return target, nil
}

func (s *Server) recordContentRouteView(ctx context.Context, r *http.Request, routeID int64, includePopularity bool) error {
	viewerID := currentClaims(r).Subject
	viewerIdentity := "anonymous:" + normalizeIPAddress(remoteIP(r.RemoteAddr)) + ":" + r.UserAgent()
	if viewerID > 0 {
		viewerIdentity = "user:" + strconv.FormatInt(viewerID, 10)
	}
	viewerHash := sha256.Sum256([]byte(viewerIdentity))
	pageKey := strings.TrimSpace(r.URL.Query().Get("pageKey"))
	if pageKey == "" {
		pageKey = "detail"
	}
	if len(pageKey) > 200 {
		pageKey = pageKey[:200]
	}
	pageHash := sha256.Sum256([]byte(pageKey))
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	shard := int16(viewerHash[0] % 32)
	if _, err = tx.Exec(ctx, `insert into content_view_daily(object_route_id,view_date,counter_shard,views)
		values($1,current_date,$2,1) on conflict(object_route_id,view_date,counter_shard) do update
		set views=content_view_daily.views+1`, routeID, shard); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into site_view_daily(metric_date,counter_shard,views)
		values(current_date,$1,1) on conflict(metric_date,counter_shard) do update
		set views=site_view_daily.views+1`, shard); err != nil {
		return err
	}
	var nullableViewerID any
	if viewerID > 0 {
		nullableViewerID = viewerID
	}
	if _, err = tx.Exec(ctx, `insert into content_unique_views(object_route_id,viewer_hash,viewer_user_id)
		values($1,$2,$3) on conflict(object_route_id,viewer_hash) do update set last_seen_at=now(),
		viewer_user_id=coalesce(content_unique_views.viewer_user_id,excluded.viewer_user_id)`,
		routeID, viewerHash[:], nullableViewerID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `insert into content_project_pages(object_route_id,page_hash)
		values($1,$2) on conflict do nothing`, routeID, pageHash[:]); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `select enqueue_content_stats_refresh($1,true,$2),enqueue_parent_project_metrics($1)`,
		routeID, includePopularity); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Server) loadRecentMetricEditors(ctx context.Context, target metricTarget) ([]contentMetricActor, error) {
	rows, err := s.db.Query(ctx, `with edit_events as (
		select revision.created_by actor_id,revision.created_at
		from content_revisions revision join change_requests request on request.proposed_revision_id=revision.id
		where revision.entity_type=$1 and revision.entity_id=$2 and request.status='approved' and revision.created_by is not null
		union all
		select imported.submitted_by,imported.created_at
		from resource_import_snapshots snapshot join catalog_import_revisions imported on imported.id=snapshot.revision_id
		where $1='resource' and snapshot.resource_id=$2 and imported.status in ('ready','partial','superseded')
		and imported.submitted_by is not null
	), actor_summary as (
		select actor_id,max(created_at) occurred_at,count(*) count from edit_events group by actor_id
	)
	select account.public_id,account.username,account.avatar_url,summary.occurred_at,summary.count
	from actor_summary summary join users account on account.id=summary.actor_id
	order by summary.occurred_at desc,summary.actor_id desc limit 8`, target.Type, target.InternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentMetricActor, 0, 8)
	for rows.Next() {
		var item contentMetricActor
		if err = rows.Scan(&item.ID, &item.Name, &item.AvatarURL, &item.OccurredAt, &item.Count); err != nil {
			return nil, err
		}
		item.URL = "/" + item.ID
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return s.resolveMetricActorAvatars(ctx, items)
}

func (s *Server) loadRecentMetricViewers(ctx context.Context, routeID int64) ([]contentMetricActor, error) {
	rows, err := s.db.Query(ctx, `select account.public_id,account.username,account.avatar_url,view.last_seen_at
		from content_unique_views view join users account on account.id=view.viewer_user_id
		where view.object_route_id=$1 and account.status='active'
		order by view.last_seen_at desc,account.id desc limit 8`, routeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentMetricActor, 0, 8)
	for rows.Next() {
		var item contentMetricActor
		if err = rows.Scan(&item.ID, &item.Name, &item.AvatarURL, &item.OccurredAt); err != nil {
			return nil, err
		}
		item.URL = "/" + item.ID
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return s.resolveMetricActorAvatars(ctx, items)
}

func (s *Server) resolveMetricActorAvatars(ctx context.Context, items []contentMetricActor) ([]contentMetricActor, error) {
	config := s.ossConfigFromSettings(ctx)
	for index := range items {
		resolved, err := s.resolveStoredOSSObjectAccessURLWithConfig(ctx, config, items[index].AvatarURL)
		if err != nil {
			return nil, err
		}
		items[index].AvatarURL = resolved
	}
	return items, nil
}

func (s *Server) loadMetricDevelopers(ctx context.Context, target metricTarget) ([]contentMetricDeveloper, error) {
	rows, err := s.db.Query(ctx, `select creator.public_id,creator.kind,creator.name,creator.avatar_url,
		coalesce(nullif(binding.role_snapshot,''),role.name,role.code,'')
		from content_creator_bindings binding join creators creator on creator.id=binding.creator_id
		left join creator_role_definitions role on role.id=binding.role_id
		where binding.subject_type=$1 and binding.subject_id=$2 and binding.status='approved'
		  and creator.review_status='approved'
		order by binding.display_order,binding.id`, target.Type, target.InternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentMetricDeveloper, 0)
	config := s.ossConfigFromSettings(ctx)
	for rows.Next() {
		var item contentMetricDeveloper
		if err = rows.Scan(&item.ID, &item.Kind, &item.Name, &item.AvatarURL, &item.Role); err != nil {
			return nil, err
		}
		item.URL = "/authors/" + item.ID
		if item.Kind == "team" {
			item.URL = "/teams/" + item.ID
		}
		if item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(ctx, config, item.AvatarURL); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) loadMetricReferences(ctx context.Context, target metricTarget, locale string) ([]contentMetricReference, error) {
	rows, err := s.db.Query(ctx, `select post.public_id,post.kind,coalesce(nullif(translation.title,''),post.title),post.published_at
		from community_posts post
		left join community_post_translations translation on translation.post_id=post.id and translation.locale=$3
		where post.status='active' and post.review_status='approved' and post.published_at is not null and (
			($1='resource' and exists(select 1 from community_post_resource_refs reference
				where reference.post_id=post.id and reference.resource_id=$2))
			or ($1<>'resource' and exists(select 1 from community_post_project_refs reference
				where reference.post_id=post.id and reference.target_type=$1 and reference.target_id=$2))
		) order by post.published_at desc,post.id desc limit 32`, target.Type, target.InternalID, locale)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]contentMetricReference, 0)
	for rows.Next() {
		var item contentMetricReference
		if err = rows.Scan(&item.ID, &item.Kind, &item.Title, &item.PublishedAt); err != nil {
			return nil, err
		}
		item.URL = metricReferenceURL(item.Kind, item.ID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func metricReferenceURL(kind, publicID string) string {
	switch kind {
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
