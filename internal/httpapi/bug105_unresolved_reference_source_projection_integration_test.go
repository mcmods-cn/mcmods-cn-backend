package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestUnresolvedReferenceCatalogProjectsEveryProducerSourceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify unresolved-reference source projection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = database.InstallEphemeralSchema(ctx, pool); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop BUG-105 ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var actorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values($1,$2,'not-used','active') returning id`, fmt.Sprintf("bug105_admin_%d", suffix), fmt.Sprintf("bug105_admin_%d@example.test", suffix)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}

	var firstServerID, secondServerID int64
	var firstServerPublicID, secondServerPublicID string
	if err = pool.QueryRow(ctx, `insert into minecraft_servers(
		slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,submitted_by)
		values($1,'one.example.test','one.example.test:25565','one.example.test','one.example.test',25565,'First server','survival',$2)
		returning id,public_id`, fmt.Sprintf("bug105-server-one-%d", suffix), actorID).Scan(&firstServerID, &firstServerPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into minecraft_servers(
		slug,address,normalized_address,handshake_host,connect_host,connect_port,name,primary_tag,submitted_by)
		values($1,'two.example.test','two.example.test:25565','two.example.test','two.example.test',25565,'Second server','survival',$2)
		returning id,public_id`, fmt.Sprintf("bug105-server-two-%d", suffix), actorID).Scan(&secondServerID, &secondServerPublicID); err != nil {
		t.Fatal(err)
	}
	var serverModID int64
	if err = pool.QueryRow(ctx, `insert into minecraft_server_mods(server_id,raw_mod_id)
		values($1,'missing-server-mod') returning id`, firstServerID).Scan(&serverModID); err != nil {
		t.Fatal(err)
	}

	var firstProjectID, secondProjectID int64
	var firstProjectPublicID, secondProjectPublicID string
	if err = pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,submitted_by)
		values('plugin',$1,'First plugin',$2) returning id,public_id`, fmt.Sprintf("bug105-plugin-one-%d", suffix), actorID).Scan(&firstProjectID, &firstProjectPublicID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,submitted_by)
		values('plugin',$1,'Second plugin',$2) returning id,public_id`, fmt.Sprintf("bug105-plugin-two-%d", suffix), actorID).Scan(&secondProjectID, &secondProjectPublicID); err != nil {
		t.Fatal(err)
	}
	var parentReferenceID int64
	if err = pool.QueryRow(ctx, `insert into simple_project_parent_refs(project_id,target_type,raw_identifier)
		values($1,'mod','missing-parent') returning id`, firstProjectID).Scan(&parentReferenceID); err != nil {
		t.Fatal(err)
	}

	var catalogEntityID int64
	var catalogEntityPublicID string
	if err = pool.QueryRow(ctx, `insert into catalog_entities(identity_key,entity_type)
		values('minecraft:bug105_source','resource') returning id,public_id`).Scan(&catalogEntityID, &catalogEntityPublicID); err != nil {
		t.Fatal(err)
	}

	modProjectCode := fmt.Sprintf("b%08x", uint32(suffix))
	var modID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,submitted_by)
		values($1,$2,'Source mod',$3) returning id`, modProjectCode, fmt.Sprintf("bug105-mod-%d", suffix), actorID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	var relationshipID int64
	if err = pool.QueryRow(ctx, `insert into mod_relationships(mod_id,relation_type,related_mod_identifier)
		values($1,'dependency','missing-related-mod') returning id`, modID).Scan(&relationshipID); err != nil {
		t.Fatal(err)
	}

	var communityPostID int64
	var communityPostPublicID string
	if err = pool.QueryRow(ctx, `insert into community_posts(kind,category,author_id,title,source_locale)
		values('discussion','general',$1,'Source discussion','en-US') returning id,public_id`, actorID).Scan(&communityPostID, &communityPostPublicID); err != nil {
		t.Fatal(err)
	}
	var communityProjectReferenceID, communityResourceReferenceID int64
	if err = pool.QueryRow(ctx, `insert into community_post_project_refs(post_id,target_type,raw_identifier)
		values($1,'mod','missing-community-project') returning id`, communityPostID).Scan(&communityProjectReferenceID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into community_post_resource_refs(post_id,kind_code,raw_resource_id)
		values($1,'minecraft.item','minecraft:missing_community_item') returning id`, communityPostID).Scan(&communityResourceReferenceID); err != nil {
		t.Fatal(err)
	}

	var modpackID int64
	var modpackPublicID string
	if err = pool.QueryRow(ctx, `insert into modpacks(slug,primary_name,submitted_by)
		values($1,'Source modpack',$2) returning id,public_id`, fmt.Sprintf("bug105-modpack-%d", suffix), actorID).Scan(&modpackID, &modpackPublicID); err != nil {
		t.Fatal(err)
	}
	var modpackEntryID int64
	if err = pool.QueryRow(ctx, `insert into modpack_mods(modpack_id,identifier)
		values($1,'missing-modpack-mod') returning id`, modpackID).Scan(&modpackEntryID); err != nil {
		t.Fatal(err)
	}

	for _, reference := range []struct {
		sourceType, fieldPath, referenceType, rawIdentifier string
		sourceID                                            int64
	}{
		{"minecraft_server_mod", "mods", "mod", "missing-server-mod", serverModID},
		{"simple_project_parent", "parentProjects", "mod", "missing-parent", parentReferenceID},
		{"mod_content_resource", "definition.tag", "tag", "#missing-tag", catalogEntityID},
		{"mod_relationship", "relatedMod", "mod", "missing-related-mod", relationshipID},
		{"community_post_project", "projects", "mod", "missing-community-project", communityProjectReferenceID},
		{"community_post_resource", "resources", "minecraft.item", "minecraft:missing_community_item", communityResourceReferenceID},
		{"modpack_mod", "mods", "mod", "missing-modpack-mod", modpackEntryID},
	} {
		if _, err = pool.Exec(ctx, `insert into unresolved_references(
			source_type,source_id,field_path,reference_type,raw_identifier,normalized_identifier)
			values($1,$2,$3,$4,$5,lower($5))`, reference.sourceType, reference.sourceID, reference.fieldPath, reference.referenceType, reference.rawIdentifier); err != nil {
			t.Fatal(err)
		}
	}

	assertBUG105ProjectedSource(t, ctx, pool, "minecraft_server_mod", serverModID, "First server", firstServerPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "simple_project_parent", parentReferenceID, "First plugin", firstProjectPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "mod_content_resource", catalogEntityID, "minecraft:bug105_source", catalogEntityPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "mod_relationship", relationshipID, "Source mod", modProjectCode)
	assertBUG105ProjectedSource(t, ctx, pool, "community_post_project", communityProjectReferenceID, "Source discussion", communityPostPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "community_post_resource", communityResourceReferenceID, "Source discussion", communityPostPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "modpack_mod", modpackEntryID, "Source modpack", modpackPublicID)

	for _, update := range []struct {
		query string
		id    int64
	}{
		{"update minecraft_servers set name='Renamed server' where id=$1", firstServerID},
		{"update simple_projects set primary_name='Renamed plugin' where id=$1", firstProjectID},
		{"update catalog_entities set identity_key='minecraft:bug105_renamed' where id=$1", catalogEntityID},
		{"update mods set primary_name='Renamed mod' where id=$1", modID},
		{"update community_posts set title='Renamed discussion' where id=$1", communityPostID},
		{"update modpacks set primary_name='Renamed modpack' where id=$1", modpackID},
	} {
		if _, err = pool.Exec(ctx, update.query, update.id); err != nil {
			t.Fatal(err)
		}
	}
	assertBUG105ProjectedSource(t, ctx, pool, "minecraft_server_mod", serverModID, "Renamed server", firstServerPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "simple_project_parent", parentReferenceID, "Renamed plugin", firstProjectPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "mod_content_resource", catalogEntityID, "minecraft:bug105_renamed", catalogEntityPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "mod_relationship", relationshipID, "Renamed mod", modProjectCode)
	assertBUG105ProjectedSource(t, ctx, pool, "community_post_project", communityProjectReferenceID, "Renamed discussion", communityPostPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "community_post_resource", communityResourceReferenceID, "Renamed discussion", communityPostPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "modpack_mod", modpackEntryID, "Renamed modpack", modpackPublicID)

	if _, err = pool.Exec(ctx, `update minecraft_server_mods set server_id=$1 where id=$2`, secondServerID, serverModID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update simple_project_parent_refs set project_id=$1 where id=$2`, secondProjectID, parentReferenceID); err != nil {
		t.Fatal(err)
	}
	assertBUG105ProjectedSource(t, ctx, pool, "minecraft_server_mod", serverModID, "Second server", secondServerPublicID)
	assertBUG105ProjectedSource(t, ctx, pool, "simple_project_parent", parentReferenceID, "Second plugin", secondProjectPublicID)
}

func assertBUG105ProjectedSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sourceType string, sourceID int64, expectedLabel, expectedPublicID string) {
	t.Helper()
	var label, publicID string
	if err := pool.QueryRow(ctx, `select source_label,source_public_id from unresolved_reference_catalog
		where origin=0 and source_type=$1 and source_id=$2`, sourceType, sourceID).Scan(&label, &publicID); err != nil {
		t.Fatal(err)
	}
	if label != expectedLabel || publicID != expectedPublicID {
		t.Fatalf("%s source=(%q,%q), want (%q,%q)", sourceType, label, publicID, expectedLabel, expectedPublicID)
	}
}
