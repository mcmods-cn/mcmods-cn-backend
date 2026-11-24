package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMaintenancePublicationPreservesProviderCompatibilityIntegration(t *testing.T) {
	db := newOCT02IsolatedDatabase(t, t.Context())
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	versions := []string{"1.20.1"}
	cfg := minecraftVersionConfig{
		Versions: []minecraftVersionOption{{Code: versions[0], Type: "release"}},
		Loaders:  []minecraftLoaderOption{{Code: "fabric", Name: "Fabric", Versions: versions}},
	}
	if _, err := saveManualMinecraftVersionConfig(ctx, db, cfg); err != nil {
		t.Fatal(err)
	}
	actorID := configureSeededAutomationFixture(t, ctx, db, "project.edit", "project.no-review")
	worker := &ProjectAutomationWorker{server: &Server{db: db}}
	files := []providerProjectFile{{GameVersions: versions, Loaders: []string{"fabric"}}}
	now := time.Now().UTC()
	for _, kind := range []string{"mod", "plugin"} {
		t.Run(kind, func(t *testing.T) {
			job := projectAutomationJob{ProjectType: kind, ActorID: actorID, Kind: "minecraft_versions", SourceType: "modrinth"}
			var base int64
			if kind == "mod" {
				if err := db.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
					values('automt001','maintenance-mod','Maintenance Mod','approved') returning id,project_code`).Scan(&job.InternalID, &job.ProjectPublicID); err != nil {
					t.Fatal(err)
				}
				base = seedPublishedMaintenanceModRevision(t, ctx, db, job.InternalID, job.ProjectPublicID, actorID, "active")
			} else {
				if err := db.QueryRow(ctx, `insert into simple_projects(project_type,slug,primary_name,review_status)
					values('plugin','maintenance-plugin','Maintenance Plugin','approved') returning id,public_id`).Scan(&job.InternalID, &job.ProjectPublicID); err != nil {
					t.Fatal(err)
				}
				base = seedPublishedMaintenanceSimpleProjectRevision(t, ctx, db, job.InternalID, job.ProjectPublicID, actorID, "active")
			}
			if err := db.QueryRow(ctx, `select id from public_routes where entity_type=$1 and internal_id=$2`, kind, job.InternalID).Scan(&job.RouteID); err != nil {
				t.Fatal(err)
			}
			if _, err := worker.mergeCompatibility(ctx, job, files); err != nil {
				t.Fatal(err)
			}
			result, err := worker.applyProjectMaintenancePolicy(ctx, job, now.AddDate(0, -7, 0), now)
			if err != nil || !result.Changed {
				t.Fatalf("expected a maintenance publication, got %+v, %v", result, err)
			}
			check := `select exists(select 1 from mod_loader_compatibilities c where c.mod_id=p.id and c.loader='fabric' and c.minecraft_version='1.20.1'),
				r.snapshot @> '{"compatibilities":[{"loader":"fabric","versions":["1.20.1"]}]}',
				not(b.snapshot @> '{"compatibilities":[{"loader":"fabric","versions":["1.20.1"]}]}')
				from mods p join content_revisions r on r.id=p.published_revision_id join content_revisions b on b.id=r.base_revision_id
				where p.id=$1 and b.id=$2`
			if kind != "mod" {
				check = `select p.loaders @> array['fabric'] and p.minecraft_versions @> array['1.20.1'],
					r.snapshot @> '{"loaders":["fabric"],"minecraftVersions":["1.20.1"]}',
					not(b.snapshot @> '{"loaders":["fabric"],"minecraftVersions":["1.20.1"]}')
					from simple_projects p join content_revisions r on r.id=p.published_revision_id join content_revisions b on b.id=r.base_revision_id
					where p.id=$1 and b.id=$2`
			}
			var live, snapshot, unchangedBase bool
			if err := db.QueryRow(ctx, check, job.InternalID, base).Scan(&live, &snapshot, &unchangedBase); err != nil {
				t.Fatal(err)
			}
			if !live || !snapshot || !unchangedBase {
				t.Errorf("post-maintenance compatibility live=%v published=%v unaffectedPreviousRevision=%v", live, snapshot, unchangedBase)
			}
			move := `update mods set review_status='pending' where id=$1`
			if kind != "mod" {
				move = `update simple_projects set review_status='pending' where id=$1`
			}
			if _, err := db.Exec(ctx, move, job.InternalID); err != nil {
				t.Fatal(err)
			}
			result, err = worker.applyProjectMaintenancePolicy(ctx, job, now, now)
			if err != nil || result.Changed {
				t.Errorf("maintenance on a project awaiting review: %+v, %v", result, err)
			}
		})
	}
	t.Run("current service account", func(t *testing.T) {
		var job projectAutomationJob
		job.ProjectType, job.Kind, job.SourceType, job.ActorID = "mod", "minecraft_versions", "modrinth", actorID
		if err := db.QueryRow(ctx, `update mods set review_status='approved' where project_code='automt001' returning id,project_code`).Scan(&job.InternalID, &job.ProjectPublicID); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(ctx, `select id from public_routes where entity_type='mod' and internal_id=$1`, job.InternalID).Scan(&job.RouteID); err != nil {
			t.Fatal(err)
		}
		configureSeededAutomationFixture(t, ctx, db, "project.edit")
		if _, err := worker.applyProjectMaintenancePolicy(ctx, job, now, now); !errors.Is(err, errAutomationPermissionDenied) {
			t.Errorf("maintenance without publish permission: %v", err)
		}
	})
}
