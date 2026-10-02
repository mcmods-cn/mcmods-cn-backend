package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

const (
	favoriteModpackExportTaskCode       = "favorite_modpack_export"
	favoriteModpackExportReportVersion  = 1
	favoriteModpackExportSourceCurrent  = "current_collection"
	favoriteModpackExportSourceOriginal = "original_snapshot"
)

var (
	errFavoriteExportConcurrencyLimit  = errors.New("favorite export active task limit reached")
	errFavoriteExportDailyLimit        = errors.New("favorite export daily task limit reached")
	errFavoriteExportDuplicateCooldown = errors.New("favorite export duplicate cooldown active")
)

// favoriteTargetNameSQL is the one export-report name projection for the
// generation 89 favorite target closed set. Keep it beside
// favoriteTargetJoinsSQL so every supported non-Mod item has an auditable
// stable name even though MRPack intentionally skips it.
const favoriteTargetNameSQL = `case item.entity_type
	when 'mod' then mods.primary_name
	when 'modpack' then modpack.primary_name
	when 'blueprint' then blueprint.title
	else null end`

func favoriteExportMaxActive(value int) int {
	if value <= 0 {
		return 2
	}
	return value
}

func favoriteExportDailyLimit(value int) int {
	if value <= 0 {
		return 20
	}
	return value
}

const (
	exportReasonNotAMod                      = "NOT_A_MOD"
	exportReasonNoModrinthSource             = "NO_MODRINTH_SOURCE"
	exportReasonNoCompatibleMinecraftVersion = "NO_COMPATIBLE_MINECRAFT_VERSION"
	exportReasonNoCompatibleLoader           = "NO_COMPATIBLE_LOADER"
	exportReasonNoCompatibleFile             = "NO_COMPATIBLE_FILE"
	exportReasonMissingDownloadURL           = "MISSING_DOWNLOAD_URL"
	exportReasonMissingSHA1                  = "MISSING_SHA1"
	exportReasonMissingSHA512                = "MISSING_SHA512"
	exportReasonMissingFileSize              = "MISSING_FILE_SIZE"
	exportReasonRequiredDependencyUnresolved = "REQUIRED_DEPENDENCY_UNRESOLVED"
	exportReasonFilePathConflict             = "FILE_PATH_CONFLICT"
	exportReasonProjectHidden                = "PROJECT_HIDDEN"
	exportReasonDuplicateProject             = "DUPLICATE_PROJECT"
	exportReasonExternalAPIError             = "EXTERNAL_API_ERROR"
	exportReasonInternalProcessingError      = "INTERNAL_PROCESSING_ERROR"
)

type favoriteModpackExportRequest struct {
	MinecraftVersion      string `json:"minecraftVersion"`
	Loader                string `json:"loader"`
	PreviewID             string `json:"previewId"`
	PreviewHash           string `json:"previewHash"`
	ExportCompatibleOnly  bool   `json:"exportCompatibleOnly"`
	ConfirmCompatibleOnly bool   `json:"confirmCompatibleOnly"`
}

type favoriteModpackExportItem struct {
	SourceCollectionItemID *int64   `json:"sourceCollectionItemId,omitempty"`
	SourceProjectRouteID   *int64   `json:"-"`
	SourceProjectID        string   `json:"sourceProjectId,omitempty"`
	SourceProjectType      string   `json:"sourceProjectType"`
	SourceProjectName      string   `json:"sourceProjectName"`
	ResultType             string   `json:"resultType"`
	ReasonCode             string   `json:"reasonCode,omitempty"`
	ReasonDetail           string   `json:"reasonDetail,omitempty"`
	ModrinthProjectID      string   `json:"modrinthProjectId,omitempty"`
	ModrinthVersionID      string   `json:"modrinthVersionId,omitempty"`
	SelectedVersionName    string   `json:"selectedVersionName,omitempty"`
	SelectedFileName       string   `json:"selectedFileName,omitempty"`
	MinecraftVersion       string   `json:"minecraftVersion,omitempty"`
	Loader                 string   `json:"loader,omitempty"`
	ReleaseType            string   `json:"releaseType,omitempty"`
	EnvironmentClient      string   `json:"environmentClient,omitempty"`
	EnvironmentServer      string   `json:"environmentServer,omitempty"`
	FileSize               int64    `json:"fileSize,omitempty"`
	SHA1                   string   `json:"sha1,omitempty"`
	SHA512                 string   `json:"sha512,omitempty"`
	DownloadURL            string   `json:"downloadUrl,omitempty"`
	DependencyOf           []string `json:"dependencyOf,omitempty"`
}

type favoriteModpackExportPreviewSnapshot struct {
	CollectionID        int64                       `json:"-"`
	CollectionPublicID  string                      `json:"collectionId"`
	CollectionName      string                      `json:"collectionName"`
	MinecraftVersion    string                      `json:"minecraftVersion"`
	Loader              string                      `json:"loader"`
	LoaderVersion       string                      `json:"loaderVersion"`
	AllowCompatibleOnly bool                        `json:"allowCompatibleOnly"`
	ReportVersion       int                         `json:"reportVersion"`
	RebuildSource       string                      `json:"rebuildSource"`
	CollectionItemCount int                         `json:"collectionItemCount"`
	ExportedModCount    int                         `json:"exportedModCount"`
	AutoDependencyCount int                         `json:"autoDependencyCount"`
	SkippedItemCount    int                         `json:"skippedItemCount"`
	FailedItemCount     int                         `json:"failedItemCount"`
	Items               []favoriteModpackExportItem `json:"items"`
}

type favoriteModpackExportPreview struct {
	PreviewID   string    `json:"previewId"`
	PreviewHash string    `json:"previewHash"`
	ExpiresAt   time.Time `json:"expiresAt"`
	favoriteModpackExportPreviewSnapshot
}

type favoriteModpackExportMessage struct {
	TaskID string `json:"taskId"`
}

func (s *Server) preflightFavoriteModpackExport(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeFavoriteModpackExportRequest(w, r)
	if !ok {
		return
	}
	preview, err := s.buildFavoriteModpackExportPreview(r.Context(), currentClaims(r), r.PathValue("id"), request)
	if err != nil {
		writeFavoriteExportError(w, err)
		return
	}
	preview.ReportVersion = favoriteModpackExportReportVersion
	preview.RebuildSource = favoriteModpackExportSourceCurrent
	preview, err = s.persistFavoriteModpackExportPreview(r.Context(), currentClaims(r).Subject, preview)
	if err != nil {
		writeFavoriteExportError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

func (s *Server) createFavoriteModpackExport(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeFavoriteModpackExportRequest(w, r)
	if !ok {
		return
	}
	if !validCatalogPublicID(request.PreviewID) || len(request.PreviewHash) != sha256.Size*2 {
		writeAPIError(w, http.StatusBadRequest, "MODPACK_EXPORT_PREVIEW_REQUIRED", "confirm a current export preview", 0, nil)
		return
	}
	userID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create export task")
		return
	}
	defer tx.Rollback(r.Context())
	collectionPublicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	preview, previewRowID, err := loadFavoriteModpackExportPreviewForCreate(r.Context(), tx, userID, collectionPublicID, request)
	if err != nil {
		writeFavoriteExportPreviewError(w, err)
		return
	}
	if favoriteExportHasPathConflict(preview.Items) {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_FILE_PATH_CONFLICT", "selected files have conflicting portable paths", 0, map[string]any{"preview": preview})
		return
	}
	if preview.ExportedModCount+preview.AutoDependencyCount == 0 {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_EMPTY", "no compatible Mod can be exported", 0, nil)
		return
	}
	if favoriteExportHasRequiredDependencyFailure(preview.Items) {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_REQUIRED_DEPENDENCY_UNRESOLVED", "a required dependency could not be resolved", 0, map[string]any{"preview": preview})
		return
	}
	if preview.RebuildSource == favoriteModpackExportSourceOriginal &&
		(request.ExportCompatibleOnly != preview.AllowCompatibleOnly || request.ConfirmCompatibleOnly != preview.AllowCompatibleOnly) {
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_REBUILD_CONFIRMATION_MISMATCH", "the original compatible-only confirmation must be preserved", 0, map[string]any{"preview": preview})
		return
	}
	if preview.SkippedItemCount+preview.FailedItemCount > 0 && (!request.ExportCompatibleOnly || !request.ConfirmCompatibleOnly) {
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_COMPATIBLE_ONLY_CONFIRMATION_REQUIRED", "confirm exporting only compatible projects", 0, map[string]any{"preview": preview})
		return
	}
	if err = reserveFavoriteModpackExportQuota(r.Context(), tx, userID, preview.CollectionPublicID,
		preview.MinecraftVersion, preview.Loader, favoriteExportMaxActive(s.cfg.FavoriteExport.MaxActivePerUser),
		favoriteExportDailyLimit(s.cfg.FavoriteExport.MaxDailyPerUser)); err != nil &&
		!errors.Is(err, errFavoriteExportConcurrencyLimit) && !errors.Is(err, errFavoriteExportDailyLimit) &&
		!errors.Is(err, errFavoriteExportDuplicateCooldown) {
		writeError(w, http.StatusInternalServerError, "failed to check export limits")
		return
	}
	if errors.Is(err, errFavoriteExportConcurrencyLimit) {
		writeAPIError(w, http.StatusTooManyRequests, "MODPACK_EXPORT_CONCURRENCY_LIMIT", "too many exports are already processing", 60, nil)
		return
	}
	if errors.Is(err, errFavoriteExportDailyLimit) {
		writeAPIError(w, http.StatusTooManyRequests, "MODPACK_EXPORT_DAILY_LIMIT", "daily export limit reached", 3600, nil)
		return
	}
	if errors.Is(err, errFavoriteExportDuplicateCooldown) {
		writeAPIError(w, http.StatusTooManyRequests, "MODPACK_EXPORT_DUPLICATE_COOLDOWN", "wait before repeating the same export", 30, nil)
		return
	}
	// Persist the pack version with the task so dispatcher retries produce the
	// same index, while separate exports receive a readable revision identifier.
	packVersion := time.Now().UTC().Format("2006.01.02-150405")
	var taskID string
	err = tx.QueryRow(r.Context(), `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,collection_public_id_snapshot,pack_name,pack_version_id,
		minecraft_version,loader_type,loader_version,allow_compatible_only,collection_item_count,exported_mod_count,
		auto_dependency_count,skipped_item_count,failed_item_count,final_file_count,report_version)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) returning public_id`, userID, nullableFavoriteCollectionID(preview.CollectionID),
		preview.CollectionPublicID, preview.CollectionName, packVersion, preview.MinecraftVersion, preview.Loader, preview.LoaderVersion, request.ExportCompatibleOnly,
		preview.CollectionItemCount, preview.ExportedModCount, preview.AutoDependencyCount, preview.SkippedItemCount,
		preview.FailedItemCount, preview.ExportedModCount+preview.AutoDependencyCount, preview.ReportVersion).Scan(&taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create export task")
		return
	}
	for _, item := range preview.Items {
		dependencyOf, _ := json.Marshal(item.DependencyOf)
		_, err = tx.Exec(r.Context(), `insert into favorite_modpack_export_items(task_id,source_collection_item_id,
			source_project_route_id,source_project_type,source_project_name_snapshot,result_type,reason_code,reason_detail,
			modrinth_project_id,modrinth_version_id,selected_version_name,selected_file_name,minecraft_version,loader,
			release_type,env_client,env_server,file_size,sha1,sha512,download_url,dependency_of)
			select id,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22::jsonb
			from favorite_modpack_export_tasks where public_id=$1`, taskID, item.SourceCollectionItemID, item.SourceProjectRouteID,
			item.SourceProjectType, item.SourceProjectName, item.ResultType, item.ReasonCode, item.ReasonDetail,
			item.ModrinthProjectID, item.ModrinthVersionID, item.SelectedVersionName, item.SelectedFileName,
			item.MinecraftVersion, item.Loader, item.ReleaseType, item.EnvironmentClient, item.EnvironmentServer,
			item.FileSize, item.SHA1, item.SHA512, item.DownloadURL, string(dependencyOf))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to save export snapshot")
			return
		}
	}
	if err = markFavoriteModpackExportPreviewConsumed(r.Context(), tx, previewRowID); err != nil {
		writeFavoriteExportPreviewError(w, err)
		return
	}
	message := favoriteModpackExportMessage{TaskID: taskID}
	if _, err = queue.EnqueueTx(r.Context(), tx, favoriteModpackExportTaskCode, "favorite.modpack_export.requested", "favorite_modpack_export", taskID, r.Header.Get("X-Request-ID"), message); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue export task")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit export task")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"taskId": taskID, "status": "pending", "preview": preview})
}

func reserveFavoriteModpackExportQuota(ctx context.Context, tx pgx.Tx, userID int64, collectionPublicID, minecraftVersion, loader string,
	maxActive, maxDaily int) error {
	// Every production task insertion passes through this transaction. Holding
	// the per-user lock until commit makes the following counts and the caller's
	// task/item/outbox insertion one atomic quota reservation across instances.
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(
		hashtextextended('favorite-modpack-export-quota:'||$1::bigint::text,0))`, userID); err != nil {
		return fmt.Errorf("lock favorite export quota: %w", err)
	}
	var active, daily, recentDuplicate int
	if err := tx.QueryRow(ctx, `select
		count(*) filter(where status in ('pending','processing')),
		count(*) filter(where created_at>=now()-interval '24 hours'),
		count(*) filter(where collection_public_id_snapshot=$2 and minecraft_version=$3 and loader_type=$4 and created_at>=now()-interval '30 seconds')
		from favorite_modpack_export_tasks where owner_user_id=$1`, userID, collectionPublicID, minecraftVersion, loader).
		Scan(&active, &daily, &recentDuplicate); err != nil {
		return err
	}
	if active >= maxActive {
		return errFavoriteExportConcurrencyLimit
	}
	if daily >= maxDaily {
		return errFavoriteExportDailyLimit
	}
	if recentDuplicate > 0 {
		return errFavoriteExportDuplicateCooldown
	}
	return nil
}

func decodeFavoriteModpackExportRequest(w http.ResponseWriter, r *http.Request) (favoriteModpackExportRequest, bool) {
	var request favoriteModpackExportRequest
	if decodeJSON(r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "MODPACK_EXPORT_INVALID_REQUEST", "invalid export settings", 0, nil)
		return request, false
	}
	request.MinecraftVersion = strings.TrimSpace(request.MinecraftVersion)
	request.Loader = strings.ToLower(strings.TrimSpace(request.Loader))
	request.PreviewID = strings.ToLower(strings.TrimSpace(request.PreviewID))
	request.PreviewHash = strings.ToLower(strings.TrimSpace(request.PreviewHash))
	if !validMinecraftVersionCode(request.MinecraftVersion) {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_INVALID_MINECRAFT_VERSION", "select one enabled exact Minecraft version", 0, nil)
		return request, false
	}
	if _, err := mrpackLoaderDependencyKey(request.Loader); err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_INVALID_LOADER", "loader must be NeoForge, Fabric, or Forge", 0, nil)
		return request, false
	}
	return request, true
}

func (s *Server) buildFavoriteModpackExportPreview(ctx context.Context, claims security.Claims, collectionID string, request favoriteModpackExportRequest) (favoriteModpackExportPreview, error) {
	collectionID = strings.ToLower(strings.TrimSpace(collectionID))
	preview := favoriteModpackExportPreview{favoriteModpackExportPreviewSnapshot: favoriteModpackExportPreviewSnapshot{}}
	preview.CollectionPublicID, preview.MinecraftVersion, preview.Loader = collectionID, request.MinecraftVersion, request.Loader
	if len(collectionID) != 9 {
		return preview, pgx.ErrNoRows
	}
	if err := s.db.QueryRow(ctx, `select id,name from favorite_collections where public_id=$1 and (user_id=$2 or is_public)`, collectionID, claims.Subject).Scan(&preview.CollectionID, &preview.CollectionName); err != nil {
		return preview, err
	}
	loaderVersion, err := resolveSynchronizedMRPackLoaderVersion(ctx, s.db, request.MinecraftVersion, request.Loader)
	if err != nil {
		return preview, fmt.Errorf("loader version: %w", err)
	}
	preview.LoaderVersion = loaderVersion
	rows, err := s.db.Query(ctx, `select item.id,
		item.entity_type,item.entity_id,route.id,route.public_id,coalesce(`+favoriteTargetNameSQL+`,''),
		coalesce(mods.review_status,coalesce(modpack.review_status,'')),coalesce(export_source.external_project_id,'')
		from favorite_collection_items item
		`+favoriteTargetJoinsSQL+`
		left join project_external_sources export_source on export_source.project_route_id=route.id and export_source.source_type='modrinth'
		where item.collection_id=$1 and `+favoriteTargetVisibilitySQL("$2", "$3")+`
		order by item.created_at,item.entity_type,item.entity_id`, preview.CollectionID, claims.Subject,
		claimsAllow(claims, "admin.*") || claimsAllow(claims, "project.review"))
	if err != nil {
		return preview, err
	}
	defer rows.Close()
	seen := map[int64]struct{}{}
	candidates := make([]favoriteExportFileCandidate, 0)
	for rows.Next() {
		var itemID, internalID, routeID int64
		var entityType, publicID, name, status, projectID string
		if err = rows.Scan(&itemID, &entityType, &internalID, &routeID, &publicID, &name, &status, &projectID); err != nil {
			return preview, err
		}
		item := favoriteModpackExportItem{SourceCollectionItemID: int64Pointer(itemID), SourceProjectRouteID: int64Pointer(routeID), SourceProjectID: publicID, SourceProjectType: entityType, SourceProjectName: name}
		switch {
		case entityType != "mod":
			item.ResultType, item.ReasonCode = "skipped", exportReasonNotAMod
		case status != "approved":
			item.ResultType, item.ReasonCode = "skipped", exportReasonProjectHidden
		case hasInt64Key(seen, internalID):
			item.ResultType, item.ReasonCode = "skipped", exportReasonDuplicateProject
		default:
			seen[internalID] = struct{}{}
			candidates = append(candidates, favoriteExportFileCandidate{ItemIndex: len(preview.Items), RouteID: routeID, ProjectID: projectID})
		}
		preview.Items = append(preview.Items, item)
	}
	if err = rows.Err(); err != nil {
		return preview, err
	}
	preview.CollectionItemCount = len(preview.Items)
	var providerConfig modImportConfig
	var providerConfigErr error
	if len(candidates) != 0 {
		providerConfig, providerConfigErr = s.modImportConfigFromSettings(ctx)
		resolutions, resolveErr := resolveFavoriteExportFileCandidates(ctx, candidates, func(resolveContext context.Context, candidate favoriteExportFileCandidate) favoriteExportFileResolution {
			if candidate.ProjectID == "" {
				return favoriteExportFileResolution{Reason: exportReasonNoModrinthSource}
			}
			if providerConfigErr != nil {
				return favoriteExportFileResolution{Reason: exportReasonExternalAPIError, Detail: "provider configuration unavailable"}
			}
			file, reason, detail := s.resolveFavoriteModrinthProjectFile(resolveContext, candidate.ProjectID, request.MinecraftVersion, request.Loader, providerConfig)
			return favoriteExportFileResolution{File: file, Reason: reason, Detail: detail}
		})
		if resolveErr != nil {
			return preview, resolveErr
		}
		for index, candidate := range candidates {
			resolution := resolutions[index]
			item := &preview.Items[candidate.ItemIndex]
			if resolution.Reason != "" {
				item.ResultType, item.ReasonCode, item.ReasonDetail = "skipped", resolution.Reason, resolution.Detail
				if resolution.Reason == exportReasonExternalAPIError || resolution.Reason == exportReasonInternalProcessingError {
					item.ResultType = "failed"
				}
			} else {
				populateFavoriteExportFile(item, resolution.File, request)
			}
		}
	}
	preview.Items, err = s.appendFavoriteExportDependencies(ctx, preview.Items, request, providerConfig)
	if err != nil {
		return preview, err
	}
	markFavoriteExportPathConflicts(preview.Items)
	for _, item := range preview.Items {
		switch item.ResultType {
		case "exported":
			preview.ExportedModCount++
		case "auto_dependency":
			preview.AutoDependencyCount++
		case "skipped":
			preview.SkippedItemCount++
		case "failed":
			preview.FailedItemCount++
		}
	}
	return preview, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func populateFavoriteExportFile(item *favoriteModpackExportItem, file providerProjectFile, request favoriteModpackExportRequest) {
	item.ResultType = "exported"
	item.ModrinthProjectID, item.ModrinthVersionID = file.ProviderProjectID, file.ProviderVersionID
	item.SelectedVersionName, item.SelectedFileName = file.VersionName, file.FileName
	item.MinecraftVersion, item.Loader, item.ReleaseType = request.MinecraftVersion, request.Loader, file.ReleaseChannel
	item.EnvironmentClient, item.EnvironmentServer = firstNonEmpty(file.ClientEnvironment, "required"), firstNonEmpty(file.ServerEnvironment, "required")
	item.FileSize, item.SHA1, item.SHA512, item.DownloadURL = file.SizeBytes, file.SHA1, file.SHA512, file.DirectURL
}

func markFavoriteExportPathConflicts(items []favoriteModpackExportItem) {
	pathGroups := make(map[string][]int)
	for index := range items {
		if items[index].ResultType != "exported" && items[index].ResultType != "auto_dependency" {
			continue
		}
		key, err := portableMRPackFilePathKey("mods/" + items[index].SelectedFileName)
		if err != nil {
			items[index].ResultType = "failed"
			items[index].ReasonCode = exportReasonNoCompatibleFile
			items[index].ReasonDetail = "unsafe JAR file path"
			continue
		}
		pathGroups[key] = append(pathGroups[key], index)
	}
	for _, indices := range pathGroups {
		if len(indices) < 2 {
			continue
		}
		for _, index := range indices {
			items[index].ResultType = "failed"
			items[index].ReasonCode = exportReasonFilePathConflict
			items[index].ReasonDetail = fmt.Sprintf("portable file path conflicts with %d other selected file(s)", len(indices)-1)
		}
	}
}

func favoriteExportHasPathConflict(items []favoriteModpackExportItem) bool {
	for _, item := range items {
		if item.ReasonCode == exportReasonFilePathConflict {
			return true
		}
	}
	return false
}

func (s *Server) resolveFavoriteModrinthProjectFile(ctx context.Context, projectID, minecraftVersion, loader string, cfg modImportConfig) (providerProjectFile, string, string) {
	files, err := s.cachedProviderProjectFiles(ctx, "modrinth", projectID, cfg)
	if err != nil {
		return providerProjectFile{}, exportReasonExternalAPIError, "Modrinth API unavailable"
	}
	versionMatch, loaderMatch := false, false
	candidates := make([]providerProjectFile, 0)
	for _, file := range files {
		if !containsFold(file.GameVersions, minecraftVersion) {
			continue
		}
		versionMatch = true
		if !containsFold(file.Loaders, loader) {
			continue
		}
		loaderMatch = true
		candidates = append(candidates, file)
	}
	if !versionMatch {
		return providerProjectFile{}, exportReasonNoCompatibleMinecraftVersion, minecraftVersion
	}
	if !loaderMatch {
		return providerProjectFile{}, exportReasonNoCompatibleLoader, loader
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Primary != candidates[j].Primary {
			return candidates[i].Primary
		}
		left, right := releaseChannelRank(candidates[i].ReleaseChannel), releaseChannelRank(candidates[j].ReleaseChannel)
		if left != right {
			return left < right
		}
		if !candidates[i].PublishedAt.Equal(candidates[j].PublishedAt) {
			return candidates[i].PublishedAt.After(candidates[j].PublishedAt)
		}
		return candidates[i].FileName < candidates[j].FileName
	})
	for _, file := range candidates {
		_, pathErr := portableMRPackFilePathKey("mods/" + file.FileName)
		if pathErr == nil && file.SHA1 != "" && file.SHA512 != "" && file.SizeBytes > 0 && verifiedModrinthDownloadURL(file.DirectURL, projectID, file.ProviderVersionID) {
			return file, "", ""
		}
	}
	if len(candidates) == 0 {
		return providerProjectFile{}, exportReasonNoCompatibleFile, ""
	}
	file := candidates[0]
	if file.SHA1 == "" {
		return providerProjectFile{}, exportReasonMissingSHA1, ""
	}
	if file.SHA512 == "" {
		return providerProjectFile{}, exportReasonMissingSHA512, ""
	}
	if file.SizeBytes <= 0 {
		return providerProjectFile{}, exportReasonMissingFileSize, ""
	}
	if file.DirectURL == "" {
		return providerProjectFile{}, exportReasonMissingDownloadURL, ""
	}
	return providerProjectFile{}, exportReasonNoCompatibleFile, "no safe JAR file"
}

func containsFold(values []string, expected string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), expected) {
			return true
		}
	}
	return false
}
func releaseChannelRank(value string) int {
	if value == "release" {
		return 0
	}
	if value == "beta" {
		return 1
	}
	return 2
}
func verifiedModrinthDownloadURL(raw, projectID, versionID string) bool {
	downloadProjectID, downloadVersionID, ok := modrinthDownloadIdentity(raw)
	return ok && downloadProjectID == projectID && downloadVersionID == versionID
}
func int64Pointer(value int64) *int64                       { return &value }
func hasInt64Key(values map[int64]struct{}, key int64) bool { _, ok := values[key]; return ok }

func nullableFavoriteCollectionID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func writeFavoriteExportError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "FAVORITE_COLLECTION_NOT_FOUND", "favorite collection was not found", 0, nil)
		return
	}
	if errors.Is(err, errFavoriteExportDependencyLimit) {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_DEPENDENCY_LIMIT", "the required dependency graph exceeds the export limit", 0, nil)
		return
	}
	if errors.Is(err, errMinecraftVersionNotAuthoritative) {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_INVALID_MINECRAFT_VERSION", "the Minecraft version is not enabled for the selected loader", 0, nil)
		return
	}
	if errors.Is(err, errMinecraftLoaderArtifactUnavailable) {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_LOADER_SNAPSHOT_UNAVAILABLE", "the selected loader is not available in the synchronized Minecraft catalog", 0, nil)
		return
	}
	if errors.Is(err, errMinecraftVersionConfigUnavailable) || errors.Is(err, errMinecraftLoaderArtifactAuthorityUnavailable) {
		writeAPIError(w, http.StatusInternalServerError, "MODPACK_EXPORT_CATALOG_UNAVAILABLE", "the synchronized Minecraft catalog is unavailable", 0, nil)
		return
	}
	if errors.Is(err, errFavoriteModpackExportPreviewAuthority) {
		writeAPIError(w, http.StatusInternalServerError, "MODPACK_EXPORT_PREVIEW_UNAVAILABLE", "failed to preserve the export preview", 0, nil)
		return
	}
	writeAPIError(w, http.StatusBadGateway, "MODPACK_EXPORT_PREFLIGHT_FAILED", "failed to inspect the collection", 0, nil)
}

func writeFavoriteExportPreviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errFavoriteModpackExportPreviewExpired):
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_PREVIEW_EXPIRED", "the export preview expired; inspect the collection again", 0, nil)
	case errors.Is(err, errFavoriteModpackExportPreviewConsumed):
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_PREVIEW_CONSUMED", "the export preview was already used", 0, nil)
	case errors.Is(err, errFavoriteModpackExportPreviewNotFound):
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_PREVIEW_NOT_FOUND", "the export preview is no longer available", 0, nil)
	case errors.Is(err, errFavoriteModpackExportPreviewMismatch):
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_PREVIEW_MISMATCH", "the export settings no longer match the confirmed preview", 0, nil)
	default:
		writeAPIError(w, http.StatusInternalServerError, "MODPACK_EXPORT_PREVIEW_UNAVAILABLE", "failed to read the confirmed export preview", 0, nil)
	}
}
