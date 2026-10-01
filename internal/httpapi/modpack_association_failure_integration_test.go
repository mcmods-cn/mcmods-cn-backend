package httpapi

import "testing"

func TestModpackAssociationIterationFailureIsReturnedIntegration(t *testing.T) {
	ctx, pool, cfg := isolatedAITestDatabase(t)
	server := &Server{cfg: cfg, db: pool}
	// Replace only a table in this ownership-verified disposable database with a
	// deterministic execution-error view. No core query is mocked.
	if _, err := pool.Exec(ctx, `alter table modpack_loader_compatibilities rename to audit_preserved_compatibilities;
 create function audit_failed_loader() returns text language plpgsql volatile as $$ begin raise exception 'synthetic loader outage'; end $$;
 create view modpack_loader_compatibilities as select 1::bigint modpack_id,audit_failed_loader() loader,'1.21'::text minecraft_version`); err != nil {
		t.Fatal(err)
	}
	if err := server.loadModpackAssociations(ctx, []modpackResponse{{ID: 1}}); err == nil {
		t.Fatal("loader iteration failure was discarded before reading the next association")
	}
}
