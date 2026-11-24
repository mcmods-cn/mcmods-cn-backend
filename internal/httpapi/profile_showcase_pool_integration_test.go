package httpapi

import (
	"context"
	"maps"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestShowcaseQueriesReleaseSingleConnectionBeforeResolvingImagesIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 with an owned test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := database.DropEphemeralSchema(context.Background(), pool); err != nil {
			t.Errorf("drop test schema: %v", err)
		}
	}()
	server := &Server{db: pool}
	for _, test := range []struct {
		name string
		load func(context.Context, int64) ([]userShowcaseItem, error)
	}{
		{"claimed authors", server.loadUserClaimedAuthors},
		{"projects", server.loadUserShowcaseProjects},
		{"uploads", server.loadUserShowcaseUploads},
	} {
		t.Run(test.name, func(t *testing.T) {
			queryCtx, queryCancel := context.WithTimeout(ctx, 2*time.Second)
			defer queryCancel()
			if _, err := test.load(queryCtx, 123456); err != nil {
				t.Fatalf("single-connection showcase failed: %v", err)
			}
			if err := queryCtx.Err(); err != nil {
				t.Fatalf("image resolution exhausted its context while holding the only connection: %v", err)
			}
		})
	}
}

func TestShowcaseImagesUsePublicMediaRoutesIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	owner := f.userIDs[f.editor]
	var profile, blueprint, skin string
	if err := f.db.QueryRow(f.ctx, "select public_id from users where id=$1", owner).Scan(&profile); err != nil {
		t.Fatal(err)
	}
	const coverKey = "blueprints/example/cover/generated.png"
	const textureKey = "skins/stored/texture-object.png"
	var coverFile, textureFile int64
	for _, asset := range []struct {
		key string
		id  *int64
	}{{coverKey, &coverFile}, {textureKey, &textureFile}} {
		if err := f.db.QueryRow(f.ctx, `insert into oss_files(bucket,endpoint,object_key,source,status,scan_status,content_type)
			values('example','https://storage.example.invalid',$1,'user','active','trusted_generated','image/png') returning id`, asset.key).Scan(asset.id); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,source_format,status,review_status,cover_file_id,cover_object_key,updated_at)
		values($1,'nbt','ready','approved',$2,$3,to_timestamp(1730000000)) returning public_id`, owner, coverFile, coverKey).Scan(&blueprint); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("1a", 32)
	if _, err := f.db.Exec(f.ctx, `insert into skin_texture_blobs(hash,oss_file_id,object_key,width,height,size_bytes) values($1,$2,$3,64,64,128)`, hash, textureFile, textureKey); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into skin_assets(owner_id,blob_hash,kind,display_name,visibility,review_status) values($1,$2,'skin','Example','public','approved') returning public_id`, owner, hash).Scan(&skin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into project_editor_assignments(target_route_id,user_id)
		select id,$1 from public_routes where public_id in ($2,$3)`, owner, blueprint, skin); err != nil {
		t.Fatal(err)
	}
	type showcase struct {
		EditorProjects []userShowcaseItem
		Uploads        []userShowcaseItem
	}
	read := func() showcase {
		return decodeTEST022Data[showcase](t, f.require(t, "", http.MethodGet, "/api/v1/users/"+profile+"/showcase", nil, http.StatusOK))
	}
	wanted := map[string]string{blueprint: "/api/v1/blueprints/" + blueprint + "/cover?v=1730000000", skin: "/api/yggdrasil/textures/" + hash}
	visible := read()
	for _, items := range [][]userShowcaseItem{visible.EditorProjects, visible.Uploads} {
		seen := make(map[string]string)
		for _, item := range items {
			if _, ok := wanted[item.PublicID]; ok {
				seen[item.PublicID] = item.IconURL
			}
		}
		if !maps.Equal(seen, wanted) {
			t.Errorf("showcase media=%v want=%v", seen, wanted)
		}
	}
	if _, err := f.db.Exec(f.ctx, `update skin_assets set visibility='unlisted' where public_id=$1`, skin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `update blueprints set review_status='pending' where public_id=$1`, blueprint); err != nil {
		t.Fatal(err)
	}
	hidden := read()
	for _, items := range [][]userShowcaseItem{hidden.EditorProjects, hidden.Uploads} {
		for _, item := range items {
			if _, ok := wanted[item.PublicID]; ok {
				t.Fatal("nonpublic asset in showcase")
			}
		}
	}
}

func TestFavoriteBlueprintCoverUsesItsGuardedMediaRouteIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	owner := f.userIDs[f.editor]
	const key = "blueprints/example/cover/generated.png"
	var ownerPublicID, blueprint, collection string
	var fileID, blueprintID, collectionID int64
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, owner).Scan(&ownerPublicID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(bucket,endpoint,object_key,source,status,scan_status,content_type)
		values('test','https://example.invalid',$1,'user','active','clean','image/png') returning id`, key).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into blueprints(owner_id,source_format,status,review_status,cover_file_id,cover_object_key,updated_at)
		values($1,'nbt','ready','approved',$2,$3,to_timestamp(1730000000)) returning id,public_id`, owner, fileID, key).Scan(&blueprintID, &blueprint); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into favorite_collections(user_id,name,is_public) values($1,'public examples',true) returning id,public_id`, owner).Scan(&collectionID, &collection); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id) values($1,'blueprint',$2)`, collectionID, blueprintID); err != nil {
		t.Fatal(err)
	}
	const prefix = "/api/v1/users/"
	for _, view := range []struct{ viewer, route string }{{f.editor, "me"}, {"", ownerPublicID}} {
		response := f.require(t, view.viewer, "GET", prefix+view.route+"/favorite-collections/"+collection+"/items", nil, 200)
		page := decodeTEST022Data[favoriteCollectionItemPageResponse](t, response)
		if len(page.Items) != 1 || page.Items[0].EntityPublicID != blueprint || page.Items[0].Metadata["iconUrl"] != "/api/v1/blueprints/"+blueprint+"/cover?v=1730000000" {
			t.Errorf("favorite blueprint cover: %s", response)
		}
	}
}
