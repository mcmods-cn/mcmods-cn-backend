package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02ProfileAndEditorLookupOutagesAreRetryable(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://test:test@127.0.0.1:1/test?sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	cache := querycache.New(config.RedisConfig{})
	defer cache.Close()
	server := &Server{db: pool, cache: cache}
	for name, handler := range map[string]http.HandlerFunc{"avatar": server.updateUserProfileSettings, "editor_revoke": server.revokeProjectEditorAssignment, "log_batch": server.createFileLogShares} {
		t.Run(name, func(t *testing.T) {
			body := `{"avatarFileId":"file00001"}`
			want := http.StatusServiceUnavailable
			if name == "log_batch" {
				body, want = `{"fileIds":["file00001"]}`, http.StatusMultiStatus
			}
			r := httptest.NewRequest(http.MethodPut, "/api/v1/test", strings.NewReader(body))
			r.SetPathValue("projectType", "mod")
			r.SetPathValue("projectId", "mod000001")
			r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, security.Claims{Subject: 1, PermissionRules: []security.PermissionRule{{Code: "user.avatar.update", Allow: true}}}))
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != want {
				t.Fatalf("lookup outage status=%d want%d", w.Code, want)
			}
			if strings.Contains(w.Body.String(), "closed") || strings.Contains(w.Body.String(), "postgres") {
				t.Fatal("internal connection details leaked")
			}
		})
	}
}

func TestProfileRelationshipsReflectViewerAndStorageAvailabilityIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	var target string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, f.userIDs[f.editor]).Scan(&target); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/users/" + target + "/profile"
	if _, err := f.db.Exec(f.ctx, `insert into user_blocks(blocker_id,blocked_id) values($1,$2)`, f.userIDs[f.editor], f.userIDs[f.otherEditor]); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_follows(follower_id,followed_id) values($1,$2)`, f.userIDs[f.reviewer], f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	type profile struct {
		Followers, Blocked                            int
		IsFollowing, IsBlocked, CanFollow, CanMessage bool
	}
	for _, view := range []struct {
		actor   string
		blocked int
		follows bool
	}{
		{f.editor, 1, false}, {f.otherEditor, 0, false}, {f.reviewer, 0, true}, {"", 0, false},
	} {
		p := decodeTEST022Data[profile](t, f.require(t, view.actor, http.MethodGet, path, nil, http.StatusOK))
		if p.Followers != 1 || p.Blocked != view.blocked || p.IsFollowing != view.follows || p.IsBlocked || view.actor == f.otherEditor && (p.CanFollow || p.CanMessage) {
			t.Fatalf("profile relationships = %+v; expected followers=1, blocked=%d, following=%t", p, view.blocked, view.follows)
		}
	}
	if _, err := f.db.Exec(f.ctx, `alter table user_follows rename to unavailable_profile_follows`); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, path, nil, http.StatusServiceUnavailable)
	if _, err := f.db.Exec(f.ctx, `alter table unavailable_profile_follows rename to user_follows`); err != nil {
		t.Fatal(err)
	}
	f.require(t, "", http.MethodGet, path, nil, http.StatusOK)

	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "user.follow.create")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "user.follow.receive")
	if _, err := f.db.Exec(f.ctx, `update users set status='deleted' where id=$1`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.reviewer, http.MethodPost, "/api/v1/users/"+target+"/follow", nil, http.StatusNotFound)
}

func TestProfileSettingsReadAndCommittedRefreshFailuresIntegration(t *testing.T) {
	t.Run("permission projection", func(t *testing.T) {
		f := newTEST013Fixture(t)
		actor := f.userIDs[f.editor]
		grantTEST044Permissions(t, f.ctx, f.db, actor, "user.message.receive", "user.avatar.update")
		// Exercise the settings handler after authentication, with no cached roles.
		cache := querycache.New(config.RedisConfig{})
		defer cache.Close()
		server := &Server{db: f.db, cache: cache}
		read := func() *httptest.ResponseRecorder {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/users/me/profile-settings", nil)
			req = req.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: actor}))
			w := httptest.NewRecorder()
			server.getUserProfileSettings(w, req)
			return w
		}
		before := read()
		settings := decodeTEST022Data[userProfileSettingsResponse](t, before.Body.Bytes())
		if before.Code != http.StatusOK || !settings.MessageReceive || !settings.CanUpdateAvatar {
			t.Fatal("profile omitted current capabilities")
		}
		if _, err := f.db.Exec(f.ctx, "alter table user_role_bindings rename to unavailable_profile_bindings"); err != nil {
			t.Fatal(err)
		}
		if got := read(); got.Code != http.StatusServiceUnavailable {
			t.Errorf("unreadable profile permissions returned HTTP %d", got.Code)
		}
		if _, err := f.db.Exec(f.ctx, "alter table unavailable_profile_bindings rename to user_role_bindings"); err != nil {
			t.Fatal(err)
		}
		after := read()
		settings = decodeTEST022Data[userProfileSettingsResponse](t, after.Body.Bytes())
		if after.Code != http.StatusOK || !settings.MessageReceive || !settings.CanUpdateAvatar {
			t.Fatal("profile failed to recover its capabilities")
		}
	})
	t.Run("committed preference", func(t *testing.T) {
		f := newTEST013Fixture(t)
		var initial bool
		actor := f.userIDs[f.editor]
		if err := f.db.QueryRow(f.ctx, "select show_online_status from users where id=$1", actor).Scan(&initial); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(f.ctx, `insert into system_settings(key,value) values('profile.config','{"signatureMaxBytes":"temporarily unavailable"}') on conflict(key) do update set value=excluded.value`); err != nil {
			t.Fatal(err)
		}
		path := "/api/v1/users/me/profile-settings"
		raw := f.require(t, f.editor, http.MethodPut, path, map[string]bool{"showOnlineStatus": !initial}, http.StatusServiceUnavailable)
		var problem struct {
			Code    string
			Details struct{ Committed bool }
		}
		if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "PROFILE_SETTINGS_REFRESH_FAILED" || !problem.Details.Committed {
			t.Fatal("failure did not distinguish committed change from unavailable read")
		}
		var saved bool
		var revision int64
		if err := f.db.QueryRow(f.ctx, "select show_online_status,profile_revision_id from users where id=$1", actor).Scan(&saved, &revision); err != nil || saved == initial || revision <= 0 {
			t.Fatalf("saved %t revision %d err %v", saved, revision, err)
		}
		if _, err := f.db.Exec(f.ctx, `update system_settings set value='{"signatureMaxBytes":256}' where key='profile.config'`); err != nil {
			t.Fatal(err)
		}
		f.origin.Config.Handler.(*Server).invalidateSettingsCache(f.ctx)
		profile := decodeTEST022Data[userProfileSettingsResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, http.StatusOK))
		if profile.ShowOnlineStatus != saved {
			t.Fatal("reloading did not return the committed change")
		}
	})
}
