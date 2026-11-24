package httpapi

import (
	"context"
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
