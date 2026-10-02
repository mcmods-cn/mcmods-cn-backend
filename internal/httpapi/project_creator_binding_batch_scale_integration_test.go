package httpapi

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestProjectCreatorBindingLookupPlanAtMillionRowsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify million-row creator-binding lookup")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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

	if _, err = pool.Exec(ctx, `create temp table creators(
		id bigint primary key,kind text not null);
		create temp table content_creator_bindings(
			id bigint primary key,subject_type text not null,subject_id bigint not null,creator_id bigint not null,
			role_id bigint,name_snapshot text not null,role_snapshot text not null,permission_granting boolean not null,
			status text not null,display_order integer not null);
		create index idx_content_creator_bindings_subject_order on content_creator_bindings(
			subject_type,subject_id,display_order,id);
		insert into creators select value,'author' from generate_series(1,64) value;
		insert into content_creator_bindings
		select value,'plugin',((value-1)/64)+1,((value-1)%64)+1,1,'Author','Developer',true,'approved',(value-1)%64
		from generate_series(1,1000000) value;
		analyze creators; analyze content_creator_bindings`); err != nil {
		t.Fatal(err)
	}

	const targetSubjectID = int64(15625)
	plan := explainCatalogCardPlan(t, ctx, pool,
		"explain (analyze,buffers,format text) "+projectCreatorBindingsForUpdateSQL, "plugin", targetSubjectID)
	if strings.Contains(plan, "Seq Scan on content_creator_bindings") ||
		!strings.Contains(plan, "idx_content_creator_bindings_subject_order") {
		t.Fatalf("million-row creator-binding lookup did not use its subject index:\n%s", plan)
	}
	rows, err := pool.Query(ctx, projectCreatorBindingsForUpdateSQL, "plugin", targetSubjectID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var binding resolvedProjectCreatorBinding
		if err = rows.Scan(&binding.ID, &binding.CreatorID, &binding.RoleID, &binding.CreatorKind,
			&binding.NameSnapshot, &binding.RoleSnapshot, &binding.PermissionGranting, &binding.Status,
			&binding.DisplayOrder); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		t.Fatal(err)
	}
	rows.Close()
	if count != 64 {
		t.Fatalf("million-row creator-binding lookup returned %d rows, want 64", count)
	}
	t.Logf("million-row creator-binding plan:\n%s", plan)
}
