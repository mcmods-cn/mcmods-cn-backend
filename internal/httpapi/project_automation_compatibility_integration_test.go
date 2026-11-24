package httpapi

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type compatibilityInsertCounter struct{ inserts atomic.Int64 }

func (counter *compatibilityInsertCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "insert into mod_loader_compatibilities") {
		counter.inserts.Add(1)
	}
	return ctx
}
func (*compatibilityInsertCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestAutomatedCompatibilityCountsNewFactsAndPreservesUnchangedProjectsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	owned := newOCT02IsolatedDatabase(t, ctx)
	pc := owned.Config()
	counter := &compatibilityInsertCounter{}
	pc.ConnConfig.Tracer = counter
	db, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	versions := []string{"1.20.1", "1.20.2", "1.20.3", "1.20.4", "1.20.5", "1.20.6"}
	cfg := minecraftVersionConfig{Loaders: []minecraftLoaderOption{{Code: "forge", Name: "Forge", Versions: versions}, {Code: "fabric", Name: "Fabric", Versions: versions}}}
	for _, v := range versions {
		cfg.Versions = append(cfg.Versions, minecraftVersionOption{Code: v, Type: "release"})
	}
	if _, err = saveManualMinecraftVersionConfig(ctx, db, cfg); err != nil {
		t.Fatal(err)
	}
	actorID := configureSeededAutomationFixture(t, ctx, db, "project.edit", "project.no-review")
	worker := &ProjectAutomationWorker{server: &Server{db: db}}
	files := []providerProjectFile{{GameVersions: versions, Loaders: []string{"Forge", "Fabric"}}, {GameVersions: versions[:2], Loaders: []string{"Forge"}}}
	for _, kind := range []string{"mod", "map"} {
		t.Run(kind, func(t *testing.T) {
			job := projectAutomationJob{ProjectType: kind, ActorID: actorID}
			if kind == "mod" {
				err = db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status) values('autocomp1','compat-mod','Compatibility','approved') returning id,project_code`).Scan(&job.InternalID, &job.ProjectPublicID)
			} else {
				err = db.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,minecraft_versions,loaders,review_status) values('map','compat-map','Compatibility',array['1.16.5'],array['neoforge'],'approved') returning id,public_id`).Scan(&job.InternalID, &job.ProjectPublicID)
			}
			if err != nil {
				t.Fatal(err)
			}
			counter.inserts.Store(0)
			first, err := worker.mergeCompatibility(ctx, job, files)
			if err != nil {
				t.Fatal(err)
			}
			expected := int64(12)
			if kind == "mod" {
				t.Logf("6 distinct versions × 2 loaders, %d compatibility INSERT statements", counter.inserts.Load())
				if counter.inserts.Load() != 1 {
					t.Errorf("compatibility batch required %d SQL inserts", counter.inserts.Load())
				}
			} else {
				expected = 8
			}
			if first["relationsAdded"] != expected {
				t.Errorf("first observation facts=%v, want %d", first["relationsAdded"], expected)
			}
			var previous time.Time
			if kind == "map" {
				if err := db.QueryRow(ctx, `select updated_at from simple_projects where id=$1`, job.InternalID).Scan(&previous); err != nil {
					t.Fatal(err)
				}
			}
			repeat, err := worker.mergeCompatibility(ctx, job, files)
			if err != nil || repeat["relationsAdded"] != int64(0) {
				t.Errorf("same observation facts=%v error=%v", repeat, err)
			}
			if kind == "map" {
				var after time.Time
				var storedVersions, loaders []string
				if err := db.QueryRow(ctx, `select updated_at,minecraft_versions,loaders from simple_projects where id=$1`, job.InternalID).Scan(&after, &storedVersions, &loaders); err != nil {
					t.Fatal(err)
				}
				if !previous.Equal(after) || !slices.Contains(storedVersions, "1.16.5") || !slices.Contains(loaders, "neoforge") {
					t.Error("repeat observation rewrote project metadata or a manual compatibility value")
				}
				if _, err := db.Exec(ctx, `update simple_projects set review_status='pending' where id=$1`, job.InternalID); err != nil {
					t.Fatal(err)
				}
				if _, err := worker.mergeCompatibility(ctx, job, files); err == nil {
					t.Error("an observation was applied while its project awaited approval")
				}
			}
		})
	}
}
