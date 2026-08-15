package httpapi

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxModpackArchiveBytes = int64(1 << 30)
	maxModpackIndexBytes   = int64(32 << 20)
)

type modrinthVersion struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	VersionNumber string   `json:"version_number"`
	GameVersions  []string `json:"game_versions"`
	Loaders       []string `json:"loaders"`
	Files         []struct {
		URL      string `json:"url"`
		Filename string `json:"filename"`
		Primary  bool   `json:"primary"`
		Size     int64  `json:"size"`
	} `json:"files"`
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

var modrinthCDNProjectPattern = regexp.MustCompile(`/data/([^/]+)/versions/([^/]+)/`)

func importModrinthModpack(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModpackRequest, error) {
	headers := providerHeaders(cfg.UserAgent, "", "")
	if cfg.Modrinth.Token != "" {
		headers.Set("Authorization", cfg.Modrinth.Token)
	}
	var project modrinthProject
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(reference), headers, &project); err != nil {
		return createModpackRequest{}, fmt.Errorf("读取 Modrinth 整合包失败: %w", err)
	}
	if project.ProjectType != "modpack" {
		return createModpackRequest{}, errors.New("该 Modrinth 项目不是整合包")
	}
	var versions []modrinthVersion
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(project.ID)+"/version", headers, &versions); err != nil {
		return createModpackRequest{}, fmt.Errorf("读取 Modrinth 整合包版本失败: %w", err)
	}
	version, archive, err := selectModrinthPackArchive(versions)
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
	authors := modrinthProjectAuthors(ctx, client, cfg, headers, project.Team)
	categoryValues := append(append([]string{}, project.Categories...), project.AdditionalCategories...)
	loaders, minecraftVersions := modrinthPackCompatibility(project, version, index)
	return createModpackRequest{
		SiteID:            modSiteIDBase(project.Slug),
		PrimaryName:       project.Title,
		Summary:           project.Description,
		DefaultLocale:     "zh-CN",
		Environment:       modpackEnvironmentFromSides(project.ClientSide, project.ServerSide),
		PrimaryCategory:   primaryCategoryFromExternal(categoryValues),
		PackType:          modpackTypeFromExternal(categoryValues),
		PackagingMethod:   "modrinth",
		Compatibilities:   compatibilitiesForLoaders(loaders, minecraftVersions),
		Tags:              modpackCategoriesFromExternal(categoryValues),
		SearchKeywords:    uniqueTrimmed([]string{project.Slug, index.Name, index.VersionID}, 80),
		Authors:           authors,
		OfficialStatus:    statusFromExternal(project.Status, false),
		SourceStatus:      sourceStatusFromLicense(project.License.ID),
		License:           normalizeExternalLicense(project.License.ID),
		ModrinthProjectID: project.ID,
		IconURL:           project.IconURL,
		BodyMarkdown:      project.Body,
		SubmissionMethod:  "modrinth",
		Mods:              modrinthIndexMods(index),
		Links: compactLinks([]modLinkPayload{
			{Type: "modrinth", URL: "https://modrinth.com/modpack/" + project.Slug},
			externalSourceLink(project.SourceURL),
			{Type: "wiki", URL: project.WikiURL},
			{Type: "issue", URL: project.IssuesURL},
			{Type: "discord", URL: project.DiscordURL},
		}),
	}, nil
}

func selectModrinthPackArchive(versions []modrinthVersion) (modrinthVersion, struct {
	URL      string
	Filename string
}, error) {
	for _, version := range versions {
		for _, primaryOnly := range []bool{true, false} {
			for _, file := range version.Files {
				if primaryOnly && !file.Primary || !strings.EqualFold(filepath.Ext(file.Filename), ".mrpack") || !validProviderDownloadURL(file.URL) {
					continue
				}
				return version, struct {
					URL      string
					Filename string
				}{URL: file.URL, Filename: file.Filename}, nil
			}
		}
	}
	return modrinthVersion{}, struct {
		URL      string
		Filename string
	}{}, errors.New("没有可下载的 Modrinth mrpack 文件")
}

func modrinthProjectAuthors(ctx context.Context, client *http.Client, cfg modImportConfig, headers http.Header, teamID string) []modAuthorPayload {
	if teamID == "" {
		return nil
	}
	var members []modrinthTeamMember
	if getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/team/"+url.PathEscape(teamID)+"/members", headers, &members) != nil {
		return nil
	}
	authors := make([]modAuthorPayload, 0, len(members))
	for _, member := range members {
		name := member.User.Name
		if name == "" {
			name = member.User.Username
		}
		authors = append(authors, modAuthorPayload{Name: name, Kind: "author", AvatarURL: member.User.AvatarURL, Role: member.Role})
	}
	return authors
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
		projectID, versionID := "", ""
		for _, download := range file.Downloads {
			if match := modrinthCDNProjectPattern.FindStringSubmatch(download); len(match) == 3 {
				projectID, versionID = match[1], match[2]
				break
			}
		}
		identifier := ""
		if projectID == "" {
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
	headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
	searchURL, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/search")
	query := searchURL.Query()
	query.Set("gameId", "432")
	query.Set("classId", "4471")
	query.Set("slug", reference)
	query.Set("pageSize", "1")
	searchURL.RawQuery = query.Encode()
	var search struct {
		Data []curseForgeMod `json:"data"`
	}
	if err := getProviderJSON(ctx, client, searchURL.String(), headers, &search); err != nil {
		return createModpackRequest{}, fmt.Errorf("搜索 CurseForge 整合包失败: %w", err)
	}
	if len(search.Data) == 0 || !strings.EqualFold(search.Data[0].Slug, reference) {
		return createModpackRequest{}, errors.New("CurseForge 整合包不存在")
	}
	project := search.Data[0]
	var description struct {
		Data string `json:"data"`
	}
	_ = getProviderJSON(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+strconv.FormatInt(project.ID, 10)+"/description", headers, &description)
	var files struct {
		Data []struct {
			ID           int64    `json:"id"`
			FileName     string   `json:"fileName"`
			DownloadURL  string   `json:"downloadUrl"`
			GameVersions []string `json:"gameVersions"`
		} `json:"data"`
	}
	filesEndpoint := cfg.CurseForge.BaseURL + "/mods/" + strconv.FormatInt(project.ID, 10) + "/files?pageSize=50"
	if err := getProviderJSON(ctx, client, filesEndpoint, headers, &files); err != nil || len(files.Data) == 0 {
		return createModpackRequest{}, errors.New("CurseForge 整合包没有可下载文件")
	}
	selected := files.Data[0]
	if selected.DownloadURL == "" {
		selected.DownloadURL, _ = sCurseForgeDownloadURL(ctx, client, cfg, project.ID, selected.ID, headers)
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
	compatibilities := curseForgePackCompatibilities(manifest, selected.GameVersions)
	categoryValues := make([]string, 0, len(project.Categories)*2)
	for _, category := range project.Categories {
		categoryValues = append(categoryValues, category.Name, category.Slug)
	}
	authors := make([]modAuthorPayload, 0, len(project.Authors))
	for _, author := range project.Authors {
		authors = append(authors, modAuthorPayload{Name: author.Name, Kind: "author", AvatarURL: author.AvatarURL, Role: "Author"})
	}
	mods := make([]modpackModPayload, 0, len(manifest.Files))
	for _, file := range manifest.Files {
		mods = append(mods, modpackModPayload{Provider: "curseforge", ProviderProjectID: strconv.FormatInt(file.ProjectID, 10),
			ProviderVersionID: strconv.FormatInt(file.FileID, 10), ClientRequired: file.Required, ServerRequired: file.Required})
	}
	return createModpackRequest{
		SiteID: modSiteIDBase(project.Slug), PrimaryName: project.Name, Summary: project.Summary, DefaultLocale: "zh-CN",
		Environment: "bothRequired", PrimaryCategory: primaryCategoryFromExternal(categoryValues),
		PackType: modpackTypeFromExternal(categoryValues), PackagingMethod: "curseforge", Compatibilities: compatibilities,
		Tags: modpackCategoriesFromExternal(categoryValues), SearchKeywords: uniqueTrimmed([]string{project.Slug, manifest.Name, manifest.Version}, 80),
		Authors: authors, OfficialStatus: statusFromExternal("active", !project.IsAvailable), SourceStatus: "unknown", License: "Custom",
		CurseForgeProjectID: strconv.FormatInt(project.ID, 10), IconURL: project.Logo.ThumbnailURL,
		BodyMarkdown: htmlToMarkdown(description.Data), SubmissionMethod: "curseforge", Mods: mods,
		Links: compactLinks([]modLinkPayload{{Type: "curseforge", URL: project.Links.WebsiteURL}, externalSourceLink(project.Links.SourceURL),
			{Type: "wiki", URL: project.Links.WikiURL}, {Type: "issue", URL: project.Links.IssuesURL}}),
	}, nil
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
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
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
