package httpapi

import (
	"context"
	"testing"
	"time"

	"mcmods-cn-backend/internal/security"
)

func TestCommunityPostProjectReferencesRevalidateVisibilityForEveryViewer(t *testing.T) {
	db := openEconomySecurityTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, statement := range []string{
		`create temp table public_routes(
			id bigint primary key,internal_id bigint not null,public_id text not null,entity_type text not null,canonical_path text not null
		) on commit drop`,
		`create temp table mods(
			id bigint primary key,primary_name text not null,review_status text not null,submitted_by bigint not null,
			updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table modpacks(
			id bigint primary key,primary_name text not null,review_status text not null,submitted_by bigint not null,
			updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table simple_projects(
			id bigint primary key,project_type text not null,primary_name text not null,review_status text not null,
			submitted_by bigint not null,updated_at timestamptz not null default now()
		) on commit drop`,
		`create temp table community_post_project_refs(
			id bigint primary key,post_id bigint not null,target_type text not null,target_id bigint,raw_identifier text not null,
			display_order integer not null
		) on commit drop`,
		`insert into public_routes values
			(100,200,'hidden001','mod','/mods/hidden-mod'),
			(101,201,'public001','mod','/mods/public-mod'),
			(102,202,'ownmod001','mod','/mods/own-mod'),
			(103,203,'plugin001','plugin','/plugins/private-plugin')`,
		`insert into mods(id,primary_name,review_status,submitted_by) values
			(200,'Hidden mod','pending',11),(201,'Public mod','approved',11),(202,'Own pending mod','pending',10)`,
		`insert into simple_projects(id,project_type,primary_name,review_status,submitted_by) values
			(203,'plugin','Private plugin','pending',11)`,
		`insert into community_post_project_refs values
			(300,500,'mod',200,'',0),(301,500,'mod',201,'',1),(302,500,'mod',202,'',2),
			(303,500,'plugin',203,'',3),(304,500,'mod',null,'unknown-mod',4)`,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	ownerClaims := security.Claims{Subject: 10}
	server := &Server{db: db}
	snapshot := communityPostSnapshot{Projects: []communityPostReference{{PublicID: "hidden001", Type: "mod"}}}
	if err = server.resolveCommunityPostSnapshot(ctx, tx, ownerClaims, 0, &snapshot); err == nil {
		t.Fatal("community author referenced somebody else's pending Mod")
	}
	snapshot = communityPostSnapshot{Projects: []communityPostReference{{PublicID: "hidden001", Type: "mod"}}}
	if err = server.resolveCommunityPostSnapshot(ctx, tx, security.Claims{Subject: 11}, 0, &snapshot); err != nil {
		t.Fatalf("project owner could not reference their pending Mod: %v", err)
	}

	ownerRefs, err := loadCommunityPostProjectReferencesWithQueryer(ctx, tx, []int64{500}, ownerClaims)
	if err != nil {
		t.Fatal(err)
	}
	assertCommunityProjectReferences(t, ownerRefs[500], map[string]string{
		"public001": "Public mod", "ownmod001": "Own pending mod", "unknown-mod": "unresolved",
	}, 2)
	viewerRefs, err := loadCommunityPostProjectReferencesWithQueryer(ctx, tx, []int64{500}, security.Claims{Subject: 12})
	if err != nil {
		t.Fatal(err)
	}
	assertCommunityProjectReferences(t, viewerRefs[500], map[string]string{
		"public001": "Public mod", "unknown-mod": "unresolved",
	}, 3)

	if _, err = tx.Exec(ctx, `update mods set review_status='approved' where id=200`); err != nil {
		t.Fatal(err)
	}
	viewerRefs, err = loadCommunityPostProjectReferencesWithQueryer(ctx, tx, []int64{500}, security.Claims{Subject: 12})
	if err != nil {
		t.Fatal(err)
	}
	assertCommunityProjectReferences(t, viewerRefs[500], map[string]string{
		"hidden001": "Hidden mod", "public001": "Public mod", "unknown-mod": "unresolved",
	}, 2)
}

func assertCommunityProjectReferences(t *testing.T, refs []communityPostReference, visible map[string]string, unavailable int) {
	t.Helper()
	if len(refs) != 5 {
		t.Fatalf("reference count=%d, want 5: %+v", len(refs), refs)
	}
	actualUnavailable := 0
	for _, ref := range refs {
		if ref.Unavailable {
			actualUnavailable++
			if ref.PublicID != "" || ref.Identifier != "" || ref.Name != "" || ref.SiteID != "" {
				t.Fatalf("unavailable reference leaked metadata: %+v", ref)
			}
			continue
		}
		key := ref.PublicID
		if ref.Unresolved {
			key = ref.Identifier
		}
		if want, ok := visible[key]; !ok || want != "unresolved" && ref.Name != want {
			t.Fatalf("unexpected visible reference %+v; expected=%v", ref, visible)
		}
	}
	if actualUnavailable != unavailable {
		t.Fatalf("unavailable=%d, want %d: %+v", actualUnavailable, unavailable, refs)
	}
}
