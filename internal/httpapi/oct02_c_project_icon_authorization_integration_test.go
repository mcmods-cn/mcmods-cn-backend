package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestOCT02CProjectIconMutationRequiresAuthorizedRasterIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.create.*", "modpack.create")
	cfg := defaultOSSConfig()
	cfg.Enabled, cfg.Region, cfg.Bucket = true, "cn-test", "oct02-c-test"
	cfg.Endpoint, cfg.PublicEndpoint = "https://oct02-storage.invalid", "https://oct02-storage.invalid"
	cfg.AccessKeyID, cfg.AccessKeySecret = "synthetic-test-id", "synthetic-test-secret"
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := security.EncryptSetting("test013-only-settings-key-at-least-32-characters", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Exec(f.ctx, `insert into system_settings(key,value) values('oss.aliyun',$1::jsonb)
		on conflict(key) do update set value=excluded.value`, string(sealed)); err != nil {
		t.Fatal(err)
	}
	for _, file := range []struct {
		key, mime, scan string
		actor           int64
	}{
		{"owner.png", "image/png", "clean", f.userIDs[f.editor]},
		{"private.png", "image/png", "clean", f.userIDs[f.otherEditor]},
		{"owner.txt", "text/plain", "clean", f.userIDs[f.editor]},
		{"unscanned.png", "image/png", "pending", f.userIDs[f.editor]},
	} {
		if _, err = f.db.Exec(f.ctx, `insert into oss_files(object_key,bucket,endpoint,original_name,content_type,size_bytes,uploader_id,status,scan_status)
			values($1,$2,$3,$1,$4,16,$5,'active',$6)`, file.key, cfg.Bucket, cfg.Endpoint, file.mime, file.actor, file.scan); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"plugin", "modpack"} {
		t.Run(kind, func(t *testing.T) {
			for _, key := range []string{"private.png", "owner.txt", "unscanned.png", "missing.png"} {
				siteID := "oct02-icon-" + kind + "-rejected"
				if kind == "plugin" {
					snapshot := test014Snapshot(f, kind, siteID, "Authorized icon", "1.21.1")
					snapshot.IconURL = cfg.Endpoint + "/" + key
					f.require(t, f.editor, http.MethodPost, "/api/v1/content-projects/plugin", snapshot, http.StatusBadRequest)
				} else {
					snapshot := perf020ModpackSnapshot(siteID, 0)
					snapshot.IconURL = cfg.Endpoint + "/" + key
					f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", snapshot, http.StatusBadRequest)
				}
			}
			if kind == "plugin" {
				snapshot := test014Snapshot(f, kind, "oct02-icon-plugin", "Authorized icon", "1.21.1")
				snapshot.IconURL = cfg.Endpoint + "/owner.png?old-signature=discard"
				item := test014Create(t, f, snapshot)
				if item.IconURL != cfg.Endpoint+"/owner.png" {
					t.Fatalf("stored icon retained unstable query: %s", item.IconURL)
				}
				test014Approve(t, f, item.SubmissionRevisionID, f.editor)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+item.PublicID)
				snapshot.IconURL = cfg.Endpoint + "/private.png"
				f.require(t, f.editor, http.MethodPut, "/api/v1/content-projects/plugin/"+item.SiteID, map[string]any{"snapshot": snapshot}, http.StatusBadRequest)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+item.PublicID)
				snapshot.IconURL = cfg.Endpoint + "/owner.png"
				f.require(t, f.otherEditor, http.MethodPut, "/api/v1/content-projects/plugin/"+item.SiteID, map[string]any{"snapshot": snapshot}, http.StatusCreated)
			} else {
				snapshot := perf020ModpackSnapshot("oct02-icon-modpack", 0)
				snapshot.IconURL = cfg.Endpoint + "/owner.png"
				item := decodeTEST022Data[modpackResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/modpacks", snapshot, http.StatusCreated))
				f.require(t, f.reviewer, http.MethodPatch, "/api/v1/content-revisions/"+item.SubmissionRevisionID,
					map[string]any{"status": "approved", "note": "owned icon publication"}, http.StatusOK)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "project.edit."+item.PublicID)
				snapshot.IconURL = cfg.Endpoint + "/private.png"
				f.require(t, f.editor, http.MethodPut, "/api/v1/modpacks/"+item.SiteID, map[string]any{"snapshot": snapshot}, http.StatusBadRequest)
				grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "project.edit."+item.PublicID)
				snapshot.IconURL = cfg.Endpoint + "/owner.png"
				f.require(t, f.otherEditor, http.MethodPut, "/api/v1/modpacks/"+item.SiteID, map[string]any{"snapshot": snapshot}, http.StatusCreated)
			}
		})
	}
}
