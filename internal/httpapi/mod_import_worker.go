package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

const maxModImportResponseBytes = int64(16 << 20)

type ModMetadataImportWorker struct {
	server *Server
	queue  *queue.Client
}

func NewModMetadataImportWorker(cfg config.Config, db *pgxpool.Pool, queueClient *queue.Client) *ModMetadataImportWorker {
	return &ModMetadataImportWorker{server: &Server{cfg: cfg, db: db, queue: queueClient}, queue: queueClient}
}

func (worker *ModMetadataImportWorker) Start(ctx context.Context) error {
	if worker == nil || worker.server == nil || worker.queue == nil {
		return queue.ErrUnavailable
	}
	if _, err := worker.server.db.Exec(context.Background(),
		`update mod_metadata_import_jobs set status='queued',progress=0,error='',started_at=null,updated_at=now()
		 where status='running' and updated_at < now() - interval '5 minutes'`); err != nil {
		return err
	}
	subscribeErr := worker.queue.SubscribeTask(modMetadataImportTaskCode, worker.handle)
	go worker.republishQueued(ctx)
	if publishErr := worker.publishQueued(ctx); publishErr != nil && subscribeErr == nil {
		return publishErr
	}
	return subscribeErr
}

func (worker *ModMetadataImportWorker) republishQueued(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = worker.publishQueued(ctx)
		}
	}
}

func (worker *ModMetadataImportWorker) publishQueued(ctx context.Context) error {
	rows, err := worker.server.db.Query(ctx, `select public_id from mod_metadata_import_jobs where status='queued' order by created_at limit 200`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var jobID string
		if rows.Scan(&jobID) == nil {
			_ = worker.queue.PublishTask(ctx, modMetadataImportTaskCode, modMetadataImportMessage{JobID: jobID})
		}
	}
	return rows.Err()
}

func (worker *ModMetadataImportWorker) handle(ctx context.Context, raw []byte) error {
	var message modMetadataImportMessage
	if err := json.Unmarshal(raw, &message); err != nil || strings.TrimSpace(message.JobID) == "" {
		return errors.New("invalid mod metadata import message")
	}
	return worker.server.runModMetadataImport(ctx, message.JobID)
}

func (s *Server) runModMetadataImport(ctx context.Context, jobID string) error {
	var provider, sourceURL string
	var userID int64
	err := s.db.QueryRow(ctx,
		`update mod_metadata_import_jobs set status='running',progress=10,error='',started_at=now(),updated_at=now()
		 where public_id=$1 and status='queued' returning provider,source_url,user_id`, jobID,
	).Scan(&provider, &sourceURL, &userID)
	if err != nil {
		return nil
	}
	fail := func(importErr error) error {
		_, _ = s.db.Exec(context.Background(),
			`update mod_metadata_import_jobs set status='failed',error=$2,finished_at=now(),updated_at=now() where public_id=$1`,
			jobID, truncateRunes(importErr.Error(), 2000))
		return nil
	}
	cfg, err := s.modImportConfigFromSettings(ctx)
	if err != nil {
		return fail(err)
	}
	if err = ensureModImportProviderAvailable(cfg, provider); err != nil {
		return fail(err)
	}
	_, _, reference, err := parseModImportSource(provider, sourceURL)
	if err != nil {
		return fail(err)
	}
	baseURL := cfg.Modrinth.BaseURL
	if provider == "curseforge" {
		baseURL = cfg.CurseForge.BaseURL
	} else if provider == "github" {
		baseURL = cfg.GitHub.BaseURL
	}
	client, err := newProviderHTTPClient(time.Duration(cfg.RequestTimeoutSeconds)*time.Second, baseURL)
	if err != nil {
		return fail(err)
	}
	_, _ = s.db.Exec(ctx, `update mod_metadata_import_jobs set progress=25,updated_at=now() where public_id=$1`, jobID)
	var draft createModRequest
	switch provider {
	case "modrinth":
		draft, err = importModrinthProject(ctx, client, cfg, reference)
	case "curseforge":
		draft, err = importCurseForgeProject(ctx, client, cfg, reference)
	case "github":
		draft, err = importGitHubRepository(ctx, client, cfg, reference)
	default:
		err = errors.New("unsupported import provider")
	}
	if err != nil {
		return fail(err)
	}
	_, _ = s.db.Exec(ctx, `update mod_metadata_import_jobs set progress=85,updated_at=now() where public_id=$1`, jobID)
	if err = normalizeAndValidateModRequest(&draft); err != nil {
		return fail(fmt.Errorf("导入数据校验失败: %w", err))
	}
	result, err := json.Marshal(draft)
	if err != nil {
		return fail(err)
	}
	if _, err = s.db.Exec(ctx,
		`update mod_metadata_import_jobs set status='completed',progress=100,result=$2::jsonb,error='',
		 finished_at=now(),updated_at=now() where public_id=$1`,
		jobID, string(result)); err != nil {
		return err
	}
	return nil
}

type modrinthProject struct {
	ID                   string   `json:"id"`
	Slug                 string   `json:"slug"`
	Title                string   `json:"title"`
	Description          string   `json:"description"`
	Body                 string   `json:"body"`
	ProjectType          string   `json:"project_type"`
	Categories           []string `json:"categories"`
	AdditionalCategories []string `json:"additional_categories"`
	ClientSide           string   `json:"client_side"`
	ServerSide           string   `json:"server_side"`
	Status               string   `json:"status"`
	IconURL              string   `json:"icon_url"`
	Team                 string   `json:"team"`
	GameVersions         []string `json:"game_versions"`
	Loaders              []string `json:"loaders"`
	SourceURL            string   `json:"source_url"`
	IssuesURL            string   `json:"issues_url"`
	WikiURL              string   `json:"wiki_url"`
	DiscordURL           string   `json:"discord_url"`
	License              struct {
		ID string `json:"id"`
	} `json:"license"`
}

type modrinthTeamMember struct {
	Role string `json:"role"`
	User struct {
		Username  string `json:"username"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	} `json:"user"`
}

func importModrinthProject(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModRequest, error) {
	var project modrinthProject
	headers := providerHeaders(cfg.UserAgent, "", "")
	if cfg.Modrinth.Token != "" {
		headers.Set("Authorization", cfg.Modrinth.Token)
	}
	if err := getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/project/"+url.PathEscape(reference), headers, &project); err != nil {
		return createModRequest{}, fmt.Errorf("读取 Modrinth 项目失败: %w", err)
	}
	if project.ProjectType != "mod" {
		return createModRequest{}, errors.New("该 Modrinth 项目不是模组")
	}
	authors := make([]modAuthorPayload, 0)
	if project.Team != "" {
		var members []modrinthTeamMember
		if getProviderJSON(ctx, client, cfg.Modrinth.BaseURL+"/team/"+url.PathEscape(project.Team)+"/members", headers, &members) == nil {
			for _, member := range members {
				name := member.User.Name
				if name == "" {
					name = member.User.Username
				}
				authors = append(authors, modAuthorPayload{Name: name, AvatarURL: member.User.AvatarURL, Role: member.Role})
			}
		}
	}
	categoryValues := append(append([]string{}, project.Categories...), project.AdditionalCategories...)
	loaders := normalizeLoaders(project.Loaders)
	return createModRequest{
		SiteID:            modSiteIDBase(project.Slug),
		PrimaryName:       project.Title,
		Summary:           project.Description,
		Environment:       environmentFromSides(project.ClientSide, project.ServerSide),
		PrimaryCategory:   primaryCategoryFromExternal(categoryValues),
		Compatibilities:   compatibilitiesForLoaders(loaders, project.GameVersions),
		Tags:              tagsFromExternal(categoryValues),
		SearchKeywords:    uniqueTrimmed([]string{project.Slug}, 40),
		Authors:           authors,
		OfficialStatus:    statusFromExternal(project.Status, false),
		SourceStatus:      sourceStatusFromLicense(project.License.ID),
		License:           normalizeExternalLicense(project.License.ID),
		ModrinthProjectID: project.ID,
		IconURL:           project.IconURL,
		BodyMarkdown:      project.Body,
		SubmissionMethod:  "modrinth",
		Links: compactLinks([]modLinkPayload{
			{Type: "modrinth", URL: "https://modrinth.com/mod/" + project.Slug},
			{Type: "github", URL: project.SourceURL},
			{Type: "wiki", URL: project.WikiURL},
			{Type: "other", URL: project.IssuesURL},
			{Type: "discord", URL: project.DiscordURL},
		}),
	}, nil
}

type curseForgeMod struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	Summary      string `json:"summary"`
	IsAvailable  bool   `json:"isAvailable"`
	DateModified string `json:"dateModified"`
	Links        struct {
		WebsiteURL string `json:"websiteUrl"`
		WikiURL    string `json:"wikiUrl"`
		IssuesURL  string `json:"issuesUrl"`
		SourceURL  string `json:"sourceUrl"`
	} `json:"links"`
	Logo struct {
		ThumbnailURL string `json:"thumbnailUrl"`
	} `json:"logo"`
	Authors []struct {
		Name      string `json:"name"`
		URL       string `json:"url"`
		AvatarURL string `json:"avatarUrl"`
	} `json:"authors"`
	Categories []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"categories"`
	LatestFilesIndexes []struct {
		GameVersion string `json:"gameVersion"`
		ModLoader   int    `json:"modLoader"`
	} `json:"latestFilesIndexes"`
}

func importCurseForgeProject(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModRequest, error) {
	headers := providerHeaders(cfg.UserAgent, "", cfg.CurseForge.APIKey)
	searchURL, _ := url.Parse(cfg.CurseForge.BaseURL + "/mods/search")
	query := searchURL.Query()
	query.Set("gameId", "432")
	query.Set("slug", reference)
	query.Set("pageSize", "1")
	searchURL.RawQuery = query.Encode()
	var search struct {
		Data []curseForgeMod `json:"data"`
	}
	if err := getProviderJSON(ctx, client, searchURL.String(), headers, &search); err != nil {
		return createModRequest{}, fmt.Errorf("搜索 CurseForge 项目失败: %w", err)
	}
	if len(search.Data) == 0 {
		return createModRequest{}, errors.New("CurseForge 项目不存在")
	}
	project := search.Data[0]
	if !strings.EqualFold(project.Slug, reference) {
		return createModRequest{}, errors.New("CurseForge 未返回匹配的模组项目")
	}
	var description struct {
		Data string `json:"data"`
	}
	_ = getProviderJSON(ctx, client, cfg.CurseForge.BaseURL+"/mods/"+strconv.FormatInt(project.ID, 10)+"/description", headers, &description)
	categoryValues := make([]string, 0, len(project.Categories)*2)
	for _, category := range project.Categories {
		categoryValues = append(categoryValues, category.Name, category.Slug)
	}
	compatibilityMap := make(map[string][]string)
	for _, index := range project.LatestFilesIndexes {
		loader := curseForgeLoader(index.ModLoader)
		if loader != "" && index.GameVersion != "" {
			compatibilityMap[loader] = append(compatibilityMap[loader], index.GameVersion)
		}
	}
	authors := make([]modAuthorPayload, 0, len(project.Authors))
	for _, author := range project.Authors {
		authors = append(authors, modAuthorPayload{Name: author.Name, AvatarURL: author.AvatarURL, Role: "Author"})
	}
	return createModRequest{
		SiteID:              modSiteIDBase(project.Slug),
		PrimaryName:         project.Name,
		Summary:             project.Summary,
		Environment:         "bothRequired",
		PrimaryCategory:     primaryCategoryFromExternal(categoryValues),
		Compatibilities:     compatibilityMapToPayload(compatibilityMap),
		Tags:                tagsFromExternal(categoryValues),
		SearchKeywords:      uniqueTrimmed([]string{project.Slug}, 40),
		Authors:             authors,
		OfficialStatus:      statusFromExternal("active", !project.IsAvailable),
		SourceStatus:        "unknown",
		License:             "Custom",
		CurseForgeProjectID: strconv.FormatInt(project.ID, 10),
		IconURL:             project.Logo.ThumbnailURL,
		BodyMarkdown:        htmlToMarkdown(description.Data),
		SubmissionMethod:    "curseforge",
		Links: compactLinks([]modLinkPayload{
			{Type: "curseforge", URL: project.Links.WebsiteURL},
			{Type: "github", URL: project.Links.SourceURL},
			{Type: "wiki", URL: project.Links.WikiURL},
			{Type: "other", URL: project.Links.IssuesURL},
		}),
	}, nil
}

type githubRepository struct {
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Description string   `json:"description"`
	Homepage    string   `json:"homepage"`
	HTMLURL     string   `json:"html_url"`
	Topics      []string `json:"topics"`
	Archived    bool     `json:"archived"`
	Private     bool     `json:"private"`
	Owner       struct {
		Login     string `json:"login"`
		AvatarURL string `json:"avatar_url"`
	} `json:"owner"`
	License *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
}

func importGitHubRepository(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModRequest, error) {
	headers := providerHeaders(cfg.UserAgent, cfg.GitHub.Token, "")
	headers.Set("Accept", "application/vnd.github+json")
	headers.Set("X-GitHub-Api-Version", "2022-11-28")
	var repository githubRepository
	if err := getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+reference, headers, &repository); err != nil {
		return createModRequest{}, fmt.Errorf("读取 GitHub 仓库失败: %w", err)
	}
	readmeHeaders := headers.Clone()
	readmeHeaders.Set("Accept", "application/vnd.github.raw+json")
	readme, _ := getProviderText(ctx, client, cfg.GitHub.BaseURL+"/repos/"+reference+"/readme", readmeHeaders)
	licenseID := ""
	if repository.License != nil {
		licenseID = repository.License.SPDXID
	}
	loaders := loadersFromText(append(repository.Topics, readme)...)
	links := []modLinkPayload{{Type: "github", URL: repository.HTMLURL}}
	if validHTTPURL(repository.Homepage) {
		links = append(links, modLinkPayload{Type: "official", URL: repository.Homepage})
	}
	return createModRequest{
		SiteID:           modSiteIDBase(repository.Name),
		PrimaryName:      repository.Name,
		Summary:          repository.Description,
		ModID:            normalizeExternalModID(repository.Name),
		Environment:      "bothRequired",
		PrimaryCategory:  primaryCategoryFromExternal(repository.Topics),
		Compatibilities:  compatibilitiesForLoaders(loaders, nil),
		Tags:             tagsFromExternal(repository.Topics),
		SearchKeywords:   uniqueTrimmed(append([]string{repository.Name, repository.FullName}, repository.Topics...), 40),
		Authors:          []modAuthorPayload{{Name: repository.Owner.Login, AvatarURL: repository.Owner.AvatarURL, Role: "Repository owner"}},
		OfficialStatus:   statusFromExternal("active", repository.Archived),
		SourceStatus:     sourceStatusFromLicense(licenseID),
		License:          normalizeExternalLicense(licenseID),
		BodyMarkdown:     readme,
		SubmissionMethod: "github",
		Links:            compactLinks(links),
	}, nil
}

func providerHeaders(userAgent, token, apiKey string) http.Header {
	headers := make(http.Header)
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", userAgent)
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	if apiKey != "" {
		headers.Set("x-api-key", apiKey)
	}
	return headers
}

func newProviderHTTPClient(timeout time.Duration, baseURL string) (*http.Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("invalid provider base URL")
	}
	allowedHost := strings.ToLower(parsed.Host)
	allowLoopback := strings.EqualFold(parsed.Hostname(), "localhost")
	if address := net.ParseIP(parsed.Hostname()); address != nil && address.IsLoopback() {
		allowLoopback = true
	}
	dialer := &net.Dialer{Timeout: min(timeout, 15*time.Second), KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			return nil, splitErr
		}
		addresses, lookupErr := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if len(addresses) == 0 {
			return nil, errors.New("provider host did not resolve")
		}
		for _, resolved := range addresses {
			if isPrivateProviderAddress(resolved) && !allowLoopback {
				return nil, errors.New("provider host resolves to a private network")
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many provider redirects")
			}
			if strings.ToLower(request.URL.Host) != allowedHost {
				return errors.New("provider redirect changed API host")
			}
			return nil
		},
	}, nil
}

func isPrivateProviderAddress(address netip.Addr) bool {
	return address.IsPrivate() ||
		address.IsLoopback() ||
		address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() ||
		address.IsUnspecified() ||
		address.IsMulticast()
}

func getProviderJSON(ctx context.Context, client *http.Client, endpoint string, headers http.Header, target any) error {
	body, err := getProviderBytes(ctx, client, endpoint, headers)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("invalid JSON response: %w", err)
	}
	return nil
}

func getProviderText(ctx context.Context, client *http.Client, endpoint string, headers http.Header) (string, error) {
	body, err := getProviderBytes(ctx, client, endpoint, headers)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func getProviderBytes(ctx context.Context, client *http.Client, endpoint string, headers http.Header) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header = headers.Clone()
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxModImportResponseBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(body)) > maxModImportResponseBytes {
		return nil, errors.New("provider response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 300 {
			message = message[:300]
		}
		return nil, fmt.Errorf("HTTP %d: %s", response.StatusCode, message)
	}
	return body, nil
}

func environmentFromSides(clientSide, serverSide string) string {
	clientSide = strings.ToLower(clientSide)
	serverSide = strings.ToLower(serverSide)
	switch {
	case clientSide == "required" && serverSide == "unsupported":
		return "clientOnly"
	case serverSide == "required" && clientSide == "unsupported":
		return "serverOnly"
	case clientSide == "optional" && serverSide == "required":
		return "clientOptional"
	case serverSide == "optional" && clientSide == "required":
		return "serverOptional"
	default:
		return "bothRequired"
	}
}

func normalizeLoaders(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "forge":
			result = append(result, "Forge")
		case "fabric":
			result = append(result, "Fabric")
		case "neoforge":
			result = append(result, "NeoForge")
		case "quilt":
			result = append(result, "Quilt")
		case "liteloader":
			result = append(result, "LiteLoader")
		}
	}
	return uniqueTrimmed(result, 30)
}

func curseForgeLoader(value int) string {
	switch value {
	case 1:
		return "Forge"
	case 3:
		return "LiteLoader"
	case 4:
		return "Fabric"
	case 5:
		return "Quilt"
	case 6:
		return "NeoForge"
	default:
		return ""
	}
}

func compatibilitiesForLoaders(loaders, versions []string) []modLoaderCompatibilityPayload {
	result := make([]modLoaderCompatibilityPayload, 0, len(loaders))
	versions = uniqueTrimmed(versions, 500)
	for _, loader := range uniqueTrimmed(loaders, 30) {
		result = append(result, modLoaderCompatibilityPayload{Loader: loader, Versions: append([]string(nil), versions...)})
	}
	return result
}

func compatibilityMapToPayload(values map[string][]string) []modLoaderCompatibilityPayload {
	loaders := make([]string, 0, len(values))
	for loader := range values {
		loaders = append(loaders, loader)
	}
	sort.Strings(loaders)
	result := make([]modLoaderCompatibilityPayload, 0, len(loaders))
	for _, loader := range loaders {
		versions := uniqueTrimmed(values[loader], 500)
		sort.Strings(versions)
		result = append(result, modLoaderCompatibilityPayload{Loader: loader, Versions: versions})
	}
	return result
}

func primaryCategoryFromExternal(values []string) string {
	joined := strings.ToLower(strings.Join(values, " "))
	checks := []struct {
		code  string
		words []string
	}{
		{"technology", []string{"technology", "tech", "automation", "energy"}},
		{"magic", []string{"magic"}},
		{"adventure", []string{"adventure", "exploration", "dungeon"}},
		{"agriculture", []string{"agriculture", "farming", "food"}},
		{"decoration", []string{"decoration", "cosmetic", "building"}},
		{"customization", []string{"customization", "kubejs", "crafttweaker"}},
		{"library", []string{"library", "api"}},
		{"assistance", []string{"optimization", "performance", "utility", "map"}},
	}
	for _, check := range checks {
		for _, word := range check.words {
			if strings.Contains(joined, word) {
				return check.code
			}
		}
	}
	return "utility"
}

func tagsFromExternal(values []string) []string {
	joined := strings.ToLower(strings.Join(values, " "))
	mapping := map[string][]string{
		"building": {"building", "decoration"}, "creatures": {"mobs", "creatures"}, "worldGeneration": {"worldgen", "world generation"},
		"biomes": {"biome"}, "structures": {"structure", "dungeon"}, "weapons": {"weapon"}, "tools": {"tool"},
		"storage": {"storage"}, "logistics": {"logistics", "transport"}, "energy": {"energy"}, "redstone": {"redstone"},
		"automation": {"automation", "technology"}, "optimization": {"optimization", "performance"}, "assistance": {"utility", "map"},
		"modpackSupport": {"modpack"}, "adventure": {"adventure", "exploration"}, "economy": {"economy"},
		"equipment": {"equipment", "armor"}, "gameMechanics": {"game mechanics", "mechanics"}, "management": {"management", "server"},
		"minigames": {"minigame"}, "social": {"social"}, "transportation": {"transportation", "vehicle"},
	}
	result := make([]string, 0)
	for tag, words := range mapping {
		for _, word := range words {
			if strings.Contains(joined, word) {
				result = append(result, tag)
				break
			}
		}
	}
	sort.Strings(result)
	return result
}

func normalizeExternalLicense(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	switch {
	case strings.HasPrefix(upper, "MIT"):
		return "MIT"
	case strings.Contains(upper, "LGPL-3"):
		return "LGPL-3.0"
	case strings.Contains(upper, "GPL-3"):
		return "GPL-3.0"
	case strings.Contains(upper, "APACHE-2"):
		return "Apache-2.0"
	case upper == "ARR" || strings.Contains(upper, "ALL RIGHTS RESERVED"):
		return "ARR"
	default:
		return "Custom"
	}
}

func sourceStatusFromLicense(value string) string {
	upper := strings.ToUpper(strings.TrimSpace(value))
	if upper == "" || upper == "UNKNOWN" {
		return "unknown"
	}
	if upper == "ARR" || strings.Contains(upper, "ALL RIGHTS RESERVED") {
		return "closed"
	}
	return "open"
}

func statusFromExternal(value string, archived bool) string {
	if archived || strings.EqualFold(value, "archived") {
		return "archived"
	}
	return "active"
}

func loadersFromText(values ...string) []string {
	joined := strings.ToLower(strings.Join(values, " "))
	result := make([]string, 0, 4)
	for _, item := range []struct{ match, name string }{{"neoforge", "NeoForge"}, {"fabric", "Fabric"}, {"forge", "Forge"}, {"quilt", "Quilt"}} {
		if strings.Contains(joined, item.match) {
			result = append(result, item.name)
		}
	}
	return uniqueTrimmed(result, 30)
}

func compactLinks(values []modLinkPayload) []modLinkPayload {
	result := make([]modLinkPayload, 0, len(values))
	seen := make(map[string]bool)
	for _, item := range values {
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" || !validHTTPURL(item.URL) {
			continue
		}
		key := item.Type + "\x00" + item.URL
		if !seen[key] {
			seen[key] = true
			result = append(result, item)
		}
	}
	return result
}

func normalizeExternalModID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9_.-]+`).ReplaceAllString(value, "_")
	return strings.Trim(value, "_.-")
}

var htmlBreakPattern = regexp.MustCompile(`(?i)<br\s*/?>|</p\s*>|</div\s*>|</h[1-6]\s*>`)
var htmlListItemPattern = regexp.MustCompile(`(?i)<li[^>]*>`)
var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)
var repeatedBlankLinePattern = regexp.MustCompile(`\n{3,}`)

func htmlToMarkdown(value string) string {
	value = htmlBreakPattern.ReplaceAllString(value, "\n\n")
	value = htmlListItemPattern.ReplaceAllString(value, "\n- ")
	value = htmlTagPattern.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	value = strings.ReplaceAll(value, "\r", "")
	return strings.TrimSpace(repeatedBlankLinePattern.ReplaceAllString(value, "\n\n"))
}
