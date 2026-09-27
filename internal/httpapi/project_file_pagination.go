package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	projectFileCursorVersion            = 1
	projectFileDefaultPageLimit         = 20
	projectFileMaximumPageLimit         = 50
	projectFileMaximumCursorLength      = 2048
	projectFileProviderJSONLimit        = int64(2 << 20)
	projectFileMaximumProviderProjects  = 1
	projectFileMaximumModrinthVersions  = 10_000
	projectFileModrinthVersionBatchSize = 10
	projectFileModrinthBatchesPerPage   = 5
	projectFileMaximumBatchFiles        = 128
	projectFileMaximumFilesPerVersion   = 32
)

type projectFilePageRequest struct {
	Source string
	Limit  int
	Scope  string
	Cursor *projectFilePageCursor
}

type projectFilePageCursor struct {
	Version    int       `json:"v"`
	Scope      string    `json:"s"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	InternalID int64     `json:"internalId,omitempty"`
	Position   int       `json:"position,omitempty"`
	Anchor     string    `json:"anchor,omitempty"`
	FileOffset int       `json:"fileOffset,omitempty"`
}

type projectFilePage struct {
	Items      []projectFileItem
	HasMore    bool
	NextCursor string
}

func parseProjectFilePageRequest(values url.Values, project projectFileContext) (projectFilePageRequest, error) {
	if strings.TrimSpace(values.Get("page")) != "" || strings.TrimSpace(values.Get("offset")) != "" {
		return projectFilePageRequest{}, errors.New("project file offset pagination is not supported")
	}
	source := strings.ToLower(strings.TrimSpace(values.Get("source")))
	if source != "internal" && source != "modrinth" && source != "curseforge" {
		return projectFilePageRequest{}, errors.New("project file source is required")
	}
	limit := projectFileDefaultPageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > projectFileMaximumPageLimit {
			return projectFilePageRequest{}, errors.New("project file page limit is invalid")
		}
		limit = parsed
	}
	scope := projectFilePageScope(project, source, limit)
	cursor, err := decodeProjectFilePageCursor(values.Get("cursor"), scope, source)
	if err != nil {
		return projectFilePageRequest{}, err
	}
	return projectFilePageRequest{Source: source, Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func projectFilePageScope(project projectFileContext, source string, limit int) string {
	material, _ := json.Marshal(struct {
		Version     int    `json:"version"`
		ProjectType string `json:"projectType"`
		ProjectID   string `json:"projectId"`
		Source      string `json:"source"`
		Limit       int    `json:"limit"`
	}{projectFileCursorVersion, project.ProjectType, project.ProjectID, source, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeProjectFilePageCursor(cursor projectFilePageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeProjectFilePageCursor(raw, scope, source string) (*projectFilePageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > projectFileMaximumCursorLength {
		return nil, errors.New("invalid project file cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid project file cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor projectFilePageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid project file cursor")
	}
	var extra json.RawMessage
	validPosition := false
	switch source {
	case "internal":
		validPosition = !cursor.CreatedAt.IsZero() && cursor.InternalID > 0 && cursor.Position == 0 && cursor.FileOffset == 0
	case "modrinth":
		validPosition = cursor.Position == 0 && cursor.InternalID == 0 && cursor.CreatedAt.IsZero() &&
			strings.TrimSpace(cursor.Anchor) != "" && len(cursor.Anchor) <= 64 && cursor.FileOffset >= 0
	case "curseforge":
		validPosition = cursor.Position > 0 && cursor.Anchor == "" && cursor.FileOffset == 0 && cursor.InternalID == 0 && cursor.CreatedAt.IsZero()
	}
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != projectFileCursorVersion ||
		cursor.Scope != scope || !validPosition {
		return nil, errors.New("invalid project file cursor")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	return &cursor, nil
}

func (s *Server) internalProjectFilePage(ctx context.Context, project projectFileContext, request projectFilePageRequest) (projectFilePage, error) {
	query := `select project_file.id,project_file.public_id,project_file.display_name,project_file.file_name,
		project_file.version_name,project_file.release_channel,project_file.game_versions,project_file.loaders,
		project_file.created_at,project_file.size_bytes,project_file.download_count,project_file.sha256,oss.scan_status
		from project_files project_file join oss_files oss on oss.id=project_file.oss_file_id and oss.status='active'
			and oss.scan_status in ('clean','trusted_generated')
		where project_file.project_type=$1 and project_file.project_internal_id=$2 and project_file.status='active'`
	arguments := []any{project.ProjectType, project.ProjectInternalID}
	if request.Cursor != nil {
		query += ` and (project_file.created_at,project_file.id)<($3,$4)`
		arguments = append(arguments, request.Cursor.CreatedAt, request.Cursor.InternalID)
	}
	query += fmt.Sprintf(` order by project_file.created_at desc,project_file.id desc limit $%d`, len(arguments)+1)
	arguments = append(arguments, request.Limit+1)
	rows, err := s.db.Query(ctx, query, arguments...)
	if err != nil {
		return projectFilePage{}, err
	}
	defer rows.Close()
	items := make([]projectFileItem, 0, request.Limit+1)
	for rows.Next() {
		var item projectFileItem
		if err = rows.Scan(&item.internalID, &item.ID, &item.DisplayName, &item.FileName, &item.VersionName, &item.ReleaseChannel,
			&item.GameVersions, &item.Loaders, &item.PublishedAt, &item.SizeBytes, &item.DownloadCount, &item.SHA256, &item.ScanStatus); err != nil {
			return projectFilePage{}, err
		}
		item.Source = "internal"
		item.DownloadCountSource = "mcmods"
		item.DownloadPath = projectFileDownloadPath(project, item.Source, item.ID)
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return projectFilePage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		last := items[len(items)-1]
		nextCursor = encodeProjectFilePageCursor(projectFilePageCursor{
			Version: projectFileCursorVersion, Scope: request.Scope, CreatedAt: last.PublishedAt.UTC(), InternalID: last.internalID,
		})
	}
	return projectFilePage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

type modrinthProjectFileManifest struct {
	ID       string   `json:"id"`
	Versions []string `json:"versions"`
}

type modrinthProjectVersion struct {
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

func (s *Server) loadModrinthProjectFilePage(ctx context.Context, project projectFileContext, request projectFilePageRequest, cfg modImportConfig) (projectFilePage, error) {
	if project.ModrinthProjectID == "" {
		return projectFilePage{Items: []projectFileItem{}}, nil
	}
	manifest, err := s.cachedModrinthProjectFileManifest(ctx, project.ModrinthProjectID, cfg)
	if err != nil {
		return projectFilePage{}, err
	}
	position, fileOffset := 0, 0
	if request.Cursor != nil {
		position = modrinthManifestPosition(manifest, request.Cursor.Anchor)
		fileOffset = request.Cursor.FileOffset
		if position < 0 {
			return projectFilePage{}, errors.New("project file cursor anchor is no longer in the provider manifest")
		}
	}
	if position > len(manifest.Versions) {
		return projectFilePage{}, errors.New("project file cursor is outside the provider manifest")
	}
	items := make([]projectFileItem, 0, request.Limit+1)
	type modrinthPageState struct{ position, fileOffset int }
	states := make([]modrinthPageState, 0, request.Limit+1)
	statePosition, stateFileOffset := position, fileOffset
	for batch := 0; batch < projectFileModrinthBatchesPerPage && statePosition < len(manifest.Versions) && len(items) <= request.Limit; batch++ {
		end := min(statePosition+projectFileModrinthVersionBatchSize, len(manifest.Versions))
		ids := make([]string, 0, end-statePosition)
		for index := statePosition; index < end; index++ {
			ids = append(ids, manifest.Versions[len(manifest.Versions)-1-index])
		}
		versions, loadErr := s.cachedModrinthProjectVersions(ctx, ids, cfg)
		if loadErr != nil {
			return projectFilePage{}, loadErr
		}
		byID := make(map[string]modrinthProjectVersion, len(versions))
		batchFiles := 0
		for _, version := range versions {
			if len(version.Files) > projectFileMaximumFilesPerVersion {
				return projectFilePage{}, errors.New("Modrinth version contains too many files")
			}
			batchFiles += len(version.Files)
			byID[version.ID] = version
		}
		if batchFiles > projectFileMaximumBatchFiles {
			return projectFilePage{}, errors.New("Modrinth page contains too many files")
		}
		for statePosition < end && len(items) <= request.Limit {
			versionID := manifest.Versions[len(manifest.Versions)-1-statePosition]
			version, exists := byID[versionID]
			if !exists {
				statePosition++
				stateFileOffset = 0
				continue
			}
			files := modrinthVersionProjectFiles(version, manifest.ID)
			if stateFileOffset > len(files) {
				return projectFilePage{}, errors.New("project file cursor is outside the provider version")
			}
			versionPosition := statePosition
			for stateFileOffset < len(files) && len(items) <= request.Limit {
				items = append(items, providerFileItem(project, files[stateFileOffset]))
				stateFileOffset++
				if stateFileOffset >= len(files) {
					statePosition++
					stateFileOffset = 0
				}
				states = append(states, modrinthPageState{statePosition, stateFileOffset})
				if statePosition != versionPosition {
					break
				}
			}
		}
	}
	hasMore := len(items) > request.Limit || statePosition < len(manifest.Versions)
	if len(items) > request.Limit {
		items = items[:request.Limit]
		statePosition, stateFileOffset = states[request.Limit-1].position, states[request.Limit-1].fileOffset
	}
	nextCursor := ""
	if hasMore {
		anchor := manifest.Versions[len(manifest.Versions)-1-statePosition]
		nextCursor = encodeProjectFilePageCursor(projectFilePageCursor{
			Version: projectFileCursorVersion, Scope: request.Scope, Anchor: anchor, FileOffset: stateFileOffset,
		})
	}
	return projectFilePage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func modrinthManifestPosition(manifest modrinthProjectFileManifest, anchor string) int {
	anchor = strings.TrimSpace(anchor)
	for position := range manifest.Versions {
		if manifest.Versions[len(manifest.Versions)-1-position] == anchor {
			return position
		}
	}
	return -1
}

func (s *Server) cachedModrinthProjectFileManifest(ctx context.Context, projectID string, cfg modImportConfig) (modrinthProjectFileManifest, error) {
	key := "project-files:modrinth:manifest:v2:" + strings.ToLower(strings.TrimSpace(projectID))
	payload, err := s.cache.GetOrLoad(ctx, key, func(loadContext context.Context) ([]byte, error) {
		client, clientErr := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.Modrinth.BaseURL)
		if clientErr != nil {
			return nil, clientErr
		}
		headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), "")
		var manifest modrinthProjectFileManifest
		endpoint := cfg.Modrinth.BaseURL + "/project/" + url.PathEscape(projectID)
		if clientErr = getProviderJSONLimited(loadContext, client, endpoint, headers, projectFileProviderJSONLimit, &manifest); clientErr != nil {
			return nil, clientErr
		}
		if clientErr = validateModrinthProjectFileManifest(manifest); clientErr != nil {
			return nil, clientErr
		}
		return json.Marshal(manifest)
	})
	if err != nil {
		return modrinthProjectFileManifest{}, err
	}
	var manifest modrinthProjectFileManifest
	if err = json.Unmarshal(payload, &manifest); err != nil {
		return modrinthProjectFileManifest{}, err
	}
	return manifest, validateModrinthProjectFileManifest(manifest)
}

func validateModrinthProjectFileManifest(manifest modrinthProjectFileManifest) error {
	if projectFileMaximumProviderProjects != 1 || strings.TrimSpace(manifest.ID) == "" {
		return errors.New("invalid Modrinth project manifest")
	}
	if len(manifest.Versions) > projectFileMaximumModrinthVersions {
		return errors.New("Modrinth project contains too many versions")
	}
	seen := make(map[string]struct{}, len(manifest.Versions))
	for _, id := range manifest.Versions {
		id = strings.TrimSpace(id)
		if id == "" || len(id) > 64 {
			return errors.New("invalid Modrinth version ID")
		}
		if _, exists := seen[id]; exists {
			return errors.New("Modrinth project manifest contains duplicate versions")
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (s *Server) cachedModrinthProjectVersions(ctx context.Context, ids []string, cfg modImportConfig) ([]modrinthProjectVersion, error) {
	if len(ids) == 0 || len(ids) > projectFileModrinthVersionBatchSize {
		return nil, errors.New("invalid Modrinth version batch")
	}
	material, _ := json.Marshal(ids)
	digest := sha256.Sum256(material)
	key := "project-files:modrinth:versions:v2:" + hex.EncodeToString(digest[:16])
	payload, err := s.cache.GetOrLoad(ctx, key, func(loadContext context.Context) ([]byte, error) {
		client, clientErr := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.Modrinth.BaseURL)
		if clientErr != nil {
			return nil, clientErr
		}
		headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), "")
		endpoint, _ := url.Parse(cfg.Modrinth.BaseURL + "/versions")
		query := endpoint.Query()
		query.Set("ids", string(material))
		endpoint.RawQuery = query.Encode()
		var versions []modrinthProjectVersion
		if clientErr = getProviderJSONLimited(loadContext, client, endpoint.String(), headers, projectFileProviderJSONLimit, &versions); clientErr != nil {
			return nil, clientErr
		}
		if len(versions) > len(ids) {
			return nil, errors.New("Modrinth returned too many versions")
		}
		return json.Marshal(versions)
	})
	if err != nil {
		return nil, err
	}
	var versions []modrinthProjectVersion
	if err = json.Unmarshal(payload, &versions); err != nil {
		return nil, err
	}
	if len(versions) > len(ids) {
		return nil, errors.New("Modrinth returned too many versions")
	}
	return versions, nil
}

func modrinthVersionProjectFiles(version modrinthProjectVersion, providerProjectID string) []providerProjectFile {
	publishedAt, _ := time.Parse(time.RFC3339, version.DatePublished)
	result := make([]providerProjectFile, 0, len(version.Files))
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
			ProviderProjectID: firstNonEmpty(version.ProjectID, providerProjectID), ProviderVersionID: version.ID,
			ClientEnvironment: "required", ServerEnvironment: "required", Primary: file.Primary,
		})
	}
	return result
}

func (s *Server) loadModrinthProjectFileByHash(ctx context.Context, projectID, fileID string, cfg modImportConfig) (providerProjectFile, error) {
	manifest, err := s.cachedModrinthProjectFileManifest(ctx, projectID, cfg)
	if err != nil {
		return providerProjectFile{}, err
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.Modrinth.BaseURL)
	if err != nil {
		return providerProjectFile{}, err
	}
	headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), "")
	fileID = strings.ToLower(strings.TrimSpace(fileID))
	var version modrinthProjectVersion
	for _, algorithm := range []string{"sha1", "sha512"} {
		endpoint, _ := url.Parse(cfg.Modrinth.BaseURL + "/version_file/" + url.PathEscape(fileID))
		query := endpoint.Query()
		query.Set("algorithm", algorithm)
		endpoint.RawQuery = query.Encode()
		err = getProviderJSONLimited(ctx, client, endpoint.String(), headers, projectFileProviderJSONLimit, &version)
		if err == nil || !isProviderNotFound(err) {
			break
		}
	}
	if err != nil {
		return providerProjectFile{}, err
	}
	if version.ProjectID != manifest.ID || len(version.Files) > projectFileMaximumFilesPerVersion {
		return providerProjectFile{}, errors.New("Modrinth file does not belong to the project")
	}
	for _, file := range modrinthVersionProjectFiles(version, manifest.ID) {
		if strings.EqualFold(file.ID, fileID) {
			return file, nil
		}
	}
	return providerProjectFile{}, errors.New("Modrinth file not found in the version")
}

type curseForgeProjectFile struct {
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

func (s *Server) loadCurseForgeProjectFilePage(ctx context.Context, project projectFileContext, request projectFilePageRequest, cfg modImportConfig) (projectFilePage, error) {
	if project.CurseForgeProjectID == "" {
		return projectFilePage{Items: []projectFileItem{}}, nil
	}
	if cfg.CurseForge.APIKey == "" {
		return projectFilePage{}, errors.New("CurseForge is not configured")
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.CurseForge.BaseURL)
	if err != nil {
		return projectFilePage{}, err
	}
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, project.CurseForgeProjectID, cfg, headers)
	if err != nil {
		return projectFilePage{}, err
	}
	position := 0
	if request.Cursor != nil {
		position = request.Cursor.Position
	}
	endpoint, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/" + numericID + "/files")
	query := endpoint.Query()
	query.Set("index", strconv.Itoa(position))
	query.Set("pageSize", strconv.Itoa(request.Limit+1))
	endpoint.RawQuery = query.Encode()
	var response struct {
		Data       []curseForgeProjectFile `json:"data"`
		Pagination struct {
			Index       int `json:"index"`
			PageSize    int `json:"pageSize"`
			ResultCount int `json:"resultCount"`
			TotalCount  int `json:"totalCount"`
		} `json:"pagination"`
	}
	if err = getProviderJSONLimited(ctx, client, endpoint.String(), headers, projectFileProviderJSONLimit, &response); err != nil {
		return projectFilePage{}, err
	}
	if len(response.Data) > request.Limit+1 || len(response.Data) > projectFileMaximumBatchFiles {
		return projectFilePage{}, errors.New("CurseForge returned too many files")
	}
	if len(response.Data) == 0 && response.Pagination.TotalCount > position {
		return projectFilePage{}, errors.New("CurseForge returned an inconsistent empty page")
	}
	items := make([]projectFileItem, 0, min(len(response.Data), request.Limit))
	for _, file := range response.Data {
		items = append(items, providerFileItem(project, curseForgeProviderProjectFile(file)))
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	} else if response.Pagination.TotalCount > 0 {
		hasMore = position+len(items) < response.Pagination.TotalCount
	}
	nextCursor := ""
	if hasMore {
		nextCursor = encodeProjectFilePageCursor(projectFilePageCursor{
			Version: projectFileCursorVersion, Scope: request.Scope, Position: position + len(items),
		})
	}
	return projectFilePage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func curseForgeProviderProjectFile(file curseForgeProjectFile) providerProjectFile {
	publishedAt, _ := time.Parse(time.RFC3339, file.FileDate)
	loaders, versions := curseForgeFileCompatibility(file.GameVersions)
	sha1 := ""
	for _, hash := range file.Hashes {
		if hash.Algo == 1 {
			sha1 = hash.Value
		}
	}
	return providerProjectFile{
		ID: strconv.FormatInt(file.ID, 10), Source: "curseforge", DisplayName: file.DisplayName,
		FileName: file.FileName, VersionName: file.DisplayName, ReleaseChannel: curseForgeReleaseChannel(file.ReleaseType),
		GameVersions: versions, Loaders: loaders, PublishedAt: publishedAt, SizeBytes: file.FileLength,
		DownloadCount: file.DownloadCount, SHA1: sha1, DirectURL: file.DownloadURL,
	}
}

func loadCurseForgeProjectFileByID(ctx context.Context, projectID, fileID string, cfg modImportConfig) (providerProjectFile, error) {
	if cfg.CurseForge.APIKey == "" {
		return providerProjectFile{}, errors.New("CurseForge is not configured")
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.CurseForge.BaseURL)
	if err != nil {
		return providerProjectFile{}, err
	}
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, projectID, cfg, headers)
	if err != nil {
		return providerProjectFile{}, err
	}
	numericFileID, err := strconv.ParseInt(strings.TrimSpace(fileID), 10, 64)
	if err != nil || numericFileID <= 0 {
		return providerProjectFile{}, errors.New("invalid CurseForge file ID")
	}
	var response struct {
		Data curseForgeProjectFile `json:"data"`
	}
	endpoint := cfg.CurseForge.BaseURL + "/mods/" + numericID + "/files/" + strconv.FormatInt(numericFileID, 10)
	if err = getProviderJSONLimited(ctx, client, endpoint, headers, projectFileProviderJSONLimit, &response); err != nil {
		return providerProjectFile{}, err
	}
	if response.Data.ID != numericFileID {
		return providerProjectFile{}, errors.New("CurseForge returned the wrong file")
	}
	return curseForgeProviderProjectFile(response.Data), nil
}

func getProviderJSONLimited(ctx context.Context, client *http.Client, endpoint string, headers http.Header, maximumBytes int64, target any) error {
	body, err := getProviderBytesLimited(ctx, client, endpoint, headers, maximumBytes)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}
	return nil
}

// cachedProviderProjectFiles remains for bounded background automation and
// favorite export selection. The public project-file list never calls it.
func (s *Server) cachedProviderProjectFiles(ctx context.Context, source, projectID string, cfg modImportConfig) ([]providerProjectFile, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("provider project ID is missing")
	}
	key := "project-files:provider:bounded:v2:" + source + ":" + strings.ToLower(projectID)
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
	if len(files) > 500 {
		return nil, errors.New("cached provider file list exceeds its hard limit")
	}
	return files, nil
}

func loadModrinthProjectFiles(ctx context.Context, projectID string, cfg modImportConfig) ([]providerProjectFile, error) {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, cfg.Modrinth.BaseURL)
	if err != nil {
		return nil, err
	}
	headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), "")
	endpoint, _ := url.Parse(cfg.Modrinth.BaseURL + "/project/" + url.PathEscape(projectID) + "/version")
	query := endpoint.Query()
	query.Set("include_changelog", "false")
	endpoint.RawQuery = query.Encode()
	var versions []modrinthProjectVersion
	if err = getProviderJSONLimited(ctx, client, endpoint.String(), headers, projectFileProviderJSONLimit, &versions); err != nil {
		return nil, err
	}
	if len(versions) > 500 {
		return nil, errors.New("Modrinth project contains too many versions for bulk automation")
	}
	result := make([]providerProjectFile, 0, len(versions))
	for _, version := range versions {
		if len(version.Files) > projectFileMaximumFilesPerVersion || len(result)+len(version.Files) > 500 {
			return nil, errors.New("Modrinth project contains too many files for bulk automation")
		}
		result = append(result, modrinthVersionProjectFiles(version, projectID)...)
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
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	numericID, err := resolveCurseForgeProjectID(ctx, client, projectID, cfg, headers)
	if err != nil {
		return nil, err
	}
	result := make([]providerProjectFile, 0, 100)
	for page, index := 0, 0; page != 10; page++ {
		endpoint, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/" + numericID + "/files")
		query := endpoint.Query()
		query.Set("index", strconv.Itoa(index))
		query.Set("pageSize", "50")
		endpoint.RawQuery = query.Encode()
		var response struct {
			Data       []curseForgeProjectFile `json:"data"`
			Pagination struct {
				ResultCount int `json:"resultCount"`
				TotalCount  int `json:"totalCount"`
			} `json:"pagination"`
		}
		if err = getProviderJSONLimited(ctx, client, endpoint.String(), headers, projectFileProviderJSONLimit, &response); err != nil {
			return nil, err
		}
		if len(response.Data) > 50 || len(result)+len(response.Data) > 500 {
			return nil, errors.New("CurseForge project contains too many files for bulk automation")
		}
		for _, file := range response.Data {
			result = append(result, curseForgeProviderProjectFile(file))
		}
		count := response.Pagination.ResultCount
		if count == 0 {
			count = len(response.Data)
		}
		index += count
		if count == 0 || response.Pagination.TotalCount > 0 && index >= response.Pagination.TotalCount {
			return result, nil
		}
		if page == 9 {
			return nil, errors.New("CurseForge project contains more than 500 files for bulk automation")
		}
	}
	return result, nil
}
