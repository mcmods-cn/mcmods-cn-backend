package httpapi

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func neoForgeArtifactPrefix(minecraftVersion string) (string, error) {
	minecraftVersion = strings.TrimSpace(minecraftVersion)
	if minecraftVersion == "1.20.1" {
		return "1.20.1-", nil
	}
	parts := strings.Split(minecraftVersion, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return "", fmt.Errorf("unsupported Minecraft version %q for NeoForge artifacts", minecraftVersion)
	}
	components := make([]int, len(parts))
	for index, part := range parts {
		if part == "" {
			return "", fmt.Errorf("unsupported Minecraft version %q for NeoForge artifacts", minecraftVersion)
		}
		for _, character := range part {
			if character < '0' || character > '9' {
				return "", fmt.Errorf("unsupported Minecraft version %q for NeoForge artifacts", minecraftVersion)
			}
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return "", fmt.Errorf("unsupported Minecraft version %q for NeoForge artifacts", minecraftVersion)
		}
		components[index] = value
	}
	patch := 0
	if len(components) == 3 {
		patch = components[2]
	}
	if components[0] == 1 && components[1] >= 20 {
		return fmt.Sprintf("%d.%d.", components[1], patch), nil
	}
	if components[0] >= 26 {
		return fmt.Sprintf("%d.%d.%d.", components[0], components[1], patch), nil
	}
	return "", fmt.Errorf("unsupported Minecraft version %q for NeoForge artifacts", minecraftVersion)
}

func selectLoaderArtifactVersion(versions []string, prefix string) (string, error) {
	matches := make([]string, 0)
	for _, version := range versions {
		if stableLoaderArtifactVersion(version, prefix) {
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

func stableLoaderArtifactVersion(version, prefix string) bool {
	if prefix == "" || !strings.HasPrefix(version, prefix) {
		return false
	}
	release := strings.TrimPrefix(version, prefix)
	if release == "" {
		return false
	}
	for _, component := range strings.Split(release, ".") {
		if component == "" {
			return false
		}
		for _, character := range component {
			if character < '0' || character > '9' {
				return false
			}
		}
	}
	return true
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
