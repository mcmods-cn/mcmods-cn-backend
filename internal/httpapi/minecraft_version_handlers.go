package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	minecraftVersionsSettingKey = "minecraft.versions"
	mojangVersionManifestURL    = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"
	maxMinecraftVersions        = 2500
)

var minecraftVersionSyncMu sync.Mutex

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
	Code         string `json:"code"`
	SourceURL    string `json:"sourceUrl"`
	Status       string `json:"status"`
	LastSyncedAt string `json:"lastSyncedAt,omitempty"`
	VersionCount int    `json:"versionCount"`
	UsedFallback bool   `json:"usedFallback,omitempty"`
	Error        string `json:"error,omitempty"`
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
	writeJSON(w, http.StatusOK, loadMinecraftVersionConfig(r.Context(), s.db))
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
	current := loadMinecraftVersionConfig(r.Context(), s.db)
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
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func loadMinecraftVersionConfig(ctx context.Context, db *pgxpool.Pool) minecraftVersionConfig {
	config := defaultMinecraftVersionConfig()
	var raw []byte
	if err := db.QueryRow(ctx, `select value from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&raw); err != nil {
		return config
	}
	var stored minecraftVersionConfig
	if json.Unmarshal(raw, &stored) != nil {
		return config
	}
	normalized, err := normalizeMinecraftVersionConfig(stored)
	if err != nil {
		return config
	}
	return mergeMinecraftVersionConfig(config, normalized)
}

func saveMinecraftVersionConfig(ctx context.Context, db *pgxpool.Pool, config minecraftVersionConfig) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `insert into system_settings (key,value,updated_at) values ($1,$2::jsonb,now())
		on conflict (key) do update set value=excluded.value,updated_at=now()`, minecraftVersionsSettingKey, string(raw))
	return err
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
		if len(version.Code) > 80 {
			return minecraftVersionConfig{}, &requestError{message: "Minecraft version name is too long"}
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
		loader.Code = strings.TrimSpace(loader.Code)
		loader.Name = strings.TrimSpace(loader.Name)
		if loader.Code == "" || loaderSet[loader.Code] {
			continue
		}
		if len(loader.Code) > 80 || len(loader.Name) > 120 {
			return minecraftVersionConfig{}, &requestError{message: "Minecraft loader name is too long"}
		}
		loaderSet[loader.Code] = true
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
		item.Code = strings.TrimSpace(item.Code)
		key := strings.ToLower(item.Code)
		if item.Code == "" || seen[key] {
			continue
		}
		seen[key] = true
		item.SourceURL = strings.TrimSpace(item.SourceURL)
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
	versionCodes := make([]string, 0, len(versions))
	for _, version := range versions {
		versionCodes = append(versionCodes, version.Code)
	}
	loaderNames := []string{"Fabric", "Forge", "NeoForge", "Babric", "BTA (Babric)", "Java Agent", "Legacy Fabric", "LiteLoader", "Risugami's ModLoader", "NilLoader", "Ornithe", "Quilt", "Rift"}
	loaders := make([]minecraftLoaderOption, 0, len(loaderNames))
	for _, name := range loaderNames {
		loaders = append(loaders, minecraftLoaderOption{Code: name, Name: name, Versions: append([]string(nil), versionCodes...)})
	}
	return minecraftVersionConfig{
		Versions:       versions,
		CommonVersions: []string{"1.21.1", "1.20.1", "1.19.2", "1.18.2", "1.16.5", "1.12.2", "1.7.10"},
		Loaders:        loaders,
		SourceURL:      mojangVersionManifestURL,
	}
}

func StartMinecraftVersionSyncScheduler(ctx context.Context, db *pgxpool.Pool) {
	go func() {
		for {
			next := nextMinecraftVersionSync(time.Now())
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				config, err := syncMinecraftVersionCatalog(ctx, db)
				if err != nil {
					log.Printf("synchronize Minecraft versions: %v", err)
				} else {
					log.Printf("Minecraft and mod loader versions synchronized")
					for _, status := range config.LoaderSyncs {
						if status.Status == "failed" {
							log.Printf("synchronize %s versions: %s", status.Code, status.Error)
						}
					}
				}
			}
		}
	}()
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
