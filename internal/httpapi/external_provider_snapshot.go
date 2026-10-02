package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type modrinthProviderSnapshot struct {
	Project  modrinthProject
	Versions []modrinthVersion
	Authors  []modAuthorPayload
}

func loadModrinthProviderSnapshot(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (modrinthProviderSnapshot, error) {
	headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, cfg.Modrinth.Token, "")
	var result modrinthProviderSnapshot
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(reference), headers, &result.Project); err != nil {
		return modrinthProviderSnapshot{}, fmt.Errorf("read Modrinth project: %w", err)
	}
	if strings.TrimSpace(result.Project.ID) == "" || strings.TrimSpace(result.Project.ProjectType) == "" {
		return modrinthProviderSnapshot{}, errors.New("Modrinth project response is incomplete")
	}
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(result.Project.ID)+"/version", headers, &result.Versions); err != nil {
		return modrinthProviderSnapshot{}, fmt.Errorf("read Modrinth project versions: %w", err)
	}
	if result.Project.Team == "" {
		return result, nil
	}
	var members []modrinthTeamMember
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/team/"+url.PathEscape(result.Project.Team)+"/members", headers, &members); err != nil {
		return modrinthProviderSnapshot{}, fmt.Errorf("read Modrinth project team: %w", err)
	}
	result.Authors = modrinthAuthorsFromMembers(members)
	return result, nil
}

func modrinthAuthorsFromMembers(members []modrinthTeamMember) []modAuthorPayload {
	authors := make([]modAuthorPayload, 0, len(members))
	for _, member := range members {
		name := strings.TrimSpace(member.User.Name)
		if name == "" {
			name = strings.TrimSpace(member.User.Username)
		}
		if name != "" {
			authors = append(authors, modAuthorPayload{Name: name, Kind: "author", AvatarURL: member.User.AvatarURL, Role: member.Role})
		}
	}
	return authors
}

type curseForgeProviderSnapshot struct {
	Project         curseForgeMod
	DescriptionHTML string
}

func loadCurseForgeProviderSnapshot(
	ctx context.Context,
	client *http.Client,
	cfg modImportConfig,
	headers http.Header,
	classID int64,
	reference string,
) (curseForgeProviderSnapshot, error) {
	searchURL, err := url.Parse(cfg.CurseForge.BaseURL + "/mods/search")
	if err != nil {
		return curseForgeProviderSnapshot{}, fmt.Errorf("build CurseForge search URL: %w", err)
	}
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
		return curseForgeProviderSnapshot{}, fmt.Errorf("search CurseForge project: %w", err)
	}
	if len(search.Data) == 0 {
		return curseForgeProviderSnapshot{}, errors.New("CurseForge project was not found")
	}
	project := search.Data[0]
	if !strings.EqualFold(project.Slug, reference) || project.ClassID != classID {
		return curseForgeProviderSnapshot{}, errors.New("CurseForge returned a project outside the requested slug or class")
	}
	var description struct {
		Data string `json:"data"`
	}
	if err = getProviderJSON(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+strconv.FormatInt(project.ID, 10)+"/description", headers, &description); err != nil {
		return curseForgeProviderSnapshot{}, fmt.Errorf("read CurseForge project description: %w", err)
	}
	return curseForgeProviderSnapshot{Project: project, DescriptionHTML: description.Data}, nil
}

func curseForgeAuthors(project curseForgeMod) []modAuthorPayload {
	authors := make([]modAuthorPayload, 0, len(project.Authors))
	for _, author := range project.Authors {
		if name := strings.TrimSpace(author.Name); name != "" {
			authors = append(authors, modAuthorPayload{Name: name, Kind: "author", AvatarURL: author.AvatarURL, Role: "Author"})
		}
	}
	return authors
}
