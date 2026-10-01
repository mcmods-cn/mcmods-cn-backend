package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
)

const favoriteModpackExportTaskCode = "favorite_modpack_export"

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
	exportReasonProjectHidden                = "PROJECT_HIDDEN"
	exportReasonDuplicateProject             = "DUPLICATE_PROJECT"
	exportReasonExternalAPIError             = "EXTERNAL_API_ERROR"
	exportReasonInternalProcessingError      = "INTERNAL_PROCESSING_ERROR"
)

type favoriteModpackExportRequest struct {
	MinecraftVersion      string `json:"minecraftVersion"`
	Loader                string `json:"loader"`
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

type favoriteModpackExportPreview struct {
	CollectionID        int64                       `json:"-"`
	CollectionPublicID  string                      `json:"collectionId"`
	CollectionName      string                      `json:"collectionName"`
	MinecraftVersion    string                      `json:"minecraftVersion"`
	Loader              string                      `json:"loader"`
	LoaderVersion       string                      `json:"loaderVersion"`
	CollectionItemCount int                         `json:"collectionItemCount"`
	ExportedModCount    int                         `json:"exportedModCount"`
	AutoDependencyCount int                         `json:"autoDependencyCount"`
	SkippedItemCount    int                         `json:"skippedItemCount"`
	FailedItemCount     int                         `json:"failedItemCount"`
	Items               []favoriteModpackExportItem `json:"items"`
}

type favoriteModpackExportMessage struct {
	TaskID string `json:"taskId"`
}

func (s *Server) preflightFavoriteModpackExport(w http.ResponseWriter, r *http.Request) {
	request, ok := decodeFavoriteModpackExportRequest(w, r)
	if !ok {
		return
	}
	preview, err := s.buildFavoriteModpackExportPreview(r.Context(), currentClaims(r).Subject, r.PathValue("id"), request)
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
	preview, err := s.buildFavoriteModpackExportPreview(r.Context(), currentClaims(r).Subject, r.PathValue("id"), request)
	if err != nil {
		writeFavoriteExportError(w, err)
		return
	}
	if preview.ExportedModCount+preview.AutoDependencyCount == 0 {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_EMPTY", "no compatible Mod can be exported", 0, nil)
		return
	}
	if preview.SkippedItemCount+preview.FailedItemCount > 0 && (!request.ExportCompatibleOnly || !request.ConfirmCompatibleOnly) {
		writeAPIError(w, http.StatusConflict, "MODPACK_EXPORT_COMPATIBLE_ONLY_CONFIRMATION_REQUIRED", "confirm exporting only compatible projects", 0, map[string]any{"preview": preview})
		return
	}
	userID := currentClaims(r).Subject
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create export task")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtextextended('favorite-export-quota:'||$1::bigint::text,0))`, userID); err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to lock export limits")
		return
	}
	var active, daily, recentDuplicate int
	if err = tx.QueryRow(r.Context(), `select
		count(*) filter(where status in ('pending','processing')),
		count(*) filter(where created_at>=now()-interval '24 hours'),
		count(*) filter(where collection_id=$2 and minecraft_version=$3 and loader_type=$4 and created_at>=now()-interval '30 seconds')
		from favorite_modpack_export_tasks where owner_user_id=$1`, userID, preview.CollectionID, preview.MinecraftVersion, preview.Loader).Scan(&active, &daily, &recentDuplicate); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check export limits")
		return
	}
	if active >= favoriteExportMaxActive(s.cfg.FavoriteExport.MaxActivePerUser) {
		writeAPIError(w, http.StatusTooManyRequests, "MODPACK_EXPORT_CONCURRENCY_LIMIT", "too many exports are already processing", 60, nil)
		return
	}
	if daily >= favoriteExportDailyLimit(s.cfg.FavoriteExport.MaxDailyPerUser) {
		writeAPIError(w, http.StatusTooManyRequests, "MODPACK_EXPORT_DAILY_LIMIT", "daily export limit reached", 3600, nil)
		return
	}
	if recentDuplicate > 0 {
		writeAPIError(w, http.StatusTooManyRequests, "MODPACK_EXPORT_DUPLICATE_COOLDOWN", "wait before repeating the same export", 30, nil)
		return
	}
	// Persist the pack version with the task so dispatcher retries produce the
	// same index, while separate exports receive a readable revision identifier.
	packVersion := time.Now().UTC().Format("2006.01.02-150405")
	var taskID string
	err = tx.QueryRow(r.Context(), `insert into favorite_modpack_export_tasks(owner_user_id,collection_id,pack_name,pack_version_id,
		minecraft_version,loader_type,loader_version,allow_compatible_only,collection_item_count,exported_mod_count,
		auto_dependency_count,skipped_item_count,failed_item_count,final_file_count)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) returning public_id`, userID, preview.CollectionID,
		preview.CollectionName, packVersion, preview.MinecraftVersion, preview.Loader, preview.LoaderVersion, request.ExportCompatibleOnly,
		preview.CollectionItemCount, preview.ExportedModCount, preview.AutoDependencyCount, preview.SkippedItemCount,
		preview.FailedItemCount, preview.ExportedModCount+preview.AutoDependencyCount).Scan(&taskID)
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

func decodeFavoriteModpackExportRequest(w http.ResponseWriter, r *http.Request) (favoriteModpackExportRequest, bool) {
	var request favoriteModpackExportRequest
	if decodeJSON(r, &request) != nil {
		writeAPIError(w, http.StatusBadRequest, "MODPACK_EXPORT_INVALID_REQUEST", "invalid export settings", 0, nil)
		return request, false
	}
	request.MinecraftVersion = strings.TrimSpace(request.MinecraftVersion)
	request.Loader = strings.ToLower(strings.TrimSpace(request.Loader))
	if request.MinecraftVersion == "" || strings.HasSuffix(strings.ToUpper(request.MinecraftVersion), ".X") {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_EXACT_VERSION_REQUIRED", "select one exact Minecraft version", 0, nil)
		return request, false
	}
	if _, err := mrpackLoaderDependencyKey(request.Loader); err != nil {
		writeAPIError(w, http.StatusUnprocessableEntity, "MODPACK_EXPORT_INVALID_LOADER", "loader must be NeoForge, Fabric, or Forge", 0, nil)
		return request, false
	}
	return request, true
}

func (s *Server) buildFavoriteModpackExportPreview(ctx context.Context, actorID int64, collectionID string, request favoriteModpackExportRequest) (favoriteModpackExportPreview, error) {
	collectionID = strings.ToLower(strings.TrimSpace(collectionID))
	var preview favoriteModpackExportPreview
	preview.CollectionPublicID, preview.MinecraftVersion, preview.Loader = collectionID, request.MinecraftVersion, request.Loader
	if len(collectionID) != 9 {
		return preview, pgx.ErrNoRows
	}
	if err := s.db.QueryRow(ctx, `select id,name from favorite_collections where public_id=$1 and (user_id=$2 or is_public)`, collectionID, actorID).Scan(&preview.CollectionID, &preview.CollectionName); err != nil {
		return preview, err
	}
	loaderVersion, err := resolveMRPackLoaderVersion(ctx, request.MinecraftVersion, request.Loader)
	if err != nil {
		return preview, fmt.Errorf("loader version: %w", err)
	}
	preview.LoaderVersion = loaderVersion
	rows, err := s.db.Query(ctx, `select item.id,
		item.entity_type,item.entity_id,route.id,route.public_id,coalesce(mods.primary_name,coalesce(modpacks.primary_name,'')),
		coalesce(mods.review_status,coalesce(modpacks.review_status,''))
		from favorite_collection_items item join public_routes route on route.entity_type=item.entity_type and route.internal_id=item.entity_id
		left join mods on item.entity_type='mod' and mods.id=item.entity_id
		left join modpacks on item.entity_type='modpack' and modpacks.id=item.entity_id
		where item.collection_id=$1 order by item.created_at,item.entity_type,item.entity_id`, preview.CollectionID)
	if err != nil {
		return preview, err
	}
	defer rows.Close()
	type collectedItem struct {
		id, internalID, routeID            int64
		entityType, publicID, name, status string
	}
	collected := make([]collectedItem, 0)
	for rows.Next() {
		var item collectedItem
		if err = rows.Scan(&item.id, &item.entityType, &item.internalID, &item.routeID, &item.publicID, &item.name, &item.status); err != nil {
			return preview, err
		}
		collected = append(collected, item)
	}
	if err = rows.Err(); err != nil {
		return preview, err
	}
	rows.Close()
	seen := map[int64]struct{}{}
	for _, collected := range collected {
		itemID, internalID, routeID := collected.id, collected.internalID, collected.routeID
		entityType, publicID, name, status := collected.entityType, collected.publicID, collected.name, collected.status
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
			file, reason, detail := s.resolveFavoriteModrinthFile(ctx, routeID, request.MinecraftVersion, request.Loader)
			if reason != "" {
				item.ResultType, item.ReasonCode, item.ReasonDetail = "skipped", reason, detail
				if reason == exportReasonExternalAPIError || reason == exportReasonInternalProcessingError {
					item.ResultType = "failed"
				}
			} else {
				populateFavoriteExportFile(&item, file, request)
			}
		}
		preview.Items = append(preview.Items, item)
	}
	if err = rows.Err(); err != nil {
		return preview, err
	}
	preview.CollectionItemCount = len(preview.Items)
	preview.Items, err = s.appendFavoriteExportDependencies(ctx, preview.Items, request)
	if err != nil {
		return preview, err
	}
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

// appendFavoriteExportDependencies follows only explicit dependency
// relationships that resolve to a real on-site Mod. Optional/name-only
// relationships are deliberately not guessed.
func (s *Server) appendFavoriteExportDependencies(ctx context.Context, items []favoriteModpackExportItem, request favoriteModpackExportRequest) ([]favoriteModpackExportItem, error) {
	knownRoutes := make(map[int64]int)
	queueRoutes := make([]int64, 0)
	for index := range items {
		if items[index].SourceProjectRouteID != nil && items[index].ResultType == "exported" {
			knownRoutes[*items[index].SourceProjectRouteID] = index
			queueRoutes = append(queueRoutes, *items[index].SourceProjectRouteID)
		}
	}
	for cursor := 0; cursor < len(queueRoutes) && cursor < 100; cursor++ {
		sourceRouteID := queueRoutes[cursor]
		sourceIndex := knownRoutes[sourceRouteID]
		rows, err := s.db.Query(ctx, `select dependent_route.id,dependent_route.public_id,dependent.primary_name
			from public_routes source_route join mod_relationships relationship
			  on source_route.entity_type='mod' and relationship.mod_id=source_route.internal_id
			join mods dependent on dependent.id=relationship.related_mod_id and dependent.review_status='approved'
			join public_routes dependent_route on dependent_route.entity_type='mod' and dependent_route.internal_id=dependent.id
			left join mod_relationship_groups relation_group on relation_group.id=relationship.group_id
			where source_route.id=$1 and relationship.relation_type='dependency'
			  and (relation_group.id is null or cardinality(relation_group.minecraft_versions)=0 or $2=any(relation_group.minecraft_versions))
			  and (relation_group.id is null or relation_group.loader='' or lower(relation_group.loader)=lower($3))
			order by relationship.display_order,relationship.id`, sourceRouteID, request.MinecraftVersion, request.Loader)
		if err != nil {
			return nil, err
		}
		type dependency struct {
			routeID        int64
			publicID, name string
		}
		dependencies := make([]dependency, 0)
		for rows.Next() {
			var dependent dependency
			if err = rows.Scan(&dependent.routeID, &dependent.publicID, &dependent.name); err != nil {
				rows.Close()
				return nil, err
			}
			dependencies = append(dependencies, dependent)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for _, dependent := range dependencies {
			routeID, publicID, name := dependent.routeID, dependent.publicID, dependent.name
			if existingIndex, exists := knownRoutes[routeID]; exists {
				items[existingIndex].DependencyOf = appendUniqueString(items[existingIndex].DependencyOf, items[sourceIndex].SourceProjectName)
				continue
			}
			item := favoriteModpackExportItem{SourceProjectRouteID: int64Pointer(routeID), SourceProjectID: publicID,
				SourceProjectType: "mod", SourceProjectName: name, ResultType: "auto_dependency",
				DependencyOf: []string{items[sourceIndex].SourceProjectName}}
			file, reason, detail := s.resolveFavoriteModrinthFile(ctx, routeID, request.MinecraftVersion, request.Loader)
			if reason != "" {
				item.ResultType, item.ReasonCode, item.ReasonDetail = "failed", exportReasonRequiredDependencyUnresolved, firstNonEmpty(detail, reason)
			} else {
				populateFavoriteExportFile(&item, file, request)
				item.ResultType = "auto_dependency"
				queueRoutes = append(queueRoutes, routeID)
			}
			knownRoutes[routeID] = len(items)
			items = append(items, item)
		}
		rows.Close()
	}
	if len(queueRoutes) > 100 {
		return nil, errors.New("favorite export dependency graph exceeds processing limit")
	}
	return items, nil
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

func (s *Server) resolveFavoriteModrinthFile(ctx context.Context, routeID int64, minecraftVersion, loader string) (providerProjectFile, string, string) {
	var projectID string
	if err := s.db.QueryRow(ctx, `select external_project_id from project_external_sources where project_route_id=$1 and source_type='modrinth'`, routeID).Scan(&projectID); errors.Is(err, pgx.ErrNoRows) {
		return providerProjectFile{}, exportReasonNoModrinthSource, ""
	} else if err != nil {
		return providerProjectFile{}, exportReasonInternalProcessingError, ""
	}
	cfg, err := s.modImportConfigFromSettings(ctx)
	if err != nil {
		return providerProjectFile{}, exportReasonExternalAPIError, "provider configuration unavailable"
	}
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
		if strings.HasSuffix(strings.ToLower(file.FileName), ".jar") && file.SHA1 != "" && file.SHA512 != "" && file.SizeBytes > 0 && verifiedModrinthDownloadURL(file.DirectURL, projectID, file.ProviderVersionID) {
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
	return strings.HasPrefix(raw, "https://cdn.modrinth.com/") && strings.Contains(raw, "/data/"+projectID+"/versions/"+versionID+"/")
}
func int64Pointer(value int64) *int64                       { return &value }
func hasInt64Key(values map[int64]struct{}, key int64) bool { _, ok := values[key]; return ok }

func writeFavoriteExportError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "FAVORITE_COLLECTION_NOT_FOUND", "favorite collection was not found", 0, nil)
		return
	}
	writeAPIError(w, http.StatusBadGateway, "MODPACK_EXPORT_PREFLIGHT_FAILED", "failed to inspect the collection", 0, nil)
}
