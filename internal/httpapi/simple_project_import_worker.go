package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func importModrinthSimpleProject(ctx context.Context, client *http.Client, cfg modImportConfig, projectType, sourceURL, reference string) (simpleProjectSnapshot, error) {
	headers := providerHeaders(cfg.UserAgent, "", "")
	if cfg.Modrinth.Token != "" {
		headers.Set("Authorization", cfg.Modrinth.Token)
	}
	var project modrinthProject
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(reference), headers, &project); err != nil {
		return simpleProjectSnapshot{}, fmt.Errorf("read Modrinth project: %w", err)
	}
	var versions []modrinthVersion
	_ = getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(project.ID)+"/version", headers, &versions)
	gameVersions := append([]string(nil), project.GameVersions...)
	externalLoaders := append([]string(nil), project.Loaders...)
	for _, version := range versions {
		gameVersions = append(gameVersions, version.GameVersions...)
		externalLoaders = append(externalLoaders, version.Loaders...)
	}
	if !modrinthProjectMatchesSimpleType(projectType, project.ProjectType, externalLoaders) {
		return simpleProjectSnapshot{}, fmt.Errorf("the Modrinth project is not a %s", projectImportDisplayName(projectType))
	}
	authors := make([]modAuthorPayload, 0)
	if project.Team != "" {
		var members []modrinthTeamMember
		if getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/team/"+url.PathEscape(project.Team)+"/members", headers, &members) == nil {
			for _, member := range members {
				name := strings.TrimSpace(member.User.Name)
				if name == "" {
					name = strings.TrimSpace(member.User.Username)
				}
				if name != "" {
					authors = append(authors, modAuthorPayload{Name: name, Kind: "author", AvatarURL: member.User.AvatarURL, Role: member.Role})
				}
			}
		}
	}
	externalValues := append(append([]string{}, project.Categories...), project.AdditionalCategories...)
	externalValues = append(externalValues, externalLoaders...)
	externalValues = append(externalValues, project.Title, project.Description, project.Body)
	return simpleProjectDraftFromExternal(simpleProjectImportData{
		ProjectType: projectType, Provider: "modrinth", ProviderURL: sourceURL, ProviderProjectID: project.ID,
		Slug: project.Slug, Name: project.Title, Summary: project.Description, BodyMarkdown: project.Body,
		IconURL: project.IconURL, MinecraftVersions: gameVersions, ExternalValues: externalValues,
		Status: project.Status, Archived: strings.EqualFold(project.Status, "archived"), License: project.License.ID,
		Authors: authors, SourceCodeURL: project.SourceURL, IssuesURL: project.IssuesURL, WikiURL: project.WikiURL,
		DiscordURL: project.DiscordURL,
	}), nil
}

func importCurseForgeSimpleProject(ctx context.Context, client *http.Client, cfg modImportConfig, projectType, sourceURL, reference string) (simpleProjectSnapshot, error) {
	headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
	section := curseForgeSectionFromURL(sourceURL)
	classID, err := curseForgeClassID(ctx, client, cfg, headers, section)
	if err != nil {
		return simpleProjectSnapshot{}, err
	}
	searchURL, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/search")
	query := searchURL.Query()
	query.Set("gameId", "432")
	query.Set("classId", strconv.FormatInt(classID, 10))
	query.Set("slug", reference)
	query.Set("pageSize", "1")
	searchURL.RawQuery = query.Encode()
	var search struct {
		Data []curseForgeMod `json:"data"`
	}
	if err = getProviderJSON(ctx, client, searchURL.String(), headers, &search); err != nil {
		return simpleProjectSnapshot{}, fmt.Errorf("search CurseForge project: %w", err)
	}
	if len(search.Data) == 0 || !strings.EqualFold(search.Data[0].Slug, reference) || search.Data[0].ClassID != classID {
		return simpleProjectSnapshot{}, errors.New("CurseForge project was not found in the requested section")
	}
	project := search.Data[0]
	var description struct {
		Data string `json:"data"`
	}
	_ = getProviderJSON(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+strconv.FormatInt(project.ID, 10)+"/description", headers, &description)
	externalValues := []string{section}
	gameVersions := make([]string, 0, len(project.LatestFilesIndexes))
	for _, category := range project.Categories {
		externalValues = append(externalValues, category.Name, category.Slug)
	}
	for _, index := range project.LatestFilesIndexes {
		gameVersions = append(gameVersions, index.GameVersion)
		if loader := curseForgeLoader(index.ModLoader); loader != "" {
			externalValues = append(externalValues, loader)
		}
	}
	for _, file := range project.LatestFiles {
		for _, value := range file.GameVersions {
			externalValues = append(externalValues, value)
			if isMinecraftVersionLabel(value) {
				gameVersions = append(gameVersions, value)
			}
		}
	}
	externalValues = append(externalValues, project.Name, project.Summary, description.Data)
	authors := make([]modAuthorPayload, 0, len(project.Authors))
	for _, author := range project.Authors {
		if name := strings.TrimSpace(author.Name); name != "" {
			authors = append(authors, modAuthorPayload{Name: name, Kind: "author", AvatarURL: author.AvatarURL, Role: "Author"})
		}
	}
	return simpleProjectDraftFromExternal(simpleProjectImportData{
		ProjectType: projectType, Provider: "curseforge", ProviderURL: sourceURL,
		ProviderProjectID: strconv.FormatInt(project.ID, 10), Slug: project.Slug, Name: project.Name,
		Summary: project.Summary, BodyMarkdown: htmlToMarkdown(description.Data), IconURL: project.Logo.ThumbnailURL,
		MinecraftVersions: gameVersions, ExternalValues: externalValues, Status: "active", Archived: !project.IsAvailable,
		Authors: authors, SourceCodeURL: project.Links.SourceURL, IssuesURL: project.Links.IssuesURL, WikiURL: project.Links.WikiURL,
	}), nil
}

type simpleProjectImportData struct {
	ProjectType, Provider, ProviderProjectID, Slug, Name, Summary, BodyMarkdown, IconURL string
	MinecraftVersions, ExternalValues                                                    []string
	Status                                                                               string
	Archived                                                                             bool
	License                                                                              string
	Authors                                                                              []modAuthorPayload
	ProviderURL, SourceCodeURL, IssuesURL, WikiURL, DiscordURL                           string
}

func simpleProjectDraftFromExternal(data simpleProjectImportData) simpleProjectSnapshot {
	loaders := simpleProjectLoadersFromExternal(data.ProjectType, data.ExternalValues)
	categories := simpleProjectCategoriesFromExternal(data.ProjectType, data.ExternalValues)
	features := simpleProjectFeaturesFromExternal(data.ProjectType, data.ExternalValues)
	draft := simpleProjectSnapshot{
		ProjectType: data.ProjectType, SiteID: modSiteIDBase(data.Slug), DefaultLocale: "en-US",
		Localizations:     []simpleProjectLocalization{{Locale: "en-US", Name: strings.TrimSpace(data.Name), Summary: strings.TrimSpace(data.Summary), BodyMarkdown: strings.TrimSpace(data.BodyMarkdown)}},
		MinecraftVersions: uniqueMinecraftVersions(data.MinecraftVersions), Loaders: loaders, Categories: categories, Features: features,
		OfficialStatus: statusFromExternal(data.Status, data.Archived), SourceStatus: sourceStatusFromLicense(data.License),
		License: normalizeExternalLicense(data.License), IconURL: strings.TrimSpace(data.IconURL), SearchKeywords: uniqueTrimmed([]string{data.Slug}, 80),
		SubmissionMethod: data.Provider, Authors: data.Authors,
		Links: compactLinks([]modLinkPayload{{Type: data.Provider, URL: data.ProviderURL}, externalSourceLink(data.SourceCodeURL),
			{Type: "wiki", URL: data.WikiURL}, {Type: "issue", URL: data.IssuesURL}, {Type: "discord", URL: data.DiscordURL}}),
	}
	if data.Provider == "modrinth" {
		draft.ModrinthProjectID = data.ProviderProjectID
	} else {
		draft.CurseForgeProjectID = data.ProviderProjectID
	}
	switch data.ProjectType {
	case "resource_pack":
		draft.Resolution = simpleProjectResolutionFromExternal(data.ExternalValues)
	case "shader_pack":
		draft.Performance = simpleProjectPerformanceFromExternal(data.ExternalValues)
	case "map":
		draft.MapSize = "medium"
	}
	return draft
}

func modrinthProjectMatchesSimpleType(projectType, providerType string, loaders []string) bool {
	providerType = strings.ToLower(strings.TrimSpace(providerType))
	switch projectType {
	case "resource_pack":
		return providerType == "resourcepack"
	case "shader_pack":
		return providerType == "shader"
	case "plugin":
		return providerType == "plugin" || providerType == "mod" && containsExternalValue(loaders,
			"bukkit", "spigot", "paper", "purpur", "folia", "sponge", "bungeecord", "waterfall", "velocity")
	case "datapack":
		return providerType == "datapack" || providerType == "mod" && containsExternalValue(loaders, "datapack", "data pack")
	case "addon":
		return providerType == "mod" || providerType == "plugin" || providerType == "datapack" ||
			providerType == "resourcepack" || providerType == "shader"
	default:
		return false
	}
}

func curseForgeSectionFromURL(sourceURL string) string {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return ""
	}
	segments := splitURLPath(parsed.Path)
	if len(segments) >= 2 && segments[0] == "minecraft" {
		return segments[1]
	}
	return ""
}

func curseForgeClassID(ctx context.Context, client *http.Client, cfg modImportConfig, headers http.Header, section string) (int64, error) {
	endpoint, _ := url.Parse(cfg.CurseForge.BaseURL + "/categories")
	query := endpoint.Query()
	query.Set("gameId", "432")
	query.Set("classesOnly", "true")
	endpoint.RawQuery = query.Encode()
	var response struct {
		Data []struct {
			ID      int64  `json:"id"`
			Slug    string `json:"slug"`
			IsClass *bool  `json:"isClass"`
		} `json:"data"`
	}
	if err := getProviderJSON(ctx, client, endpoint.String(), headers, &response); err != nil {
		return 0, fmt.Errorf("read CurseForge project sections: %w", err)
	}
	for _, category := range response.Data {
		if strings.EqualFold(category.Slug, section) && (category.IsClass == nil || *category.IsClass) {
			return category.ID, nil
		}
	}
	return 0, fmt.Errorf("CurseForge project section %q is unavailable", section)
}

func uniqueMinecraftVersions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" && isMinecraftVersionLabel(value) {
			result = append(result, value)
		}
	}
	return uniqueTrimmed(result, 500)
}

func isMinecraftVersionLabel(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 64 {
		return false
	}
	hasDigit := false
	for _, character := range value {
		if character >= '0' && character <= '9' {
			hasDigit = true
			continue
		}
		if character != '.' && character != '-' && character != '+' && character != '_' &&
			(character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
			return false
		}
	}
	return hasDigit
}

func simpleProjectLoadersFromExternal(projectType string, values []string) []string {
	joined := externalClassificationText(values)
	allowed := map[string][]string{
		"plugin":      {"bukkit", "spigot", "paper", "purpur", "folia", "sponge", "bungeecord", "waterfall", "velocity"},
		"shader_pack": {"optifine", "iris", "oculus", "canvas"},
		"datapack":    {"vanilla", "fabric", "forge", "neoforge", "quilt"},
		"addon":       {"vanilla", "fabric", "forge", "neoforge", "quilt", "bukkit", "spigot", "paper"},
	}[projectType]
	result := make([]string, 0, len(allowed))
	for _, loader := range allowed {
		markers := []string{loader}
		if loader == "vanilla" {
			markers = []string{"vanilla", "datapack", "data pack", "minecraft"}
		}
		if containsJoinedMarker(joined, markers...) {
			result = append(result, loader)
		}
	}
	if projectType == "plugin" && len(result) == 0 && containsJoinedMarker(joined, "bukkit plugins") {
		result = append(result, "bukkit")
	}
	if projectType == "shader_pack" && len(result) == 0 {
		result = append(result, "optifine")
	}
	if projectType == "datapack" && len(result) == 0 {
		result = append(result, "vanilla")
	}
	return result
}

func simpleProjectCategoriesFromExternal(projectType string, values []string) []string {
	mappings := map[string]map[string][]string{
		"plugin": {
			"administration": {"administration", "admin tools"}, "chat": {"chat"}, "economy": {"economy"}, "gameplay": {"gameplay", "mechanics"},
			"minigame": {"minigame", "mini game"}, "permissions": {"permission"}, "protection": {"protection", "anti grief"},
			"roleplay": {"roleplay", "role play"}, "utility": {"utility", "tools"}, "world_management": {"world management", "world manager"},
		},
		"map": {
			"puzzle": {"puzzle"}, "parkour": {"parkour"}, "survival": {"survival"}, "redstone": {"redstone"}, "city": {"city"},
			"rpg": {"rpg", "role playing"}, "adventure": {"adventure"}, "pvp": {"pvp"}, "minigame": {"minigame", "mini game"},
			"horror": {"horror"}, "creation": {"creation", "creative"}, "story": {"story"}, "education": {"education"},
		},
		"resource_pack": {
			"combat": {"combat"}, "cursed": {"cursed"}, "decoration": {"decoration", "decorative"}, "modded": {"modded", "mod support"},
			"realistic": {"realistic", "photo realism"}, "simplistic": {"simplistic", "simple"}, "themed": {"themed", "medieval", "fantasy"},
			"tweaks": {"tweaks"}, "utility": {"utility"}, "vanilla_like": {"vanilla like", "traditional"},
		},
		"shader_pack": {
			"cartoon": {"cartoon", "stylized"}, "semi_realistic": {"semi realistic"}, "realistic": {"realistic", "photo realistic"},
			"vanilla": {"vanilla"}, "functional": {"functional", "utility"},
		},
		"datapack": {
			"adventure": {"adventure"}, "building": {"building"}, "decoration": {"decoration"}, "game_mechanics": {"game mechanics", "mechanics"},
			"magic": {"magic", "fantasy"}, "technology": {"technology", "tech"}, "utility": {"utility", "miscellaneous"},
			"world_generation": {"world generation", "worldgen", "structures"}, "challenge": {"challenge", "hardcore"},
			"optimization": {"optimization", "performance"},
		},
	}
	return mappedExternalOptions(values, mappings[projectType])
}

func simpleProjectFeaturesFromExternal(projectType string, values []string) []string {
	mappings := map[string]map[string][]string{
		"resource_pack": {
			"audio": {"audio", "sound"}, "blocks": {"blocks"}, "core_shaders": {"core shaders"}, "entities": {"entities", "mobs"},
			"environment": {"environment"}, "equipment": {"equipment", "armor", "weapons"}, "fonts": {"fonts"}, "gui": {"gui", "interface"},
			"items": {"items"}, "locale": {"locale", "language"}, "models": {"models", "3d"},
		},
		"shader_pack": {
			"ambient_light": {"ambient light", "ambient occlusion"}, "bloom": {"bloom", "glow"},
			"colored_lighting": {"colored lighting", "colour lighting", "multicolored lighting"}, "pbr": {"pbr", "physically based"},
			"reflection": {"reflection", "ssr"}, "shadows": {"shadow"},
		},
	}
	return mappedExternalOptions(values, mappings[projectType])
}

func simpleProjectResolutionFromExternal(values []string) string {
	joined := externalClassificationText(values)
	for _, option := range []struct {
		code    string
		markers []string
	}{
		{"512x_or_higher", []string{"512x", "1024x", "2048x"}}, {"256x", []string{"256x"}}, {"128x", []string{"128x"}},
		{"64x", []string{"64x"}}, {"48x", []string{"48x"}}, {"32x", []string{"32x"}}, {"16x", []string{"16x"}}, {"8x_or_lower", []string{"8x", "4x"}},
	} {
		if containsJoinedMarker(joined, option.markers...) {
			return option.code
		}
	}
	return "16x"
}

func simpleProjectPerformanceFromExternal(values []string) string {
	joined := externalClassificationText(values)
	for _, option := range []struct {
		code    string
		markers []string
	}{
		{"supercomputer", []string{"path tracing", "ray tracing", "extreme"}}, {"cinematic", []string{"cinematic", "filmic"}},
		{"high", []string{"high performance cost", "high end"}}, {"low", []string{"low end", "lightweight"}}, {"potato", []string{"potato"}},
	} {
		if containsJoinedMarker(joined, option.markers...) {
			return option.code
		}
	}
	return "medium"
}

func mappedExternalOptions(values []string, mapping map[string][]string) []string {
	joined := externalClassificationText(values)
	result := make([]string, 0, len(mapping))
	for option, markers := range mapping {
		if containsJoinedMarker(joined, markers...) {
			result = append(result, option)
		}
	}
	sort.Strings(result)
	return result
}

func externalClassificationText(values []string) string {
	joined := strings.ToLower(strings.Join(values, " "))
	fields := strings.FieldsFunc(joined, func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	})
	return strings.Join(fields, " ")
}

func containsJoinedMarker(joined string, markers ...string) bool {
	haystack := " " + strings.TrimSpace(joined) + " "
	for _, marker := range markers {
		marker = externalClassificationText([]string{marker})
		if marker != "" && strings.Contains(haystack, " "+marker+" ") {
			return true
		}
	}
	return false
}

func containsExternalValue(values []string, markers ...string) bool {
	return containsJoinedMarker(externalClassificationText(values), markers...)
}
