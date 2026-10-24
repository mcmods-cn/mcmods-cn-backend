package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Controlled TLS provider integration of the real importer. The production
// configuration API correctly disallows binding CF credentials to this local
// origin, so this does not assert CF task/Worker or real-provider acceptance.
func TestAuditTP003TP004TP005CurseForgeArchiveHTTPContracts(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		version    int
		projectID  int
		allowed    bool
	}{
		{"valid-quests", "minecraftModpack", 1, 22, true},
		{"wrong-kind", "unrelatedFormat", 1, 22, false},
		{"zero-version", "minecraftModpack", 0, 22, false},
		{"negative-version", "minecraftModpack", -1, 22, false},
		{"future-version", "minecraftModpack", 2, 22, false},
		{"invalid-reference", "minecraftModpack", 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buffer bytes.Buffer
			archive := zip.NewWriter(&buffer)
			entry, err := archive.Create("manifest.json")
			if err != nil {
				t.Fatal(err)
			}
			manifest := map[string]any{"manifestType": tc.kind, "manifestVersion": tc.version, "name": "Quest Pack", "version": "1.0", "minecraft": map[string]any{"version": "1.20.1", "modLoaders": []map[string]any{{"id": "forge-47.4.0", "primary": true}}}, "files": []map[string]any{{"projectID": tc.projectID, "fileID": 33, "required": true}}}
			if err = json.NewEncoder(entry).Encode(manifest); err != nil {
				t.Fatal(err)
			}
			if err = archive.Close(); err != nil {
				t.Fatal(err)
			}
			pages := 0
			var provider *httptest.Server
			provider = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" {
					t.Error("public fixture received credential")
				}
				switch r.URL.Path {
				case "/mods/search":
					_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": 1, "classId": 4471, "slug": "quest-pack", "name": "Quest Pack", "summary": "Synthetic quest pack", "isAvailable": true, "categories": []map[string]string{{"slug": "quests", "name": "Quests"}}, "links": map[string]string{"websiteUrl": "https://www.curseforge.com/minecraft/modpacks/quest-pack"}}}})
				case "/mods/1/description":
					_ = json.NewEncoder(w).Encode(map[string]string{"data": "<p>Synthetic quest progression</p>"})
				case "/mods/1/files":
					pages++
					index, _ := strconv.Atoi(r.URL.Query().Get("index"))
					files := make([]curseForgePackFile, 50)
					for i := range files {
						files[i].ID = int64(index + i + 1)
					}
					if index == 50 {
						files = []curseForgePackFile{{ID: 51, FileName: "quest-pack.zip", DisplayName: "Quest pack 1.0", DownloadURL: provider.URL + "/archive.zip", ReleaseType: 1, FileStatus: 4, IsAvailable: true, FileDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), GameVersions: []string{"1.20.1"}}}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": files, "pagination": map[string]int{"index": index, "resultCount": len(files), "totalCount": 51}})
				case "/archive.zip":
					w.Header().Set("Content-Type", "application/zip")
					_, _ = w.Write(buffer.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()
			original := http.DefaultTransport
			http.DefaultTransport = provider.Client().Transport
			defer func() { http.DefaultTransport = original }()
			cfg := defaultModImportConfig()
			cfg.CurseForge.BaseURL = provider.URL
			cfg.CurseForge.APIKey = ""
			draft, err := importCurseForgeModpack(context.Background(), provider.Client(), cfg, "quest-pack")
			if pages != 2 {
				t.Fatalf("pagination pages=%d want=2", pages)
			}
			if !tc.allowed {
				if err == nil || draft.PrimaryName != "" {
					t.Fatalf("unsupported archive produced a draft: %#v err=%v", draft, err)
				}
				expected := "unsupported CurseForge manifest"
				if tc.name == "invalid-reference" {
					expected = "invalid project or file reference"
				}
				if !strings.Contains(err.Error(), expected) {
					t.Fatalf("wrong rejection: %v want=%s", err, expected)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = normalizeAndValidateModpackImportDraft(&draft); err != nil {
				t.Fatalf("valid quest pack disagrees with consumer: %v", err)
			}
			if draft.PrimaryCategory != "quests" || len(draft.Mods) != 1 || draft.Mods[0].ProviderProjectID != "22" || draft.ImportSelection.VersionID != "51" {
				t.Fatalf("producer/consumer contract mismatch: %#v", draft)
			}
		})
	}
}
