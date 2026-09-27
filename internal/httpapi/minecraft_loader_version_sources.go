package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/singleflight"
)

const (
	bmclMojangVersionManifestURL              = "https://bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json"
	forgeMavenMetadataURL                     = "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	bmclForgeVersionsURL                      = "https://bmclapi2.bangbang93.com/forge/minecraft"
	neoForgeMavenMetadataURL                  = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	neoForgeLegacyMetadataURL                 = "https://maven.neoforged.net/releases/net/neoforged/forge/maven-metadata.xml"
	bmclNeoForgeMetadataURL                   = "https://bmclapi2.bangbang93.com/maven/net/neoforged/neoforge/maven-metadata.xml"
	bmclNeoForgeLegacyURL                     = "https://bmclapi2.bangbang93.com/maven/net/neoforged/forge/maven-metadata.xml"
	fabricGameVersionsURL                     = "https://meta.fabricmc.net/v2/versions/game"
	bmclFabricGameVersionsURL                 = "https://bmclapi2.bangbang93.com/fabric-meta/v2/versions/game"
	liteLoaderVersionsURL                     = "https://dl.liteloader.com/versions/versions.json"
	bmclLiteLoaderVersionsURL                 = "https://bmclapi2.bangbang93.com/maven/com/mumfrey/liteloader/versions.json"
	maxMinecraftSourceBytes                   = 16 << 20
	minecraftSourceCacheTTL                   = 15 * time.Minute
	maxMinecraftSourceCacheBytes              = 64 << 20
	maxMinecraftSourceCacheItems              = 64
	maxMinecraftSourceFetches                 = 4
	minecraftVersionSyncAdvisoryLockKey int64 = 0x4d434d4f44535653
)

var (
	minecraftVersionHTTPClient = &http.Client{Timeout: 30 * time.Second}
	minecraftSourceResponses   = newMinecraftSourceResponseCache()
	minecraftSourceFetchGroup  singleflight.Group
	minecraftSourceFetchSlots  = make(chan struct{}, maxMinecraftSourceFetches)
)

type minecraftSourceCacheEntry struct {
	payload      []byte
	etag         string
	lastModified string
	freshUntil   time.Time
	lastAccess   uint64
}

type minecraftSourceResponseCache struct {
	mu         sync.Mutex
	entries    map[string]minecraftSourceCacheEntry
	totalBytes int
	access     uint64
}

func newMinecraftSourceResponseCache() *minecraftSourceResponseCache {
	return &minecraftSourceResponseCache{entries: make(map[string]minecraftSourceCacheEntry)}
}

func (cache *minecraftSourceResponseCache) fresh(sourceURL string, now time.Time) ([]byte, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[sourceURL]
	if !ok || !now.Before(entry.freshUntil) {
		return nil, false
	}
	cache.access++
	entry.lastAccess = cache.access
	cache.entries[sourceURL] = entry
	return entry.payload, true
}

func (cache *minecraftSourceResponseCache) stale(sourceURL string) (minecraftSourceCacheEntry, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.entries[sourceURL]
	return entry, ok
}

func (cache *minecraftSourceResponseCache) store(sourceURL string, entry minecraftSourceCacheEntry) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if previous, ok := cache.entries[sourceURL]; ok {
		cache.totalBytes -= len(previous.payload)
		delete(cache.entries, sourceURL)
	}
	for len(cache.entries) >= maxMinecraftSourceCacheItems || cache.totalBytes+len(entry.payload) > maxMinecraftSourceCacheBytes {
		oldestURL := ""
		var oldestAccess uint64
		for candidateURL, candidate := range cache.entries {
			if oldestURL == "" || candidate.lastAccess < oldestAccess {
				oldestURL, oldestAccess = candidateURL, candidate.lastAccess
			}
		}
		if oldestURL == "" {
			break
		}
		cache.totalBytes -= len(cache.entries[oldestURL].payload)
		delete(cache.entries, oldestURL)
	}
	cache.access++
	entry.lastAccess = cache.access
	cache.entries[sourceURL] = entry
	cache.totalBytes += len(entry.payload)
}

func (cache *minecraftSourceResponseCache) reset() {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	cache.entries = make(map[string]minecraftSourceCacheEntry)
	cache.totalBytes = 0
	cache.access = 0
}

func resetMinecraftSourceCacheForTest() {
	minecraftSourceResponses.reset()
}

type minecraftLoaderVersionSource struct {
	code       string
	primaryURL string
	fetch      func(context.Context, []string) ([]string, []string, bool, error)
}

type minecraftLoaderVersionResult struct {
	source       minecraftLoaderVersionSource
	versions     []string
	sourceURLs   []string
	usedFallback bool
	err          error
}

type minecraftSourceAttempt struct {
	url    string
	decode func([]byte) ([]string, error)
}

type minecraftVersionSyncLease struct {
	connection *pgxpool.Conn
}

func acquireMinecraftVersionSyncLease(ctx context.Context, db *pgxpool.Pool) (*minecraftVersionSyncLease, error) {
	connection, err := db.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire Minecraft version synchronization connection: %w", err)
	}
	var acquired bool
	if err = connection.QueryRow(ctx, `select pg_try_advisory_lock($1)`, minecraftVersionSyncAdvisoryLockKey).Scan(&acquired); err != nil {
		connection.Release()
		return nil, fmt.Errorf("acquire Minecraft version synchronization lease: %w", err)
	}
	if !acquired {
		connection.Release()
		return nil, errMinecraftVersionSyncInProgress
	}
	return &minecraftVersionSyncLease{connection: connection}, nil
}

func (lease *minecraftVersionSyncLease) Release(ctx context.Context) error {
	if lease == nil || lease.connection == nil {
		return nil
	}
	connection := lease.connection
	lease.connection = nil
	var released bool
	err := connection.QueryRow(ctx, `select pg_advisory_unlock($1)`, minecraftVersionSyncAdvisoryLockKey).Scan(&released)
	if err != nil {
		raw := connection.Hijack()
		_ = raw.Close(ctx)
		return fmt.Errorf("release Minecraft version synchronization lease: %w", err)
	}
	connection.Release()
	if !released {
		return errors.New("Minecraft version synchronization lease was not held")
	}
	return nil
}

func syncMinecraftVersionCatalog(ctx context.Context, db *pgxpool.Pool) (config minecraftVersionConfig, returnErr error) {
	lease, err := acquireMinecraftVersionSyncLease(ctx, db)
	if err != nil {
		return minecraftVersionConfig{}, err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := lease.Release(releaseCtx); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()

	manifest, manifestSourceURL, err := fetchMojangVersionManifest(ctx)
	if err != nil {
		return minecraftVersionConfig{}, err
	}

	current, err := loadMinecraftVersionConfig(ctx, db)
	if err != nil {
		return minecraftVersionConfig{}, fmt.Errorf("load Minecraft version configuration before synchronization: %w", err)
	}
	current.Versions = mergeMojangVersions(manifest, current.Versions)
	current.SourceURL = manifestSourceURL
	current.LastSyncedAt = time.Now().UTC().Format(time.RFC3339)
	current.LatestRelease = strings.TrimSpace(manifest.Latest.Release)
	current.LatestSnapshot = strings.TrimSpace(manifest.Latest.Snapshot)
	syncConfiguredLoaderVersions(ctx, &current)
	artifacts, err := synchronizeMRPackLoaderArtifacts(ctx, &current)
	if err != nil {
		return minecraftVersionConfig{}, fmt.Errorf("failed to synchronize Minecraft loader artifacts: %w", err)
	}

	normalized, err := normalizeMinecraftVersionConfig(current)
	if err != nil {
		return minecraftVersionConfig{}, err
	}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, db, normalized, artifacts); err != nil {
		return minecraftVersionConfig{}, fmt.Errorf("failed to save synchronized Minecraft versions: %w", err)
	}
	return normalized, nil
}

func fetchMojangVersionManifest(ctx context.Context) (mojangVersionManifest, string, error) {
	urls := []string{mojangVersionManifestURL, bmclMojangVersionManifestURL}
	errors := make([]string, 0, len(urls))
	for _, sourceURL := range urls {
		payload, err := fetchMinecraftSource(ctx, sourceURL)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		var manifest mojangVersionManifest
		if err = json.Unmarshal(payload, &manifest); err != nil {
			errors = append(errors, fmt.Sprintf("decode %s: %v", sourceURL, err))
			continue
		}
		if len(manifest.Versions) == 0 {
			errors = append(errors, fmt.Sprintf("%s returned no Minecraft versions", sourceURL))
			continue
		}
		return manifest, sourceURL, nil
	}
	return mojangVersionManifest{}, "", fmt.Errorf("failed to synchronize the Mojang version manifest: %s", strings.Join(errors, "; "))
}

func mergeMojangVersions(manifest mojangVersionManifest, current []minecraftVersionOption) []minecraftVersionOption {
	knownTypes := make(map[string]string, len(current))
	for _, version := range current {
		knownTypes[version.Code] = version.Type
	}
	versions := make([]minecraftVersionOption, 0, len(manifest.Versions)+len(current))
	seen := make(map[string]bool, cap(versions))
	for _, version := range manifest.Versions {
		code := strings.TrimSpace(version.ID)
		if code == "" || seen[code] {
			continue
		}
		versionType := minecraftVersionType(version.Type, code)
		if knownTypes[code] == "april_fools" {
			versionType = "april_fools"
		}
		seen[code] = true
		versions = append(versions, minecraftVersionOption{Code: code, Type: versionType})
	}
	for _, version := range current {
		if seen[version.Code] {
			continue
		}
		seen[version.Code] = true
		versions = append(versions, version)
	}
	return versions
}

func syncConfiguredLoaderVersions(ctx context.Context, config *minecraftVersionConfig) {
	sources := []minecraftLoaderVersionSource{
		{code: "Forge", primaryURL: forgeMavenMetadataURL, fetch: fetchForgeMinecraftVersions},
		{code: "NeoForge", primaryURL: neoForgeMavenMetadataURL, fetch: fetchNeoForgeMinecraftVersions},
		{code: "Fabric", primaryURL: fabricGameVersionsURL, fetch: fetchFabricMinecraftVersions},
		{code: "LiteLoader", primaryURL: liteLoaderVersionsURL, fetch: fetchLiteLoaderMinecraftVersions},
	}
	knownVersions := make([]string, 0, len(config.Versions))
	for _, version := range config.Versions {
		knownVersions = append(knownVersions, version.Code)
	}

	results := make(chan minecraftLoaderVersionResult, len(sources))
	var wait sync.WaitGroup
	activeSources := 0
	for _, source := range sources {
		if findMinecraftLoader(config.Loaders, source.code) < 0 {
			continue
		}
		activeSources++
		wait.Add(1)
		go func(source minecraftLoaderVersionSource) {
			defer wait.Done()
			versions, sourceURLs, fallback, err := source.fetch(ctx, knownVersions)
			results <- minecraftLoaderVersionResult{source: source, versions: versions, sourceURLs: sourceURLs, usedFallback: fallback, err: err}
		}(source)
	}
	wait.Wait()
	close(results)

	byCode := make(map[string]minecraftLoaderVersionResult, activeSources)
	for result := range results {
		byCode[strings.ToLower(result.source.code)] = result
	}
	applyMinecraftLoaderVersionResults(config, sources, byCode)
}

func applyMinecraftLoaderVersionResults(config *minecraftVersionConfig, sources []minecraftLoaderVersionSource, byCode map[string]minecraftLoaderVersionResult) {
	previousStatuses := make(map[string]minecraftLoaderSyncStatus, len(config.LoaderSyncs))
	for _, status := range config.LoaderSyncs {
		previousStatuses[strings.ToLower(status.Code)] = status
	}
	statuses := make([]minecraftLoaderSyncStatus, 0, len(byCode))
	for _, source := range sources {
		loaderIndex := findMinecraftLoader(config.Loaders, source.code)
		if loaderIndex < 0 {
			continue
		}
		result := byCode[strings.ToLower(source.code)]
		status := minecraftLoaderSyncStatus{
			Code: source.code, SourceURL: source.primaryURL, Status: "failed",
			SourceURLs:   []string{source.primaryURL},
			VersionCount: len(config.Loaders[loaderIndex].Versions),
		}
		if previous, ok := previousStatuses[strings.ToLower(source.code)]; ok {
			status.LastSyncedAt = previous.LastSyncedAt
		}
		if result.err != nil {
			status.Error = truncateMinecraftSyncError(result.err.Error())
			statuses = append(statuses, status)
			continue
		}
		config.Loaders[loaderIndex].Versions = orderSupportedMinecraftVersions(config.Versions, result.versions)
		status.SourceURLs = uniqueTrimmed(result.sourceURLs, 8)
		if len(status.SourceURLs) > 0 {
			status.SourceURL = status.SourceURLs[0]
		}
		status.Status = "synced"
		status.LastSyncedAt = config.LastSyncedAt
		status.VersionCount = len(config.Loaders[loaderIndex].Versions)
		status.UsedFallback = result.usedFallback
		statuses = append(statuses, status)
	}
	config.LoaderSyncs = statuses
}

func fetchForgeMinecraftVersions(ctx context.Context, known []string) ([]string, []string, bool, error) {
	versions, sourceURL, fallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: forgeMavenMetadataURL, decode: decodeMavenMetadataVersions},
		{url: bmclForgeVersionsURL, decode: decodeStringArray},
	})
	if err != nil {
		return nil, nil, false, err
	}
	return requireResolvedLoaderVersions("Forge", resolveArtifactMinecraftVersions(versions, known), []string{sourceURL}, fallback)
}

func fetchNeoForgeMinecraftVersions(ctx context.Context, known []string) ([]string, []string, bool, error) {
	modern, modernURL, modernFallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: neoForgeMavenMetadataURL, decode: decodeMavenMetadataVersions},
		{url: bmclNeoForgeMetadataURL, decode: decodeMavenMetadataVersions},
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("NeoForge metadata: %w", err)
	}
	legacy, legacyURL, legacyFallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: neoForgeLegacyMetadataURL, decode: decodeMavenMetadataVersions},
		{url: bmclNeoForgeLegacyURL, decode: decodeMavenMetadataVersions},
	})
	if err != nil {
		return nil, nil, false, fmt.Errorf("NeoForge 1.20.1 metadata: %w", err)
	}
	values := append(modern, legacy...)
	resolved := resolveNeoForgeMinecraftVersions(values, known)
	usedFallback := modernFallback || legacyFallback
	return requireResolvedLoaderVersions("NeoForge", resolved, []string{modernURL, legacyURL}, usedFallback)
}

func fetchFabricMinecraftVersions(ctx context.Context, known []string) ([]string, []string, bool, error) {
	versions, sourceURL, fallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: fabricGameVersionsURL, decode: decodeFabricGameVersions},
		{url: bmclFabricGameVersionsURL, decode: decodeFabricGameVersions},
	})
	if err != nil {
		return nil, nil, false, err
	}
	return requireResolvedLoaderVersions("Fabric", filterKnownMinecraftVersions(versions, known), []string{sourceURL}, fallback)
}

func fetchLiteLoaderMinecraftVersions(ctx context.Context, known []string) ([]string, []string, bool, error) {
	versions, sourceURL, fallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: liteLoaderVersionsURL, decode: decodeLiteLoaderMinecraftVersions},
		{url: bmclLiteLoaderVersionsURL, decode: decodeLiteLoaderMinecraftVersions},
	})
	if err != nil {
		return nil, nil, false, err
	}
	return requireResolvedLoaderVersions("LiteLoader", filterKnownMinecraftVersions(versions, known), []string{sourceURL}, fallback)
}

func requireResolvedLoaderVersions(loader string, versions, sourceURLs []string, fallback bool) ([]string, []string, bool, error) {
	if len(versions) == 0 {
		return nil, nil, false, fmt.Errorf("%s source returned no versions present in the Minecraft catalog", loader)
	}
	return versions, uniqueTrimmed(sourceURLs, 8), fallback, nil
}

func fetchMinecraftVersionAttempts(ctx context.Context, attempts []minecraftSourceAttempt) ([]string, string, bool, error) {
	errors := make([]string, 0, len(attempts))
	for index, attempt := range attempts {
		payload, err := fetchMinecraftSource(ctx, attempt.url)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		versions, err := attempt.decode(payload)
		if err != nil {
			errors = append(errors, fmt.Sprintf("decode %s: %v", attempt.url, err))
			continue
		}
		if len(versions) == 0 {
			errors = append(errors, fmt.Sprintf("%s returned no supported versions", attempt.url))
			continue
		}
		return versions, attempt.url, index > 0, nil
	}
	return nil, "", false, fmt.Errorf("all version sources failed: %s", strings.Join(errors, "; "))
}

func fetchMinecraftSource(ctx context.Context, sourceURL string) ([]byte, error) {
	if payload, ok := minecraftSourceResponses.fresh(sourceURL, time.Now()); ok {
		return payload, nil
	}
	result := minecraftSourceFetchGroup.DoChan(sourceURL, func() (any, error) {
		if payload, ok := minecraftSourceResponses.fresh(sourceURL, time.Now()); ok {
			return payload, nil
		}
		return downloadMinecraftSource(context.WithoutCancel(ctx), sourceURL)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resolved := <-result:
		if resolved.Err != nil {
			return nil, resolved.Err
		}
		payload, ok := resolved.Val.([]byte)
		if !ok {
			return nil, errors.New("invalid Minecraft source cache result")
		}
		return payload, nil
	}
}

func downloadMinecraftSource(ctx context.Context, sourceURL string) ([]byte, error) {
	select {
	case minecraftSourceFetchSlots <- struct{}{}:
		defer func() { <-minecraftSourceFetchSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	stale, hasStale := minecraftSourceResponses.stale(sourceURL)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "mcmods.cn/version-sync")
	request.Header.Set("Accept", "application/json, application/xml, text/xml;q=0.9, */*;q=0.5")
	if hasStale {
		if stale.etag != "" {
			request.Header.Set("If-None-Match", stale.etag)
		}
		if stale.lastModified != "" {
			request.Header.Set("If-Modified-Since", stale.lastModified)
		}
	}
	response, err := minecraftVersionHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", sourceURL, err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotModified && hasStale {
		stale.freshUntil = time.Now().Add(minecraftSourceCacheTTL)
		minecraftSourceResponses.store(sourceURL, stale)
		return stale.payload, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", sourceURL, response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxMinecraftSourceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sourceURL, err)
	}
	if len(payload) > maxMinecraftSourceBytes {
		return nil, fmt.Errorf("%s exceeds %d bytes", sourceURL, maxMinecraftSourceBytes)
	}
	minecraftSourceResponses.store(sourceURL, minecraftSourceCacheEntry{
		payload:      payload,
		etag:         strings.TrimSpace(response.Header.Get("ETag")),
		lastModified: strings.TrimSpace(response.Header.Get("Last-Modified")),
		freshUntil:   time.Now().Add(minecraftSourceCacheTTL),
	})
	return payload, nil
}

func decodeMavenMetadataVersions(payload []byte) ([]string, error) {
	var metadata struct {
		Versioning struct {
			Versions struct {
				Version []string `xml:"version"`
			} `xml:"versions"`
		} `xml:"versioning"`
	}
	if err := xml.Unmarshal(payload, &metadata); err != nil {
		return nil, err
	}
	return uniqueTrimmed(metadata.Versioning.Versions.Version, maxMinecraftVersions*4), nil
}

func decodeStringArray(payload []byte) ([]string, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(raw))
	for _, item := range raw {
		var version string
		if json.Unmarshal(item, &version) == nil {
			versions = append(versions, version)
		}
	}
	return uniqueTrimmed(versions, maxMinecraftVersions*4), nil
}

func decodeFabricGameVersions(payload []byte) ([]string, error) {
	var items []struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(payload, &items); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(items))
	for _, item := range items {
		versions = append(versions, item.Version)
	}
	return uniqueTrimmed(versions, maxMinecraftVersions*4), nil
}

func decodeLiteLoaderMinecraftVersions(payload []byte) ([]string, error) {
	var root struct {
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(payload, &root); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(root.Versions))
	for version := range root.Versions {
		versions = append(versions, version)
	}
	return versions, nil
}

func resolveArtifactMinecraftVersions(artifacts, known []string) []string {
	sortedKnown := append([]string(nil), known...)
	sort.SliceStable(sortedKnown, func(i, j int) bool { return len(sortedKnown[i]) > len(sortedKnown[j]) })
	resolved := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		artifact = strings.TrimSpace(artifact)
		for _, version := range sortedKnown {
			if artifact == version || strings.HasPrefix(artifact, version+"-") || strings.HasSuffix(artifact, "-"+version) {
				resolved = append(resolved, version)
				break
			}
		}
	}
	return uniqueTrimmed(resolved, maxMinecraftVersions)
}

func resolveNeoForgeMinecraftVersions(artifacts, known []string) []string {
	knownSet := make(map[string]bool, len(known))
	for _, version := range known {
		knownSet[version] = true
	}
	resolved := resolveArtifactMinecraftVersions(artifacts, known)
	for _, artifact := range artifacts {
		for _, candidate := range neoForgeMinecraftVersionCandidates(artifact) {
			if knownSet[candidate] {
				resolved = append(resolved, candidate)
				break
			}
		}
	}
	return uniqueTrimmed(resolved, maxMinecraftVersions)
}

func neoForgeMinecraftVersionCandidates(raw string) []string {
	version := strings.TrimSpace(raw)
	if strings.HasPrefix(version, "0.") {
		snapshot := strings.TrimPrefix(version, "0.")
		if separator := strings.IndexByte(snapshot, '.'); separator >= 0 {
			snapshot = snapshot[:separator]
		}
		return []string{snapshot}
	}
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return nil
	}
	major, majorErr := strconv.Atoi(parts[0])
	minor, minorErr := strconv.Atoi(parts[1])
	if majorErr != nil || minorErr != nil {
		return nil
	}
	if major >= 26 {
		return []string{fmt.Sprintf("%d.%d", major, minor), fmt.Sprintf("1.%d.%d", major, minor)}
	}
	if major < 20 {
		return nil
	}
	if minor == 0 {
		return []string{fmt.Sprintf("1.%d", major), fmt.Sprintf("1.%d.0", major)}
	}
	return []string{fmt.Sprintf("1.%d.%d", major, minor)}
}

func filterKnownMinecraftVersions(values, known []string) []string {
	knownSet := make(map[string]bool, len(known))
	for _, version := range known {
		knownSet[version] = true
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); knownSet[value] {
			result = append(result, value)
		}
	}
	return uniqueTrimmed(result, maxMinecraftVersions)
}

func orderSupportedMinecraftVersions(all []minecraftVersionOption, supported []string) []string {
	supportedSet := make(map[string]bool, len(supported))
	for _, version := range supported {
		supportedSet[version] = true
	}
	ordered := make([]string, 0, len(supportedSet))
	for _, version := range all {
		if supportedSet[version.Code] {
			ordered = append(ordered, version.Code)
		}
	}
	return ordered
}

func findMinecraftLoader(loaders []minecraftLoaderOption, code string) int {
	code = canonicalMinecraftLoaderCode(code)
	for index, loader := range loaders {
		if canonicalMinecraftLoaderCode(loader.Code) == code {
			return index
		}
	}
	return -1
}

func truncateMinecraftSyncError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
