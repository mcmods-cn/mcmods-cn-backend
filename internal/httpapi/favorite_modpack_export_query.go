package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type favoriteModpackExportSummary struct {
	ID                  string     `json:"id"`
	CollectionID        string     `json:"collectionId"`
	PackName            string     `json:"packName"`
	PackVersion         string     `json:"packVersion"`
	MinecraftVersion    string     `json:"minecraftVersion"`
	Loader              string     `json:"loader"`
	LoaderVersion       string     `json:"loaderVersion"`
	AllowCompatibleOnly bool       `json:"allowCompatibleOnly"`
	ReportVersion       int        `json:"reportVersion"`
	Status              string     `json:"status"`
	CollectionItemCount int        `json:"collectionItemCount"`
	ExportedModCount    int        `json:"exportedModCount"`
	AutoDependencyCount int        `json:"autoDependencyCount"`
	SkippedItemCount    int        `json:"skippedItemCount"`
	FailedItemCount     int        `json:"failedItemCount"`
	FinalFileCount      int        `json:"finalFileCount"`
	FileSize            int64      `json:"fileSize"`
	ResultSHA256        string     `json:"resultSha256,omitempty"`
	ErrorCode           string     `json:"errorCode,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	FinishedAt          *time.Time `json:"finishedAt,omitempty"`
	ExpiresAt           *time.Time `json:"expiresAt,omitempty"`
}

func (s *Server) favoriteModpackExports(w http.ResponseWriter, r *http.Request) {
	request, err := parseFavoriteExportPageRequest(r.URL.Query(), currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	query, arguments := favoriteExportPageSQL(request)
	rows, err := s.db.Query(r.Context(), query, arguments...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load export history")
		return
	}
	defer rows.Close()
	type exportPageRow struct {
		internalID int64
		item       favoriteModpackExportSummary
	}
	pageRows := make([]exportPageRow, 0, request.Limit+1)
	for rows.Next() {
		var row exportPageRow
		item := &row.item
		if err = rows.Scan(&row.internalID, &item.ID, &item.CollectionID, &item.PackName, &item.PackVersion, &item.MinecraftVersion, &item.Loader, &item.LoaderVersion,
			&item.AllowCompatibleOnly, &item.ReportVersion, &item.Status, &item.CollectionItemCount, &item.ExportedModCount, &item.AutoDependencyCount, &item.SkippedItemCount,
			&item.FailedItemCount, &item.FinalFileCount, &item.FileSize, &item.ResultSHA256, &item.ErrorCode, &item.CreatedAt, &item.FinishedAt, &item.ExpiresAt); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export history")
			return
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load export history")
		return
	}
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	items := make([]favoriteModpackExportSummary, len(pageRows))
	for index := range pageRows {
		items[index] = pageRows[index].item
	}
	nextCursor := ""
	if hasMore && len(pageRows) != 0 {
		last := pageRows[len(pageRows)-1]
		nextCursor, err = encodeFavoriteExportPageCursor(request, last.item.CreatedAt, last.internalID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode export history cursor")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "limit": request.Limit, "hasMore": hasMore, "nextCursor": nextCursor,
	})
}

func (s *Server) favoriteModpackExportDetail(w http.ResponseWriter, r *http.Request) {
	taskID := strings.ToLower(strings.TrimSpace(r.PathValue("taskId")))
	var task favoriteModpackExportSummary
	err := s.db.QueryRow(r.Context(), `select task.public_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,
		loader_version,allow_compatible_only,report_version,status,collection_item_count,exported_mod_count,auto_dependency_count,skipped_item_count,
		failed_item_count,final_file_count,result_file_size,result_sha256,error_code,task.created_at,finished_at,expires_at
		from favorite_modpack_export_tasks task
		where task.public_id=$1 and owner_user_id=$2`, taskID, currentClaims(r).Subject).
		Scan(&task.ID, &task.CollectionID, &task.PackName, &task.PackVersion, &task.MinecraftVersion, &task.Loader, &task.LoaderVersion,
			&task.AllowCompatibleOnly, &task.ReportVersion, &task.Status,
			&task.CollectionItemCount, &task.ExportedModCount, &task.AutoDependencyCount, &task.SkippedItemCount, &task.FailedItemCount,
			&task.FinalFileCount, &task.FileSize, &task.ResultSHA256, &task.ErrorCode, &task.CreatedAt, &task.FinishedAt, &task.ExpiresAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "export task not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load export task")
		}
		return
	}
	rows, err := s.db.Query(r.Context(), `select coalesce(route.public_id,''),source_project_type,source_project_name_snapshot,
		result_type,reason_code,reason_detail,modrinth_project_id,modrinth_version_id,selected_version_name,selected_file_name,
		minecraft_version,loader,release_type,env_client,env_server,file_size,sha1,sha512,download_url,dependency_of
		from favorite_modpack_export_items item left join public_routes route on route.id=item.source_project_route_id
		where task_id=(select id from favorite_modpack_export_tasks where public_id=$1) order by item.id`, taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load export report")
		return
	}
	defer rows.Close()
	items := make([]favoriteModpackExportItem, 0)
	for rows.Next() {
		var item favoriteModpackExportItem
		var dependencies []byte
		if err = rows.Scan(&item.SourceProjectID, &item.SourceProjectType, &item.SourceProjectName, &item.ResultType, &item.ReasonCode,
			&item.ReasonDetail, &item.ModrinthProjectID, &item.ModrinthVersionID, &item.SelectedVersionName, &item.SelectedFileName,
			&item.MinecraftVersion, &item.Loader, &item.ReleaseType, &item.EnvironmentClient, &item.EnvironmentServer, &item.FileSize,
			&item.SHA1, &item.SHA512, &item.DownloadURL, &dependencies); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export report")
			return
		}
		if err = json.Unmarshal(dependencies, &item.DependencyOf); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to decode export report")
			return
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load export report")
		return
	}
	downloadAvailable := task.Status == "ready" && task.ExpiresAt != nil && task.ExpiresAt.After(time.Now())
	writeJSON(w, http.StatusOK, map[string]any{"task": task, "items": items, "downloadAvailable": downloadAvailable})
}

func (s *Server) downloadFavoriteModpackExport(w http.ResponseWriter, r *http.Request) {
	taskID := strings.ToLower(strings.TrimSpace(r.PathValue("taskId")))
	var objectKey, name, minecraftVersion, loader, status string
	var expires *time.Time
	err := s.db.QueryRow(r.Context(), `select file.object_key,task.pack_name,task.minecraft_version,task.loader_type,task.status,task.expires_at
		from favorite_modpack_export_tasks task join oss_files file on file.id=task.result_file_id and file.status='active'
		where task.public_id=$1 and task.owner_user_id=$2`, taskID, currentClaims(r).Subject).Scan(&objectKey, &name, &minecraftVersion, &loader, &status, &expires)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeAPIError(w, http.StatusGone, "MODPACK_EXPORT_DOWNLOAD_EXPIRED", "the temporary download is unavailable", 0, nil)
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load export download")
		}
		return
	}
	if status != "ready" || expires == nil || !expires.After(time.Now()) {
		writeAPIError(w, http.StatusGone, "MODPACK_EXPORT_DOWNLOAD_EXPIRED", "the temporary download is unavailable", 0, nil)
		return
	}
	filename := safeMRPackDownloadName(name, minecraftVersion, loader)
	s.redirectOSSObjectAccess(w, r, objectKey, ossObjectAccessOptions{Expires: 10 * time.Minute, ContentDisposition: downloadContentDisposition(filename)})
}

func safeMRPackDownloadName(name, minecraftVersion, loader string) string {
	base := strings.TrimSpace(name)
	base = strings.NewReplacer("/", "-", "\\", "-", ":", "-", "\x00", "").Replace(base)
	if base == "" {
		base = "mcmods-collection"
	}
	runes := []rune(base)
	if len(runes) > 80 {
		base = string(runes[:80])
	}
	return base + "-" + minecraftVersion + "-" + loader + ".mrpack"
}
