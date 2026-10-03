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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

const maxModImportResponseBytes = int64(16 << 20)

var nonExternalLoaderTokenCharacters = regexp.MustCompile(`[^a-z0-9]+`)

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
	subscribeErr := worker.queue.SubscribeTask(modMetadataImportTaskCode, worker.handle)
	go worker.recoverQueuedJobs(ctx)
	if recoverErr := worker.recoverQueued(ctx); recoverErr != nil && subscribeErr == nil {
		return recoverErr
	}
	return subscribeErr
}

func (worker *ModMetadataImportWorker) recoverQueuedJobs(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = worker.recoverQueued(ctx)
		}
	}
}

func (worker *ModMetadataImportWorker) recoverQueued(ctx context.Context) error {
	tx, err := worker.server.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = recoverModMetadataImportOutboxTx(ctx, tx, 5*time.Minute, 200); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func recoverModMetadataImportOutboxTx(ctx context.Context, tx pgx.Tx, staleAfter time.Duration, limit int) (int, error) {
	if staleAfter <= 0 {
		staleAfter = 5 * time.Minute
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	interval := fmt.Sprintf("%f seconds", staleAfter.Seconds())
	rows, err := tx.Query(ctx, `select job.public_id,job.status from mod_metadata_import_jobs job
		where (job.status='queued' and not exists(
			select 1 from nats_outbox event where event.aggregate_type='mod_metadata_import_job' and event.aggregate_id=job.public_id
			and (event.status in ('pending','failed','publishing') or event.occurred_at>now()-$1::interval)
		)) or (job.status='running' and job.updated_at<now()-$1::interval)
		order by job.updated_at,job.public_id for update skip locked limit $2`, interval, limit)
	if err != nil {
		return 0, err
	}
	type recovery struct{ jobID, status string }
	jobs := make([]recovery, 0, limit)
	for rows.Next() {
		var item recovery
		if err = rows.Scan(&item.jobID, &item.status); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	for _, item := range jobs {
		if item.status == "running" {
			tag, updateErr := tx.Exec(ctx, `update mod_metadata_import_jobs set status='queued',progress=0,error='',started_at=null,updated_at=now()
				where public_id=$1 and status='running'`, item.jobID)
			if updateErr != nil {
				return 0, updateErr
			}
			if tag.RowsAffected() != 1 {
				return 0, fmt.Errorf("mod metadata import job %s lost recovery ownership", item.jobID)
			}
		}
		if _, err = queue.EnqueueTx(ctx, tx, modMetadataImportTaskCode, "mod.metadata.import.recovered", "mod_metadata_import_job", item.jobID, "", modMetadataImportMessage{JobID: item.jobID}); err != nil {
			return 0, err
		}
	}
	return len(jobs), nil
}

func (worker *ModMetadataImportWorker) handle(ctx context.Context, raw []byte) error {
	var message modMetadataImportMessage
	if err := json.Unmarshal(raw, &message); err != nil || strings.TrimSpace(message.JobID) == "" {
		return errors.New("invalid mod metadata import message")
	}
	return worker.server.runModMetadataImport(ctx, message.JobID)
}

func (s *Server) runModMetadataImport(ctx context.Context, jobID string) error {
	var projectType, provider, sourceURL string
	var userID int64
	var startedAt time.Time
	err := s.db.QueryRow(ctx,
		`update mod_metadata_import_jobs set status='running',progress=10,error='',started_at=now(),updated_at=now()
		 where public_id=$1 and status='queued' returning project_type,provider,source_url,user_id,started_at`, jobID,
	).Scan(&projectType, &provider, &sourceURL, &userID, &startedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	fail := func(importErr error) error {
		failureCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, writeErr := s.db.Exec(failureCtx,
			`update mod_metadata_import_jobs set status='failed',error=$2,finished_at=now(),updated_at=now()
			 where public_id=$1 and status='running' and started_at=$3`,
			jobID, truncateRunes(importErr.Error(), 2000), startedAt)
		return writeErr
	}
	cfg, err := s.modImportConfigFromSettings(ctx)
	if err != nil {
		return fail(err)
	}
	if err = ensureModImportProviderAvailable(cfg, provider); err != nil {
		return fail(err)
	}
	_, _, reference, err := parseProjectImportSource(projectType, provider, sourceURL)
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
	if owned, progressErr := s.updateModMetadataImportProgress(ctx, jobID, startedAt, 25); progressErr != nil || !owned {
		return progressErr
	}
	if projectType == "modpack" {
		var draft createModpackRequest
		switch provider {
		case "modrinth":
			draft, err = importModrinthModpack(ctx, client, cfg, reference)
		case "curseforge":
			draft, err = importCurseForgeModpack(ctx, client, cfg, reference)
		default:
			err = errors.New("unsupported modpack import provider")
		}
		if err != nil {
			return fail(err)
		}
		if owned, progressErr := s.updateModMetadataImportProgress(ctx, jobID, startedAt, 85); progressErr != nil || !owned {
			return progressErr
		}
		if err = normalizeAndValidateModpackImportDraft(&draft); err != nil {
			return fail(fmt.Errorf("导入数据校验失败: %w", err))
		}
		result, marshalErr := json.Marshal(draft)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		if _, err = s.db.Exec(ctx, `update mod_metadata_import_jobs set status='completed',progress=100,result=$2::jsonb,error='',
			finished_at=now(),updated_at=now() where public_id=$1 and status='running' and started_at=$3`, jobID, string(result), startedAt); err != nil {
			return err
		}
		return nil
	}
	if simpleProjectTypes[projectType] {
		var draft simpleProjectSnapshot
		switch provider {
		case "modrinth":
			draft, err = importModrinthSimpleProject(ctx, client, cfg, projectType, sourceURL, reference)
		case "curseforge":
			draft, err = importCurseForgeSimpleProject(ctx, client, cfg, projectType, sourceURL, reference)
		default:
			err = errors.New("unsupported project import provider")
		}
		if err != nil {
			return fail(err)
		}
		if owned, progressErr := s.updateModMetadataImportProgress(ctx, jobID, startedAt, 85); progressErr != nil || !owned {
			return progressErr
		}
		if err = normalizeAndValidateSimpleProjectDraft(&draft, true); err != nil {
			return fail(fmt.Errorf("imported project data is invalid: %w", err))
		}
		result, marshalErr := json.Marshal(draft)
		if marshalErr != nil {
			return fail(marshalErr)
		}
		if _, err = s.db.Exec(ctx, `update mod_metadata_import_jobs set status='completed',progress=100,result=$2::jsonb,error='',
			finished_at=now(),updated_at=now() where public_id=$1 and status='running' and started_at=$3`, jobID, string(result), startedAt); err != nil {
			return err
		}
		return nil
	}
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
	if owned, progressErr := s.updateModMetadataImportProgress(ctx, jobID, startedAt, 85); progressErr != nil || !owned {
		return progressErr
	}
	if err = normalizeAndValidateModRequest(&draft); err != nil {
		return fail(fmt.Errorf("导入数据校验失败: %w", err))
	}
	result, err := json.Marshal(draft)
	if err != nil {
		return fail(err)
	}
	if _, err = s.db.Exec(ctx,
		`update mod_metadata_import_jobs set status='completed',progress=100,result=$2::jsonb,error='',
		 finished_at=now(),updated_at=now() where public_id=$1 and status='running' and started_at=$3`,
		jobID, string(result), startedAt); err != nil {
		return err
	}
	return nil
}

// started_at is the current claim's fencing value. Recovery clears it before a
// later worker claims the job, so a late result cannot overwrite that attempt.
func (s *Server) updateModMetadataImportProgress(ctx context.Context, jobID string, startedAt time.Time, progress int) (bool, error) {
	tag, err := s.db.Exec(ctx, `update mod_metadata_import_jobs set progress=$3,updated_at=now()
		where public_id=$1 and status='running' and started_at=$2`, jobID, startedAt, progress)
	return tag.RowsAffected() == 1, err
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
	snapshot, err := loadModrinthProviderSnapshot(ctx, client, cfg, reference)
	if err != nil {
		return createModRequest{}, err
	}
	project := snapshot.Project
	if project.ProjectType != "mod" {
		return createModRequest{}, errors.New("该 Modrinth 项目不是模组")
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
		Authors:           snapshot.Authors,
		OfficialStatus:    statusFromExternal(project.Status, false),
		SourceStatus:      sourceStatusFromLicense(project.License.ID),
		License:           normalizeExternalLicense(project.License.ID),
		ModrinthProjectID: project.ID,
		IconURL:           project.IconURL,
		BodyMarkdown:      project.Body,
		SubmissionMethod:  "modrinth",
		Links: compactLinks([]modLinkPayload{
			{Type: "modrinth", URL: "https://modrinth.com/mod/" + project.Slug},
			externalSourceLink(project.SourceURL),
			{Type: "wiki", URL: project.WikiURL},
			{Type: "issue", URL: project.IssuesURL},
			{Type: "discord", URL: project.DiscordURL},
		}),
	}, nil
}

type curseForgeMod struct {
	ID           int64  `json:"id"`
	ClassID      int64  `json:"classId"`
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
	LatestFiles []struct {
		GameVersions []string `json:"gameVersions"`
	} `json:"latestFiles"`
}

func importCurseForgeProject(ctx context.Context, client *http.Client, cfg modImportConfig, reference string) (createModRequest, error) {
	headers := providerCredentialHeaders(cfg.UserAgent, "curseforge", cfg.CurseForge.BaseURL, "", cfg.CurseForge.APIKey)
	snapshot, err := loadCurseForgeProviderSnapshot(ctx, client, cfg, headers, 6, reference)
	if err != nil {
		return createModRequest{}, err
	}
	project := snapshot.Project
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
	return createModRequest{
		SiteID:              modSiteIDBase(project.Slug),
		PrimaryName:         project.Name,
		Summary:             project.Summary,
		Environment:         "bothRequired",
		PrimaryCategory:     primaryCategoryFromExternal(categoryValues),
		Compatibilities:     compatibilityMapToPayload(compatibilityMap),
		Tags:                tagsFromExternal(categoryValues),
		SearchKeywords:      uniqueTrimmed([]string{project.Slug}, 40),
		Authors:             curseForgeAuthors(project),
		OfficialStatus:      statusFromExternal("active", !project.IsAvailable),
		SourceStatus:        "unknown",
		License:             "Custom",
		CurseForgeProjectID: strconv.FormatInt(project.ID, 10),
		IconURL:             project.Logo.ThumbnailURL,
		BodyMarkdown:        htmlToMarkdown(snapshot.DescriptionHTML),
		SubmissionMethod:    "curseforge",
		Links: compactLinks([]modLinkPayload{
			{Type: "curseforge", URL: project.Links.WebsiteURL},
			externalSourceLink(project.Links.SourceURL),
			{Type: "wiki", URL: project.Links.WikiURL},
			{Type: "issue", URL: project.Links.IssuesURL},
		}),
	}, nil
}

func externalSourceLink(value string) modLinkPayload {
	linkType := "other"
	if parsed, err := url.Parse(strings.TrimSpace(value)); err == nil {
		switch strings.ToLower(parsed.Hostname()) {
		case "github.com", "www.github.com":
			linkType = "github"
		case "gitlab.com", "www.gitlab.com":
			linkType = "gitlab"
		case "gitee.com", "www.gitee.com":
			linkType = "gitee"
		case "bitbucket.org", "www.bitbucket.org":
			linkType = "bitbucket"
		case "sourceforge.net", "www.sourceforge.net":
			linkType = "sourceforge"
		}
	}
	return modLinkPayload{Type: linkType, URL: value}
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
	headers := providerCredentialHeaders(cfg.UserAgent, "github", cfg.GitHub.BaseURL, bearerAuthorization(cfg.GitHub.Token), "")
	headers.Set("Accept", "application/vnd.github+json")
	headers.Set("X-GitHub-Api-Version", "2022-11-28")
	var repository githubRepository
	if err := getProviderJSON(ctx, client, cfg.GitHub.BaseURL+"/repos/"+reference, headers, &repository); err != nil {
		return createModRequest{}, fmt.Errorf("读取 GitHub 仓库失败: %w", err)
	}
	readmeHeaders := headers.Clone()
	readmeHeaders.Set("Accept", "application/vnd.github.raw+json")
	readme, err := getProviderText(ctx, client, cfg.GitHub.BaseURL+"/repos/"+reference+"/readme", readmeHeaders)
	if err != nil && !isProviderNotFound(err) {
		return createModRequest{}, fmt.Errorf("读取 GitHub README 失败: %w", err)
	}
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
		ModIDs:           []modIdentifierPayload{{Identifier: normalizeExternalModID(repository.Name), Primary: true}},
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

func publicProviderHeaders(userAgent string) http.Header {
	headers := make(http.Header)
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", userAgent)
	return headers
}

func providerCredentialHeaders(userAgent, provider, baseURL, authorization, apiKey string) http.Header {
	headers := publicProviderHeaders(userAgent)
	if !providerCredentialOriginAllowed(provider, baseURL) {
		return headers
	}
	if authorization = strings.TrimSpace(authorization); authorization != "" {
		headers.Set("Authorization", authorization)
	}
	if apiKey = strings.TrimSpace(apiKey); apiKey != "" {
		headers.Set("x-api-key", apiKey)
	}
	return headers
}

func bearerAuthorization(token string) string {
	if token = strings.TrimSpace(token); token != "" {
		return "Bearer " + token
	}
	return ""
}

func newProviderHTTPClient(timeout time.Duration, baseURL string) (*http.Client, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" {
		return nil, errors.New("invalid provider base URL")
	}
	allowLoopback := strings.EqualFold(parsed.Hostname(), "localhost")
	if address := net.ParseIP(parsed.Hostname()); address != nil && address.IsLoopback() {
		allowLoopback = true
	}
	dialer := &net.Dialer{Timeout: min(timeout, 15*time.Second), KeepAlive: 30 * time.Second}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// An environment proxy would resolve the origin itself and bypass the DNS
	// address policy below, which would only inspect the proxy's address.
	transport.Proxy = nil
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
			if isPrivateProviderAddress(resolved) && !(allowLoopback && resolved.IsLoopback()) {
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
			if !sameProviderOrigin(request.URL.String(), baseURL) {
				return errors.New("provider redirect changed API origin")
			}
			return nil
		},
	}, nil
}

func isPrivateProviderAddress(address netip.Addr) bool {
	address = address.Unmap()
	return !address.IsGlobalUnicast() || address.IsPrivate() ||
		address.IsLoopback() ||
		address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() ||
		address.IsUnspecified() ||
		address.IsMulticast() || netip.MustParsePrefix("100.64.0.0/10").Contains(address)
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

type providerHTTPStatusError struct {
	statusCode int
	message    string
}

func (err *providerHTTPStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", err.statusCode, err.message)
}

func isProviderNotFound(err error) bool {
	var statusErr *providerHTTPStatusError
	return errors.As(err, &statusErr) && statusErr.statusCode == http.StatusNotFound
}

func getProviderBytes(ctx context.Context, client *http.Client, endpoint string, headers http.Header) ([]byte, error) {
	return getProviderBytesLimited(ctx, client, endpoint, headers, maxModImportResponseBytes)
}

func getProviderBytesLimited(ctx context.Context, client *http.Client, endpoint string, headers http.Header, maximumBytes int64) ([]byte, error) {
	if maximumBytes < 1 || maximumBytes > maxModImportResponseBytes {
		return nil, errors.New("invalid provider response limit")
	}
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
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if readErr != nil {
		return nil, readErr
	}
	if int64(len(body)) > maximumBytes {
		return nil, errors.New("provider response is too large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message := strings.TrimSpace(string(body))
		if len(message) > 300 {
			message = message[:300]
		}
		return nil, &providerHTTPStatusError{statusCode: response.StatusCode, message: message}
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
	tokens := strings.Fields(nonExternalLoaderTokenCharacters.ReplaceAllString(strings.ToLower(strings.Join(values, " ")), " "))
	found := map[string]bool{}
	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if token == "neo" && index+1 < len(tokens) && tokens[index+1] == "forge" {
			found["NeoForge"] = true
			index++
			continue
		}
		switch token {
		case "neoforge", "neoforged":
			found["NeoForge"] = true
		case "fabric", "fabricmc":
			found["Fabric"] = true
		case "forge", "minecraftforge":
			found["Forge"] = true
		case "quilt", "quiltmc":
			found["Quilt"] = true
		}
	}
	result := make([]string, 0, 4)
	for _, name := range []string{"NeoForge", "Fabric", "Forge", "Quilt"} {
		if found[name] {
			result = append(result, name)
		}
	}
	return result
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
