package searchindex

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestLoadServerDocumentsQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to validate the server search projection against PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	documents, err := NewWorker(pool, nil).loadServerDocuments(ctx, []int64{})
	if err != nil {
		t.Fatalf("load empty server search projection: %v", err)
	}
	if len(documents) != 0 {
		t.Fatalf("empty ID selection returned %d server documents", len(documents))
	}
	if _, err = pool.Exec(ctx, `
		create temporary table minecraft_servers (like public.minecraft_servers including all);
		create temporary table minecraft_server_mods (like public.minecraft_server_mods including all);
		create temporary table mods (like public.mods including all);
		create temporary table mod_identifiers (like public.mod_identifiers including all);
		create temporary table public_routes (like public.public_routes including all);
		create temporary table content_popularity_stats (like public.content_popularity_stats including all);
		insert into minecraft_servers(
			id,public_id,slug,address,normalized_address,handshake_host,connect_host,connect_port,
			name,primary_tag,submitted_by,review_status,last_online,created_at,updated_at
		) values(4242,'sperf021a','perf-021','perf.example:25565','perf.example:25565',
			'perf.example','127.0.0.1',25565,'Performance Server','technology',1,'approved',true,
			timestamptz '2025-01-02 03:04:05+00',timestamptz '2025-02-03 04:05:06+00');
		insert into public_routes(id,public_id,entity_type,internal_id,canonical_path)
		values(7171,'rperf021a','minecraft_server',4242,'/servers/sperf021a');
		insert into content_popularity_stats(
			object_route_id,heat_score,download_count,favorite_count,bayesian_rating,rating_count,view_count,comment_count
		) values(7171,12.345678,123,45,4.1250,17,678,9)`); err != nil {
		t.Fatal(err)
	}
	documents, err = NewWorker(pool, nil).loadServerDocuments(ctx, []int64{4242})
	if err != nil {
		t.Fatal(err)
	}
	if len(documents) != 1 {
		t.Fatalf("server documents=%d want 1", len(documents))
	}
	document := documents[0]
	for field, expected := range map[string]any{
		"internal_id": int64(4242),
		"created_at":  int64(1735787045), "updated_at": int64(1738555506),
		"heat_sort_asc": int64(24691356), "heat_sort_desc": int64(24691357),
		"download_count": int64(123), "favorite_count": int64(45), "rating_score": int64(41250),
		"rating_count": int64(17), "view_count": int64(678), "comment_count": int64(9),
	} {
		if document[field] != expected {
			t.Errorf("%s=%#v want %#v", field, document[field], expected)
		}
	}
}
