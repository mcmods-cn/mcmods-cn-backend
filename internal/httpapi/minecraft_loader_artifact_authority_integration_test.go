package httpapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMinecraftLoaderArtifactSnapshotsAreCatalogBoundAndOfflineReadable(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("MCMODS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to verify loader artifact authority")
		}
		databaseURL = "postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable"
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx := context.Background()
	if _, err = pool.Exec(ctx, `
		create temp table system_settings(
			key text primary key,
			value jsonb not null,
			updated_at timestamptz not null default now()
		) on commit preserve rows;
		create temp table minecraft_loader_artifact_versions(
			catalog_hash text not null,
			minecraft_version text not null,
			loader_type text not null,
			loader_version text not null,
			source_url text not null,
			observed_at timestamptz not null,
			primary key(catalog_hash,minecraft_version,loader_type)
		) on commit preserve rows`); err != nil {
		t.Fatal(err)
	}

	observedAt := time.Date(2026, time.August, 23, 4, 0, 0, 0, time.UTC)
	current := minecraftVersionConfig{
		Versions:       []minecraftVersionOption{{Code: "1.20.1", Type: "release"}},
		CommonVersions: []string{"1.20.1"},
		Loaders:        []minecraftLoaderOption{{Code: "Forge", Name: "Forge", Versions: []string{"1.20.1"}}},
	}
	artifacts := []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.20.1", Loader: "forge", LoaderVersion: "47.4.10",
		SourceURL: forgeMavenMetadataURL, ObservedAt: observedAt,
	}}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, pool, current, artifacts); err != nil {
		t.Fatal(err)
	}

	originalClient := minecraftVersionHTTPClient
	defer func() { minecraftVersionHTTPClient = originalClient }()
	var remoteRequests atomic.Int64
	minecraftVersionHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		remoteRequests.Add(1)
		return nil, errors.New("upstream is offline")
	})}
	version, err := resolveSynchronizedMRPackLoaderVersion(ctx, pool, "1.20.1", "forge")
	if err != nil || version != "47.4.10" {
		t.Fatalf("durable loader version = %q, %v", version, err)
	}
	if remoteRequests.Load() != 0 {
		t.Fatalf("durable loader resolution made %d upstream request(s)", remoteRequests.Load())
	}
	var sourceURL string
	var storedObservedAt time.Time
	if err = pool.QueryRow(ctx, `select source_url,observed_at from minecraft_loader_artifact_versions`).Scan(&sourceURL, &storedObservedAt); err != nil {
		t.Fatal(err)
	}
	if sourceURL != forgeMavenMetadataURL || !storedObservedAt.Equal(observedAt) {
		t.Fatalf("snapshot provenance = %q/%s", sourceURL, storedObservedAt)
	}

	changed := minecraftVersionConfig{
		Versions:       []minecraftVersionOption{{Code: "1.21.1", Type: "release"}},
		CommonVersions: []string{"1.21.1"},
		Loaders:        []minecraftLoaderOption{{Code: "Forge", Name: "Forge", Versions: []string{"1.21.1"}}},
	}
	changedArtifacts := []minecraftLoaderArtifactSnapshot{{
		MinecraftVersion: "1.21.1", Loader: "forge", LoaderVersion: "52.0.1",
		SourceURL: forgeMavenMetadataURL, ObservedAt: observedAt.Add(time.Hour),
	}}
	var beforeFailedPublish string
	if err = pool.QueryRow(ctx, `select value::text from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&beforeFailedPublish); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table minecraft_loader_artifact_versions rename column source_url to arch017_broken_source_url`); err != nil {
		t.Fatal(err)
	}
	if err = saveSynchronizedMinecraftVersionConfig(ctx, pool, changed, changedArtifacts); err == nil {
		t.Fatal("synchronized publish succeeded with a broken artifact table")
	}
	var afterFailedPublish string
	if err = pool.QueryRow(ctx, `select value::text from system_settings where key=$1`, minecraftVersionsSettingKey).Scan(&afterFailedPublish); err != nil {
		t.Fatal(err)
	}
	if afterFailedPublish != beforeFailedPublish {
		t.Fatal("failed artifact publish committed a different Minecraft catalog")
	}
	if _, err = pool.Exec(ctx, `alter table minecraft_loader_artifact_versions rename column arch017_broken_source_url to source_url`); err != nil {
		t.Fatal(err)
	}
	if err = saveMinecraftVersionConfig(ctx, pool, changed); err != nil {
		t.Fatal(err)
	}
	if _, err = resolveSynchronizedMRPackLoaderVersion(ctx, pool, "1.20.1", "forge"); !errors.Is(err, errMinecraftLoaderArtifactUnavailable) {
		t.Fatalf("stale snapshot resolution error = %v", err)
	}
	var snapshotCount int
	if err = pool.QueryRow(ctx, `select count(*) from minecraft_loader_artifact_versions`).Scan(&snapshotCount); err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 0 {
		t.Fatalf("manual catalog change retained %d stale snapshots", snapshotCount)
	}
}
