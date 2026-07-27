package httpapi

import (
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	ossProjectDirectory = "project"
	ossUserDirectory    = "user"
)

func ossRoot(prefix string) string {
	if normalized := normalizeObjectPrefix(prefix); normalized != "" {
		return normalized
	}
	return "mcmods"
}

func normalizeOSSProjectKind(value string) string {
	switch normalizeObjectSegment(value) {
	case "mod", "mods":
		return "mods"
	case "plugin", "plugins":
		return "plugins"
	case "modpack", "modpacks":
		return "modpacks"
	case "map", "maps":
		return "maps"
	case "resourcepack", "resourcepacks", "resource_pack", "resource_packs", "texturepack", "texturepacks", "texture_pack", "texture_packs":
		return "resourcepacks"
	case "shader", "shaders", "shaderpack", "shaderpacks", "shader_pack", "shader_packs":
		return "shaders"
	case "datapack", "datapacks", "data_pack", "data_packs":
		return "datapacks"
	case "blueprint", "blueprints", "bluemap", "bluemaps":
		return "blueprints"
	case "skin", "skins", "cape", "capes":
		return "skins"
	case "author", "authors", "creator", "creators":
		return "authors"
	case "team", "teams":
		return "teams"
	case "catalog", "catalogs":
		return "catalogs"
	default:
		return "other"
	}
}

func ossProjectCategory(projectType, publicID string, segments ...string) string {
	parts := []string{ossProjectDirectory, normalizeOSSProjectKind(projectType), normalizeProjectObjectSegment(publicID)}
	for _, segment := range segments {
		if normalized := normalizeObjectSegment(segment); normalized != "" {
			parts = append(parts, normalized)
		}
	}
	return path.Join(parts...)
}

func ossProjectReleaseCategory(projectType, publicID string, segments ...string) string {
	return ossProjectCategory(projectType, publicID, append([]string{"files", "releases"}, segments...)...)
}

func ossProjectTextCategory(projectType, publicID, contentPublicID string, segments ...string) string {
	contentPublicID = normalizeProjectObjectSegment(contentPublicID)
	return ossProjectCategory(projectType, publicID, append([]string{"files", "text", contentPublicID}, segments...)...)
}

func ossModImportCategory(projectPublicID, source string, segments ...string) string {
	source = normalizeObjectSegment(strings.ReplaceAll(source, "_", "-"))
	if source == "" {
		source = "unknown"
	}
	return ossProjectCategory("mod", projectPublicID, append([]string{"files", "imports", source}, segments...)...)
}

func ossUserCategory(userID int64, fileScope string, segments ...string) string {
	parts := []string{ossUserDirectory, strconv.FormatInt(userID, 10), "files", normalizeOSSUserFileScope(fileScope, fileScope)}
	for _, segment := range segments {
		if normalized := normalizeObjectSegment(segment); normalized != "" {
			parts = append(parts, normalized)
		}
	}
	return path.Join(parts...)
}

func ossUserPrefix(prefix string, userID int64) string {
	return path.Join(ossRoot(prefix), ossUserDirectory, strconv.FormatInt(userID, 10))
}

func ossBlueprintReleaseCategory(publicID string, segments ...string) string {
	return ossProjectReleaseCategory("blueprint", publicID, segments...)
}

func ossBlueprintTextCategory(publicID string, segments ...string) string {
	return ossProjectTextCategory("blueprint", publicID, publicID, segments...)
}

func ossSharedSkinTextureCategory() string {
	return ossProjectReleaseCategory("skin", "_shared", "textures")
}

func ossObjectPrefix(prefix, category string) string {
	return path.Join(ossRoot(prefix), strings.Trim(category, "/"))
}

func ossCategoryFromObjectKey(objectKey, prefix string) string {
	root := ossRoot(prefix)
	relative := strings.TrimPrefix(strings.TrimSpace(objectKey), root+"/")
	if relative == objectKey || relative == "" {
		return "misc"
	}
	return path.Dir(relative)
}

func buildOSSObjectKeyForFile(prefix, category, originalName string) string {
	extension := strings.ToLower(filepath.Ext(strings.TrimSpace(originalName)))
	if len(extension) > 16 || extension == "." {
		extension = ""
	}
	return path.Join(ossRoot(prefix), category, randomObjectName()+extension)
}

func modExportMediaObjectCategory(projectPublicID, revisionID, assetPath string) string {
	cleaned := sanitizeOSSAssetPath(assetPath)
	switch {
	case strings.HasPrefix(cleaned, "icons/"):
		parts := strings.Split(cleaned, "/")
		if len(parts) >= 4 {
			return ossProjectCategory("mod", projectPublicID, "icons", parts[1], parts[2], revisionID)
		}
		return ossProjectCategory("mod", projectPublicID, "icons", "imported", revisionID)
	case strings.HasPrefix(cleaned, "recipes/") && strings.Contains(cleaned, "/backgrounds/"):
		return ossProjectCategory("mod", projectPublicID, "recipe-gui", revisionID)
	case strings.HasPrefix(cleaned, "assets/") || strings.HasPrefix(cleaned, "block_entities/"):
		return ossProjectCategory("mod", projectPublicID, "models", revisionID)
	default:
		return ossProjectCategory("mod", projectPublicID, "assets", revisionID)
	}
}

func modExportMediaObjectSuffix(assetPath string) string {
	cleaned := sanitizeOSSAssetPath(assetPath)
	if strings.HasPrefix(cleaned, "icons/") {
		parts := strings.Split(cleaned, "/")
		if len(parts) >= 4 {
			return path.Join(parts[3:]...)
		}
	}
	if strings.HasPrefix(cleaned, "recipes/") {
		return strings.TrimPrefix(cleaned, "recipes/")
	}
	return cleaned
}

func sanitizeOSSAssetPath(value string) string {
	value = strings.Trim(strings.ReplaceAll(value, "\\", "/"), "/ ")
	parts := strings.Split(value, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			cleaned = append(cleaned, "_parent_")
			continue
		}
		var builder strings.Builder
		for _, current := range strings.ToLower(part) {
			if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' || current == '.' || current == '_' || current == '-' {
				builder.WriteRune(current)
			} else {
				builder.WriteByte('-')
			}
		}
		if segment := strings.Trim(builder.String(), ". "); segment != "" {
			cleaned = append(cleaned, segment)
		}
	}
	if len(cleaned) == 0 {
		return "asset"
	}
	return path.Join(cleaned...)
}
