package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type adminDashboardOverview struct {
	OnlineUsers      int64                  `json:"onlineUsers"`
	MonthlyActive    int64                  `json:"monthlyActiveUsers"`
	TotalUsers       int64                  `json:"totalUsers"`
	TotalProjects    int64                  `json:"totalProjects"`
	ApprovedProjects int64                  `json:"approvedProjects"`
	PendingReviews   int64                  `json:"pendingReviews"`
	ViewsToday       int64                  `json:"viewsToday"`
	ActionsToday     int64                  `json:"actionsToday"`
	OSS              adminDashboardOSSStats `json:"oss"`
	Trend            []adminSiteMetricPoint `json:"trend"`
	UpdatedAt        time.Time              `json:"updatedAt"`
}

type adminDashboardOSSStats struct {
	ActiveFiles      int64 `json:"activeFiles"`
	StoredBytes      int64 `json:"storedBytes"`
	SourceBytes      int64 `json:"sourceBytes"`
	PendingScans     int64 `json:"pendingScans"`
	QuarantinedFiles int64 `json:"quarantinedFiles"`
	UploadsToday     int64 `json:"uploadsToday"`
}

type adminSiteMetricPoint struct {
	Date              string `json:"date"`
	ActiveUsers       int64  `json:"activeUsers"`
	Views             int64  `json:"views"`
	Actions           int64  `json:"actions"`
	NewUsers          int64  `json:"newUsers"`
	ReviewSubmissions int64  `json:"reviewSubmissions"`
}

type adminProjectSummary struct {
	ID           string     `json:"id"`
	Type         string     `json:"type"`
	Name         string     `json:"name"`
	URL          string     `json:"url"`
	ReviewStatus string     `json:"reviewStatus"`
	Views        int64      `json:"views"`
	EditCount    int64      `json:"editCount"`
	Heat         float64    `json:"heat"`
	Rating       float64    `json:"rating"`
	Favorites    int64      `json:"favorites"`
	Comments     int64      `json:"comments"`
	Downloads    int64      `json:"downloads"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	LastEditedAt *time.Time `json:"lastEditedAt,omitempty"`
}

type adminProjectMetricPoint struct {
	Date      string  `json:"date"`
	Views     int64   `json:"views"`
	Heat      float64 `json:"heat"`
	Favorites int64   `json:"favorites"`
	Comments  int64   `json:"comments"`
	Ratings   int64   `json:"ratings"`
	Downloads int64   `json:"downloads"`
}

func (s *Server) loadAdminDashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	overview := adminDashboardOverview{Trend: make([]adminSiteMetricPoint, 0, 30)}
	overview.OnlineUsers = s.cache.OnlinePresenceCount(r.Context(), now)
	err := s.db.QueryRow(r.Context(), `select
		(select count(*) from users where status<>'deleted'),
		(select count(*) from top_level_project_catalog),
		(select count(*) from top_level_project_catalog where review_status='approved'),
		((select count(*) from change_requests where status='pending')+
		 (select count(*) from minecraft_servers where review_status='pending')+
		 (select count(*) from creator_claims where status='pending')+
		 (select count(*) from reports where status in ('pending','in_review'))+
		 (select count(*) from project_editor_applications where status='pending')),
		coalesce((select sum(views) from site_view_daily where metric_date=current_date),0),
		coalesce((select actions from site_daily_metrics where metric_date=current_date),0),
		(select count(*) from site_monthly_active_users active join users account on account.id=active.user_id
		 where active.activity_month=date_trunc('month',current_date)::date and account.status<>'deleted'),
		coalesce((select updated_at from site_daily_metrics where metric_date=current_date),now())`).
		Scan(&overview.TotalUsers, &overview.TotalProjects, &overview.ApprovedProjects, &overview.PendingReviews,
			&overview.ViewsToday, &overview.ActionsToday, &overview.MonthlyActive, &overview.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load administration overview")
		return
	}
	err = s.db.QueryRow(r.Context(), `select
		count(*) filter (where status='active'),
		coalesce(sum(size_bytes) filter (where status='active'),0),
		coalesce(sum(coalesce(nullif(source_size_bytes,0),size_bytes)) filter (where status='active'),0),
		count(*) filter (where status='active' and scan_status='pending'),
		count(*) filter (where status='quarantined'),
		(select count(*) from oss_upload_logs where result='success' and created_at>=current_date)
		from oss_files`).Scan(
		&overview.OSS.ActiveFiles,
		&overview.OSS.StoredBytes,
		&overview.OSS.SourceBytes,
		&overview.OSS.PendingScans,
		&overview.OSS.QuarantinedFiles,
		&overview.OSS.UploadsToday,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load OSS overview")
		return
	}
	rows, err := s.db.Query(r.Context(), `select day::date::text,coalesce(metric.active_users,0),coalesce(live_views.views,metric.views,0),
		coalesce(metric.actions,0),coalesce(metric.new_users,0),coalesce(metric.review_submissions,0)
		from generate_series(current_date-29,current_date,interval '1 day') day
		left join site_daily_metrics metric on metric.metric_date=day::date
		left join (select metric_date,sum(views) views from site_view_daily
			where metric_date>=current_date-29 group by metric_date) live_views on live_views.metric_date=day::date
		order by day`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load administration trend")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var point adminSiteMetricPoint
		if err = rows.Scan(&point.Date, &point.ActiveUsers, &point.Views, &point.Actions, &point.NewUsers, &point.ReviewSubmissions); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode administration trend")
			return
		}
		overview.Trend = append(overview.Trend, point)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load administration trend")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cards": []map[string]any{
			{"label": "onlineUsers", "value": overview.OnlineUsers, "tone": "green"},
			{"label": "monthlyActiveUsers", "value": overview.MonthlyActive, "tone": "blue"},
			{"label": "totalProjects", "value": overview.TotalProjects, "tone": "violet"},
			{"label": "pendingReviews", "value": overview.PendingReviews, "tone": "red"},
		},
		"overview": overview,
	})
}

func (s *Server) adminDashboardProjects(w http.ResponseWriter, r *http.Request) {
	limit := boundedLimit(r.URL.Query().Get("limit"), 30, 100)
	offset := boundedOffset(r.URL.Query().Get("offset"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	projectType := normalizeAdminProjectType(r.URL.Query().Get("type"))
	if strings.TrimSpace(r.URL.Query().Get("type")) != "" && projectType == "" {
		writeError(w, http.StatusBadRequest, "invalid project type")
		return
	}
	var total int64
	if err := s.db.QueryRow(r.Context(), `select count(*) from top_level_project_catalog project
		where ($1='' or project.entity_type=$1) and ($2='' or project.name ilike '%'||$2||'%' or project.public_id=$2)`,
		projectType, query).Scan(&total); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to count projects")
		return
	}
	rows, err := s.db.Query(r.Context(), `select project.public_id,project.entity_type,project.name,project.canonical_path,
		project.review_status,coalesce(metrics.total_view_count,0),coalesce(metrics.edit_count,0),
		coalesce(popularity.heat_score,0),coalesce(popularity.rating_average,0),coalesce(popularity.favorite_count,0),
		coalesce(popularity.comment_count,0),coalesce(popularity.download_count,0),project.created_at,project.updated_at,
		metrics.last_edited_at
		from top_level_project_catalog project
		left join content_route_metrics metrics on metrics.object_route_id=project.object_route_id
		left join content_popularity_stats popularity on popularity.object_route_id=project.object_route_id
		where ($1='' or project.entity_type=$1) and ($2='' or project.name ilike '%'||$2||'%' or project.public_id=$2)
		order by coalesce(popularity.heat_score,0) desc,project.updated_at desc,project.object_route_id desc
		limit $3 offset $4`, projectType, query, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load projects")
		return
	}
	defer rows.Close()
	items := make([]adminProjectSummary, 0, limit)
	for rows.Next() {
		var item adminProjectSummary
		if err = rows.Scan(&item.ID, &item.Type, &item.Name, &item.URL, &item.ReviewStatus, &item.Views,
			&item.EditCount, &item.Heat, &item.Rating, &item.Favorites, &item.Comments, &item.Downloads,
			&item.CreatedAt, &item.UpdatedAt, &item.LastEditedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode projects")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load projects")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) adminDashboardProject(w http.ResponseWriter, r *http.Request) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("publicId")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "invalid project")
		return
	}
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days != 7 && days != 30 && days != 90 && days != 365 {
		days = 30
	}
	item, routeID, err := s.loadAdminProjectSummary(r, publicID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project")
		return
	}
	rows, err := s.db.Query(r.Context(), `select day::date::text,
		coalesce((select sum(views) from content_view_daily view where view.object_route_id=$1 and view.view_date=day::date),0),
		coalesce(snapshot.heat_score,0),coalesce(snapshot.favorite_count,0),coalesce(snapshot.comment_count,0),
		coalesce(snapshot.rating_count,0),coalesce(snapshot.download_count,0)
		from generate_series(current_date-($2::integer-1),current_date,interval '1 day') day
		left join lateral (select heat_score,favorite_count,comment_count,rating_count,download_count
			from content_popularity_daily_snapshots snapshot
			where snapshot.object_route_id=$1 and snapshot.metric_date<=day::date
			order by snapshot.metric_date desc limit 1) snapshot on true order by day`, routeID, days)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project trend")
		return
	}
	defer rows.Close()
	trend := make([]adminProjectMetricPoint, 0, days)
	for rows.Next() {
		var point adminProjectMetricPoint
		if err = rows.Scan(&point.Date, &point.Views, &point.Heat, &point.Favorites, &point.Comments,
			&point.Ratings, &point.Downloads); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode project trend")
			return
		}
		trend = append(trend, point)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project trend")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": item, "trend": trend, "days": days})
}

func (s *Server) loadAdminProjectSummary(r *http.Request, publicID string) (adminProjectSummary, int64, error) {
	var item adminProjectSummary
	var routeID int64
	err := s.db.QueryRow(r.Context(), `select project.object_route_id,project.public_id,project.entity_type,project.name,
		project.canonical_path,project.review_status,coalesce(metrics.total_view_count,0),coalesce(metrics.edit_count,0),
		coalesce(popularity.heat_score,0),coalesce(popularity.rating_average,0),coalesce(popularity.favorite_count,0),
		coalesce(popularity.comment_count,0),coalesce(popularity.download_count,0),project.created_at,project.updated_at,
		metrics.last_edited_at from top_level_project_catalog project
		left join content_route_metrics metrics on metrics.object_route_id=project.object_route_id
		left join content_popularity_stats popularity on popularity.object_route_id=project.object_route_id
		where project.public_id=$1`, publicID).Scan(&routeID, &item.ID, &item.Type, &item.Name, &item.URL,
		&item.ReviewStatus, &item.Views, &item.EditCount, &item.Heat, &item.Rating, &item.Favorites,
		&item.Comments, &item.Downloads, &item.CreatedAt, &item.UpdatedAt, &item.LastEditedAt)
	return item, routeID, err
}

func normalizeAdminProjectType(value string) string {
	value = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
	if value == "server" {
		value = "minecraft_server"
	}
	for _, allowed := range []string{"mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon", "minecraft_server"} {
		if value == allowed {
			return value
		}
	}
	return ""
}
