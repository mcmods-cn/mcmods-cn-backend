package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type projectFileContext struct {
	ProjectType         string
	ProjectID           string
	ProjectInternalID   int64
	SiteID              string
	ReviewStatus        string
	ModrinthProjectID   string
	CurseForgeProjectID string
}

type projectFileItem struct {
	internalID          int64
	ID                  string    `json:"id"`
	Source              string    `json:"source"`
	DisplayName         string    `json:"displayName"`
	FileName            string    `json:"fileName"`
	VersionName         string    `json:"versionName"`
	ReleaseChannel      string    `json:"releaseChannel"`
	GameVersions        []string  `json:"gameVersions"`
	Loaders             []string  `json:"loaders"`
	PublishedAt         time.Time `json:"publishedAt"`
	SizeBytes           int64     `json:"sizeBytes"`
	DownloadCount       int64     `json:"downloadCount"`
	DownloadCountSource string    `json:"downloadCountSource"`
	SHA1                string    `json:"sha1,omitempty"`
	SHA256              string    `json:"sha256,omitempty"`
	SHA512              string    `json:"sha512,omitempty"`
	ScanStatus          string    `json:"scanStatus,omitempty"`
	DownloadPath        string    `json:"downloadPath"`
}

type providerProjectFile struct {
	ID                string    `json:"id"`
	Source            string    `json:"source"`
	DisplayName       string    `json:"displayName"`
	FileName          string    `json:"fileName"`
	VersionName       string    `json:"versionName"`
	ReleaseChannel    string    `json:"releaseChannel"`
	GameVersions      []string  `json:"gameVersions"`
	Loaders           []string  `json:"loaders"`
	PublishedAt       time.Time `json:"publishedAt"`
	SizeBytes         int64     `json:"sizeBytes"`
	DownloadCount     int64     `json:"downloadCount"`
	SHA1              string    `json:"sha1,omitempty"`
	SHA512            string    `json:"sha512,omitempty"`
	DirectURL         string    `json:"directUrl,omitempty"`
	ProviderProjectID string    `json:"providerProjectId,omitempty"`
	ProviderVersionID string    `json:"providerVersionId,omitempty"`
	ClientEnvironment string    `json:"clientEnvironment,omitempty"`
	ServerEnvironment string    `json:"serverEnvironment,omitempty"`
	Primary           bool      `json:"primary,omitempty"`
}

type createProjectFileRequest struct {
	OSSFileID      string   `json:"ossFileId"`
	DisplayName    string   `json:"displayName"`
	VersionName    string   `json:"versionName"`
	ReleaseChannel string   `json:"releaseChannel"`
	GameVersions   []string `json:"gameVersions"`
	Loaders        []string `json:"loaders"`
}

func (s *Server) projectFiles(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectFileContext(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	canUpload := s.canUploadProjectFile(r.Context(), currentClaims(r).Subject, project.ProjectID)
	if project.ReviewStatus != "approved" && !canUpload {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if r.Method == http.MethodPost {
		if !canUpload {
			writeError(w, http.StatusForbidden, "project file upload permission is required")
			return
		}
		s.createProjectFile(w, r, project)
		return
	}

	request, err := parseProjectFilePageRequest(r.URL.Query(), project)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, cfgErr := s.modImportConfigFromSettings(r.Context())
	warnings := map[string]string{}
	providers := map[string]bool{
		"internal": true, "modrinth": project.ModrinthProjectID != "",
		"curseforge": project.CurseForgeProjectID != "" && cfgErr == nil && cfg.CurseForge.APIKey != "",
	}
	page := projectFilePage{Items: []projectFileItem{}}
	switch request.Source {
	case "internal":
		page, err = s.internalProjectFilePage(r.Context(), project, request)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load on-site project files")
			return
		}
	case "modrinth":
		if cfgErr != nil {
			warnings["providers"] = "external download sources are temporarily unavailable"
		} else if project.ModrinthProjectID != "" {
			page, err = s.loadModrinthProjectFilePage(r.Context(), project, request, cfg)
			if err != nil {
				warnings["modrinth"] = "Modrinth file list is temporarily unavailable"
				page = projectFilePage{Items: []projectFileItem{}}
			}
		}
	case "curseforge":
		if cfgErr != nil {
			warnings["providers"] = "external download sources are temporarily unavailable"
		} else if project.CurseForgeProjectID != "" && cfg.CurseForge.APIKey == "" {
			warnings["curseforge"] = "CurseForge API key is not configured"
		} else if project.CurseForgeProjectID != "" {
			page, err = s.loadCurseForgeProjectFilePage(r.Context(), project, request, cfg)
			if err != nil {
				warnings["curseforge"] = "CurseForge file list is temporarily unavailable"
				page = projectFilePage{Items: []projectFileItem{}}
			}
		}
	}
	versionConfig, versionErr := loadMinecraftVersionConfig(r.Context(), s.db)
	if versionErr != nil {
		writeError(w, http.StatusInternalServerError, "failed to load Minecraft version settings")
		return
	}
	versions, loaders := projectFileFilters(page.Items, minecraftVersionOrder(versionConfig))
	writeBoundedCatalogJSON(w, map[string]any{
		"items": page.Items, "source": request.Source, "limit": request.Limit,
		"hasMore": page.HasMore, "nextCursor": page.NextCursor,
		"versions": versions, "loaders": loaders,
		"providers": providers, "warnings": warnings, "canUpload": canUpload,
		"uploadPermission": "project.download.upload." + project.ProjectID,
	})
}

func (s *Server) projectFile(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectFileContext(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if !s.canUploadProjectFile(r.Context(), currentClaims(r).Subject, project.ProjectID) {
		writeError(w, http.StatusForbidden, "project file upload permission is required")
		return
	}
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("fileId")))
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start project file deletion")
		return
	}
	defer tx.Rollback(r.Context())
	var previousStatus string
	var publicationGeneration int
	err = tx.QueryRow(r.Context(), `select status,publication_generation from project_files
		where public_id=$1 and project_type=$2 and project_internal_id=$3 and status<>'deleted' for update`,
		publicID, project.ProjectType, project.ProjectInternalID).Scan(&previousStatus, &publicationGeneration)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read project file")
		return
	}
	if _, err = tx.Exec(r.Context(), `update project_files set status='deleted',updated_at=now() where public_id=$1`, publicID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project file")
		return
	}
	_, _ = tx.Exec(r.Context(), `insert into audit_events(aggregate_type,aggregate_key,actor_id,action,ip,user_agent,metadata)
		values('project_file',$1,$2,'delete',$3,$4,jsonb_build_object('projectType',$5,'projectId',$6))`,
		publicID, currentClaims(r).Subject, s.requestClientLocation(r).IP, r.UserAgent(), project.ProjectType, project.ProjectID)
	if project.ReviewStatus == "approved" && previousStatus == "active" {
		if err = enqueueProjectFileUpdateEventTx(r.Context(), tx, project, currentClaims(r).Subject,
			"download_removed", publicID, publicationGeneration); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to enqueue project update")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project file deletion")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) createProjectFile(w http.ResponseWriter, r *http.Request, project projectFileContext) {
	var request createProjectFileRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid project file metadata")
		return
	}
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	request.VersionName = strings.TrimSpace(request.VersionName)
	request.ReleaseChannel = strings.ToLower(strings.TrimSpace(request.ReleaseChannel))
	request.Loaders = normalizeLoaders(request.Loaders)
	request.OSSFileID = strings.ToLower(strings.TrimSpace(request.OSSFileID))
	if !validCatalogPublicID(request.OSSFileID) || request.VersionName == "" || len(request.GameVersions) == 0 || projectFileRequiresLoader(project.ProjectType) && len(request.Loaders) == 0 {
		writeError(w, http.StatusBadRequest, "file, version, game version, and applicable loader are required")
		return
	}
	if request.ReleaseChannel != "release" && request.ReleaseChannel != "beta" && request.ReleaseChannel != "alpha" {
		writeError(w, http.StatusBadRequest, "invalid release channel")
		return
	}
	if len(request.DisplayName) > 200 || len(request.VersionName) > 120 {
		writeError(w, http.StatusBadRequest, "project file metadata is too long")
		return
	}
	gameVersions, versionErr := authoritativeMinecraftVersionCodes(r.Context(), s.db, request.GameVersions, 100)
	if versionErr != nil || len(gameVersions) == 0 {
		if versionErr != nil && !errors.Is(versionErr, errInvalidMinecraftVersionCodes) && !errors.Is(versionErr, errUnknownMinecraftVersionCodes) {
			writeError(w, http.StatusInternalServerError, "failed to load Minecraft version settings")
			return
		}
		writeError(w, http.StatusBadRequest, "unknown or invalid Minecraft version")
		return
	}
	request.GameVersions = gameVersions
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start project file creation")
		return
	}
	defer tx.Rollback(r.Context())
	var fileName, contentType, sha256, category, scanStatus string
	var fileID, sizeBytes, uploaderID int64
	err = tx.QueryRow(r.Context(), `select id,original_name,content_type,size_bytes,sha256,uploader_id,category,scan_status
		from oss_files where public_id=$1 and status='active'
		  and scan_status in ('pending','clean','trusted_generated') for update`, request.OSSFileID).
		Scan(&fileID, &fileName, &contentType, &sizeBytes, &sha256, &uploaderID, &category, &scanStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "uploaded OSS file not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to inspect uploaded file")
		return
	}
	expectedCategory := ossProjectReleaseCategory(project.ProjectType, project.ProjectID)
	extension := strings.ToLower(filepath.Ext(fileName))
	validExtension := projectFileExtensionAllowed(project.ProjectType, extension)
	if uploaderID != currentClaims(r).Subject || category != expectedCategory || !validExtension {
		writeError(w, http.StatusBadRequest, "the uploaded file does not belong to this project or has an unsupported format")
		return
	}
	if request.DisplayName == "" {
		request.DisplayName = fileName
	}
	publicationStatus := projectFileStatusForOSSScan(scanStatus)
	publicationGeneration := 0
	if publicationStatus == "active" {
		publicationGeneration = 1
	}
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into project_files(
		project_type,project_internal_id,oss_file_id,display_name,version_name,release_channel,game_versions,loaders,
		file_name,content_type,size_bytes,sha256,uploaded_by,status,publication_generation)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) returning public_id`,
		project.ProjectType, project.ProjectInternalID, fileID, request.DisplayName, request.VersionName,
		request.ReleaseChannel, request.GameVersions, request.Loaders, fileName, contentType, sizeBytes, sha256,
		currentClaims(r).Subject, publicationStatus, publicationGeneration).Scan(&publicID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this uploaded file is already attached to the project")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create project file")
		return
	}
	metadata, _ := json.Marshal(map[string]any{"projectType": project.ProjectType, "projectId": project.ProjectID, "ossFileId": request.OSSFileID})
	_, _ = tx.Exec(r.Context(), `insert into audit_events(aggregate_type,aggregate_key,actor_id,action,ip,user_agent,metadata)
		values('project_file',$1,$2,'create',$3,$4,$5::jsonb)`, publicID, currentClaims(r).Subject, s.requestClientLocation(r).IP, r.UserAgent(), metadata)
	if project.ReviewStatus == "approved" && publicationStatus == "active" {
		if err = enqueueProjectFileUpdateEventTx(r.Context(), tx, project, currentClaims(r).Subject,
			"download_added", publicID, publicationGeneration); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to enqueue project update")
			return
		}
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit project file creation")
		return
	}
	file, err := s.internalProjectFileByPublicID(r.Context(), project, publicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read created project file")
		return
	}
	writeJSON(w, http.StatusCreated, file)
}

func enqueueProjectFileUpdateEventTx(ctx context.Context, tx pgx.Tx, project projectFileContext, actorID int64,
	updateKind, filePublicID string, publicationGeneration int,
) error {
	var routeID int64
	if err := tx.QueryRow(ctx, `select id from public_routes where entity_type=$1 and internal_id=$2`,
		project.ProjectType, project.ProjectInternalID).Scan(&routeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return enqueueProjectFileUpdateEventByRouteTx(ctx, tx, routeID, actorID, updateKind, filePublicID, publicationGeneration)
}

func enqueueProjectFileUpdateEventByRouteTx(ctx context.Context, tx pgx.Tx, routeID, actorID int64,
	updateKind, filePublicID string, publicationGeneration int,
) error {
	publicationBatchID := fmt.Sprintf("project-file:%s:%s:%d", updateKind, filePublicID, publicationGeneration)
	return enqueueProjectUpdateEventTx(ctx, tx, routeID, 0, actorID, updateKind,
		[]string{"download_files"}, publicationBatchID)
}

func projectFileTargetIsApprovedTx(ctx context.Context, tx pgx.Tx, projectType string, projectInternalID int64) (bool, error) {
	var query string
	switch projectType {
	case "mod":
		query = `select review_status='approved' from mods where id=$1 for share`
	case "modpack":
		query = `select review_status='approved' from modpacks where id=$1 for share`
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		query = `select review_status='approved' from simple_projects where id=$1 and project_type=$2 for share`
	default:
		return false, fmt.Errorf("unsupported project file target type %q", projectType)
	}
	var approved bool
	var err error
	if projectType == "mod" || projectType == "modpack" {
		err = tx.QueryRow(ctx, query, projectInternalID).Scan(&approved)
	} else {
		err = tx.QueryRow(ctx, query, projectInternalID, projectType).Scan(&approved)
	}
	return approved, err
}

func projectFileStatusForOSSScan(scanStatus string) string {
	switch scanStatus {
	case "clean", "trusted_generated":
		return "active"
	case "rejected":
		return "rejected"
	default:
		return "processing"
	}
}

func synchronizeProjectFilesForOSSScanTx(ctx context.Context, tx pgx.Tx, ossFileID, scanActorID int64, scanStatus string) error {
	rows, err := tx.Query(ctx, `select project_file.id,project_file.public_id,project_file.status,
		project_file.publication_generation,coalesce(project_file.uploaded_by,0),route.id,
		coalesce(target_mod.review_status,target_modpack.review_status,target_simple.review_status,'')='approved'
		from project_files project_file
		join public_routes route on route.entity_type=project_file.project_type and route.internal_id=project_file.project_internal_id
		left join mods target_mod on route.entity_type='mod' and target_mod.id=route.internal_id
		left join modpacks target_modpack on route.entity_type='modpack' and target_modpack.id=route.internal_id
		left join simple_projects target_simple on route.entity_type=target_simple.project_type and target_simple.id=route.internal_id
		where project_file.oss_file_id=$1 and project_file.status<>'deleted'
		order by project_file.id for update of project_file`, ossFileID)
	if err != nil {
		return err
	}
	type transition struct {
		id, uploaderID, routeID    int64
		publicID, previousStatus   string
		publicationGeneration      int
		projectCurrentlyIsApproved bool
	}
	transitions := make([]transition, 0, 1)
	for rows.Next() {
		var item transition
		if err = rows.Scan(&item.id, &item.publicID, &item.previousStatus, &item.publicationGeneration,
			&item.uploaderID, &item.routeID, &item.projectCurrentlyIsApproved); err != nil {
			rows.Close()
			return err
		}
		transitions = append(transitions, item)
	}
	if err = finishRows(rows); err != nil {
		return err
	}
	targetStatus := projectFileStatusForOSSScan(scanStatus)
	for _, item := range transitions {
		if item.previousStatus == targetStatus {
			continue
		}
		nextGeneration := item.publicationGeneration
		if targetStatus == "active" {
			nextGeneration++
		}
		if _, err = tx.Exec(ctx, `update project_files set status=$2,publication_generation=$3,updated_at=now()
			where id=$1`, item.id, targetStatus, nextGeneration); err != nil {
			return err
		}
		if !item.projectCurrentlyIsApproved {
			continue
		}
		if item.previousStatus != "active" && targetStatus == "active" {
			actorID := item.uploaderID
			if actorID == 0 {
				actorID = scanActorID
			}
			if err = enqueueProjectFileUpdateEventByRouteTx(ctx, tx, item.routeID, actorID,
				"download_added", item.publicID, nextGeneration); err != nil {
				return err
			}
		} else if item.previousStatus == "active" && targetStatus != "active" {
			if err = enqueueProjectFileUpdateEventByRouteTx(ctx, tx, item.routeID, scanActorID,
				"download_removed", item.publicID, item.publicationGeneration); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) createProjectFileUpload(w http.ResponseWriter, r *http.Request) {
	project, ok := s.authorizeProjectFileUpload(w, r)
	if !ok {
		return
	}
	s.createOSSDirectUploadWithScope(w, r, ossProjectDownloadScope(project.ProjectType, project.ProjectID))
}

func (s *Server) completeProjectFileUpload(w http.ResponseWriter, r *http.Request) {
	project, ok := s.authorizeProjectFileUpload(w, r)
	if !ok {
		return
	}
	s.completeOSSDirectUploadWithScope(w, r, ossProjectDownloadScope(project.ProjectType, project.ProjectID))
}

func (s *Server) authorizeProjectFileUpload(w http.ResponseWriter, r *http.Request) (projectFileContext, bool) {
	project, err := s.projectFileContext(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return projectFileContext{}, false
	}
	if !s.canUploadProjectFile(r.Context(), currentClaims(r).Subject, project.ProjectID) {
		writeError(w, http.StatusForbidden, "project file upload permission is required")
		return projectFileContext{}, false
	}
	return project, true
}

func (s *Server) downloadProjectFile(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectFileContext(r.Context(), r.PathValue("projectType"), r.PathValue("projectId"))
	if err != nil || project.ReviewStatus != "approved" {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	source := strings.ToLower(strings.TrimSpace(r.PathValue("source")))
	fileID := strings.TrimSpace(r.PathValue("fileId"))
	if source == "internal" {
		var objectKey, fileName string
		err = s.db.QueryRow(r.Context(), `select oss.object_key,project_file.file_name
			from project_files project_file join oss_files oss
				on oss.id=project_file.oss_file_id and oss.status='active' and oss.scan_status in ('clean','trusted_generated')
			where project_file.public_id=$1 and project_file.project_type=$2 and project_file.project_internal_id=$3 and project_file.status='active'`,
			strings.ToLower(fileID), project.ProjectType, project.ProjectInternalID).Scan(&objectKey, &fileName)
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project file not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load project file")
			return
		}
		if s.presignOSSFileWithRequest(w, r, ossPresignRequest{ObjectKey: objectKey}) {
			_, _ = s.db.Exec(r.Context(), `update project_files set download_count=download_count+1,updated_at=now()
				where public_id=$1 and status='active'`, strings.ToLower(fileID))
			s.writeAppLog(r.Context(), "download", "info", "download_project_file", fileName, currentClaims(r).Subject, r, http.StatusOK, 0,
				map[string]any{"projectType": project.ProjectType, "projectId": project.ProjectID, "source": source, "fileId": fileID})
		}
		return
	}
	if source != "modrinth" && source != "curseforge" {
		writeError(w, http.StatusBadRequest, "invalid project file source")
		return
	}
	cfg, err := s.modImportConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "external download source is unavailable")
		return
	}
	providerProjectID := project.ModrinthProjectID
	if source == "curseforge" {
		providerProjectID = project.CurseForgeProjectID
	}
	var selected providerProjectFile
	if source == "modrinth" {
		selected, err = s.loadModrinthProjectFileByHash(r.Context(), providerProjectID, fileID, cfg)
	} else {
		selected, err = loadCurseForgeProjectFileByID(r.Context(), providerProjectID, fileID, cfg)
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "external project file not found")
		return
	}
	downloadURL := selected.DirectURL
	if source == "curseforge" && downloadURL == "" {
		downloadURL, err = s.curseForgeDownloadURL(r.Context(), providerProjectID, selected.ID, cfg)
	}
	if err != nil || !validProviderDownloadURL(downloadURL) {
		writeError(w, http.StatusBadGateway, "the provider did not return a downloadable file URL")
		return
	}
	s.writeAppLog(r.Context(), "download", "info", "download_project_file", selected.FileName, currentClaims(r).Subject, r, http.StatusOK, 0,
		map[string]any{"projectType": project.ProjectType, "projectId": project.ProjectID, "source": source, "fileId": fileID})
	writeJSON(w, http.StatusOK, map[string]any{"url": downloadURL, "filename": selected.FileName})
}

func (s *Server) projectFileContext(ctx context.Context, rawType, rawID string) (projectFileContext, error) {
	projectType := normalizeProjectFileType(rawType)
	projectID := strings.ToLower(strings.TrimSpace(rawID))
	if projectType == "" || len(projectID) != 9 {
		return projectFileContext{}, errors.New("invalid project reference")
	}
	result := projectFileContext{ProjectType: projectType, ProjectID: projectID}
	var err error
	switch projectType {
	case "mod":
		err = s.db.QueryRow(ctx, `select id,slug,review_status,modrinth_project_id,curseforge_project_id
			from mods where project_code=$1`, projectID).Scan(&result.ProjectInternalID, &result.SiteID, &result.ReviewStatus,
			&result.ModrinthProjectID, &result.CurseForgeProjectID)
	case "modpack":
		err = s.db.QueryRow(ctx, `select id,slug,review_status,modrinth_project_id,curseforge_project_id
			from modpacks where public_id=$1`, projectID).Scan(&result.ProjectInternalID, &result.SiteID, &result.ReviewStatus,
			&result.ModrinthProjectID, &result.CurseForgeProjectID)
	case "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		err = s.db.QueryRow(ctx, `select id,slug,review_status,modrinth_project_id,curseforge_project_id
			from simple_projects where public_id=$1 and project_type=$2`, projectID, projectType).
			Scan(&result.ProjectInternalID, &result.SiteID, &result.ReviewStatus,
				&result.ModrinthProjectID, &result.CurseForgeProjectID)
	default:
		return projectFileContext{}, errors.New("project type is not connected to downloads yet")
	}
	return result, err
}

func normalizeProjectFileType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "mod", "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon":
		return value
	case "resourcepack", "texture_pack", "texturepack":
		return "resource_pack"
	case "shader", "shaderpack":
		return "shader_pack"
	default:
		return ""
	}
}

func projectFileRequiresLoader(projectType string) bool {
	return projectType == "mod" || projectType == "modpack" || projectType == "plugin" || projectType == "addon"
}

func projectFileExtensionAllowed(projectType, extension string) bool {
	switch projectType {
	case "mod", "plugin":
		return extension == ".jar"
	case "modpack":
		return extension == ".mrpack" || extension == ".zip"
	case "map", "resource_pack", "shader_pack", "datapack":
		return extension == ".zip"
	case "addon":
		return extension == ".jar" || extension == ".zip"
	default:
		return false
	}
}

func (s *Server) canUploadProjectFile(ctx context.Context, userID int64, projectID string) bool {
	return userID > 0 && s.userHasPermission(ctx, userID, "project.download.upload."+projectID)
}

func (s *Server) internalProjectFileByPublicID(ctx context.Context, project projectFileContext, publicID string) (projectFileItem, error) {
	var item projectFileItem
	err := s.db.QueryRow(ctx, `select project_file.id,project_file.public_id,project_file.display_name,project_file.file_name,
		project_file.version_name,project_file.release_channel,project_file.game_versions,project_file.loaders,
		project_file.created_at,project_file.size_bytes,project_file.download_count,project_file.sha256,oss.scan_status
		from project_files project_file join oss_files oss on oss.id=project_file.oss_file_id and oss.status='active'
		where project_file.public_id=$1 and project_file.project_type=$2 and project_file.project_internal_id=$3
			and project_file.status in ('processing','active')`, strings.ToLower(strings.TrimSpace(publicID)), project.ProjectType, project.ProjectInternalID).
		Scan(&item.internalID, &item.ID, &item.DisplayName, &item.FileName, &item.VersionName, &item.ReleaseChannel,
			&item.GameVersions, &item.Loaders, &item.PublishedAt, &item.SizeBytes, &item.DownloadCount, &item.SHA256, &item.ScanStatus)
	if err != nil {
		return projectFileItem{}, err
	}
	item.Source = "internal"
	item.DownloadCountSource = "mcmods"
	item.DownloadPath = projectFileDownloadPath(project, item.Source, item.ID)
	return item, nil
}

func providerFileItem(project projectFileContext, file providerProjectFile) projectFileItem {
	return projectFileItem{
		ID: file.ID, Source: file.Source, DisplayName: file.DisplayName, FileName: file.FileName,
		VersionName: file.VersionName, ReleaseChannel: file.ReleaseChannel, GameVersions: file.GameVersions,
		Loaders: file.Loaders, PublishedAt: file.PublishedAt, SizeBytes: file.SizeBytes,
		DownloadCount: file.DownloadCount, DownloadCountSource: file.Source, SHA1: file.SHA1, SHA512: file.SHA512,
		DownloadPath: projectFileDownloadPath(project, file.Source, file.ID),
	}
}

func projectFileDownloadPath(project projectFileContext, source, fileID string) string {
	return "/api/v1/projects/" + url.PathEscape(project.ProjectType) + "/" + url.PathEscape(project.ProjectID) +
		"/files/" + url.PathEscape(source) + "/" + url.PathEscape(fileID) + "/download"
}

func projectFileFilters(items []projectFileItem, versionOrder map[string]int) ([]string, []string) {
	versions, loaders := make([]string, 0), make([]string, 0)
	for _, item := range items {
		versions = append(versions, item.GameVersions...)
		loaders = append(loaders, item.Loaders...)
	}
	versions = uniqueTrimmed(versions, 500)
	loaders = uniqueTrimmed(loaders, 100)
	sortMinecraftVersionCodes(versions, versionOrder)
	sort.Strings(loaders)
	return versions, loaders
}

func resolveCurseForgeProjectID(ctx context.Context, client *http.Client, projectID string, cfg modImportConfig, headers http.Header) (string, error) {
	if numeric, err := strconv.ParseInt(projectID, 10, 64); err == nil && numeric > 0 {
		return strconv.FormatInt(numeric, 10), nil
	}
	endpoint, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/search")
	query := endpoint.Query()
	query.Set("gameId", "432")
	query.Set("slug", projectID)
	query.Set("pageSize", "1")
	endpoint.RawQuery = query.Encode()
	var response struct {
		Data []struct {
			ID int64 `json:"id"`
		} `json:"data"`
	}
	if err := getProviderJSONLimited(ctx, client, endpoint.String(), headers, projectFileProviderJSONLimit, &response); err != nil {
		return "", err
	}
	if len(response.Data) > projectFileMaximumProviderProjects {
		return "", errors.New("CurseForge returned too many projects")
	}
	if len(response.Data) == 0 {
		return "", errors.New("CurseForge project not found")
	}
	return strconv.FormatInt(response.Data[0].ID, 10), nil
}

func (s *Server) curseForgeDownloadURL(ctx context.Context, projectID, fileID string, cfg modImportConfig) (string, error) {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.CurseForge.BaseURL)
	if err != nil {
		return "", err
	}
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, projectID, cfg, headers)
	if err != nil {
		return "", err
	}
	var response struct {
		Data string `json:"data"`
	}
	endpoint := cfg.CurseForge.BaseURL + "/mods/" + numericID + "/files/" + url.PathEscape(fileID) + "/download-url"
	if err = getProviderJSONLimited(ctx, client, endpoint, headers, projectFileProviderJSONLimit, &response); err != nil {
		return "", err
	}
	return response.Data, nil
}

func normalizeReleaseChannel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "alpha":
		return "alpha"
	case "beta":
		return "beta"
	default:
		return "release"
	}
}

func curseForgeReleaseChannel(value int) string {
	switch value {
	case 2:
		return "beta"
	case 3:
		return "alpha"
	default:
		return "release"
	}
}

func curseForgeFileCompatibility(values []string) ([]string, []string) {
	loaders := normalizeLoaders(values)
	versions := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		lower := strings.ToLower(trimmed)
		if trimmed == "" || len(normalizeLoaders([]string{trimmed})) > 0 || strings.HasPrefix(lower, "java ") || lower == "client" || lower == "server" {
			continue
		}
		versions = append(versions, trimmed)
	}
	return uniqueTrimmed(loaders, 30), uniqueTrimmed(versions, 100)
}

func validProviderDownloadURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}
