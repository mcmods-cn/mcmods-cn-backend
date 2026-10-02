package httpapi

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
	"mcmods-cn-backend/internal/security"
)

func TestFavoriteModpackPreviewNamesEverySupportedFavoriteTypeIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify favorite export project names")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
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
		if dropErr := database.DropEphemeralSchema(context.Background(), pool); dropErr != nil {
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	var ownerID, collectionID, modID, modpackID, blueprintID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('bug067-owner','bug067-owner@example.test','not-used','active') returning id`).Scan(&ownerID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into favorite_collections(public_id,user_id,name)
		values('b067names',$1,'BUG-067 names') returning id`, ownerID).Scan(&collectionID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by)
		values('bug067mod','bug067-mod','BUG-067 Mod','approved',$1) returning id`, ownerID).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into modpacks(slug,primary_name,review_status,submitted_by)
		values('bug067-modpack','BUG-067 Modpack','approved',$1) returning id`, ownerID).Scan(&modpackID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,status,review_status)
		values($1,'BUG-067 Blueprint','litematic','ready','approved') returning id`, ownerID).Scan(&blueprintID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into favorite_collection_items(collection_id,entity_type,entity_id) values
		($1,'mod',$2),($1,'modpack',$3),($1,'blueprint',$4)`, collectionID, modID, modpackID, blueprintID); err != nil {
		t.Fatal(err)
	}

	minecraftConfig := defaultMinecraftVersionConfig()
	for index := range minecraftConfig.Loaders {
		if minecraftConfig.Loaders[index].Code == "NeoForge" {
			minecraftConfig.Loaders[index].Versions = []string{"1.21.1"}
		}
	}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, pool, minecraftConfig, []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.21.1", Loader: "neoforge", LoaderVersion: "21.1.100",
		SourceURL: neoForgeMavenMetadataURL, ObservedAt: time.Now().UTC(),
	}}); err != nil {
		t.Fatal(err)
	}
	server := &Server{db: pool, cfg: config.Load()}
	preview, err := server.buildFavoriteModpackExportPreview(ctx, security.Claims{Subject: ownerID}, "b067names",
		favoriteModpackExportRequest{MinecraftVersion: "1.21.1", Loader: "neoforge"})
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]string, len(preview.Items))
	for _, item := range preview.Items {
		names[item.SourceProjectType] = item.SourceProjectName
		if item.SourceProjectType != "mod" && item.ReasonCode != exportReasonNotAMod {
			t.Errorf("%s reason=%q want %s", item.SourceProjectType, item.ReasonCode, exportReasonNotAMod)
		}
	}
	for projectType, want := range map[string]string{
		"mod": "BUG-067 Mod", "modpack": "BUG-067 Modpack", "blueprint": "BUG-067 Blueprint",
	} {
		if got := names[projectType]; got != want {
			t.Errorf("%s export name=%q want %q", projectType, got, want)
		}
	}
}
