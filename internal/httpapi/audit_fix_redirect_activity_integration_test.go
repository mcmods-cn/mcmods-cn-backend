package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestAuditFixCMP003PreparedDownloadRedirectActivityBeforeCapabilityIntegration(t *testing.T) {
	for _, kind := range []string{"comment", "favorite-export"} {
		t.Run(kind, func(t *testing.T) {
			f := newTEST038Fixture(t)
			raw := []byte("Owned ordinary attachment bytes\n")
			name, contentType := "notes.txt", "text/plain"
			if kind == "favorite-export" {
				built, err := buildMRPack(mrpackBuildInput{Name: "Owned artifact", VersionID: "audit.1", MinecraftVersion: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14", Files: []mrpackFile{{Path: "mods/synthetic.jar", Hashes: map[string]string{"sha1": strings.Repeat("1", 40), "sha512": strings.Repeat("2", 128)}, Env: mrpackEnvironment{Client: "required", Server: "required"}, Downloads: []string{"https://cdn.modrinth.com/data/project/versions/version/synthetic.jar"}, FileSize: 123}}})
				if err != nil {
					t.Fatal(err)
				}
				raw = built.Data
				name = "owned.mrpack"
				contentType = "application/zip"
			}
			var completed struct {
				ID string `json:"id"`
			}
			if kind == "comment" {
				ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", name, contentType, "comment", raw)
				f.put(t, ticket, raw)
				completed = decodeTEST022Data[struct {
					ID string `json:"id"`
				}](t, auditOSSRequire(t, f.test037Fixture, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
				if _, err := f.db.Exec(f.ctx, `update oss_files set scan_status='clean' where public_id=$1`, completed.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				// Generated export artifacts are installed by the business Worker, not by
				// the ordinary user extension allow-list. Seed only that typed cache stage.
				key := "mcmods/favorite_exports/owned.mrpack"
				digest := sha256.Sum256(raw)
				hash := hex.EncodeToString(digest[:])
				f.store.mu.Lock()
				f.store.objects[key] = test036Object{data: raw, hash: hash, contentType: contentType}
				f.store.mu.Unlock()
				if err := f.db.QueryRow(f.ctx, `insert into oss_files(object_key,category,source,original_name,content_type,size_bytes,source_size_bytes,sha256,stored_sha256,source_hash_verified,uploader_id,status,scan_status)
 values($1,'favorite_exports','favorite_modpack_export',$2,$3,$4,$4,$5,$5,true,$6,'active','trusted_generated') returning public_id`, key, name, contentType, len(raw), hash, f.userIDs[f.editor]).Scan(&completed.ID); err != nil {
					t.Fatal(err)
				}
			}
			var path string
			object := int16(activity.ObjectComment)
			if kind == "comment" {
				comment := f.comment(t, f.editor, f.modCode, "download-activity", "Owned attachment source", "", completed.ID)
				path = "/api/v1/comments/" + comment.ID + "/attachments/" + completed.ID + "/download"
			} else {
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "favorite.modpack_export.view_own")
				var task string
				// Explicit typed ready-artifact fixture; this HTTP download test does not
				// claim the real favorite-export producer/worker generated the task row.
				if err := f.db.QueryRow(f.ctx, `insert into favorite_modpack_export_tasks(owner_user_id,collection_public_id_snapshot,pack_name,pack_version_id,minecraft_version,loader_type,loader_version,status,stage,collection_item_count,exported_mod_count,final_file_count,result_file_id,expires_at)
     select $1,'auditcol1','Owned artifact','audit.1','1.21.1','fabric','0.16.14','ready','completed',1,1,1,id,now()+interval '1 hour' from oss_files where public_id=$2 returning public_id`, f.userIDs[f.editor], completed.ID).Scan(&task); err != nil {
					t.Fatal(err)
				}
				path = "/api/v1/users/me/modpack-exports/" + task + "/download"
				object = activity.ObjectUser
			}
			client := &http.Client{Transport: f.origin.Client().Transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			invoke := func(token string, want int) string {
				t.Helper()
				request, err := http.NewRequestWithContext(f.ctx, http.MethodGet, f.origin.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response, err := client.Do(request)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != want {
					t.Fatalf("prepared download status=%d want=%d", response.StatusCode, want)
				}
				location := response.Header.Get("Location")
				if want == 500 {
					if location != "" || !bytes.Contains(body, []byte("failed to persist download activity")) || bytes.Contains(body, []byte(f.cfg.AccessKeyID)) {
						t.Fatal("failed intent disclosed capability or returned unrelated failure")
					}
					return ""
				}
				if want == 307 && (location == "" || response.Header.Get("Cache-Control") != "private, no-store") {
					t.Fatal("missing private successful redirect")
				}
				return location
			}
			facts := func() map[string]string {
				result := f.facts(t)
				for table, value := range auditFixFavoriteActivityFacts(t, f.test013Fixture) {
					result[table] = value
				}
				return result
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], true)
			before := facts()
			invoke(f.editor, 500)
			if !reflect.DeepEqual(before, facts()) {
				t.Fatal("failed disclosure changed business rows/intent")
			}
			auditFixInjectActivityFailure(t, f.test013Fixture, f.userIDs[f.editor], false)
			location := invoke(f.editor, 307)
			if actual := f.get(t, location, 200); !bytes.Equal(actual, raw) {
				t.Fatal("actual signed source bytes changed")
			}
			if got := auditFixActivityIntentCount(t, f.test013Fixture, f.editor, activity.ActionDownload, object); got != 1 {
				t.Fatalf("accepted download intents=%d", got)
			}
			if kind == "comment" {
				// Public anonymous access retains bytes and creates no authenticated event.
				before = facts()
				if actual := f.get(t, invoke("", 307), 200); !bytes.Equal(actual, raw) {
					t.Fatal("anonymous bytes changed")
				}
				if !reflect.DeepEqual(before, facts()) {
					t.Fatal("anonymous disclosure invented activity")
				}
			} else {
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "favorite.modpack_export.view_own")
				before = facts()
				invoke(f.otherEditor, 410)
				if !reflect.DeepEqual(before, facts()) {
					t.Fatal("foreign download changed rows")
				}
			}
		})
	}
}
