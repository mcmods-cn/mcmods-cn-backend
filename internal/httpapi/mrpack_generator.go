package httpapi

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

const modrinthIndexName = "modrinth.index.json"

var safeMRPackFileName = regexp.MustCompile(`^[^\x00-\x1f\\/:*?"<>|]+\.jar$`)

type mrpackEnvironment struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

type mrpackFile struct {
	Path      string            `json:"path"`
	Hashes    map[string]string `json:"hashes"`
	Env       mrpackEnvironment `json:"env"`
	Downloads []string          `json:"downloads"`
	FileSize  int64             `json:"fileSize"`
}

type mrpackIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Game          string            `json:"game"`
	VersionID     string            `json:"versionId"`
	Name          string            `json:"name"`
	Summary       string            `json:"summary,omitempty"`
	Files         []mrpackFile      `json:"files"`
	Dependencies  map[string]string `json:"dependencies"`
}

type mrpackBuildInput struct {
	Name             string
	VersionID        string
	Summary          string
	MinecraftVersion string
	Loader           string
	LoaderVersion    string
	Files            []mrpackFile
}

type mrpackBuildResult struct {
	Data   []byte
	SHA256 string
	Size   int64
}

func buildMRPack(input mrpackBuildInput) (mrpackBuildResult, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.VersionID = strings.TrimSpace(input.VersionID)
	input.MinecraftVersion = strings.TrimSpace(input.MinecraftVersion)
	input.Loader = strings.ToLower(strings.TrimSpace(input.Loader))
	input.LoaderVersion = strings.TrimSpace(input.LoaderVersion)
	if input.Name == "" || input.VersionID == "" || input.MinecraftVersion == "" || input.LoaderVersion == "" {
		return mrpackBuildResult{}, errors.New("mrpack metadata is incomplete")
	}
	if input.VersionID == input.MinecraftVersion {
		return mrpackBuildResult{}, errors.New("pack versionId must not be the Minecraft version")
	}
	loaderKey, err := mrpackLoaderDependencyKey(input.Loader)
	if err != nil {
		return mrpackBuildResult{}, err
	}
	if len(input.Files) == 0 {
		return mrpackBuildResult{}, errors.New("mrpack must contain at least one compatible file")
	}
	seenPaths := make(map[string]struct{}, len(input.Files))
	files := append([]mrpackFile(nil), input.Files...)
	for index := range files {
		if err = validateMRPackFile(&files[index]); err != nil {
			return mrpackBuildResult{}, fmt.Errorf("file %d: %w", index+1, err)
		}
		if _, exists := seenPaths[files[index].Path]; exists {
			return mrpackBuildResult{}, fmt.Errorf("duplicate file path %q", files[index].Path)
		}
		seenPaths[files[index].Path] = struct{}{}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	index := mrpackIndex{
		FormatVersion: 1,
		Game:          "minecraft",
		VersionID:     input.VersionID,
		Name:          input.Name,
		Summary:       strings.TrimSpace(input.Summary),
		Files:         files,
		Dependencies: map[string]string{
			"minecraft": input.MinecraftVersion,
			loaderKey:   input.LoaderVersion,
		},
	}
	payload, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return mrpackBuildResult{}, err
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	header := &zip.FileHeader{Name: modrinthIndexName, Method: zip.Deflate}
	header.SetMode(0o644)
	entry, err := writer.CreateHeader(header)
	if err == nil {
		_, err = entry.Write(payload)
	}
	if closeErr := writer.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return mrpackBuildResult{}, err
	}
	digest := sha256.Sum256(buffer.Bytes())
	return mrpackBuildResult{Data: buffer.Bytes(), SHA256: hex.EncodeToString(digest[:]), Size: int64(buffer.Len())}, nil
}

func mrpackLoaderDependencyKey(loader string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(loader)) {
	case "fabric":
		return "fabric-loader", nil
	case "forge":
		return "forge", nil
	case "neoforge":
		return "neoforge", nil
	default:
		return "", errors.New("unsupported mrpack loader")
	}
}

func validateMRPackFile(file *mrpackFile) error {
	file.Path = strings.TrimSpace(strings.ReplaceAll(file.Path, "\\", "/"))
	if !strings.HasPrefix(file.Path, "mods/") || path.Clean(file.Path) != file.Path || strings.Contains(file.Path, "../") {
		return errors.New("file path must be a clean relative mods/ path")
	}
	name := strings.TrimPrefix(file.Path, "mods/")
	if name == "" || strings.Contains(name, "/") || len(name) > 180 || !safeMRPackFileName.MatchString(name) {
		return errors.New("unsafe or non-JAR file name")
	}
	if file.FileSize <= 0 {
		return errors.New("fileSize must be a positive byte count")
	}
	if !validHexDigest(file.Hashes["sha1"], 40) {
		return errors.New("valid SHA-1 is required")
	}
	if !validHexDigest(file.Hashes["sha512"], 128) {
		return errors.New("valid SHA-512 is required")
	}
	if len(file.Downloads) == 0 {
		return errors.New("an HTTPS Modrinth download is required")
	}
	for _, download := range file.Downloads {
		parsed, err := url.Parse(download)
		if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "cdn.modrinth.com") {
			return errors.New("download must use the official Modrinth CDN over HTTPS")
		}
	}
	if !validMRPackEnvironment(file.Env.Client) || !validMRPackEnvironment(file.Env.Server) {
		return errors.New("invalid client or server environment")
	}
	return nil
}

func validHexDigest(value string, length int) bool {
	value = strings.TrimSpace(value)
	if len(value) != length {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validMRPackEnvironment(value string) bool {
	return value == "required" || value == "optional" || value == "unsupported"
}
