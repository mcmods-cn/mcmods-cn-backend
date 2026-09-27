package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	minecraftVersionsSettingKey = "minecraft.versions"
	mojangVersionManifestURL    = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	maxMinecraftVersions        = 2500
)

var errMinecraftVersionConfigUnavailable = errors.New("Minecraft version configuration is unavailable")
var errMinecraftVersionSyncInProgress = errors.New("Minecraft version synchronization is already in progress")
var errInvalidMinecraftVersionCodes = errors.New("invalid Minecraft version codes")
var errUnknownMinecraftVersionCodes = errors.New("unknown Minecraft version codes")

var minecraftVersionCodePattern = regexp.MustCompile(`^[A-Za-z0-9][-A-Za-z0-9._()' +]{0,79}$`)

type minecraftVersionConfigQueryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type minecraftVersionOption struct {
	Code string `json:"code"`
	Type string `json:"type"`
}

type minecraftLoaderOption struct {
	Code     string   `json:"code"`
	Name     string   `json:"name"`
	Versions []string `json:"versions"`
}

type minecraftLoaderSyncStatus struct {
	Code         string   `json:"code"`
	SourceURL    string   `json:"sourceUrl"`
	SourceURLs   []string `json:"sourceUrls,omitempty"`
	Status       string   `json:"status"`
	LastSyncedAt string   `json:"lastSyncedAt,omitempty"`
	VersionCount int      `json:"versionCount"`
	UsedFallback bool     `json:"usedFallback,omitempty"`
	Error        string   `json:"error,omitempty"`
}

type minecraftVersionConfig struct {
	Versions       []minecraftVersionOption    `json:"versions"`
	CommonVersions []string                    `json:"commonVersions"`
	Loaders        []minecraftLoaderOption     `json:"loaders"`
	SourceURL      string                      `json:"sourceUrl"`
	LastSyncedAt   string                      `json:"lastSyncedAt,omitempty"`
	LatestRelease  string                      `json:"latestRelease,omitempty"`
	LatestSnapshot string                      `json:"latestSnapshot,omitempty"`
	LoaderSyncs    []minecraftLoaderSyncStatus `json:"loaderSyncs,omitempty"`
}

type mojangVersionManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"versions"`
}

func (s *Server) publicMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	config, err := loadMinecraftVersionConfig(r.Context(), s.db)
	if err != nil {
		log.Printf("load public Minecraft version configuration: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load Minecraft version settings")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) updateMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	var payload minecraftVersionConfig
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "request body is invalid")
		return
	}
	config, err := normalizeMinecraftVersionConfig(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := loadMinecraftVersionConfig(r.Context(), s.db)
	if err != nil {
		log.Printf("load Minecraft version configuration before update: %v", err)
		writeError(w, http.StatusInternalServerError, "failed to load Minecraft version settings")
		return
	}
	copyMinecraftSyncMetadata(&config, current)
	if err = saveMinecraftVersionConfig(r.Context(), s.db, config); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save Minecraft version settings")
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) syncMinecraftVersions(w http.ResponseWriter, r *http.Request) {
	config, err := syncMinecraftVersionCatalog(r.Context(), s.db)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errMinecraftVersionConfigUnavailable) {
			status = http.StatusInternalServerError
		} else if errors.Is(err, errMinecraftVersionSyncInProgress) {
			status = http.StatusConflict
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func loadMinecraftVersionConfig(ctx context.Context, db minecraftVersionConfigQueryRower) (minecraftVersionConfig, error) {
	config := defaultMinecraftVersionConfig()
	var raw []byte
	err := db.QueryRow(ctx, `select value from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return config, nil
	}
	if err != nil {
		return minecraftVersionConfig{}, fmt.Errorf("%w: read setting: %v", errMinecraftVersionConfigUnavailable, err)
	}
	var stored minecraftVersionConfig
	if err = json.Unmarshal(raw, &stored); err != nil {
		return minecraftVersionConfig{}, fmt.Errorf("%w: decode setting: %v", errMinecraftVersionConfigUnavailable, err)
	}
	normalized, err := normalizeMinecraftVersionConfig(stored)
	if err != nil {
		return minecraftVersionConfig{}, fmt.Errorf("%w: normalize setting: %v", errMinecraftVersionConfigUnavailable, err)
	}
	return mergeMinecraftVersionConfig(config, normalized), nil
}

func classifyMinecraftVersionCodes(config minecraftVersionConfig, values []string, maximum int) (recognized, unknown []string, err error) {
	if len(values) > maximum {
		return nil, nil, errInvalidMinecraftVersionCodes
	}
	valid := make(map[string]struct{}, len(config.Versions))
	for _, version := range config.Versions {
		valid[version.Code] = struct{}{}
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		if len(value) > 80 {
			unknown = append(unknown, value)
			continue
		}
		if _, exists := valid[value]; exists {
			recognized = append(recognized, value)
		} else {
			unknown = append(unknown, value)
		}
	}
	return recognized, unknown, nil
}

func authoritativeMinecraftVersionCodes(ctx context.Context, db minecraftVersionConfigQueryRower, values []string, maximum int) ([]string, error) {
	config, err := loadMinecraftVersionConfig(ctx, db)
	if err != nil {
		return nil, err
	}
	recognized, unknown, err := classifyMinecraftVersionCodes(config, values, maximum)
	if err != nil {
		return nil, err
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("%w: %s", errUnknownMinecraftVersionCodes, strings.Join(unknown, ", "))
	}
	return recognized, nil
}

func validMinecraftVersionCode(value string) bool {
	return value != "" && len(value) <= 80 && strings.TrimSpace(value) == value && minecraftVersionCodePattern.MatchString(value)
}

func saveMinecraftVersionConfig(ctx context.Context, db *pgxpool.Pool, config minecraftVersionConfig) error {
	return saveMinecraftVersionConfigAndInvalidateStaleArtifacts(ctx, db, config)
}

func minecraftVersionType(value string, codes ...string) string {
	code := ""
	if len(codes) > 0 {
		code = strings.ToLower(strings.TrimSpace(codes[0]))
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "snapshot":
		if strings.Contains(code, "-pre") {
			return "pre_release"
		}
		if strings.Contains(code, "-rc") {
			return "release_candidate"
		}
		return "snapshot"
	case "old_alpha", "old_beta":
		return "legacy"
	default:
		return "release"
	}
}

func mergeMinecraftVersionConfig(base, stored minecraftVersionConfig) minecraftVersionConfig {
	if len(stored.Versions) > 0 {
		base.Versions = stored.Versions
	}
	if len(stored.CommonVersions) > 0 {
		base.CommonVersions = stored.CommonVersions
	}
	if len(stored.Loaders) > 0 {
		base.Loaders = stored.Loaders
	}
	copyMinecraftSyncMetadata(&base, stored)
	return base
}

func copyMinecraftSyncMetadata(target *minecraftVersionConfig, source minecraftVersionConfig) {
	target.SourceURL = source.SourceURL
	if target.SourceURL == "" {
		target.SourceURL = mojangVersionManifestURL
	}
	target.LastSyncedAt = source.LastSyncedAt
	target.LatestRelease = source.LatestRelease
	target.LatestSnapshot = source.LatestSnapshot
	target.LoaderSyncs = append([]minecraftLoaderSyncStatus(nil), source.LoaderSyncs...)
}

func normalizeMinecraftVersionConfig(payload minecraftVersionConfig) (minecraftVersionConfig, error) {
	versionSet := map[string]bool{}
	versions := make([]minecraftVersionOption, 0, len(payload.Versions))
	for _, version := range payload.Versions {
		version.Code = strings.TrimSpace(version.Code)
		version.Type = strings.TrimSpace(version.Type)
		if version.Code == "" || versionSet[version.Code] {
			continue
		}
		if !validMinecraftVersionCode(version.Code) {
			return minecraftVersionConfig{}, &requestError{message: "Minecraft version code is invalid"}
		}
		if version.Type != "release" && version.Type != "snapshot" && version.Type != "pre_release" &&
			version.Type != "release_candidate" && version.Type != "april_fools" && version.Type != "legacy" {
			version.Type = "release"
		}
		versionSet[version.Code] = true
		versions = append(versions, version)
	}
	if len(versions) > maxMinecraftVersions {
		return minecraftVersionConfig{}, &requestError{message: "too many Minecraft versions"}
	}

	loaderSet := map[string]bool{}
	loaders := make([]minecraftLoaderOption, 0, len(payload.Loaders))
	for _, loader := range payload.Loaders {
		loader.Code = canonicalMinecraftLoaderCode(loader.Code)
		loader.Name = strings.TrimSpace(loader.Name)
		if loader.Code == "" {
			continue
		}
		loaderKey := strings.ToLower(loader.Code)
		if loaderSet[loaderKey] {
			return minecraftVersionConfig{}, &requestError{message: "duplicate Minecraft loader code"}
		}
		if len(loader.Code) > 80 || len(loader.Name) > 120 {
			return minecraftVersionConfig{}, &requestError{message: "Minecraft loader name is too long"}
		}
		loaderSet[loaderKey] = true
		loader.Versions = uniqueTrimmed(loader.Versions, maxMinecraftVersions)
		filtered := loader.Versions[:0]
		for _, version := range loader.Versions {
			if versionSet[version] {
				filtered = append(filtered, version)
			}
		}
		loader.Versions = filtered
		if loader.Name == "" {
			loader.Name = loader.Code
		}
		loaders = append(loaders, loader)
	}
	if len(loaders) > 100 {
		return minecraftVersionConfig{}, &requestError{message: "too many Minecraft loaders"}
	}
	commonVersions := uniqueTrimmed(payload.CommonVersions, 20)
	filteredCommonVersions := commonVersions[:0]
	for _, version := range commonVersions {
		if versionSet[version] {
			filteredCommonVersions = append(filteredCommonVersions, version)
		}
	}
	return minecraftVersionConfig{
		Versions: versions, CommonVersions: filteredCommonVersions, Loaders: loaders, SourceURL: normalizedMinecraftSourceURL(payload.SourceURL),
		LastSyncedAt: strings.TrimSpace(payload.LastSyncedAt), LatestRelease: strings.TrimSpace(payload.LatestRelease),
		LatestSnapshot: strings.TrimSpace(payload.LatestSnapshot), LoaderSyncs: normalizeMinecraftLoaderSyncs(payload.LoaderSyncs),
	}, nil
}

func normalizedMinecraftSourceURL(value string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return mojangVersionManifestURL
}

func normalizeMinecraftLoaderSyncs(items []minecraftLoaderSyncStatus) []minecraftLoaderSyncStatus {
	result := make([]minecraftLoaderSyncStatus, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		item.Code = canonicalMinecraftLoaderCode(item.Code)
		key := strings.ToLower(item.Code)
		if item.Code == "" || seen[key] {
			continue
		}
		seen[key] = true
		item.SourceURL = strings.TrimSpace(item.SourceURL)
		item.SourceURLs = uniqueTrimmed(append([]string{item.SourceURL}, item.SourceURLs...), 8)
		if len(item.SourceURLs) > 0 {
			item.SourceURL = item.SourceURLs[0]
		}
		item.LastSyncedAt = strings.TrimSpace(item.LastSyncedAt)
		item.Error = strings.TrimSpace(item.Error)
		if len(item.Error) > 500 {
			item.Error = item.Error[:500]
		}
		if item.Status != "synced" && item.Status != "failed" {
			item.Status = "failed"
		}
		if item.VersionCount < 0 {
			item.VersionCount = 0
		}
		result = append(result, item)
	}
	return result
}

func canonicalMinecraftLoaderCode(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "fabric":
		return "Fabric"
	case "forge":
		return "Forge"
	case "neoforge":
		return "NeoForge"
	case "babric":
		return "Babric"
	case "bta (babric)":
		return "BTA (Babric)"
	case "java agent":
		return "Java Agent"
	case "legacy fabric":
		return "Legacy Fabric"
	case "liteloader":
		return "LiteLoader"
	case "risugami's modloader":
		return "Risugami's ModLoader"
	case "nilloader":
		return "NilLoader"
	case "ornithe":
		return "Ornithe"
	case "quilt":
		return "Quilt"
	case "rift":
		return "Rift"
	default:
		return strings.ToLower(value)
	}
}

func defaultMinecraftVersionConfig() minecraftVersionConfig {
	versions := []minecraftVersionOption{
		{Code: "1.21.5", Type: "release"}, {Code: "1.21.4", Type: "release"}, {Code: "1.21.1", Type: "release"},
		{Code: "1.20.6", Type: "release"}, {Code: "1.20.4", Type: "release"}, {Code: "1.20.1", Type: "release"},
		{Code: "1.19.4", Type: "release"}, {Code: "1.19.2", Type: "release"}, {Code: "1.18.2", Type: "release"},
		{Code: "1.17.1", Type: "release"}, {Code: "1.16.5", Type: "release"}, {Code: "1.15.2", Type: "release"},
		{Code: "1.14.4", Type: "release"}, {Code: "1.13.2", Type: "release"}, {Code: "1.12.2", Type: "release"},
		{Code: "1.11.2", Type: "release"}, {Code: "1.10.2", Type: "release"}, {Code: "1.9.4", Type: "release"},
		{Code: "1.8.9", Type: "release"}, {Code: "1.7.10", Type: "release"}, {Code: "1.6.4", Type: "release"},
		{Code: "25w10a", Type: "snapshot"},
		{Code: "25w14craftmine", Type: "april_fools"}, {Code: "24w14potato", Type: "april_fools"},
		{Code: "23w13a_or_b", Type: "april_fools"}, {Code: "22w13oneBlockAtATime", Type: "april_fools"},
		{Code: "20w14infinite", Type: "april_fools"}, {Code: "3D Shareware v1.34", Type: "april_fools"},
		{Code: "1.RV-Pre1", Type: "april_fools"}, {Code: "15w14a", Type: "april_fools"},
		{Code: "b1.7.3", Type: "legacy"}, {Code: "a1.2.6", Type: "legacy"}, {Code: "rd-132211", Type: "legacy"},
	}
	loaderNames := []string{"Fabric", "Forge", "NeoForge", "Babric", "BTA (Babric)", "Java Agent", "Legacy Fabric", "LiteLoader", "Risugami's ModLoader", "NilLoader", "Ornithe", "Quilt", "Rift"}
	loaders := make([]minecraftLoaderOption, 0, len(loaderNames))
	for _, name := range loaderNames {
		loaders = append(loaders, minecraftLoaderOption{Code: name, Name: name, Versions: []string{}})
	}
	return minecraftVersionConfig{
		Versions:       versions,
		CommonVersions: []string{"1.21.1", "1.20.1", "1.19.2", "1.18.2", "1.16.5", "1.12.2", "1.7.10"},
		Loaders:        loaders,
		SourceURL:      mojangVersionManifestURL,
	}
}

func StartMinecraftVersionSyncScheduler(ctx context.Context, db *pgxpool.Pool) {
	go runMinecraftVersionSyncScheduler(ctx, func(ctx context.Context) (minecraftVersionConfig, error) {
		return syncMinecraftVersionCatalog(ctx, db)
	})
}

func runMinecraftVersionSyncScheduler(ctx context.Context, synchronize func(context.Context) (minecraftVersionConfig, error)) {
	for {
		if ctx.Err() != nil {
			return
		}
		config, err := synchronize(ctx)
		if err != nil {
			if errors.Is(err, errMinecraftVersionSyncInProgress) {
				log.Printf("skip Minecraft version synchronization: %v", err)
			} else {
				log.Printf("synchronize Minecraft versions: %v", err)
			}
		} else {
			log.Printf("Minecraft and mod loader versions synchronized")
			for _, status := range config.LoaderSyncs {
				if status.Status == "failed" {
					log.Printf("synchronize %s versions: %s", status.Code, status.Error)
				}
			}
		}
		if ctx.Err() != nil {
			return
		}
		next := nextMinecraftVersionSync(time.Now())
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func nextMinecraftVersionSync(now time.Time) time.Time {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	localNow := now.In(location)
	next := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 4, 0, 0, 0, location)
	if !next.After(localNow) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
