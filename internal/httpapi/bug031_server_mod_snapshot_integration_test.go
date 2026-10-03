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
	"mcmods-cn-backend/internal/serverprobe"
)

func TestBUG031CompleteProbeReplacesOnlyMachineEvidenceIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify complete server-mod snapshot reconciliation")
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
			t.Errorf("drop ephemeral schema: %v", dropErr)
		}
	}()

	suffix := time.Now().UnixNano()
	var actorID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'bug031',true) returning id`, fmt.Sprintf("bug031-%d", suffix),
		fmt.Sprintf("bug031-%d@example.invalid", suffix)).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	var serverID int64
	if err = pool.QueryRow(ctx, `insert into minecraft_servers(
		slug,address,normalized_address,handshake_host,connect_host,connect_port,name,
		minecraft_versions,languages,primary_tag,submitted_by
	) values($1,$2,$2,$2,$2,25565,'BUG031 snapshot test',array['1.21.1'],array['zh-CN'],'survival',$3)
	returning id`, fmt.Sprintf("bug031-%d", suffix), fmt.Sprintf("bug031-%d.invalid", suffix), actorID).Scan(&serverID); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if err = insertMinecraftServerMods(ctx, tx, serverID, []trustedServerModEvidence{
		{ID: "mixed_mod", Version: "declared", Source: "manual", Confidence: "declared"},
		{ID: "mixed_mod", Version: "observed", Source: "configuration", Confidence: "inferred"},
		{ID: "stale_machine_mod", Source: "forge_status", Confidence: "exact"},
		{ID: "current_machine_mod", Version: "old", Source: "forge_status", Confidence: "high"},
		{ID: "declared_only_mod", Source: "manual", Confidence: "declared"},
	}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err = persistMinecraftServerProbe(ctx, pool, serverID, serverprobe.Result{
		Online: true, Modded: true, ModListComplete: true,
		Mods: []serverprobe.Mod{{ID: "current_machine_mod", Version: "new", Source: "agent", Confidence: "exact"}},
	}, nil); err != nil {
		t.Fatal(err)
	}

	assertBUG031Evidence(t, ctx, pool, serverID, "mixed_mod", map[string]string{"manual": "declared"})
	assertBUG031Evidence(t, ctx, pool, serverID, "declared_only_mod", map[string]string{"manual": ""})
	assertBUG031Evidence(t, ctx, pool, serverID, "current_machine_mod", map[string]string{"agent": "new"})
	assertBUG031Evidence(t, ctx, pool, serverID, "stale_machine_mod", nil)

	var staleUnresolved int
	if err = pool.QueryRow(ctx, `select count(*) from unresolved_references unresolved
		where unresolved.source_type='minecraft_server_mod' and unresolved.raw_identifier='stale_machine_mod'`).Scan(&staleUnresolved); err != nil {
		t.Fatal(err)
	}
	if staleUnresolved != 0 {
		t.Fatalf("complete snapshot retained %d unresolved references for a removed machine-only mod", staleUnresolved)
	}

	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if err = insertMinecraftServerMods(ctx, tx, serverID, []trustedServerModEvidence{
		{ID: "incomplete_retained_mod", Source: "configuration", Confidence: "inferred"},
	}); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = persistMinecraftServerProbe(ctx, pool, serverID, serverprobe.Result{
		Online: true, Modded: true, ModListComplete: false,
		Mods: []serverprobe.Mod{{ID: "current_machine_mod", Version: "newer", Source: "agent", Confidence: "exact"}},
	}, nil); err != nil {
		t.Fatal(err)
	}
	assertBUG031Evidence(t, ctx, pool, serverID, "incomplete_retained_mod", map[string]string{"configuration": ""})
}

func assertBUG031Evidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID int64, modID string, want map[string]string) {
	t.Helper()
	rows, err := pool.Query(ctx, `select evidence.source,evidence.version
		from minecraft_server_mods server_mod
		join minecraft_server_mod_evidence evidence on evidence.server_mod_id=server_mod.id
		where server_mod.server_id=$1 and server_mod.raw_mod_id=$2
		order by evidence.source`, serverID, modID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := make(map[string]string)
	for rows.Next() {
		var source, version string
		if err = rows.Scan(&source, &version); err != nil {
			t.Fatal(err)
		}
		got[source] = version
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("mod %q evidence=%v want %v", modID, got, want)
	}
	for source, version := range want {
		if got[source] != version {
			t.Fatalf("mod %q source %q version=%q want %q; all evidence=%v", modID, source, got[source], version, got)
		}
	}
}
