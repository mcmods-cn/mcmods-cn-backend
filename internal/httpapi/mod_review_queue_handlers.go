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
	rows, err := s.db.Query(
		r.Context(),
		`select q.id,q.source,q.slug,q.mod_name,q.user_id,coalesce(u.username,''),coalesce(u.display_name,''),q.title,q.summary,q.created_at
		 from (
		   select r.id::text id,'revision'::text source,m.slug,m.primary_name mod_name,r.submitted_by user_id,
		          '模组资料修订 #' || r.version title,r.change_reason summary,r.created_at
		   from mod_revisions r join mods m on m.id=r.mod_id where r.status='pending'
		   union all
		   select e.id,'export'::text source,m.slug,m.primary_name mod_name,j.created_by user_id,
		          'mcmods_exporter ' || e.minecraft_version || ' / ' || e.loader title,
		          e.source_namespace || ' · revision ' || e.revision_no summary,e.created_at
		   from mod_export_revisions e join mods m on m.id=e.mod_id
		   left join mod_export_jobs j on j.mod_id=e.mod_id and j.package_id=e.package_id
		   where e.status in ('ready','partial') and not e.is_active
		 ) q left join users u on u.id=q.user_id
		 order by q.created_at asc`,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取待审核内容失败")
		return
	}
	defer rows.Close()
	items := make([]modContentReviewItem, 0)
	for rows.Next() {
		var item modContentReviewItem
		if err = rows.Scan(&item.ID, &item.Source, &item.ModSiteID, &item.ModName, &item.UserID, &item.Username, &item.DisplayName, &item.Title, &item.Summary, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "解析待审核内容失败")
			return
		}
		if item.Source == "revision" {
			item.ReviewURL = "/api/v1/mods/" + item.ModSiteID + "/revisions/" + item.ID
		} else {
			item.ReviewURL = "/api/v1/admin/export-revisions/" + item.ID + "/activate"
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
