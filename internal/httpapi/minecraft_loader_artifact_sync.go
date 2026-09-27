package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const fabricLoaderCatalogURL = "https://meta.fabricmc.net/v2/versions/loader"

type fabricLoaderArtifact struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func synchronizeMRPackLoaderArtifacts(ctx context.Context, config *minecraftVersionConfig) ([]minecraftLoaderArtifactSnapshot, error) {
	observedAt := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, config.LastSyncedAt); err == nil {
		observedAt = parsed
	}
	artifacts := make([]minecraftLoaderArtifactSnapshot, 0)
	for loaderIndex := range config.Loaders {
		loaderCode := strings.ToLower(strings.TrimSpace(config.Loaders[loaderIndex].Code))
		versions := config.Loaders[loaderIndex].Versions
		if !isMRPackLoader(loaderCode) || len(versions) == 0 {
			continue
		}
		resolved, err := fetchSynchronizedLoaderArtifactVersions(ctx, loaderCode, versions, observedAt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", loaderCode, err)
		}
		if len(resolved) == 0 {
			return nil, fmt.Errorf("%s metadata resolved none of its %d supported Minecraft versions", loaderCode, len(versions))
		}
		resolvedVersions := make(map[string]bool, len(resolved))
		for _, artifact := range resolved {
			resolvedVersions[artifact.MinecraftVersion] = true
		}
		retained := make([]string, 0, len(resolved))
		for _, version := range versions {
			if resolvedVersions[version] {
				retained = append(retained, version)
			}
		}
		config.Loaders[loaderIndex].Versions = retained
		if omitted := len(versions) - len(retained); omitted > 0 {
			markMinecraftLoaderArtifactOmissions(config, loaderCode, len(retained), omitted)
		}
		artifacts = append(artifacts, resolved...)
	}
	return artifacts, nil
}

func fetchSynchronizedLoaderArtifactVersions(ctx context.Context, loader string, minecraftVersions []string, observedAt time.Time) ([]minecraftLoaderArtifactSnapshot, error) {
	switch loader {
	case "fabric":
		payload, err := fetchMinecraftSource(ctx, fabricLoaderCatalogURL)
		if err != nil {
			return nil, err
		}
		loaderVersion, err := decodeCurrentFabricLoaderVersion(payload)
		if err != nil {
			return nil, err
		}
		artifacts := make([]minecraftLoaderArtifactSnapshot, 0, len(minecraftVersions))
		for _, minecraftVersion := range minecraftVersions {
			artifacts = append(artifacts, minecraftLoaderArtifactSnapshot{
				MinecraftVersion: minecraftVersion, Loader: loader, LoaderVersion: loaderVersion,
				SourceURL: fabricLoaderCatalogURL, ObservedAt: observedAt,
			})
		}
		return artifacts, nil
	case "forge":
		payload, err := fetchMinecraftSource(ctx, forgeMavenMetadataURL)
		if err != nil {
			return nil, err
		}
		versions, err := decodeMavenMetadataVersions(payload)
		if err != nil {
			return nil, err
		}
		return selectSynchronizedMavenLoaderArtifacts(minecraftVersions, loader, forgeMavenMetadataURL, observedAt, func(minecraftVersion string) (string, error) {
			return selectLoaderArtifactVersion(versions, minecraftVersion+"-")
		}), nil
	case "neoforge":
		modernMinecraftVersions, legacyMinecraftVersions := make([]string, 0, len(minecraftVersions)), make([]string, 0, 1)
		for _, minecraftVersion := range minecraftVersions {
			if minecraftVersion == "1.20.1" {
				legacyMinecraftVersions = append(legacyMinecraftVersions, minecraftVersion)
			} else {
				modernMinecraftVersions = append(modernMinecraftVersions, minecraftVersion)
			}
		}
		artifacts := make([]minecraftLoaderArtifactSnapshot, 0, len(minecraftVersions))
		if len(modernMinecraftVersions) > 0 {
			payload, err := fetchMinecraftSource(ctx, neoForgeMavenMetadataURL)
			if err != nil {
				return nil, err
			}
			versions, err := decodeMavenMetadataVersions(payload)
			if err != nil {
				return nil, err
			}
			artifacts = append(artifacts, selectSynchronizedMavenLoaderArtifacts(modernMinecraftVersions, loader, neoForgeMavenMetadataURL, observedAt, func(minecraftVersion string) (string, error) {
				prefix, prefixErr := neoForgeArtifactPrefix(minecraftVersion)
				if prefixErr != nil {
					return "", prefixErr
				}
				return selectLoaderArtifactVersion(versions, prefix)
			})...)
		}
		if len(legacyMinecraftVersions) > 0 {
			payload, err := fetchMinecraftSource(ctx, neoForgeLegacyMetadataURL)
			if err != nil {
				return nil, err
			}
			versions, err := decodeMavenMetadataVersions(payload)
			if err != nil {
				return nil, err
			}
			artifacts = append(artifacts, selectSynchronizedMavenLoaderArtifacts(legacyMinecraftVersions, loader, neoForgeLegacyMetadataURL, observedAt, func(minecraftVersion string) (string, error) {
				prefix, prefixErr := neoForgeArtifactPrefix(minecraftVersion)
				if prefixErr != nil {
					return "", prefixErr
				}
				return selectLoaderArtifactVersion(versions, prefix)
			})...)
		}
		return artifacts, nil
	default:
		return nil, fmt.Errorf("unsupported MRPack loader %q", loader)
	}
}

func selectSynchronizedMavenLoaderArtifacts(minecraftVersions []string, loader, sourceURL string, observedAt time.Time, selectVersion func(string) (string, error)) []minecraftLoaderArtifactSnapshot {
	artifacts := make([]minecraftLoaderArtifactSnapshot, 0, len(minecraftVersions))
	for _, minecraftVersion := range minecraftVersions {
		loaderVersion, err := selectVersion(minecraftVersion)
		if err != nil {
			continue
		}
		artifacts = append(artifacts, minecraftLoaderArtifactSnapshot{
			MinecraftVersion: minecraftVersion, Loader: loader, LoaderVersion: loaderVersion,
			SourceURL: sourceURL, ObservedAt: observedAt,
		})
	}
	return artifacts
}

func decodeCurrentFabricLoaderVersion(payload []byte) (string, error) {
	var items []fabricLoaderArtifact
	if err := json.Unmarshal(payload, &items); err != nil {
		return "", err
	}
	stable, all := make([]string, 0, len(items)), make([]string, 0, len(items))
	for _, item := range items {
		if item.Version = strings.TrimSpace(item.Version); item.Version == "" {
			continue
		}
		all = append(all, item.Version)
		if item.Stable {
			stable = append(stable, item.Version)
		}
	}
	candidates := stable
	if len(candidates) == 0 {
		candidates = all
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("Fabric loader metadata returned no versions")
	}
	sort.SliceStable(candidates, func(i, j int) bool { return compareNumericVersion(candidates[i], candidates[j]) > 0 })
	return candidates[0], nil
}

func markMinecraftLoaderArtifactOmissions(config *minecraftVersionConfig, loaderCode string, retained, omitted int) {
	message := fmt.Sprintf("%d supported Minecraft version(s) omitted because no concrete loader artifact was resolved", omitted)
	for index := range config.LoaderSyncs {
		if !strings.EqualFold(config.LoaderSyncs[index].Code, loaderCode) {
			continue
		}
		config.LoaderSyncs[index].Status = "failed"
		config.LoaderSyncs[index].VersionCount = retained
		if config.LoaderSyncs[index].Error == "" {
			config.LoaderSyncs[index].Error = message
		} else {
			config.LoaderSyncs[index].Error = truncateMinecraftSyncError(config.LoaderSyncs[index].Error + "; " + message)
		}
		return
	}
}
