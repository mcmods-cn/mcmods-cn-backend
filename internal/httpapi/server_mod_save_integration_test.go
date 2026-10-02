package httpapi

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestInsertMinecraftServerUnresolvedModIntegration(t *testing.T) {
	databaseURL := os.Getenv("MCMODS_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
			t.Skip("set MCMODS_TEST_DATABASE_URL or MCMODS_RUN_DB_INTEGRATION=1 to run the server mod integration test")
		}
		databaseURL = config.Load().DB.ConnString()
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	suffix := time.Now().UnixNano()
	username := fmt.Sprintf("srvmod%d", suffix)
	email := username + "@example.invalid"
	var actorID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash,email_verified)
		values($1,$2,'test',true) returning id`, username, email).Scan(&actorID); err != nil {
		t.Fatal(err)
	}
	projectCode := fmt.Sprintf("smt%06d", suffix%1_000_000)
	resolvedModID := fmt.Sprintf("resolved_test_mod_%d", suffix)
	resolvedModSlug := fmt.Sprintf("resolved-test-mod-%d", suffix)
	var resolvedModInternalID int64
	if err = tx.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,icon_url,review_status,submitted_by)
		values($1,$2,'Resolved test mod','https://example.invalid/mod.png','approved',$3) returning id`,
		projectCode, resolvedModSlug, actorID).Scan(&resolvedModInternalID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mod_identifiers(mod_id,identifier,is_primary)
		values($1,$2,true)`, resolvedModInternalID, resolvedModID); err != nil {
		t.Fatal(err)
	}
	address := fmt.Sprintf("server-mod-%d.invalid", suffix)
	var serverID int64
	var serverPublicID string
	if err = tx.QueryRow(ctx, `insert into minecraft_servers(
		slug,address,normalized_address,handshake_host,connect_host,connect_port,name,
		minecraft_versions,languages,primary_tag,submitted_by
	) values($1,$2,$2,$2,$2,25565,'Server mod save test',array['1.21.1'],array['zh-CN'],'survival',$3)
	returning id,public_id`, fmt.Sprintf("server-mod-save-%d", suffix), address, actorID).Scan(&serverID, &serverPublicID); err != nil {
		t.Fatal(err)
	}
	unresolvedModIDs := []string{"unrecorded_test_mod", "another_unrecorded_test_mod"}
	if err = insertMinecraftServerMods(ctx, tx, serverID, []trustedServerModEvidence{
		{ID: unresolvedModIDs[0], Source: "configuration", Confidence: "inferred"},
		{ID: unresolvedModIDs[1], Source: "manual", Confidence: "declared"},
		{ID: resolvedModID, Source: "manual", Confidence: "declared"},
	}); err != nil {
		t.Fatal(err)
	}
	var unresolvedCount int
	if err = tx.QueryRow(ctx, `select count(*)
		from unresolved_references unresolved
		join minecraft_server_mods server_mod on server_mod.id=unresolved.source_id
		where unresolved.source_type='minecraft_server_mod' and server_mod.server_id=$1
		  and unresolved.raw_identifier=any($2::text[]) and unresolved.status='pending'
		  and (unresolved.metadata->>'serverId')::bigint=$1`, serverID, unresolvedModIDs).Scan(&unresolvedCount); err != nil {
		t.Fatal(err)
	}
	if unresolvedCount != len(unresolvedModIDs) {
		t.Fatalf("saved %d unresolved server mods, want %d", unresolvedCount, len(unresolvedModIDs))
	}
	serverMods, err := readMinecraftServerMods(ctx, tx, serverPublicID)
	if err != nil {
		t.Fatal(err)
	}
	if len(serverMods) != len(unresolvedModIDs)+1 {
		t.Fatalf("read %d server mods, want %d", len(serverMods), len(unresolvedModIDs)+1)
	}
	seen := make(map[string]bool, len(serverMods))
	for _, mod := range serverMods {
		if mod.ID == resolvedModID {
			if !mod.Resolved || mod.ModPublicID != projectCode || mod.ModID != resolvedModID ||
				mod.ModName != "Resolved test mod" || mod.ModSlug != resolvedModSlug || mod.IconURL == "" {
				t.Fatalf("resolved server mod fields are incorrect: %#v", mod)
			}
			seen[mod.ID] = true
			continue
		}
		if mod.Resolved || mod.ModPublicID != "" || mod.ModID != "" {
			t.Fatalf("unresolved server mod returned resolved fields: %#v", mod)
		}
		if mod.ID == unresolvedModIDs[0] && (mod.Source != "configuration" || mod.Confidence != "inferred") {
			t.Fatalf("trusted probe evidence was not persisted: %#v", mod)
		}
		if mod.ID == unresolvedModIDs[1] && (mod.Source != "manual" || mod.Confidence != "declared") {
			t.Fatalf("manual declaration provenance is incorrect: %#v", mod)
		}
		seen[mod.ID] = true
	}
	for _, modID := range unresolvedModIDs {
		if !seen[modID] {
			t.Fatalf("server detail omitted unresolved mod %q", modID)
		}
	}
	if !seen[resolvedModID] {
		t.Fatalf("server detail omitted resolved mod %q", resolvedModID)
	}

	declarations := []createServerModRequest{
		{ID: unresolvedModIDs[0], Version: "client-version"},
		{ID: unresolvedModIDs[1]},
		{ID: "new_client_declared_mod"},
	}
	if err = replaceMinecraftServerDeclarations(ctx, tx, serverID, declarations); err != nil {
		t.Fatal(err)
	}
	var selectedEvidenceCount int
	if err = tx.QueryRow(ctx, `select count(*) from minecraft_server_mod_evidence evidence
		join minecraft_server_mods server_mod on server_mod.id=evidence.server_mod_id
		where server_mod.server_id=$1 and server_mod.raw_mod_id=$2`, serverID, unresolvedModIDs[0]).Scan(&selectedEvidenceCount); err != nil {
		t.Fatal(err)
	}
	if selectedEvidenceCount != 2 {
		t.Fatalf("selected mod retained %d independent evidence records, want machine and manual", selectedEvidenceCount)
	}
	updated, err := readMinecraftServerMods(ctx, tx, serverPublicID)
	if err != nil {
		t.Fatal(err)
	}
	updatedByID := make(map[string]minecraftServerMod, len(updated))
	for _, mod := range updated {
		updatedByID[mod.ID] = mod
	}
	if mod := updatedByID[unresolvedModIDs[0]]; mod.Source != "configuration" || mod.Confidence != "inferred" || mod.Version != "client-version" {
		t.Fatalf("selected trusted evidence was not preserved across update: %#v", mod)
	}
	for _, modID := range []string{unresolvedModIDs[1], "new_client_declared_mod"} {
		if mod := updatedByID[modID]; mod.Source != "manual" || mod.Confidence != "declared" {
			t.Fatalf("client declaration %q acquired trusted evidence: %#v", modID, mod)
		}
	}
}
