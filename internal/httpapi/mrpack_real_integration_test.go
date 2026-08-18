package httpapi

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"
)

// This test is intentionally opt-in because it proves the package generator
// against current metadata from the official Modrinth API. The ordinary unit
// tests remain deterministic and do not require network access.
func TestBuildMRPackWithRealModrinthMetadata(t *testing.T) {
	if os.Getenv("MCMODS_REAL_MODRINTH_TEST") != "1" {
		t.Skip("set MCMODS_REAL_MODRINTH_TEST=1 to query the official Modrinth API")
	}
	request, err := http.NewRequest(http.MethodGet, "https://api.modrinth.com/v2/project/P7dR8mSH/version?game_versions=%5B%221.21.1%22%5D&loaders=%5B%22fabric%22%5D", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("User-Agent", "mcmods-cn-development-mrpack-test/1.0")
	response, err := (&http.Client{Timeout: 20 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("query Modrinth: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("Modrinth returned %s", response.Status)
	}
	var versions []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		VersionType string `json:"version_type"`
		Files       []struct {
			URL      string            `json:"url"`
			Filename string            `json:"filename"`
			Primary  bool              `json:"primary"`
			Size     int64             `json:"size"`
			Hashes   map[string]string `json:"hashes"`
		} `json:"files"`
	}
	if err = json.NewDecoder(response.Body).Decode(&versions); err != nil || len(versions) == 0 {
		t.Fatalf("decode Modrinth versions: count=%d err=%v", len(versions), err)
	}
	if len(versions[0].Files) == 0 {
		t.Fatal("Modrinth version has no files")
	}
	var selected = versions[0].Files[0]
	for _, candidate := range versions[0].Files {
		if candidate.Primary {
			selected = candidate
			break
		}
	}
	result, err := buildMRPack(mrpackBuildInput{
		Name: "MCMods real Modrinth metadata test", VersionID: "real-modrinth-fixture-1",
		MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14",
		Files: []mrpackFile{{Path: "mods/" + selected.Filename,
			Hashes: selected.Hashes, Downloads: []string{selected.URL}, FileSize: selected.Size,
			Env: mrpackEnvironment{Client: "required", Server: "required"}}},
	})
	if err != nil {
		t.Fatalf("build real-metadata mrpack: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(result.Data), int64(len(result.Data)))
	if err != nil {
		t.Fatalf("open generated mrpack as ZIP: %v", err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != modrinthIndexName {
		t.Fatalf("unexpected root entries: %#v", reader.File)
	}
}
