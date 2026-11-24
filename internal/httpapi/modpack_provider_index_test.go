package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestModrinthPackImportsDeclaredVersionDependenciesAndFileSides(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			var archive bytes.Buffer
			zw := zip.NewWriter(&archive)
			entry, err := zw.Create("modrinth.index.json")
			if err != nil {
				t.Fatal(err)
			}
			_, err = fmt.Fprintf(entry, `{"formatVersion":%d,"game":"minecraft","name":"Example","versionId":"v1",
   "dependencies":{"fabric-loader":"0.16","quilt-loader":"0.1","minecraft":"1.20.1"},"files":[
   {"path":"mods/common.jar","downloads":[]},{"path":"mods/optional.jar","downloads":[],"env":{"server":"optional","client":"unsupported"}},
   {"path":"mods/client.jar","downloads":[],"env":{"client":"required","server":"unsupported"}}]}`, version)
			if err != nil {
				t.Fatal(err)
			}
			if err = zw.Close(); err != nil {
				t.Fatal(err)
			}
			var provider *httptest.Server
			provider = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/project/example":
					fmt.Fprint(w, `{"id":"id","project_type":"modpack","title":"Example","status":"approved"}`)
				case "/project/id/version":
					fmt.Fprintf(w, `[{"id":"ver","version_type":"release","status":"listed","date_published":"2026-01-02T00:00:00Z","files":[{"url":"%s/download","filename":"example.mrpack","primary":true}]}]`, provider.URL)
				case "/download":
					w.Write(archive.Bytes())
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()
			cfg := defaultModImportConfig()
			cfg.Modrinth.BaseURL = provider.URL
			draft, err := importModrinthModpackWithArchiveClientFactory(t.Context(), provider.Client(), cfg, "example",
				func(_ time.Duration, _ string, _ ...string) (*http.Client, error) { return provider.Client(), nil })
			if version != 1 {
				if err == nil || !strings.Contains(err.Error(), "format version") {
					t.Fatalf("unsupported revision produced a draft or the wrong failure: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(draft.Compatibilities) != 2 || draft.Compatibilities[0].Loader != "Fabric" || draft.Compatibilities[1].Loader != "Quilt" || !slices.Equal(draft.Compatibilities[0].Versions, []string{"1.20.1"}) || !slices.Equal(draft.Compatibilities[1].Versions, []string{"1.20.1"}) {
				t.Errorf("index compatibility = %+v", draft.Compatibilities)
			}
			if len(draft.Mods) != 3 {
				t.Fatalf("index produced %d mods", len(draft.Mods))
			}
			if !draft.Mods[0].ClientRequired || !draft.Mods[0].ServerRequired || draft.Mods[1].ClientRequired || draft.Mods[1].ServerRequired || !draft.Mods[2].ClientRequired || draft.Mods[2].ServerRequired {
				t.Errorf("declared sides and absent env were not preserved: %+v", draft.Mods)
			}
		})
	}
}

func TestCurseForgeShortPagesAdvanceByActualResults(t *testing.T) {
	var requested []int
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(r.URL.Query().Get("index"))
		if err != nil {
			http.Error(w, "index", 400)
			return
		}
		requested = append(requested, index)
		if index >= 3 {
			http.Error(w, "index out of range", 400)
			return
		}
		fmt.Fprintf(w, `{"data":[{"id":%d,"fileName":"main.zip","releaseType":1,"fileStatus":4,"isAvailable":true,"fileDate":"2026-01-0%dT00:00:00Z"}],"pagination":{"index":%d,"resultCount":1,"totalCount":3}}`, index+1, index+1, index)
	}))
	defer provider.Close()
	cfg := defaultModImportConfig()
	cfg.CurseForge.BaseURL = provider.URL
	selected, err := loadCurseForgePackFile(context.Background(), provider.Client(), cfg, 10, publicProviderHeaders("test"))
	if err != nil || selected.ID != 3 || !slices.Equal(requested, []int{0, 1, 2}) {
		t.Fatalf("short pages advanced through %v; selected %d: %v", requested, selected.ID, err)
	}
}
