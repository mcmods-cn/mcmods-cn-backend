package httpapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

func TestBlueprintJobAdmissionAndBulkMaterialsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify blueprint processing budgets")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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

	var userID, blueprintID int64
	if err = pool.QueryRow(ctx, `insert into users(username,email,password_hash,status)
		values('perf042-owner','perf042-owner@example.test','not-used','active') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into blueprints(owner_id,title,source_format,status)
		values($1,'PERF-042','schem','processing') returning id`, userID).Scan(&blueprintID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into blueprint_jobs(blueprint_id,operation,target_format,status,created_by)
		select $1,'convert','format-'||value,'queued',$2 from generate_series(1,$3) value`,
		blueprintID, userID, maxBlueprintActiveJobsPerUser); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if reserveErr := reserveBlueprintUserJobBudgetTx(ctx, tx, userID); !errors.Is(reserveErr, errBlueprintUserJobLimit) {
		_ = tx.Rollback(ctx)
		t.Fatalf("fifth active job reservation error = %v", reserveErr)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `update blueprint_jobs set status='completed' where id=(select min(id) from blueprint_jobs)`); err != nil {
		t.Fatal(err)
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	if err = reserveBlueprintUserJobBudgetTx(ctx, tx, userID); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("reservation after one completion: %v", err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	materials := make([]blueprintMaterial, maxBlueprintMaterialCount)
	for index := range materials {
		materials[index] = blueprintMaterial{
			State: fmt.Sprintf("example:block_%05d[axis=x]", index), BlockID: fmt.Sprintf("example:block_%05d", index),
			Properties: map[string]string{"axis": "x"}, Count: int64(index + 1),
		}
	}
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollbackIntegrationTransaction(tx)
	started := time.Now()
	if err = replaceBlueprintMaterialsTx(ctx, tx, blueprintID, materials); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	duration := time.Since(started)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = pool.QueryRow(ctx, `select count(*) from blueprint_materials where blueprint_id=$1`, blueprintID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != maxBlueprintMaterialCount || duration > 2*time.Second {
		t.Fatalf("bulk materials = %d rows in %s, want %d rows under 2s", count, duration, maxBlueprintMaterialCount)
	}
	t.Logf("bulk persisted %d materials in %s", count, duration)
}

func TestBlueprintJobAdmissionStaysIndexedAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify blueprint job admission at scale")
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
	if _, err = pool.Exec(ctx, `create temporary table blueprint_jobs(
		id bigint primary key,created_by bigint,status text not null);
	create index idx_blueprint_jobs_creator_active on blueprint_jobs(created_by,id)
		where status in ('queued','processing') and created_by is not null`); err != nil {
		t.Fatal(err)
	}
	checkPlan := func(scale int) {
		t.Helper()
		if _, execErr := pool.Exec(ctx, `analyze blueprint_jobs`); execErr != nil {
			t.Fatal(execErr)
		}
		started := time.Now()
		rows, queryErr := pool.Query(ctx, "explain (analyze,buffers,format text) "+blueprintActiveJobCountSQL,
			int64(42), maxBlueprintActiveJobsPerUser)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		var plan strings.Builder
		for rows.Next() {
			var line string
			if queryErr = rows.Scan(&line); queryErr != nil {
				rows.Close()
				t.Fatal(queryErr)
			}
			plan.WriteString(line)
			plan.WriteByte('\n')
		}
		if queryErr = rows.Err(); queryErr != nil {
			rows.Close()
			t.Fatal(queryErr)
		}
		rows.Close()
		duration := time.Since(started)
		planText := plan.String()
		if duration > 2*time.Second || strings.Contains(planText, "Seq Scan on blueprint_jobs") ||
			!strings.Contains(planText, "idx_blueprint_jobs_creator_active") {
			t.Fatalf("%d-job admission took %s or missed its index:\n%s", scale, duration, planText)
		}
		t.Logf("%d-job admission: %s\n%s", scale, duration, planText)
	}
	if _, err = pool.Exec(ctx, `insert into blueprint_jobs
		select value,case when value%100=0 then 42 else value%100 end,
			case when value%3=0 then 'completed' when value%3=1 then 'queued' else 'processing' end
		from generate_series(1,100000) value`); err != nil {
		t.Fatal(err)
	}
	checkPlan(100_000)
	if _, err = pool.Exec(ctx, `insert into blueprint_jobs
		select value,case when value%100=0 then 42 else value%100 end,
			case when value%3=0 then 'completed' when value%3=1 then 'queued' else 'processing' end
		from generate_series(100001,1000000) value`); err != nil {
		t.Fatal(err)
	}
	checkPlan(1_000_000)
}

func TestBlueprintMaximumDocumentProcessingHasBoundedAllocationsIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify maximum blueprint processing allocations")
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	document := blueprintDocument{
		SchemaVersion: blueprintSchemaVersion, Name: "Maximum budget", SourceFormat: "schem",
		Size: [3]int{500, 1, 500}, Blocks: make([]blueprintBlock, maxBlueprintNonAirBlockCount),
	}
	state := blueprintBlockState{ID: "minecraft:stone"}
	for index := range document.Blocks {
		document.Blocks[index] = blueprintBlock{Position: [3]int{index % 500, 0, index / 500}, State: state}
	}
	if err := validateBlueprintDocument(document); err != nil {
		t.Fatal(err)
	}
	materials, err := blueprintMaterials(document)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := encodeNormalizedBlueprintJSON(document)
	if err != nil {
		t.Fatal(err)
	}
	cover, err := renderBlueprintCover(document)
	if err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(document)
	runtime.KeepAlive(materials)
	runtime.KeepAlive(normalized)
	runtime.KeepAlive(cover)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	duration := time.Since(started)
	if len(materials) != 1 || len(normalized) > maxBlueprintNormalizedBytes || allocated > 512<<20 || duration > 10*time.Second {
		t.Fatalf("maximum document = %d materials, %d normalized bytes, %d allocated bytes, %s", len(materials), len(normalized), allocated, duration)
	}
	t.Logf("maximum document: %d normalized bytes, %d allocated bytes, %s", len(normalized), allocated, duration)
}
