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
}
