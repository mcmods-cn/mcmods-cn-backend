package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
)

type auditTP008Case struct {
	Index       int    `json:"index"`
	ProjectType string `json:"projectType"`
	Provider    string `json:"provider"`
	SampleID    string `json:"sampleId"`
	SourceURL   string `json:"sourceURL"`
	Actor       string `json:"actor"`
	CFClass     int    `json:"-"`
	CFProject   int    `json:"-"`
}

type auditTP008Sources struct {
	MR        *httptest.Server
	CF        *httptest.Server
	Cases     []auditTP008Case
	Transport *auditTP008DeclaredTransport
	mu        sync.Mutex
	Responses []map[string]any
}

func newAuditTP008Sources(t *testing.T, nonce, cfKey string) *auditTP008Sources {
	t.Helper()
	result := &auditTP008Sources{}
	classes := []map[string]any{}
	for n, kind := range []string{"plugin", "map", "resource_pack", "shader_pack", "datapack", "addon"} {
		spec := projectImportSourceSpecs[kind]
		classes = append(classes, map[string]any{"id": 200 + n, "slug": spec.CurseForgeSections[0], "isClass": true})
		for _, provider := range []string{"modrinth", "curseforge"} {
			if provider == "modrinth" && len(spec.ModrinthSections) == 0 {
				continue
			}
			sample := "tp008-" + nonce + "-" + strings.ReplaceAll(kind, "_", "-") + "-" + provider
			source := "https://www.curseforge.com/minecraft/" + spec.CurseForgeSections[0] + "/" + sample
			if provider == "modrinth" {
				source = "https://modrinth.com/" + spec.ModrinthSections[0] + "/" + sample
			}
			index := len(result.Cases)
			actor := "creatorA"
			if index%2 == 1 {
				actor = "creatorB"
			}
			result.Cases = append(result.Cases, auditTP008Case{Index: index, ProjectType: kind, Provider: provider, SampleID: sample, SourceURL: source, Actor: actor, CFClass: 200 + n, CFProject: 90000 + n})
		}
	}
	if len(result.Cases) != 11 {
		t.Fatal("TP008 requires all eleven supported combinations")
	}
	write := func(w http.ResponseWriter, r *http.Request, provider string, payload any) {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Error("cannot encode declared source fixture")
			http.Error(w, "fixture unavailable", 500)
			return
		}
		digest := sha256.Sum256(raw)
		result.mu.Lock()
		result.Responses = append(result.Responses, map[string]any{"provider": provider, "method": r.Method, "path": r.URL.Path, "canonicalQuery": r.URL.Query().Encode(), "bytes": len(raw), "sha256": hex.EncodeToString(digest[:]), "status": 200, "syntheticCredentialPresent": provider == "curseforge"})
		result.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
	result.MR = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("x-api-key") != "" || r.URL.RawQuery != "" {
			http.Error(w, "undeclared public fixture request", 400)
			return
		}
		for _, sample := range result.Cases {
			if sample.Provider != "modrinth" {
				continue
			}
			if r.URL.Path == "/project/"+sample.SampleID+"/version" {
				write(w, r, "modrinth", []any{})
				return
			}
			if r.URL.Path == "/project/"+sample.SampleID {
				kind := projectImportSourceSpecs[sample.ProjectType].ModrinthSections[0]
				write(w, r, "modrinth", map[string]any{"id": sample.SampleID, "slug": sample.SampleID, "title": "TP008 " + sample.SampleID, "description": "Declared controlled public source", "body": "TP008 unchanged source body " + sample.SampleID, "project_type": kind, "status": "approved", "game_versions": []string{"1.20.1"}, "loaders": []string{"paper", "iris", "fabric", "datapack"}, "license": map[string]string{"id": "MIT"}})
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(func() { result.MR.CloseClientConnections(); result.MR.Close() })
	allowed := map[string]bool{}
	origin := "https://api.curseforge.com"
	categoriesQuery := url.Values{"gameId": {"432"}, "classesOnly": {"true"}}.Encode()
	allowed[auditTP008RequestKey(origin, "/v1/categories", categoriesQuery)] = true
	for _, sample := range result.Cases {
		if sample.Provider != "curseforge" {
			continue
		}
		query := url.Values{"gameId": {"432"}, "classId": {strconv.Itoa(sample.CFClass)}, "slug": {sample.SampleID}, "pageSize": {"1"}}.Encode()
		allowed[auditTP008RequestKey(origin, "/v1/mods/search", query)] = true
		allowed[auditTP008RequestKey(origin, fmt.Sprintf("/v1/mods/%d/description", sample.CFProject), "")] = true
	}
	result.CF = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("x-api-key") != cfKey || r.Header.Get("Authorization") != "" {
			http.Error(w, "undeclared synthetic credential request", 403)
			return
		}
		key := auditTP008RequestKey(origin, r.URL.Path, r.URL.Query().Encode())
		if !allowed[key] {
			http.Error(w, "undeclared metadata request", 400)
			return
		}
		if r.URL.Path == "/v1/categories" {
			write(w, r, "curseforge", map[string]any{"data": classes})
			return
		}
		for _, sample := range result.Cases {
			if sample.Provider != "curseforge" {
				continue
			}
			if r.URL.Path == fmt.Sprintf("/v1/mods/%d/description", sample.CFProject) {
				write(w, r, "curseforge", map[string]any{"data": "<p>TP008 unchanged source body " + sample.SampleID + "</p>"})
				return
			}
			if r.URL.Path == "/v1/mods/search" && r.URL.Query().Get("slug") == sample.SampleID {
				write(w, r, "curseforge", map[string]any{"data": []map[string]any{{"id": sample.CFProject, "classId": sample.CFClass, "slug": sample.SampleID, "name": "TP008 " + sample.SampleID, "summary": "Declared controlled public source", "isAvailable": true, "latestFilesIndexes": []map[string]any{{"gameVersion": "1.20.1", "modLoader": 4}}, "links": map[string]string{"websiteUrl": sample.SourceURL}}}})
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(func() { result.CF.CloseClientConnections(); result.CF.Close() })
	result.Transport = newAuditTP008DeclaredTransport(t, result.CF, cfKey, allowed)
	return result
}

func (sources *auditTP008Sources) snapshot() map[string]any {
	sources.mu.Lock()
	responses := append([]map[string]any{}, sources.Responses...)
	sources.mu.Unlock()
	return map[string]any{"responses": responses, "cfTransport": sources.Transport.snapshot(), "mrScope": "uncredentialed owned HTTP through unchanged production provider client, including completed-result visibility GET", "binaryDownloadExercised": false}
}
