package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

var (
	errFavoriteModpackExportRebuildSourceUnavailable = errors.New("favorite modpack export rebuild source is unavailable")
	errFavoriteModpackExportRebuildReportInvalid     = errors.New("favorite modpack export rebuild report is invalid")
)

type favoriteModpackExportRebuildRequest struct {
	Source string `json:"source"`
}

type favoriteModpackExportRebuildTask struct {
	InternalID           int64
	CollectionInternalID int64
	favoriteModpackExportSummary
}

func (s *Server) rebuildFavoriteModpackExportPreview(w http.ResponseWriter, r *http.Request) {
	var request favoriteModpackExportRebuildRequest
	if decodeJSON(r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "MODPACK_EXPORT_REBUILD_INVALID_REQUEST", "select one rebuild source", 0, nil)
		return
	}
	request.Source = strings.ToLower(strings.TrimSpace(request.Source))
	if request.Source != favoriteModpackExportSourceCurrent && request.Source != favoriteModpackExportSourceOriginal {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_REBUILD_INVALID_SOURCE", "source must be current_collection or original_snapshot", 0, nil)
		return
	}
	taskID := strings.ToLower(strings.TrimSpace(r.PathValue("taskId")))
	if !validCatalogPublicID(taskID) {
		writeError(w, http.StatusNotFound, "export task not found")
		return
	}
	task, err := s.loadFavoriteModpackExportRebuildTask(r.Context(), currentClaims(r).Subject, taskID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "export task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load export task")
		return
	}

	var preview favoriteModpackExportPreview
	switch request.Source {
	case favoriteModpackExportSourceCurrent:
		if task.CollectionInternalID <= 0 {
			writeFavoriteModpackExportRebuildError(w, errFavoriteModpackExportRebuildSourceUnavailable)
			return
		}
		preview, err = s.buildFavoriteModpackExportPreview(r.Context(), currentClaims(r), task.CollectionID, favoriteModpackExportRequest{
			MinecraftVersion: task.MinecraftVersion,
			Loader:           task.Loader,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			err = errFavoriteModpackExportRebuildSourceUnavailable
		}
		preview.ReportVersion = favoriteModpackExportReportVersion
	case favoriteModpackExportSourceOriginal:
		preview, err = s.loadFavoriteModpackExportOriginalPreview(r.Context(), task)
	}
	if err != nil {
		if errors.Is(err, errFavoriteModpackExportRebuildSourceUnavailable) || errors.Is(err, errFavoriteModpackExportRebuildReportInvalid) {
			writeFavoriteModpackExportRebuildError(w, err)
		} else {
			writeFavoriteExportError(w, err)
		}
		return
	}
	preview.AllowCompatibleOnly = task.AllowCompatibleOnly
	preview.RebuildSource = request.Source
	preview, err = s.persistFavoriteModpackExportPreview(r.Context(), currentClaims(r).Subject, preview)
	if err != nil {
		writeFavoriteExportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) loadFavoriteModpackExportRebuildTask(ctx context.Context, ownerID int64, taskID string) (favoriteModpackExportRebuildTask, error) {
	var task favoriteModpackExportRebuildTask
	err := s.db.QueryRow(ctx, `select task.id,coalesce(task.collection_id,0),task.public_id,task.collection_public_id_snapshot,
		task.pack_name,task.pack_version_id,task.minecraft_version,task.loader_type,task.loader_version,task.allow_compatible_only,
		task.report_version,task.status,task.collection_item_count,task.exported_mod_count,task.auto_dependency_count,
		task.skipped_item_count,task.failed_item_count,task.final_file_count
		from favorite_modpack_export_tasks task where task.public_id=$1 and task.owner_user_id=$2`, taskID, ownerID).
		Scan(&task.InternalID, &task.CollectionInternalID, &task.ID, &task.CollectionID,
			&task.PackName, &task.PackVersion, &task.MinecraftVersion, &task.Loader, &task.LoaderVersion,
			&task.AllowCompatibleOnly, &task.ReportVersion, &task.Status, &task.CollectionItemCount, &task.ExportedModCount,
			&task.AutoDependencyCount, &task.SkippedItemCount, &task.FailedItemCount, &task.FinalFileCount)
	return task, err
}

func (s *Server) loadFavoriteModpackExportOriginalPreview(ctx context.Context, task favoriteModpackExportRebuildTask) (favoriteModpackExportPreview, error) {
	preview := favoriteModpackExportPreview{favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{
		CollectionID: task.CollectionInternalID, CollectionPublicID: task.CollectionID, CollectionName: task.PackName,
		MinecraftVersion: task.MinecraftVersion, Loader: task.Loader, LoaderVersion: task.LoaderVersion,
		AllowCompatibleOnly: task.AllowCompatibleOnly, ReportVersion: task.ReportVersion, RebuildSource: favoriteModpackExportSourceOriginal,
		CollectionItemCount: task.CollectionItemCount, ExportedModCount: task.ExportedModCount,
		AutoDependencyCount: task.AutoDependencyCount, SkippedItemCount: task.SkippedItemCount,
		FailedItemCount: task.FailedItemCount, Items: make([]favoriteModpackExportItem, 0),
	}}
	if task.ReportVersion != favoriteModpackExportReportVersion || !validCatalogPublicID(preview.CollectionPublicID) ||
		strings.TrimSpace(preview.CollectionName) == "" || strings.TrimSpace(preview.MinecraftVersion) == "" ||
		strings.TrimSpace(preview.LoaderVersion) == "" {
		return preview, fmt.Errorf("%w: unsupported or incomplete task metadata", errFavoriteModpackExportRebuildReportInvalid)
	}
	if _, err := mrpackLoaderDependencyKey(preview.Loader); err != nil {
		return preview, fmt.Errorf("%w: %v", errFavoriteModpackExportRebuildReportInvalid, err)
	}
	rows, err := s.db.Query(ctx, `select item.source_collection_item_id,item.source_project_route_id,coalesce(route.public_id,''),
		item.source_project_type,item.source_project_name_snapshot,item.result_type,item.reason_code,item.reason_detail,
		item.modrinth_project_id,item.modrinth_version_id,item.selected_version_name,item.selected_file_name,
		item.minecraft_version,item.loader,item.release_type,item.env_client,item.env_server,item.file_size,item.sha1,item.sha512,
		item.download_url,item.dependency_of
		from favorite_modpack_export_items item left join public_routes route on route.id=item.source_project_route_id
		where item.task_id=$1 order by item.id`, task.InternalID)
	if err != nil {
		return preview, fmt.Errorf("load original export report: %w", err)
	}
	defer rows.Close()
	reportCounts := favoriteExportResultCounts{CollectionItems: task.CollectionItemCount}
	paths := make(map[string]string)
	for rows.Next() {
		var item favoriteModpackExportItem
		var dependencies []byte
		if err = rows.Scan(&item.SourceCollectionItemID, &item.SourceProjectRouteID, &item.SourceProjectID,
			&item.SourceProjectType, &item.SourceProjectName, &item.ResultType, &item.ReasonCode, &item.ReasonDetail,
			&item.ModrinthProjectID, &item.ModrinthVersionID, &item.SelectedVersionName, &item.SelectedFileName,
			&item.MinecraftVersion, &item.Loader, &item.ReleaseType, &item.EnvironmentClient, &item.EnvironmentServer,
			&item.FileSize, &item.SHA1, &item.SHA512, &item.DownloadURL, &dependencies); err != nil {
			return preview, fmt.Errorf("decode original export report: %w", err)
		}
		if err = json.Unmarshal(dependencies, &item.DependencyOf); err != nil {
			return preview, fmt.Errorf("%w: invalid dependency report", errFavoriteModpackExportRebuildReportInvalid)
		}
		switch item.ResultType {
		case "exported":
			reportCounts.Exported++
			fallthrough
		case "auto_dependency":
			if item.ResultType == "auto_dependency" {
				reportCounts.AutoDependencies++
			}
			if item.MinecraftVersion != task.MinecraftVersion || item.Loader != task.Loader ||
				!verifiedModrinthDownloadURL(item.DownloadURL, item.ModrinthProjectID, item.ModrinthVersionID) {
				return preview, fmt.Errorf("%w: output identity does not match task", errFavoriteModpackExportRebuildReportInvalid)
			}
			file := mrpackFile{Path: "mods/" + item.SelectedFileName, Hashes: map[string]string{"sha1": item.SHA1, "sha512": item.SHA512},
				Env:       mrpackEnvironment{Client: item.EnvironmentClient, Server: item.EnvironmentServer},
				Downloads: []string{item.DownloadURL}, FileSize: item.FileSize}
			if err = validateMRPackFile(&file); err != nil {
				return preview, fmt.Errorf("%w: invalid output file: %v", errFavoriteModpackExportRebuildReportInvalid, err)
			}
			pathKey, pathErr := portableMRPackFilePathKey(file.Path)
			if pathErr != nil {
				return preview, fmt.Errorf("%w: invalid output path: %v", errFavoriteModpackExportRebuildReportInvalid, pathErr)
			}
			if existing, exists := paths[pathKey]; exists {
				return preview, fmt.Errorf("%w: output path collision between %q and %q", errFavoriteModpackExportRebuildReportInvalid, existing, file.Path)
			}
			paths[pathKey] = file.Path
		case "skipped":
			reportCounts.Skipped++
		case "failed":
			reportCounts.Failed++
		default:
			return preview, fmt.Errorf("%w: unknown result type %q", errFavoriteModpackExportRebuildReportInvalid, item.ResultType)
		}
		preview.Items = append(preview.Items, item)
	}
	if err = rows.Err(); err != nil {
		return preview, fmt.Errorf("load original export report: %w", err)
	}
	reportCounts.FinalFiles = reportCounts.Exported + reportCounts.AutoDependencies
	taskCounts := favoriteExportResultCounts{CollectionItems: task.CollectionItemCount, Exported: task.ExportedModCount,
		AutoDependencies: task.AutoDependencyCount, Skipped: task.SkippedItemCount, Failed: task.FailedItemCount,
		FinalFiles: task.FinalFileCount}
	if err = validateFavoriteExportCompletionCounts(taskCounts, reportCounts, reportCounts.FinalFiles); err != nil {
		return preview, fmt.Errorf("%w: %v", errFavoriteModpackExportRebuildReportInvalid, err)
	}
	return preview, nil
}

func writeFavoriteModpackExportRebuildError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errFavoriteModpackExportRebuildSourceUnavailable):
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_REBUILD_SOURCE_UNAVAILABLE", "the current source collection is no longer available; select the original snapshot", 0, nil)
	case errors.Is(err, errFavoriteModpackExportRebuildReportInvalid):
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_REBUILD_REPORT_INVALID", "the original report cannot be rebuilt safely", 0, nil)
	default:
		writeError(w, http.StatusInternalServerError, "failed to rebuild export preview")
	}
}
