package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxModpackArchiveBytes = int64(1 << 30)
	maxModpackIndexBytes   = int64(32 << 20)
	maxModpackArchiveFiles = 4096
	maxModpackIndexFiles   = 2000
	maxModpackCentralBytes = uint64(8 << 20)
	externalModpackLocale  = "und"
)

type modrinthVersionFile struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Primary  bool   `json:"primary"`
	Size     int64  `json:"size"`
}

type modrinthVersion struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	VersionNumber string                `json:"version_number"`
	VersionType   string                `json:"version_type"`
	Status        string                `json:"status"`
	DatePublished time.Time             `json:"date_published"`
	GameVersions  []string              `json:"game_versions"`
	Loaders       []string              `json:"loaders"`
	Files         []modrinthVersionFile `json:"files"`
}

type curseForgePackFile struct {
	ID                  int64     `json:"id"`
	DisplayName         string    `json:"displayName"`
	FileName            string    `json:"fileName"`
	DownloadURL         string    `json:"downloadUrl"`
	GameVersions        []string  `json:"gameVersions"`
	ReleaseType         int       `json:"releaseType"`
	FileStatus          int       `json:"fileStatus"`
	IsAvailable         bool      `json:"isAvailable"`
	IsServerPack        bool      `json:"isServerPack"`
	ExposeAsAlternative bool      `json:"exposeAsAlternative"`
	ParentProjectFileID int64     `json:"parentProjectFileId"`
	FileDate            time.Time `json:"fileDate"`
}

type modrinthPackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Dependencies  map[string]string `json:"dependencies"`
	Files         []struct {
		Path      string   `json:"path"`
		Downloads []string `json:"downloads"`
		Env       struct {
			Client string `json:"client"`
			Server string `json:"server"`
		} `json:"env"`
	} `json:"files"`
}

type curseForgePackManifest struct {
	Minecraft struct {
		Version    string `json:"version"`
		ModLoaders []struct {
			ID      string `json:"id"`
			Primary bool   `json:"primary"`
		} `json:"modLoaders"`
	} `json:"minecraft"`
	ManifestType    string `json:"manifestType"`
	ManifestVersion int    `json:"manifestVersion"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Files           []struct {
		ProjectID int64 `json:"projectID"`
		FileID    int64 `json:"fileID"`
		Required  bool  `json:"required"`
	} `json:"files"`
}

func importModrinthModpack(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModpackRequest, error) {
	snapshot, err := loadModrinthProviderSnapshot(ctx, client, cfg, reference)
	if err != nil {
		return createModpackRequest{}, err
	}
	project := snapshot.Project
	if project.ProjectType != "modpack" {
		return createModpackRequest{}, errors.New("该 Modrinth 项目不是整合包")
	}
	version, archive, err := selectModrinthPackArchive(snapshot.Versions)
	if err != nil {
		return createModpackRequest{}, err
	}
	temporaryDirectory, err := os.MkdirTemp("", "mcmods-modpack-import-*")
	if err != nil {
		return createModpackRequest{}, fmt.Errorf("创建整合包临时目录失败: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	archivePath := filepath.Join(temporaryDirectory, safeTemporaryArchiveName(archive.Filename, ".mrpack"))
	if err = downloadProviderArchive(ctx, archive.URL, archivePath, cfg.RequestTimeoutSeconds); err != nil {
		return createModpackRequest{}, fmt.Errorf("下载 Modrinth 整合包失败: %w", err)
	}
	var index modrinthPackIndex
	if err = readJSONFromArchive(archivePath, []string{"modrinth.index.json"}, &index); err != nil {
		return createModpackRequest{}, fmt.Errorf("读取 modrinth.index.json 失败: %w", err)
	}
	if index.Game != "minecraft" || index.FormatVersion <= 0 {
		return createModpackRequest{}, errors.New("modrinth.index.json 不是有效的 Minecraft 整合包索引")
	}
	if len(index.Files) > maxModpackIndexFiles {
		return createModpackRequest{}, fmt.Errorf("modrinth.index.json contains more than %d files", maxModpackIndexFiles)
	}
	categoryValues := append(append([]string{}, project.Categories...), project.AdditionalCategories...)
	loaders, minecraftVersions := modrinthPackCompatibility(project, version, index)
	return createModpackRequest{
		SiteID:            modSiteIDBase(project.Slug),
		PrimaryName:       project.Title,
		Summary:           project.Description,
		DefaultLocale:     externalModpackLocale,
		Environment:       modpackEnvironmentFromSides(project.ClientSide, project.ServerSide),
		PrimaryCategory:   primaryCategoryFromExternal(categoryValues),
		PackType:          modpackTypeFromExternal(categoryValues),
		PackagingMethod:   "modrinth",
		Compatibilities:   compatibilitiesForLoaders(loaders, minecraftVersions),
		Tags:              modpackCategoriesFromExternal(categoryValues),
		SearchKeywords:    uniqueTrimmed([]string{project.Slug, index.Name, index.VersionID}, 80),
		Authors:           snapshot.Authors,
		OfficialStatus:    statusFromExternal(project.Status, false),
		SourceStatus:      sourceStatusFromLicense(project.License.ID),
		License:           normalizeExternalLicense(project.License.ID),
		ModrinthProjectID: project.ID,
		IconURL:           project.IconURL,
		BodyMarkdown:      project.Body,
		SubmissionMethod:  "modrinth",
		Mods:              modrinthIndexMods(index),
		ImportSelection: &modpackImportSelection{
			Provider: "modrinth", ProjectID: project.ID, VersionID: version.ID, VersionName: firstNonEmpty(version.Name, version.VersionNumber),
			FileName: archive.Filename, ReleaseType: "release", PublishedAt: version.DatePublished.UTC().Format(time.RFC3339),
		},
		Links: compactLinks([]modLinkPayload{
			{Type: "modrinth", URL: "https://modrinth.com/modpack/" + project.Slug},
			externalSourceLink(project.SourceURL),
			{Type: "wiki", URL: project.WikiURL},
			{Type: "issue", URL: project.IssuesURL},
			{Type: "discord", URL: project.DiscordURL},
		}),
	}, nil
}

func selectModrinthPackArchive(versions []modrinthVersion) (modrinthVersion, modrinthVersionFile, error) {
	type candidate struct {
		version modrinthVersion
		file    modrinthVersionFile
	}
	candidates := make([]candidate, 0)
	for _, version := range versions {
		if !strings.EqualFold(version.VersionType, "release") || !strings.EqualFold(version.Status, "listed") || version.DatePublished.IsZero() {
			continue
		}
		for _, file := range version.Files {
			if !file.Primary || !strings.EqualFold(filepath.Ext(file.Filename), ".mrpack") || !validProviderDownloadURL(file.URL) {
				continue
			}
			candidates = append(candidates, candidate{version: version, file: file})
		}
	}
	sort.Slice(candidates, func(left, right int) bool {
		if !candidates[left].version.DatePublished.Equal(candidates[right].version.DatePublished) {
			return candidates[left].version.DatePublished.After(candidates[right].version.DatePublished)
		}
		if candidates[left].version.ID != candidates[right].version.ID {
			return candidates[left].version.ID > candidates[right].version.ID
		}
		if candidates[left].file.Filename != candidates[right].file.Filename {
			return candidates[left].file.Filename < candidates[right].file.Filename
		}
		return candidates[left].file.URL < candidates[right].file.URL
	})
	if len(candidates) == 0 {
		return modrinthVersion{}, modrinthVersionFile{}, errors.New("没有已列出的正式 Modrinth 主 mrpack 文件")
	}
	return candidates[0].version, candidates[0].file, nil
}

func modrinthPackCompatibility(project modrinthProject, version modrinthVersion, index modrinthPackIndex) ([]string, []string) {
	loaders := normalizeLoaders(version.Loaders)
	if len(loaders) == 0 {
		loaders = normalizeLoaders(project.Loaders)
	}
	if len(loaders) == 0 {
		for dependency := range index.Dependencies {
			loaders = append(loaders, normalizeLoaders([]string{dependency})...)
		}
	}
	versions := uniqueTrimmed(version.GameVersions, 500)
	if len(versions) == 0 && index.Dependencies["minecraft"] != "" {
		versions = []string{index.Dependencies["minecraft"]}
	}
	if len(versions) == 0 {
		versions = uniqueTrimmed(project.GameVersions, 500)
	}
	return uniqueTrimmed(loaders, 30), versions
}

func modrinthIndexMods(index modrinthPackIndex) []modpackModPayload {
	mods := make([]modpackModPayload, 0, len(index.Files))
	for _, file := range index.Files {
		if !strings.HasPrefix(strings.ReplaceAll(file.Path, "\\", "/"), "mods/") {
			continue
		}
		projectID, versionID, trustedIdentity := consistentModrinthDownloadIdentity(file.Downloads)
		identifier := ""
		if !trustedIdentity {
			projectID, versionID = "", ""
			identifier = strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))
		}
		mods = append(mods, modpackModPayload{
			ModName: filepath.Base(file.Path), Provider: "modrinth", ProviderProjectID: projectID,
			ProviderVersionID: versionID, Identifier: identifier, FileName: filepath.Base(file.Path),
			ClientRequired: sideRequired(file.Env.Client), ServerRequired: sideRequired(file.Env.Server),
		})
	}
	return mods
}

func importCurseForgeModpack(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModpackRequest, error) {
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	snapshot, err := loadCurseForgeProviderSnapshot(ctx, client, cfg, headers, 4471, reference)
	if err != nil {
		return createModpackRequest{}, err
	}
	project := snapshot.Project
	var files struct {
		Data []curseForgePackFile `json:"data"`
	}
	filesEndpoint := cfg.CurseForge.BaseURL + "/mods/" + strconv.FormatInt(project.ID, 10) + "/files?pageSize=50"
	if err := getProviderJSON(ctx, client, filesEndpoint, headers, &files); err != nil || len(files.Data) == 0 {
		return createModpackRequest{}, errors.New("CurseForge 整合包没有可下载文件")
	}
	selected, err := selectCurseForgePackFile(files.Data)
	if err != nil {
		return createModpackRequest{}, err
	}
	if selected.DownloadURL == "" {
		selected.DownloadURL, err = sCurseForgeDownloadURL(ctx, client, cfg, project.ID, selected.ID, headers)
		if err != nil {
			return createModpackRequest{}, fmt.Errorf("读取 CurseForge 整合包下载地址失败: %w", err)
		}
	}
	if !validProviderDownloadURL(selected.DownloadURL) {
		return createModpackRequest{}, errors.New("CurseForge 未返回安全的整合包下载地址")
	}
	temporaryDirectory, err := os.MkdirTemp("", "mcmods-modpack-import-*")
	if err != nil {
		return createModpackRequest{}, fmt.Errorf("创建整合包临时目录失败: %w", err)
	}
	defer os.RemoveAll(temporaryDirectory)
	archivePath := filepath.Join(temporaryDirectory, safeTemporaryArchiveName(selected.FileName, ".zip"))
	if err = downloadProviderArchive(ctx, selected.DownloadURL, archivePath, cfg.RequestTimeoutSeconds); err != nil {
		return createModpackRequest{}, fmt.Errorf("下载 CurseForge 整合包失败: %w", err)
	}
	var manifest curseForgePackManifest
	if err = readJSONFromArchive(archivePath, []string{"manifest.json"}, &manifest); err != nil {
		return createModpackRequest{}, fmt.Errorf("读取 CurseForge manifest.json 失败: %w", err)
	}
	if len(manifest.Files) > maxModpackIndexFiles {
		return createModpackRequest{}, fmt.Errorf("CurseForge manifest contains more than %d files", maxModpackIndexFiles)
	}
	compatibilities := curseForgePackCompatibilities(manifest, selected.GameVersions)
	categoryValues := make([]string, 0, len(project.Categories)*2)
	for _, category := range project.Categories {
		categoryValues = append(categoryValues, category.Name, category.Slug)
	}
	mods := make([]modpackModPayload, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		mods = append(mods, modpackModPayload{Provider: "curseforge", ProviderProjectID: strconv.FormatInt(file.ProjectID, 10),
			ProviderVersionID: strconv.FormatInt(file.FileID, 10), ClientRequired: file.Required, ServerRequired: file.Required})
	}
	return createModpackRequest{
		SiteID: modSiteIDBase(project.Slug), PrimaryName: project.Name, Summary: project.Summary, DefaultLocale: externalModpackLocale,
		Environment: "bothRequired", PrimaryCategory: primaryCategoryFromExternal(categoryValues),
		PackType: modpackTypeFromExternal(categoryValues), PackagingMethod: "curseforge", Compatibilities: compatibilities,
		Tags: modpackCategoriesFromExternal(categoryValues), SearchKeywords: uniqueTrimmed([]string{project.Slug, manifest.Name, manifest.Version}, 80),
		Authors: curseForgeAuthors(project), OfficialStatus: statusFromExternal("active", !project.IsAvailable), SourceStatus: "unknown", License: "Custom",
		CurseForgeProjectID: strconv.FormatInt(project.ID, 10), IconURL: project.Logo.ThumbnailURL,
		BodyMarkdown: htmlToMarkdown(snapshot.DescriptionHTML), SubmissionMethod: "curseforge", Mods: mods,
		ImportSelection: &modpackImportSelection{
			Provider: "curseforge", ProjectID: strconv.FormatInt(project.ID, 10), VersionID: strconv.FormatInt(selected.ID, 10),
			VersionName: selected.DisplayName, FileID: strconv.FormatInt(selected.ID, 10), FileName: selected.FileName,
			ReleaseType: "release", PublishedAt: selected.FileDate.UTC().Format(time.RFC3339),
		},
		Links: compactLinks([]modLinkPayload{{Type: "curseforge", URL: project.Links.WebsiteURL}, externalSourceLink(project.Links.SourceURL),
			{Type: "wiki", URL: project.Links.WikiURL}, {Type: "issue", URL: project.Links.IssuesURL}}),
	}, nil
}

func selectCurseForgePackFile(files []curseForgePackFile) (curseForgePackFile, error) {
	candidates := make([]curseForgePackFile, 0, len(files))
	for _, file := range files {
		if file.ID <= 0 || file.ReleaseType != 1 || file.FileStatus != 4 || !file.IsAvailable || file.IsServerPack ||
			file.ExposeAsAlternative || file.ParentProjectFileID != 0 || file.FileDate.IsZero() || !strings.EqualFold(filepath.Ext(file.FileName), ".zip") {
			continue
		}
		candidates = append(candidates, file)
	}
	sort.Slice(candidates, func(left, right int) bool {
		if !candidates[left].FileDate.Equal(candidates[right].FileDate) {
			return candidates[left].FileDate.After(candidates[right].FileDate)
		}
		if candidates[left].ID != candidates[right].ID {
			return candidates[left].ID > candidates[right].ID
		}
		return candidates[left].FileName < candidates[right].FileName
	})
	if len(candidates) == 0 {
		return curseForgePackFile{}, errors.New("CurseForge 整合包没有已批准的正式主文件")
	}
	return candidates[0], nil
}

func modpackCategoriesFromExternal(values []string) []string {
	joined := strings.ToLower(strings.Join(values, " "))
	mapping := map[string][]string{
		"technology":   {"technology", "tech", "automation"},
		"magic":        {"magic"},
		"adventure":    {"adventure", "exploration"},
		"building":     {"building", "decoration"},
		"map":          {"map-based", "map based"},
		"quests":       {"quest"},
		"optimization": {"optimization", "performance"},
		"hardcore":     {"hardcore", "expert"},
		"casual":       {"casual"},
		"large":        {"large", "kitchen sink"},
		"lightweight":  {"lightweight", "light pack", "vanilla+"},
		"story":        {"story", "storyline"},
		"kitchen_sink": {"kitchen sink", "kitchen-sink"},
		"skyblock":     {"skyblock", "sky block"},
		"pvp":          {"pvp"},
		"chinese":      {"chinese", "国创"},
	}
	result := make([]string, 0, len(mapping))
	for category, words := range mapping {
		for _, word := range words {
			if strings.Contains(joined, word) {
				result = append(result, category)
				break
			}
		}
	}
	sort.Strings(result)
	return result
}

func modpackTypeFromExternal(values []string) string {
	joined := strings.ToLower(strings.Join(values, " "))
	for _, marker := range []string{"expert", "hardcore", "questing", "progression", "customized", "overhauled"} {
		if strings.Contains(joined, marker) {
			return "customized"
		}
	}
	return "native"
}

func sCurseForgeDownloadURL(ctx context.Context, client *http.Client, cfg modImportConfig, projectID, fileID int64, headers http.Header) (string, error) {
	var result struct {
		Data string `json:"data"`
	}
	endpoint := fmt.Sprintf("%s/mods/%d/files/%d/download-url", cfg.CurseForge.BaseURL, projectID, fileID)
	if err := getProviderJSON(ctx, client, endpoint, headers, &result); err != nil {
		return "", err
	}
	return result.Data, nil
}

func curseForgePackCompatibilities(manifest curseForgePackManifest, fallbackVersions []string) []modLoaderCompatibilityPayload {
	loaders := make([]string, 0, len(manifest.Minecraft.ModLoaders))
	for _, loader := range manifest.Minecraft.ModLoaders {
		name := strings.SplitN(loader.ID, "-", 2)[0]
		loaders = append(loaders, normalizeLoaders([]string{name})...)
	}
	versions := []string{manifest.Minecraft.Version}
	if manifest.Minecraft.Version == "" {
		versions = fallbackVersions
	}
	return compatibilitiesForLoaders(uniqueTrimmed(loaders, 30), versions)
}

func downloadProviderArchive(ctx context.Context, rawURL, destination string, timeoutSeconds int) error {
	if !validProviderDownloadURL(rawURL) {
		return errors.New("provider archive URL is not allowed")
	}
	client, err := newProviderHTTPClient(time.Duration(timeoutSeconds)*time.Second, rawURL)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("archive download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxModpackArchiveBytes {
		return errors.New("modpack archive is too large")
	}
	output, err := os.Create(destination)
	if err != nil {
		return err
	}
	limited := io.LimitReader(response.Body, maxModpackArchiveBytes+1)
	written, copyErr := io.Copy(output, limited)
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxModpackArchiveBytes {
		return errors.New("modpack archive is too large")
	}
	return nil
}

func readJSONFromArchive(archivePath string, candidateNames []string, target any) error {
	if err := validateModpackArchiveCentralDirectory(archivePath); err != nil {
		return err
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) > maxModpackArchiveFiles {
		return fmt.Errorf("modpack archive central directory contains more than %d entries", maxModpackArchiveFiles)
	}
	candidates := make(map[string]bool, len(candidateNames))
	for _, name := range candidateNames {
		candidates[strings.ToLower(strings.TrimLeft(filepath.ToSlash(name), "/"))] = true
	}
	for _, file := range archive.File {
		name := strings.ToLower(strings.TrimLeft(filepath.ToSlash(file.Name), "/"))
		if !candidates[name] {
			continue
		}
		if file.UncompressedSize64 > uint64(maxModpackIndexBytes) {
			return errors.New("modpack index is too large")
		}
		reader, openErr := file.Open()
		if openErr != nil {
			return openErr
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, maxModpackIndexBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if int64(len(body)) > maxModpackIndexBytes {
			return errors.New("modpack index is too large")
		}
		return json.Unmarshal(body, target)
	}
	return errors.New("modpack index file is missing")
}

func validateModpackArchiveCentralDirectory(archivePath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > maxModpackArchiveBytes {
		return errors.New("modpack archive is too large")
	}
	const (
		endOfCentralDirectorySize = int64(22)
		maximumZIPCommentBytes    = int64(1<<16 - 1)
	)
	if info.Size() < endOfCentralDirectorySize {
		return errors.New("modpack archive central directory is missing")
	}
	tailSize := min(info.Size(), endOfCentralDirectorySize+maximumZIPCommentBytes)
	tail := make([]byte, int(tailSize))
	if _, err = file.ReadAt(tail, info.Size()-tailSize); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	signature := []byte{'P', 'K', 0x05, 0x06}
	recordOffset := bytes.LastIndex(tail, signature)
	if recordOffset < 0 || recordOffset+int(endOfCentralDirectorySize) > len(tail) {
		return errors.New("modpack archive central directory is missing")
	}
	record := tail[recordOffset:]
	commentLength := int(binary.LittleEndian.Uint16(record[20:22]))
	if recordOffset+int(endOfCentralDirectorySize)+commentLength != len(tail) {
		return errors.New("modpack archive central directory terminator is invalid")
	}
	diskNumber := binary.LittleEndian.Uint16(record[4:6])
	centralDisk := binary.LittleEndian.Uint16(record[6:8])
	entriesOnDisk := binary.LittleEndian.Uint16(record[8:10])
	totalEntries := binary.LittleEndian.Uint16(record[10:12])
	if diskNumber != 0 || centralDisk != 0 || entriesOnDisk != totalEntries {
		return errors.New("multi-disk modpack archive central directory is not supported")
	}
	if int(totalEntries) > maxModpackArchiveFiles {
		return fmt.Errorf("modpack archive central directory contains more than %d entries", maxModpackArchiveFiles)
	}
	centralSize := uint64(binary.LittleEndian.Uint32(record[12:16]))
	centralOffset := uint64(binary.LittleEndian.Uint32(record[16:20]))
	if centralSize > maxModpackCentralBytes {
		return fmt.Errorf("modpack archive central directory exceeds %d bytes", maxModpackCentralBytes)
	}
	if centralOffset > uint64(info.Size()) || centralSize > uint64(info.Size())-centralOffset {
		return errors.New("modpack archive central directory points outside the file")
	}
	return nil
}

func safeTemporaryArchiveName(name, fallbackExtension string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" {
		return "modpack" + fallbackExtension
	}
	return name
}

func sideRequired(value string) bool {
	return strings.EqualFold(value, "required")
}

func modpackEnvironmentFromSides(clientSide, serverSide string) string {
	clientSide = strings.ToLower(clientSide)
	serverSide = strings.ToLower(serverSide)
	switch {
	case clientSide == "required" && serverSide == "unsupported":
		return "clientOnly"
	case serverSide == "required" && clientSide == "unsupported":
		return "serverOnly"
	default:
		return "bothRequired"
	}
}
