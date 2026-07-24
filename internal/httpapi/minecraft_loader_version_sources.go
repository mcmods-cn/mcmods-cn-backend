package httpapi

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	bmclMojangVersionManifestURL = "https://bmclapi2.bangbang93.com/mc/game/version_manifest_v2.json"
	forgeMavenMetadataURL        = "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	bmclForgeVersionsURL         = "https://bmclapi2.bangbang93.com/forge/minecraft"
	neoForgeMavenMetadataURL     = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	neoForgeLegacyMetadataURL    = "https://maven.neoforged.net/releases/net/neoforged/forge/maven-metadata.xml"
	bmclNeoForgeMetadataURL      = "https://bmclapi2.bangbang93.com/maven/net/neoforged/neoforge/maven-metadata.xml"
	bmclNeoForgeLegacyURL        = "https://bmclapi2.bangbang93.com/maven/net/neoforged/forge/maven-metadata.xml"
	fabricGameVersionsURL        = "https://meta.fabricmc.net/v2/versions/game"
	bmclFabricGameVersionsURL    = "https://bmclapi2.bangbang93.com/fabric-meta/v2/versions/game"
	liteLoaderVersionsURL        = "https://dl.liteloader.com/versions/versions.json"
	bmclLiteLoaderVersionsURL    = "https://bmclapi2.bangbang93.com/maven/com/mumfrey/liteloader/versions.json"
	maxMinecraftSourceBytes      = 16 << 20
)

var minecraftVersionHTTPClient = &http.Client{Timeout: 30 * time.Second}

type minecraftLoaderVersionSource struct {
	code       string
	primaryURL string
	fetch      func(context.Context, []string) ([]string, string, bool, error)
}

type minecraftLoaderVersionResult struct {
	source       minecraftLoaderVersionSource
	versions     []string
	sourceURL    string
	usedFallback bool
	err          error
}

type minecraftSourceAttempt struct {
	url    string
	decode func([]byte) ([]string, error)
}

func syncMinecraftVersionCatalog(ctx context.Context, db *pgxpool.Pool) (minecraftVersionConfig, error) {
	minecraftVersionSyncMu.Lock()
	defer minecraftVersionSyncMu.Unlock()

	manifest, manifestSourceURL, err := fetchMojangVersionManifest(ctx)
	if err != nil {
		return minecraftVersionConfig{}, err
	}

	current := loadMinecraftVersionConfig(ctx, db)
	current.Versions = mergeMojangVersions(manifest, current.Versions)
	current.SourceURL = manifestSourceURL
	current.LastSyncedAt = time.Now().UTC().Format(time.RFC3339)
	current.LatestRelease = strings.TrimSpace(manifest.Latest.Release)
	current.LatestSnapshot = strings.TrimSpace(manifest.Latest.Snapshot)
	syncConfiguredLoaderVersions(ctx, &current)

	normalized, err := normalizeMinecraftVersionConfig(current)
	if err != nil {
		return minecraftVersionConfig{}, err
	}
	if err = saveMinecraftVersionConfig(ctx, db, normalized); err != nil {
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
		versionType := minecraftVersionType(version.Type)
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
			versions, sourceURL, fallback, err := source.fetch(ctx, knownVersions)
			results <- minecraftLoaderVersionResult{source: source, versions: versions, sourceURL: sourceURL, usedFallback: fallback, err: err}
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
		status.SourceURL = result.sourceURL
		status.Status = "synced"
		status.LastSyncedAt = config.LastSyncedAt
		status.VersionCount = len(config.Loaders[loaderIndex].Versions)
		status.UsedFallback = result.usedFallback
		statuses = append(statuses, status)
	}
	config.LoaderSyncs = statuses
}

func fetchForgeMinecraftVersions(ctx context.Context, known []string) ([]string, string, bool, error) {
	versions, sourceURL, fallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: forgeMavenMetadataURL, decode: decodeMavenMetadataVersions},
		{url: bmclForgeVersionsURL, decode: decodeStringArray},
	})
	if err != nil {
		return nil, "", false, err
	}
	return requireResolvedLoaderVersions("Forge", resolveArtifactMinecraftVersions(versions, known), sourceURL, fallback)
}

func fetchNeoForgeMinecraftVersions(ctx context.Context, known []string) ([]string, string, bool, error) {
	modern, modernURL, modernFallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: neoForgeMavenMetadataURL, decode: decodeMavenMetadataVersions},
		{url: bmclNeoForgeMetadataURL, decode: decodeMavenMetadataVersions},
	})
	if err != nil {
		return nil, "", false, fmt.Errorf("NeoForge metadata: %w", err)
	}
	legacy, legacyURL, legacyFallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: neoForgeLegacyMetadataURL, decode: decodeMavenMetadataVersions},
		{url: bmclNeoForgeLegacyURL, decode: decodeMavenMetadataVersions},
	})
	if err != nil {
		return nil, "", false, fmt.Errorf("NeoForge 1.20.1 metadata: %w", err)
	}
	values := append(modern, legacy...)
	resolved := resolveNeoForgeMinecraftVersions(values, known)
	usedFallback := modernFallback || legacyFallback
	sourceURL := modernURL
	if legacyFallback && !modernFallback {
		sourceURL = legacyURL
	}
	return requireResolvedLoaderVersions("NeoForge", resolved, sourceURL, usedFallback)
}

func fetchFabricMinecraftVersions(ctx context.Context, known []string) ([]string, string, bool, error) {
	versions, sourceURL, fallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: fabricGameVersionsURL, decode: decodeFabricGameVersions},
		{url: bmclFabricGameVersionsURL, decode: decodeFabricGameVersions},
	})
	if err != nil {
		return nil, "", false, err
	}
	return requireResolvedLoaderVersions("Fabric", filterKnownMinecraftVersions(versions, known), sourceURL, fallback)
}

func fetchLiteLoaderMinecraftVersions(ctx context.Context, known []string) ([]string, string, bool, error) {
	versions, sourceURL, fallback, err := fetchMinecraftVersionAttempts(ctx, []minecraftSourceAttempt{
		{url: liteLoaderVersionsURL, decode: decodeLiteLoaderMinecraftVersions},
		{url: bmclLiteLoaderVersionsURL, decode: decodeLiteLoaderMinecraftVersions},
	})
	if err != nil {
		return nil, "", false, err
	}
	return requireResolvedLoaderVersions("LiteLoader", filterKnownMinecraftVersions(versions, known), sourceURL, fallback)
}

func requireResolvedLoaderVersions(loader string, versions []string, sourceURL string, fallback bool) ([]string, string, bool, error) {
	if len(versions) == 0 {
		return nil, "", false, fmt.Errorf("%s source returned no versions present in the Minecraft catalog", loader)
	}
	return versions, sourceURL, fallback, nil
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
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "mcmods.cn/version-sync")
	request.Header.Set("Accept", "application/json, application/xml, text/xml;q=0.9, */*;q=0.5")
	response, err := minecraftVersionHTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", sourceURL, err)
	}
	defer response.Body.Close()
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
	for index, loader := range loaders {
		if strings.EqualFold(loader.Code, code) {
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
