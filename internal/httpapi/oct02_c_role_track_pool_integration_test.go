package httpapi

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestOCT02CRoleTrackListWithSingleConnectionIntegration(t *testing.T) {
	db := openGlobalCatalogTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `create temp table permission_role_tracks(code text primary key,name text,description text);
		create temp table roles(id bigint primary key,code text);
		create temp table permission_role_track_roles(track_code text,role_id bigint,position integer);
		insert into permission_role_tracks values('a','A','First'),('b','B','Second'),('empty','Empty','No roles');
		insert into roles values(1,'low'),(2,'high');
		insert into permission_role_track_roles values('a',2,1),('a',1,0),('b',2,0)`); err != nil {
		t.Fatal(err)
	}
	if db.Config().MaxConns != 1 {
		t.Fatal("regression requires a single-connection test pool")
	}
	server := &Server{db: db}
	got, err := server.loadRoleTracks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []roleTrackPayload{
		{Code: "a", Name: "A", Description: "First", Roles: []string{"low", "high"}},
		{Code: "b", Name: "B", Description: "Second", Roles: []string{"high"}},
		{Code: "empty", Name: "Empty", Description: "No roles", Roles: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("track order, role order or empty track changed: got=%#v want=%#v", got, want)
	}
}
