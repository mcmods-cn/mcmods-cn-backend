package httpapi

import (
	"net/http"
	"reflect"
	"testing"
)

func TestRoleTrackUpdateRejectsAnEditForTheOldCodeIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "permission.read", "permission.write", "level.write")
	if _, err := f.db.Exec(f.ctx, `insert into roles(code,name) values
		('track_edit_low','Low'),('track_edit_middle','Middle'),('track_edit_high','High')`); err != nil {
		t.Fatal(err)
	}

	basePath := "/api/v1/admin/role-tracks"
	first := roleTrackPayload{Code: "first_track", Name: "First", Roles: []string{"track_edit_low", "track_edit_middle"}}
	other := roleTrackPayload{Code: "other_track", Name: "Other", Roles: []string{"track_edit_middle", "track_edit_high"}}
	f.require(t, f.reviewer, http.MethodPost, basePath, first, http.StatusOK)
	f.require(t, f.reviewer, http.MethodPost, basePath, other, http.StatusOK)

	renamed := first
	renamed.Code = "renamed_track"
	f.require(t, f.reviewer, http.MethodPut, basePath+"/"+first.Code, renamed, http.StatusOK)

	t.Run("stale edit is rejected", func(t *testing.T) {
		stale := first
		stale.Code = other.Code
		f.require(t, f.reviewer, http.MethodPut, basePath+"/"+first.Code, stale, http.StatusNotFound)
	})
	readTrack := func(code string) roleTrackPayload {
		t.Helper()
		raw := f.require(t, f.reviewer, http.MethodGet, basePath, nil, http.StatusOK)
		for _, current := range decodeTEST022Data[[]roleTrackPayload](t, raw) {
			if current.Code == code {
				return current
			}
		}
		t.Fatalf("track %s is missing from the published lineup", code)
		return roleTrackPayload{}
	}
	if got := readTrack(other.Code); !reflect.DeepEqual(got, other) {
		t.Errorf("other track changed after stale edit: got %#v, want %#v", got, other)
	}
	if got := readTrack(renamed.Code); !reflect.DeepEqual(got, renamed) {
		t.Errorf("renamed track changed after stale edit: got %#v, want %#v", got, renamed)
	}

	t.Run("unknown role and unavailable storage roll back the whole edit", func(t *testing.T) {
		edit := other
		edit.Roles = []string{"track_edit_high", "unknown_track_role"}
		f.require(t, f.reviewer, http.MethodPut, basePath+"/"+other.Code, edit, http.StatusBadRequest)
		if _, err := f.db.Exec(f.ctx, `create function refuse_test_track_insert() returns trigger language plpgsql as $$
			begin raise exception 'simulated storage error'; end $$;
			create trigger refuse_test_track_insert before insert on permission_role_track_roles
			for each row execute function refuse_test_track_insert()`); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := f.db.Exec(f.ctx, `drop trigger refuse_test_track_insert on permission_role_track_roles`); err != nil {
				t.Fatal(err)
			}
		}()
		f.require(t, f.reviewer, http.MethodPut, basePath+"/"+other.Code, other, http.StatusInternalServerError)
		if got := readTrack(other.Code); !reflect.DeepEqual(got, other) {
			t.Fatalf("failed edit changed persisted membership: got %v want %v", got.Roles, other.Roles)
		}
	})

	other.Roles = []string{"track_edit_high", "track_edit_low"}
	f.require(t, f.reviewer, http.MethodPut, basePath+"/"+other.Code, other, http.StatusOK)
	if got := readTrack(other.Code); !reflect.DeepEqual(got, other) {
		t.Fatalf("existing track edit was not persisted in order: got %#v, want %#v", got, other)
	}

	levelPath := "/api/v1/admin/levels/config"
	f.require(t, f.reviewer, http.MethodPut, levelPath, levelConfigPayload{
		RoleTrackCode: renamed.Code, LevelThresholds: []int64{10, 20},
	}, http.StatusOK)
	referencedRename := renamed
	referencedRename.Code = "referenced_rename"
	f.require(t, f.reviewer, http.MethodPut, basePath+"/"+renamed.Code, referencedRename, http.StatusConflict)
	if got := readTrack(renamed.Code); !reflect.DeepEqual(got, renamed) {
		t.Fatalf("blocked rename changed referenced roles: got %#v, want %#v", got, renamed)
	}
	renamed.Description = "Current level and manual progression"
	f.require(t, f.reviewer, http.MethodPut, basePath+"/"+renamed.Code, renamed, http.StatusOK)
	reordered := renamed
	reordered.Roles = []string{renamed.Roles[1], renamed.Roles[0]}
	t.Run("level members keep the published order", func(t *testing.T) {
		f.require(t, f.reviewer, http.MethodPut, basePath+"/"+renamed.Code, reordered, http.StatusConflict)
	})
	t.Run("the selected level track stays available", func(t *testing.T) {
		f.require(t, f.reviewer, http.MethodDelete, basePath+"/"+renamed.Code, nil, http.StatusConflict)
	})
	if got := readTrack(renamed.Code); !reflect.DeepEqual(got, renamed) {
		t.Fatalf("active level track changed: got %#v, want %#v", got, renamed)
	}
	// The level update already holds the new track while its config row is
	// busy. A concurrent delete must see that selection after taking the lock.
	barrier, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(f.ctx)
	if _, err = barrier.Exec(f.ctx, "select singleton from level_system_config where singleton for update"); err != nil {
		t.Fatal(err)
	}
	configured := auditCompletionAsyncJSON(f, f.reviewer, http.MethodPut, levelPath, levelConfigPayload{
		RoleTrackCode: other.Code, LevelThresholds: []int64{20, 40},
	})
	auditCompletionAwaitLocks(t, f, 1)
	deleted := auditCompletionAsyncJSON(f, f.reviewer, http.MethodDelete, basePath+"/"+other.Code, nil)
	auditCompletionAwaitLocks(t, f, 2)
	if err = barrier.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if got := <-configured; got.err != nil || got.status != http.StatusOK {
		t.Fatalf("level track switch status=%d err=%v", got.status, got.err)
	}
	if got := <-deleted; got.err != nil || got.status != http.StatusConflict {
		t.Fatalf("deletion of concurrently selected level track status=%d err=%v", got.status, got.err)
	}
	f.require(t, f.reviewer, http.MethodPut, basePath+"/"+renamed.Code, reordered, http.StatusOK)
	f.require(t, f.reviewer, http.MethodDelete, basePath+"/"+renamed.Code, nil, http.StatusOK)

}
