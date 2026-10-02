package httpapi

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/database"
)

type modRelationshipQueryCounter struct{ queries atomic.Int64 }

func (counter *modRelationshipQueryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "from mod_relationship_groups") {
		counter.queries.Add(1)
	}
	return ctx
}

func (*modRelationshipQueryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestModAssociationsLoadFiftyGroupsInOneRelationshipQueryIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify bounded Mod association queries")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	counter := &modRelationshipQueryCounter{}
	poolConfig, err := pgxpool.ParseConfig(config.Load().DB.ConnString())
	if err != nil {
		t.Fatal(err)
	}
	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1
	poolConfig.ConnConfig.Tracer = counter
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

	var modID, approvedTargetID, pendingTargetID int64
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,'association-source','Association source','approved') returning id`, randomCatalogPublicID()).Scan(&modID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,'association-approved','Approved target','approved') returning id`, randomCatalogPublicID()).Scan(&approvedTargetID); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, `insert into mods(project_code,slug,primary_name,review_status)
		values($1,'association-pending','Pending target','pending') returning id`, randomCatalogPublicID()).Scan(&pendingTargetID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_relationship_groups(
		mod_id,label,loader,minecraft_versions,mod_version,display_order)
		select $1,'group-'||lpad(value::text,2,'0'),'neoforge',array['1.21.1'],'1.0',value
		from generate_series(0,49) value`, modID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `insert into mod_relationships(
		mod_id,group_id,relation_type,related_mod_id,related_mod_name,related_mod_identifier,display_order)
		select $1,relationship_group.id,'dependency',
			case when relationship_group.display_order=0 then $2::bigint else $3::bigint end,
			'Stored target','target-'||ordinal,ordinal
		from mod_relationship_groups relationship_group cross join generate_series(0,3) ordinal
		where relationship_group.mod_id=$1`, modID, pendingTargetID, approvedTargetID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `analyze mod_relationship_groups; analyze mod_relationships; analyze mods`); err != nil {
		t.Fatal(err)
	}

	counter.queries.Store(0)
	server := &Server{db: pool}
	result := modResponse{ID: modID, SiteID: "association-source"}
	if err = server.loadModAssociations(ctx, &result); err != nil {
		t.Fatal(err)
	}
	if got := counter.queries.Load(); got != 1 {
		t.Fatalf("50 relationship groups used %d relationship queries; want 1", got)
	}
	if len(result.RelationshipGroups) != 50 {
		t.Fatalf("relationship groups=%d want=50", len(result.RelationshipGroups))
	}
	visibleRelationships := 0
	for index, group := range result.RelationshipGroups {
		wantLabel := fmt.Sprintf("group-%02d", index)
		if group.Label != wantLabel || group.Direction != "outgoing" {
			t.Fatalf("group %d label=%q direction=%q", index, group.Label, group.Direction)
		}
		if index == 0 {
			if len(group.Relationships) != 0 {
				t.Fatalf("pending-only group exposed %d relationships", len(group.Relationships))
			}
			continue
		}
		if len(group.Relationships) != 4 {
			t.Fatalf("group %d relationships=%d want=4", index, len(group.Relationships))
		}
		for _, relationship := range group.Relationships {
			if relationship.RelatedModSiteID != "association-approved" || relationship.RelatedModPublicID == "" {
				t.Fatalf("group %d returned wrong approved target: %+v", index, relationship)
			}
		}
		visibleRelationships += len(group.Relationships)
	}
	if visibleRelationships != 196 {
		t.Fatalf("visible relationships=%d want=196", visibleRelationships)
	}
}
