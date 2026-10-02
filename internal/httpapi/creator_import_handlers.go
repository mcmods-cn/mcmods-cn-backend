package httpapi

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type creatorImportRequest struct {
	Kind string `json:"kind"`
	URL  string `json:"url"`
}

type creatorImportReference struct {
	Provider   string
	Kind       string
	Identifier string
	URL        string
}

type importedCreatorProfile struct {
	Kind      string
	Name      string
	AvatarURL string
	Links     []creatorLinkPayload
	Members   []importedCreatorMember
}

type importedCreatorMember struct {
	Name      string
	AvatarURL string
	Role      string
	Owner     bool
	Links     []creatorLinkPayload
}

type creatorImportMemberResponse struct {
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	AvatarURL          string `json:"avatarUrl"`
	ProfileURL         string `json:"profileUrl"`
	ExternalRole       string `json:"externalRole"`
	SuggestedRoleCode  string `json:"suggestedRoleCode"`
	SuggestedRole      string `json:"suggestedRole"`
	PermissionGranting bool   `json:"permissionGranting"`
	Title              string `json:"title"`
}

type creatorImportResponse struct {
	Kind      string                        `json:"kind"`
	Name      string                        `json:"name"`
	AvatarURL string                        `json:"avatarUrl"`
	Links     []creatorLinkPayload          `json:"links"`
	Members   []creatorImportMemberResponse `json:"members"`
}

type modrinthCreatorUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

type modrinthOrganizationMember struct {
	User    modrinthCreatorUser `json:"user"`
	Role    string              `json:"role"`
	IsOwner bool                `json:"is_owner"`
}

type modrinthOrganization struct {
	ID      string                       `json:"id"`
	Slug    string                       `json:"slug"`
	Name    string                       `json:"name"`
	IconURL string                       `json:"icon_url"`
	Members []modrinthOrganizationMember `json:"members"`
}

var (
	htmlMetaTagPattern   = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	htmlAttributePattern = regexp.MustCompile(`(?i)([a-z_:][-a-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

func (s *Server) importCreator(w http.ResponseWriter, r *http.Request) {
	var request creatorImportRequest
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid creator import payload")
		return
	}
	request.Kind = strings.ToLower(strings.TrimSpace(request.Kind))
	reference, err := parseCreatorImportReference(request.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if request.Kind != "author" && request.Kind != "team" {
		writeError(w, http.StatusBadRequest, "creator kind must be author or team")
		return
	}
	if reference.Kind != request.Kind {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("the supplied link contains a %s, not a %s", reference.Kind, request.Kind))
		return
	}

	cfg, err := s.modImportConfigFromSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "creator import settings are unavailable")
		return
	}
	provider := cfg.Modrinth
	if reference.Provider == "curseforge" {
		provider = cfg.CurseForge
	}
	if !provider.Enabled {
		writeError(w, http.StatusServiceUnavailable, reference.Provider+" imports are disabled")
		return
	}
	if reference.Provider == "curseforge" && provider.APIKey == "" {
		writeError(w, http.StatusServiceUnavailable, "CurseForge API key is not configured")
		return
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, provider.BaseURL)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "creator import provider is unavailable")
		return
	}

	var profile importedCreatorProfile
	if reference.Provider == "modrinth" {
		profile, err = importModrinthCreator(r.Context(), client, cfg, reference)
	} else {
		profile, err = importCurseForgeAuthor(r.Context(), client, cfg, reference)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	profile.Name = strings.TrimSpace(profile.Name)
	if profile.Name == "" || len([]byte(profile.Name)) > 160 {
		writeError(w, http.StatusBadGateway, "the provider returned an invalid creator name")
		return
	}

	response, err := buildCreatorImportPreview(profile)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if err = s.hydrateCreatorImportRoleFacts(r.Context(), response.Members); err != nil {
		writeError(w, http.StatusInternalServerError, "creator role definitions are unavailable")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func buildCreatorImportPreview(profile importedCreatorProfile) (creatorImportResponse, error) {
	if len(profile.Members) > maximumCreatorTeamMembers {
		return creatorImportResponse{}, errors.New("the provider returned too many team members")
	}
	response := creatorImportResponse{
		Kind: profile.Kind, Name: strings.TrimSpace(profile.Name),
		AvatarURL: externalCreatorAvatarPreviewURL(profile.AvatarURL), Links: profile.Links,
		Members: make([]creatorImportMemberResponse, 0, len(profile.Members)),
	}
	seen := make(map[string]struct{}, len(profile.Members))
	for _, member := range profile.Members {
		member.Name = strings.TrimSpace(member.Name)
		if member.Name == "" || len([]byte(member.Name)) > 160 {
			return creatorImportResponse{}, errors.New("the provider returned an invalid team member")
		}
		_, profileURL := creatorIdentityLink(member.Links)
		identity := normalizeCreatorName(member.Name) + "\x00" + strings.ToLower(strings.TrimSpace(profileURL))
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}
		externalRole := truncateRunes(strings.TrimSpace(member.Role), 160)
		roleCode := importedCreatorRoleCode(externalRole, member.Owner)
		response.Members = append(response.Members, creatorImportMemberResponse{
			Kind: "author", Name: member.Name, AvatarURL: externalCreatorAvatarPreviewURL(member.AvatarURL),
			ProfileURL: profileURL, ExternalRole: externalRole,
			SuggestedRoleCode: roleCode, Title: importedCreatorRoleTitle(externalRole, roleCode),
		})
	}
	return response, nil
}

func (s *Server) hydrateCreatorImportRoleFacts(ctx context.Context, members []creatorImportMemberResponse) error {
	if len(members) == 0 {
		return nil
	}
	codes := make([]string, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		if _, exists := seen[member.SuggestedRoleCode]; exists {
			continue
		}
		seen[member.SuggestedRoleCode] = struct{}{}
		codes = append(codes, member.SuggestedRoleCode)
	}
	rows, err := s.db.Query(ctx, `select code,name,permission_granting from creator_role_definitions where code=any($1)`, codes)
	if err != nil {
		return err
	}
	defer rows.Close()
	type roleFact struct {
		name               string
		permissionGranting bool
	}
	facts := make(map[string]roleFact, len(codes))
	for rows.Next() {
		var code string
		var fact roleFact
		if err = rows.Scan(&code, &fact.name, &fact.permissionGranting); err != nil {
			return err
		}
		facts[code] = fact
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(facts) != len(codes) {
		return errors.New("creator import role definition is missing")
	}
	for index := range members {
		fact := facts[members[index].SuggestedRoleCode]
		members[index].SuggestedRole = fact.name
		members[index].PermissionGranting = fact.permissionGranting
	}
	return nil
}

func parseCreatorImportReference(raw string) (creatorImportReference, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" {
		return creatorImportReference{}, errors.New("enter a valid HTTPS Modrinth or CurseForge profile link")
	}
	host := strings.TrimPrefix(strings.ToLower(strings.TrimSuffix(parsed.Hostname(), ".")), "www.")
	segments := strings.FieldsFunc(parsed.EscapedPath(), func(character rune) bool { return character == '/' })
	if len(segments) < 2 {
		return creatorImportReference{}, errors.New("the creator profile link is incomplete")
	}
	kind := ""
	provider := ""
	switch {
	case host == "modrinth.com" && strings.EqualFold(segments[0], "organization"):
		provider, kind = "modrinth", "team"
	case host == "modrinth.com" && strings.EqualFold(segments[0], "user"):
		provider, kind = "modrinth", "author"
	case host == "curseforge.com" && strings.EqualFold(segments[0], "members"):
		provider, kind = "curseforge", "author"
	default:
		return creatorImportReference{}, errors.New("only Modrinth user/organization links and CurseForge member links are supported")
	}
	identifier, err := url.PathUnescape(segments[1])
	if err != nil || !validCreatorImportIdentifier(identifier) {
		return creatorImportReference{}, errors.New("the creator identifier in the link is invalid")
	}
	canonicalURL := "https://modrinth.com/" + segments[0] + "/" + url.PathEscape(identifier)
	if provider == "curseforge" {
		canonicalURL = "https://www.curseforge.com/members/" + url.PathEscape(identifier) + "/projects"
	}
	return creatorImportReference{Provider: provider, Kind: kind, Identifier: identifier, URL: canonicalURL}, nil
}

func validCreatorImportIdentifier(value string) bool {
	if value == "" || len(value) > 100 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' || character == '.' {
			continue
		}
		return false
	}
	return value != "." && value != ".."
}

func importModrinthCreator(ctx context.Context, client *http.Client, cfg modImportConfig, reference creatorImportReference) (importedCreatorProfile, error) {
	headers := providerCredentialHeaders(cfg.UserAgent, "modrinth", cfg.Modrinth.BaseURL, bearerAuthorization(cfg.Modrinth.Token), "")
	if reference.Kind == "author" {
		var user modrinthCreatorUser
		endpoint := cfg.Modrinth.BaseURL + "/user/" + url.PathEscape(reference.Identifier)
		if err := getProviderJSON(ctx, client, endpoint, headers, &user); err != nil {
			return importedCreatorProfile{}, fmt.Errorf("failed to load Modrinth user: %w", err)
		}
		return importedCreatorProfile{
			Kind: "author", Name: modrinthCreatorName(user), AvatarURL: user.AvatarURL,
			Links: []creatorLinkPayload{{Type: "modrinth", URL: reference.URL, Label: "Modrinth"}},
		}, nil
	}

	baseURL := modrinthV3BaseURL(cfg.Modrinth.BaseURL)
	endpoint := baseURL + "/organization/" + url.PathEscape(reference.Identifier)
	var organization modrinthOrganization
	if err := getProviderJSON(ctx, client, endpoint, headers, &organization); err != nil {
		return importedCreatorProfile{}, fmt.Errorf("failed to load Modrinth organization: %w", err)
	}
	if organization.Members == nil {
		if err := getProviderJSON(ctx, client, endpoint+"/members", headers, &organization.Members); err != nil {
			return importedCreatorProfile{}, fmt.Errorf("failed to load Modrinth organization members: %w", err)
		}
	}
	members := make([]importedCreatorMember, 0, len(organization.Members))
	for _, member := range organization.Members {
		username := strings.TrimSpace(member.User.Username)
		if username == "" {
			continue
		}
		members = append(members, importedCreatorMember{
			Name: modrinthCreatorName(member.User), AvatarURL: member.User.AvatarURL,
			Role: member.Role, Owner: member.IsOwner,
			Links: []creatorLinkPayload{{Type: "modrinth", URL: "https://modrinth.com/user/" + url.PathEscape(username), Label: "Modrinth"}},
		})
	}
	return importedCreatorProfile{
		Kind: "team", Name: organization.Name, AvatarURL: organization.IconURL, Members: members,
		Links: []creatorLinkPayload{{Type: "modrinth", URL: reference.URL, Label: "Modrinth"}},
	}, nil
}

func importCurseForgeAuthor(ctx context.Context, client *http.Client, cfg modImportConfig, reference creatorImportReference) (importedCreatorProfile, error) {
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	searchURL, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/search")
	query := searchURL.Query()
	query.Set("gameId", "432")
	query.Set("searchFilter", reference.Identifier)
	query.Set("pageSize", "50")
	searchURL.RawQuery = query.Encode()
	var search struct {
		Data []curseForgeMod `json:"data"`
	}
	if err := getProviderJSON(ctx, client, searchURL.String(), headers, &search); err != nil {
		return importedCreatorProfile{}, fmt.Errorf("failed to search CurseForge author: %w", err)
	}
	var name, avatarURL, profileURL string
	for _, project := range search.Data {
		for _, author := range project.Authors {
			if !strings.EqualFold(author.Name, reference.Identifier) && !curseForgeAuthorURLMatches(author.URL, reference.Identifier) {
				continue
			}
			name, avatarURL, profileURL = author.Name, author.AvatarURL, author.URL
			break
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		return importedCreatorProfile{}, errors.New("CurseForge author was not found")
	}
	if !validHTTPURL(profileURL) {
		profileURL = reference.URL
	}
	if avatarURL == "" {
		avatarURL = fetchCurseForgeProfileImage(ctx, cfg, reference.URL)
	}
	return importedCreatorProfile{
		Kind: "author", Name: name, AvatarURL: avatarURL,
		Links: []creatorLinkPayload{{Type: "curseforge", URL: profileURL, Label: "CurseForge"}},
	}, nil
}

func fetchCurseForgeProfileImage(ctx context.Context, cfg modImportConfig, profileURL string) string {
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, "https://www.curseforge.com")
	if err != nil {
		return ""
	}
	headers := publicProviderHeaders(cfg.UserAgent)
	headers.Set("Accept", "text/html,application/xhtml+xml")
	page, err := getProviderText(ctx, client, profileURL, headers)
	if err != nil {
		return ""
	}
	metadata := extractHTMLMetadata(page)
	for _, key := range []string{"og:image", "twitter:image"} {
		candidate := metadata[key]
		parsed, parseErr := url.Parse(candidate)
		if parseErr == nil && parsed.Scheme == "https" && isAllowedExternalModIconHost(parsed.Hostname()) {
			return candidate
		}
	}
	return ""
}

func extractHTMLMetadata(page string) map[string]string {
	metadata := make(map[string]string)
	for _, tag := range htmlMetaTagPattern.FindAllString(page, -1) {
		attributes := make(map[string]string)
		for _, match := range htmlAttributePattern.FindAllStringSubmatch(tag, -1) {
			value := match[2]
			if value == "" {
				value = match[3]
			}
			attributes[strings.ToLower(match[1])] = html.UnescapeString(value)
		}
		key := strings.ToLower(strings.TrimSpace(attributes["property"]))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(attributes["name"]))
		}
		if key != "" && attributes["content"] != "" {
			metadata[key] = strings.TrimSpace(attributes["content"])
		}
	}
	return metadata
}

func curseForgeAuthorURLMatches(rawURL, username string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	segments := strings.FieldsFunc(parsed.Path, func(character rune) bool { return character == '/' })
	return len(segments) >= 2 && strings.EqualFold(segments[0], "members") && strings.EqualFold(segments[1], username)
}

func modrinthCreatorName(user modrinthCreatorUser) string {
	if name := strings.TrimSpace(user.Name); name != "" {
		return name
	}
	return strings.TrimSpace(user.Username)
}

func modrinthV3BaseURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(strings.ToLower(baseURL), "/v2") {
		return baseURL[:len(baseURL)-3] + "/v3"
	}
	if strings.HasSuffix(strings.ToLower(baseURL), "/v3") {
		return baseURL
	}
	return baseURL + "/v3"
}

func importedCreatorRoleCode(role string, owner bool) string {
	if owner {
		return "owner"
	}
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "project owner", "organization owner":
		return "owner"
	case "developer", "dev":
		return "developer"
	case "maintainer":
		return "maintainer"
	case "artist", "art", "graphics":
		return "artist"
	case "leader", "lead", "project lead":
		return "leader"
	case "sponsor":
		return "sponsor"
	case "former developer", "former dev":
		return "former_developer"
	case "former artist":
		return "former_artist"
	case "former owner":
		return "former_owner"
	default:
		return "contributor"
	}
}

func externalCreatorAvatarPreviewURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Port() != "" || !isAllowedExternalModIconHost(parsed.Hostname()) {
		return ""
	}
	return parsed.String()
}

func importedCreatorRoleTitle(externalRole, roleCode string) string {
	title := strings.TrimSpace(externalRole)
	if strings.EqualFold(title, roleCode) ||
		(roleCode == "developer" && (strings.EqualFold(title, "dev") || strings.EqualFold(title, "developer"))) {
		return ""
	}
	return title
}
