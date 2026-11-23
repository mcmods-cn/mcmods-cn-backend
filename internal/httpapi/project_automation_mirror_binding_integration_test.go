package httpapi

import (
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProjectMirrorRetainsIndependentDownloadsWhenSourceReboundIntegration(t *testing.T) {
	artifact := test016Archive(t)
	externalFileID := fmt.Sprintf("%x", sha512.Sum512(artifact))
	const source = "mirror-scope-source"
	const replacement = "mirror-scope-replacement"
	const artifactPath = "/files/owned-mirror.jar"
	published := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/" + source:
			test016MirrorJSON(w, map[string]string{"id": source})
		case "/project/" + replacement:
			test016MirrorJSON(w, map[string]string{"id": replacement})
		case "/project/" + source + "/version":
			test016MirrorJSON(w, []map[string]any{{
				"id": "mirror-scope-v1", "project_id": source, "name": "Mirror release", "version_number": "1.0",
				"date_published": published, "version_type": "release", "game_versions": []string{"1.21.1"}, "loaders": []string{"neoforge"},
				"files": []map[string]any{{"filename": "owned-mirror.jar", "url": "https://" + r.Host + artifactPath,
					"size": len(artifact), "primary": true, "hashes": map[string]string{"sha512": externalFileID}}},
			}})
		case artifactPath:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(artifact)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	// The production provider client clones the default transport. Trust only
	// the loopback fixture certificate for this sequential test, as in TEST035.
	originalTransport := http.DefaultTransport
	http.DefaultTransport = upstream.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	f := newTEST016FileFixture(t)
	worker := &ProjectAutomationWorker{server: f.server}
	configureSeededAutomationFixture(t, f.ctx, f.db, "project.edit", "project.no-review", "content.no-review")
	f.modrinthConfig(t, upstream.URL, source)
	if _, err := f.db.Exec(f.ctx, `update mods set license='MIT' where id=any($1)`, []int64{f.modID, f.otherModID}); err != nil {
		t.Fatal(err)
	}

	const restPath = "/api/v1/projects/mod/"
	bind := func(token, projectID, sourceID string) {
		t.Helper()
		f.require(t, token, http.MethodPost, restPath+projectID+"/automation/sources", bindProjectSourceRequest{
			SourceType: "modrinth", URL: "https://modrinth.com/mod/" + sourceID,
		}, http.StatusOK)
	}
	configure := func(token, projectID string) {
		t.Helper()
		f.require(t, token, http.MethodPut, restPath+projectID+"/automation", map[string]any{"items": []map[string]any{
			{"kind": "minecraft_versions", "interval": "never"},
			{"kind": "changelog", "interval": "never"},
			{"kind": "site_downloads", "sourceType": "modrinth", "interval": "week", "enabled": true},
		}}, http.StatusOK)
	}
	run := func(token, projectID string, wantQueued, wantExisting int) {
		t.Helper()
		raw := f.require(t, token, http.MethodPost, restPath+projectID+"/automation/runs", map[string]string{"kind": "site_downloads"}, http.StatusAccepted)
		var submitted struct {
			Data struct{ ID, Status string }
		}
		if err := json.Unmarshal(raw, &submitted); err != nil || submitted.Data.ID == "" || submitted.Data.Status != "pending" {
			t.Fatalf("expected a pending mirror job: %s, err=%v", raw, err)
		}
		if processed, err := worker.processOne(f.ctx); err != nil || !processed {
			t.Fatalf("run project %s mirror job: processed=%t, err=%v", projectID, processed, err)
		}
		var status, detail string
		var queued, existing int
		if err := f.db.QueryRow(f.ctx, `select status,coalesce((result->>'queuedForScan')::int,0),coalesce((result->>'existing')::int,0),last_error
			from project_auto_update_runs where public_id=$1`, submitted.Data.ID).Scan(&status, &queued, &existing, &detail); err != nil {
			t.Fatal(err)
		}
		if status != "completed" || queued != wantQueued || existing != wantExisting {
			t.Fatalf("project %s mirror job status=%s queued=%d existing=%d detail=%q, want completed queued=%d existing=%d", projectID, status, queued, existing, detail, wantQueued, wantExisting)
		}
	}
	publish := func(projectID string) (fileID, objectKey string) {
		t.Helper()
		base := restPath + projectID + "/files"
		f.assertList(t, "", base)
		var ossID string
		if err := f.db.QueryRow(f.ctx, `select oss.public_id,oss.object_key from mirrored_project_files mirror
			join public_routes route on route.id=mirror.project_route_id join oss_files oss on oss.id=mirror.oss_file_id
			where route.public_id=$1 and mirror.source_type='modrinth' and mirror.external_file_id=$2 and mirror.status='scanning'`,
			projectID, externalFileID).Scan(&ossID, &objectKey); err != nil {
			t.Fatal(err)
		}
		f.scan(t, ossID, "clean", http.StatusOK)
		if err := worker.promoteCleanMirrors(f.ctx); err != nil {
			t.Fatal(err)
		}
		if err := f.db.QueryRow(f.ctx, `select file.public_id from project_files file join oss_files oss on oss.id=file.oss_file_id
			where oss.public_id=$1`, ossID).Scan(&fileID); err != nil {
			t.Fatal(err)
		}
		f.assertList(t, "", base, fileID)
		f.assertFile(t, fileID, "active", 1, 1, 0)
		return fileID, objectKey
	}

	bind(f.editor, f.modCode, source)
	configure(f.editor, f.modCode)
	run(f.editor, f.modCode, 1, 0)
	first, firstKey := publish(f.modCode)

	bind(f.editor, f.modCode, replacement)
	bind(f.otherEditor, f.otherModCode, source)
	configure(f.otherEditor, f.otherModCode)
	run(f.otherEditor, f.otherModCode, 1, 0)
	second, secondKey := publish(f.otherModCode)

	run(f.otherEditor, f.otherModCode, 0, 1)
	f.assertList(t, "", restPath+f.modCode+"/files", first)
	f.assertList(t, "", restPath+f.otherModCode+"/files", second)
	objects := f.objects()
	if first == second || !strings.Contains(firstKey, "/automated/"+f.modCode+"/") ||
		!strings.Contains(secondKey, "/automated/"+f.otherModCode+"/") ||
		len(objects) != 2 || objects[firstKey] != sha256Hex(artifact) || objects[secondKey] != sha256Hex(artifact) {
		t.Fatalf("expected distinct project downloads and one retained object per project: files=%q/%q, objects=%v", first, second, objects)
	}
}

func test016MirrorJSON(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(data)
}
