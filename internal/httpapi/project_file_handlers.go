package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
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

	internalFiles, err := s.internalProjectFiles(r.Context(), project)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load on-site project files")
		return
	}
	cfg, cfgErr := s.modImportConfigFromSettings(r.Context())
	warnings := map[string]string{}
	providers := map[string]bool{"internal": true, "modrinth": false, "curseforge": false}
	var modrinthFiles, curseForgeFiles []providerProjectFile
	if cfgErr != nil {
		warnings["providers"] = "external download sources are temporarily unavailable"
	} else {
		var group errgroup.Group
		var modrinthErr, curseForgeErr error
		if project.ModrinthProjectID != "" {
			providers["modrinth"] = true
			group.Go(func() error {
				modrinthFiles, modrinthErr = s.cachedProviderProjectFiles(r.Context(), "modrinth", project.ModrinthProjectID, cfg)
				return nil
			})
		}
		if project.CurseForgeProjectID != "" && cfg.CurseForge.APIKey != "" {
			providers["curseforge"] = true
			group.Go(func() error {
				curseForgeFiles, curseForgeErr = s.cachedProviderProjectFiles(r.Context(), "curseforge", project.CurseForgeProjectID, cfg)
				return nil
			})
		}
		_ = group.Wait()
		if modrinthErr != nil {
			warnings["modrinth"] = "Modrinth file list is temporarily unavailable"
		}
		if curseForgeErr != nil {
			warnings["curseforge"] = "CurseForge file list is temporarily unavailable"
		}
		if project.CurseForgeProjectID != "" && cfg.CurseForge.APIKey == "" {
			warnings["curseforge"] = "CurseForge API key is not configured"
		}
	}

	items := make([]projectFileItem, 0, len(internalFiles)+len(modrinthFiles)+len(curseForgeFiles))
	items = append(items, internalFiles...)
	for _, file := range append(modrinthFiles, curseForgeFiles...) {
		items = append(items, providerFileItem(project, file))
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].PublishedAt.Equal(items[j].PublishedAt) {
			return items[i].FileName < items[j].FileName
		}
		return items[i].PublishedAt.After(items[j].PublishedAt)
	})
	versions, loaders := projectFileFilters(items)
	var internalDownloadCount int64
	for _, file := range internalFiles {
		internalDownloadCount += file.DownloadCount
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "versions": versions, "loaders": loaders,
		"providers": providers, "warnings": warnings, "canUpload": canUpload,
		"uploadPermission": "project.download.upload." + project.ProjectID,
		"totals":           map[string]any{"files": len(items), "internalDownloads": internalDownloadCount},
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
	command, err := tx.Exec(r.Context(), `update project_files set status='deleted',updated_at=now()
		where public_id=$1 and project_type=$2 and project_internal_id=$3 and status='active'`,
		publicID, project.ProjectType, project.ProjectInternalID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete project file")
		return
	}
	if command.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "project file not found")
		return
	}
	_, _ = tx.Exec(r.Context(), `insert into audit_events(aggregate_type,aggregate_key,actor_id,action,ip,user_agent,metadata)
		values('project_file',$1,$2,'delete',$3,$4,jsonb_build_object('projectType',$5,'projectId',$6))`,
		publicID, currentClaims(r).Subject, s.requestClientLocation(r).IP, r.UserAgent(), project.ProjectType, project.ProjectID)
	if project.ReviewStatus == "approved" {
		if err = enqueueProjectFileUpdateEventTx(r.Context(), tx, project, currentClaims(r).Subject, "download_removed", publicID); err != nil {
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
	request.GameVersions = uniqueTrimmed(request.GameVersions, 100)
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
	var fileName, contentType, sha256, category string
	var fileID, sizeBytes, uploaderID int64
	err := s.db.QueryRow(r.Context(), `select id,original_name,content_type,size_bytes,sha256,uploader_id,category
		from oss_files where public_id=$1 and status='active'`, request.OSSFileID).Scan(&fileID, &fileName, &contentType, &sizeBytes, &sha256, &uploaderID, &category)
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
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start project file creation")
		return
	}
	defer tx.Rollback(r.Context())
	var publicID string
	err = tx.QueryRow(r.Context(), `insert into project_files(
		project_type,project_internal_id,oss_file_id,display_name,version_name,release_channel,game_versions,loaders,
		file_name,content_type,size_bytes,sha256,uploaded_by)
		values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) returning public_id`,
		project.ProjectType, project.ProjectInternalID, fileID, request.DisplayName, request.VersionName,
		request.ReleaseChannel, request.GameVersions, request.Loaders, fileName, contentType, sizeBytes, sha256,
		currentClaims(r).Subject).Scan(&publicID)
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
	if project.ReviewStatus == "approved" {
		if err = enqueueProjectFileUpdateEventTx(r.Context(), tx, project, currentClaims(r).Subject, "download_added", publicID); err != nil {
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

func enqueueProjectFileUpdateEventTx(ctx context.Context, tx pgx.Tx, project projectFileContext, actorID int64, updateKind, filePublicID string) error {
	var routeID int64
	if err := tx.QueryRow(ctx, `select id from public_routes where entity_type=$1 and internal_id=$2`,
		project.ProjectType, project.ProjectInternalID).Scan(&routeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return enqueueProjectUpdateEventTx(ctx, tx, routeID, 0, actorID, updateKind,
		[]string{"download_files"}, "project-file:"+updateKind+":"+filePublicID)
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
				on oss.id=project_file.oss_file_id and oss.status='active' and oss.scan_status='clean'
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
	files, err := s.cachedProviderProjectFiles(r.Context(), source, providerProjectID, cfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to load external project files")
		return
	}
	var selected *providerProjectFile
	for index := range files {
		if files[index].ID == fileID {
			selected = &files[index]
			break
		}
	}
	if selected == nil {
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

func (s *Server) internalProjectFiles(ctx context.Context, project projectFileContext) ([]projectFileItem, error) {
	rows, err := s.db.Query(ctx, `select project_file.public_id,project_file.display_name,project_file.file_name,
		project_file.version_name,project_file.release_channel,project_file.game_versions,project_file.loaders,
		project_file.created_at,project_file.size_bytes,project_file.download_count,project_file.sha256,oss.scan_status
		from project_files project_file join oss_files oss on oss.id=project_file.oss_file_id and oss.status='active'
		where project_file.project_type=$1 and project_file.project_internal_id=$2 and project_file.status='active'
		order by project_file.created_at desc,project_file.id desc`, project.ProjectType, project.ProjectInternalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]projectFileItem, 0)
	for rows.Next() {
		var item projectFileItem
		if err = rows.Scan(&item.ID, &item.DisplayName, &item.FileName, &item.VersionName, &item.ReleaseChannel,
			&item.GameVersions, &item.Loaders, &item.PublishedAt, &item.SizeBytes, &item.DownloadCount, &item.SHA256, &item.ScanStatus); err != nil {
			return nil, err
		}
		item.Source = "internal"
		item.DownloadCountSource = "mcmods"
		item.DownloadPath = projectFileDownloadPath(project, item.Source, item.ID)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Server) internalProjectFileByPublicID(ctx context.Context, project projectFileContext, publicID string) (projectFileItem, error) {
	items, err := s.internalProjectFiles(ctx, project)
	if err != nil {
		return projectFileItem{}, err
	}
	for _, item := range items {
		if item.ID == publicID {
			return item, nil
		}
	}
	return projectFileItem{}, pgx.ErrNoRows
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

func projectFileFilters(items []projectFileItem) ([]string, []string) {
	versions, loaders := make([]string, 0), make([]string, 0)
	for _, item := range items {
		versions = append(versions, item.GameVersions...)
		loaders = append(loaders, item.Loaders...)
	}
	versions = uniqueTrimmed(versions, 500)
	loaders = uniqueTrimmed(loaders, 100)
	sort.SliceStable(versions, func(i, j int) bool { return minecraftVersionLess(versions[j], versions[i]) })
	sort.Strings(loaders)
	return versions, loaders
}

func minecraftVersionLess(left, right string) bool {
	leftParts, rightParts := strings.Split(left, "."), strings.Split(right, ".")
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		leftValue, rightValue := 0, 0
		if index < len(leftParts) {
			leftValue, _ = strconv.Atoi(leftParts[index])
		}
		if index < len(rightParts) {
			rightValue, _ = strconv.Atoi(rightParts[index])
		}
		if leftValue != rightValue {
			return leftValue < rightValue
		}
	}
	return left < right
}

func (s *Server) cachedProviderProjectFiles(ctx context.Context, source, projectID string, cfg modImportConfig) ([]providerProjectFile, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("provider project ID is missing")
	}
	key := "project-files:provider:" + source + ":" + strings.ToLower(projectID)
	payload, err := s.cache.GetOrLoad(ctx, key, func(loadContext context.Context) ([]byte, error) {
		var files []providerProjectFile
		var loadErr error
		switch source {
		case "modrinth":
			files, loadErr = loadModrinthProjectFiles(loadContext, projectID, cfg)
		case "curseforge":
			files, loadErr = loadCurseForgeProjectFiles(loadContext, projectID, cfg)
		default:
			loadErr = errors.New("unsupported provider")
		}
		if loadErr != nil {
			return nil, loadErr
		}
		return json.Marshal(files)
	})
	if err != nil {
		return nil, err
	}
	var files []providerProjectFile
	if err = json.Unmarshal(payload, &files); err != nil {
		return nil, err
	}
	return files, nil
}

func loadModrinthProjectFiles(ctx context.Context, projectID string, cfg modImportConfig) ([]providerProjectFile, error) {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.Modrinth.BaseURL)
	if err != nil {
		return nil, err
	}
	var versions []struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		VersionNumber string   `json:"version_number"`
		VersionType   string   `json:"version_type"`
		GameVersions  []string `json:"game_versions"`
		Loaders       []string `json:"loaders"`
		DatePublished string   `json:"date_published"`
		Downloads     int64    `json:"downloads"`
		Files         []struct {
			Hashes   map[string]string `json:"hashes"`
			URL      string            `json:"url"`
			FileName string            `json:"filename"`
			Primary  bool              `json:"primary"`
			Size     int64             `json:"size"`
		} `json:"files"`
		ProjectID string `json:"project_id"`
	}
	headers := providerHeaders(cfg.UserAgent, cfg.Modrinth.Token, "")
	endpoint := cfg.Modrinth.BaseURL + "/project/" + url.PathEscape(projectID) + "/version"
	if err = getProviderJSON(ctx, client, endpoint, headers, &versions); err != nil {
		return nil, err
	}
	result := make([]providerProjectFile, 0)
	for _, version := range versions {
		publishedAt, _ := time.Parse(time.RFC3339, version.DatePublished)
		for index, file := range version.Files {
			id := strings.ToLower(strings.TrimSpace(file.Hashes["sha1"]))
			if id == "" {
				id = strings.ToLower(strings.TrimSpace(file.Hashes["sha512"]))
			}
			if id == "" || !validProviderDownloadURL(file.URL) {
				continue
			}
			displayName := strings.TrimSpace(version.Name)
			if displayName == "" {
				displayName = strings.TrimSpace(version.VersionNumber)
			}
			if len(version.Files) > 1 && (!file.Primary || index > 0) {
				displayName += " · " + file.FileName
			}
			result = append(result, providerProjectFile{
				ID: id, Source: "modrinth", DisplayName: displayName, FileName: file.FileName,
				VersionName: version.VersionNumber, ReleaseChannel: normalizeReleaseChannel(version.VersionType),
				GameVersions: uniqueTrimmed(version.GameVersions, 100), Loaders: normalizeLoaders(version.Loaders),
				PublishedAt: publishedAt, SizeBytes: file.Size, DownloadCount: version.Downloads,
				SHA1: file.Hashes["sha1"], SHA512: file.Hashes["sha512"], DirectURL: file.URL,
				ProviderProjectID: firstNonEmpty(version.ProjectID, projectID), ProviderVersionID: version.ID,
				ClientEnvironment: "required", ServerEnvironment: "required",
				Primary: file.Primary,
			})
		}
	}
	return result, nil
}

func loadCurseForgeProjectFiles(ctx context.Context, projectID string, cfg modImportConfig) ([]providerProjectFile, error) {
	if cfg.CurseForge.APIKey == "" {
		return nil, errors.New("CurseForge is not configured")
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.CurseForge.BaseURL)
	if err != nil {
		return nil, err
	}
	headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, projectID, cfg, headers)
	if err != nil {
		return nil, err
	}
	type curseForgeFile struct {
		ID            int64    `json:"id"`
		DisplayName   string   `json:"displayName"`
		FileName      string   `json:"fileName"`
		ReleaseType   int      `json:"releaseType"`
		FileDate      string   `json:"fileDate"`
		FileLength    int64    `json:"fileLength"`
		DownloadCount int64    `json:"downloadCount"`
		DownloadURL   string   `json:"downloadUrl"`
		GameVersions  []string `json:"gameVersions"`
		Hashes        []struct {
			Value string `json:"value"`
			Algo  int    `json:"algo"`
		} `json:"hashes"`
	}
	result := make([]providerProjectFile, 0)
	for index, pages := 0, 0; pages < 10; pages++ {
		endpoint, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/" + numericID + "/files")
		query := endpoint.Query()
		query.Set("index", strconv.Itoa(index))
		query.Set("pageSize", "50")
		endpoint.RawQuery = query.Encode()
		var response struct {
			Data       []curseForgeFile `json:"data"`
			Pagination struct {
				Index       int `json:"index"`
				PageSize    int `json:"pageSize"`
				ResultCount int `json:"resultCount"`
				TotalCount  int `json:"totalCount"`
			} `json:"pagination"`
		}
		if err = getProviderJSON(ctx, client, endpoint.String(), headers, &response); err != nil {
			return nil, err
		}
		for _, file := range response.Data {
			publishedAt, _ := time.Parse(time.RFC3339, file.FileDate)
			loaders, versions := curseForgeFileCompatibility(file.GameVersions)
			sha1 := ""
			for _, hash := range file.Hashes {
				if hash.Algo == 1 {
					sha1 = hash.Value
				}
			}
			result = append(result, providerProjectFile{
				ID: strconv.FormatInt(file.ID, 10), Source: "curseforge", DisplayName: file.DisplayName,
				FileName: file.FileName, VersionName: file.DisplayName, ReleaseChannel: curseForgeReleaseChannel(file.ReleaseType),
				GameVersions: versions, Loaders: loaders, PublishedAt: publishedAt, SizeBytes: file.FileLength,
				DownloadCount: file.DownloadCount, SHA1: sha1, DirectURL: file.DownloadURL,
			})
		}
		count := response.Pagination.ResultCount
		if count == 0 {
			count = len(response.Data)
		}
		index += count
		if count == 0 || (response.Pagination.TotalCount > 0 && index >= response.Pagination.TotalCount) || len(result) >= 500 {
			break
		}
	}
	return result, nil
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
	if err := getProviderJSON(ctx, client, endpoint.String(), headers, &response); err != nil {
		return "", err
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
	headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, projectID, cfg, headers)
	if err != nil {
		return "", err
	}
	var response struct {
		Data string `json:"data"`
	}
	endpoint := cfg.CurseForge.BaseURL + "/mods/" + numericID + "/files/" + url.PathEscape(fileID) + "/download-url"
	if err = getProviderJSON(ctx, client, endpoint, headers, &response); err != nil {
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
