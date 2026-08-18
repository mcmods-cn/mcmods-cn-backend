package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const fabricLoaderVersionsURL = "https://meta.fabricmc.net/v2/versions/loader/"

func resolveMRPackLoaderVersion(ctx context.Context, minecraftVersion, loader string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "fabric":
		payload, err := fetchMinecraftSource(ctx, fabricLoaderVersionsURL+minecraftVersion)
		if err != nil {
			return "", err
		}
		var items []struct {
			Loader struct {
				Version string `json:"version"`
				Stable  bool   `json:"stable"`
			} `json:"loader"`
		}
		if err = json.Unmarshal(payload, &items); err != nil {
			return "", err
		}
		for _, item := range items {
			if item.Loader.Stable && item.Loader.Version != "" {
				return item.Loader.Version, nil
			}
		}
		for _, item := range items {
			if item.Loader.Version != "" {
				return item.Loader.Version, nil
			}
		}
	case "forge":
		payload, err := fetchMinecraftSource(ctx, forgeMavenMetadataURL)
		if err != nil {
			return "", err
		}
		versions, err := decodeMavenMetadataVersions(payload)
		if err != nil {
			return "", err
		}
		return selectLoaderArtifactVersion(versions, minecraftVersion+"-")
	case "neoforge":
		url := neoForgeMavenMetadataURL
		if minecraftVersion == "1.20.1" {
			url = neoForgeLegacyMetadataURL
		}
		payload, err := fetchMinecraftSource(ctx, url)
		if err != nil {
			return "", err
		}
		versions, err := decodeMavenMetadataVersions(payload)
		if err != nil {
			return "", err
		}
		prefix := neoForgeArtifactPrefix(minecraftVersion)
		return selectLoaderArtifactVersion(versions, prefix)
	}
	return "", errors.New("no loader version is available for the selected Minecraft version")
}

func neoForgeArtifactPrefix(minecraftVersion string) string {
	if minecraftVersion == "1.20.1" {
		return "1.20.1-"
	}
	parts := strings.Split(minecraftVersion, ".")
	if len(parts) >= 2 {
		return strings.TrimLeft(parts[1], "0") + "." + firstNonEmpty(strings.TrimLeft(parts[len(parts)-1], "0"), "0") + "."
	}
	return minecraftVersion + "-"
}

func selectLoaderArtifactVersion(versions []string, prefix string) (string, error) {
	matches := make([]string, 0)
	for _, version := range versions {
		if strings.HasPrefix(version, prefix) && !strings.Contains(strings.ToLower(version), "beta") {
			matches = append(matches, version)
		}
	}
	if len(matches) == 0 {
		return "", errors.New("loader metadata has no matching stable version")
	}
	sort.SliceStable(matches, func(i, j int) bool { return compareNumericVersion(matches[i], matches[j]) > 0 })
	selected := matches[0]
	if strings.Contains(selected, "-") && strings.HasPrefix(selected, prefix) {
		selected = strings.TrimPrefix(selected, prefix)
	}
	return selected, nil
}

func compareNumericVersion(left, right string) int {
	leftParts, rightParts := strings.FieldsFunc(left, func(r rune) bool { return r == '.' || r == '-' || r == '+' }), strings.FieldsFunc(right, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	for i := 0; i < len(leftParts) || i < len(rightParts); i++ {
		var l, r string
		if i < len(leftParts) {
			l = leftParts[i]
		}
		if i < len(rightParts) {
			r = rightParts[i]
		}
		if l == r {
			continue
		}
		if len(l) != len(r) {
			if len(l) > len(r) {
				return 1
			}
			return -1
		}
		if l > r {
			return 1
		}
		return -1
	}
	return 0
}
