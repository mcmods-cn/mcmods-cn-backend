package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

func TestActivityCleanupPreviewPlansStayBoundedAtScaleIntegration(t *testing.T) {
	if os.Getenv("MCMODS_RUN_DB_INTEGRATION") != "1" {
		t.Skip("set MCMODS_RUN_DB_INTEGRATION=1 to verify 100k/1M/10M activity preview plans")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	counter := &integrationQueryCounter{}
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
	if _, err = pool.Exec(ctx, `create temp table activity_actions(id smallint primary key,code text not null unique);
		create temp table activity_object_types(id smallint primary key,code text not null unique);
		create temp table users(id bigint primary key,public_id text not null unique);
		create temp table public_routes(id bigint primary key,public_id text not null unique);
		create temp table user_activity_events(
			id bigserial primary key,user_id bigint,action_id smallint not null,object_type_id smallint not null,
			object_route_id bigint,occurred_at timestamptz not null);
		create index idx_activity_action_time on user_activity_events(action_id,occurred_at,id);
		create index idx_activity_object_time on user_activity_events(object_type_id,object_route_id,occurred_at desc,id desc);
		create index idx_activity_time_brin on user_activity_events using brin(occurred_at);
		insert into activity_actions values(3,'view');
		insert into activity_object_types values(2,'mod');
		insert into users values(1,'user00001');
		insert into public_routes values(1,'route0001')`); err != nil {
		t.Fatal(err)
	}

	loaded := int64(0)
	filter := normalizedActivityCleanupFilter{
		ActionIDs: []int16{3}, ObjectTypeIDs: []int16{2},
		SnapshotBefore: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	where, baseArgs := activityCleanupWhere(filter, 1)
	query := activityCleanupPreviewSummarySQL(where, len(baseArgs)+1)
	for _, target := range []int64{100000, 1000000, 10000000} {
		if _, err = pool.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,object_route_id,occurred_at)
			select 1,3,2,1,timestamptz '2020-01-01 00:00:00+00' + value * interval '1 millisecond'
			from generate_series($1::bigint,$2::bigint) value`, loaded+1, target); err != nil {
			t.Fatalf("load %d events: %v", target, err)
		}
		loaded = target
		if _, err = pool.Exec(ctx, `analyze user_activity_events`); err != nil {
			t.Fatal(err)
		}

		counter.queries.Store(0)
		summary, summaryErr := (&Server{db: pool}).loadActivityCleanupPreviewSummary(ctx, where, append([]any(nil), baseArgs...))
		if summaryErr != nil {
			t.Fatalf("summarize %d events: %v", target, summaryErr)
		}
		wantTotal := target
		if wantTotal > maxActivityCleanupPreviewEvents {
			wantTotal = maxActivityCleanupPreviewEvents + 1
		}
		if summary.Total != wantTotal {
			t.Fatalf("%d-row preview total=%d want=%d", target, summary.Total, wantTotal)
		}
		if target == maxActivityCleanupPreviewEvents {
			if summary.ByAction["view"] != target || summary.ByObjectType["mod"] != target ||
				summary.ByUser["user00001"] != target || len(summary.Samples) != 10 {
				t.Fatalf("100k complete summary is incomplete: %#v %#v %#v samples=%d",
					summary.ByAction, summary.ByObjectType, summary.ByUser, len(summary.Samples))
			}
			if want := time.Date(2020, 1, 1, 0, 0, 0, int(time.Millisecond), time.UTC); !summary.Samples[0].OccurredAt.Equal(want) {
				t.Fatalf("first sample timestamp=%s want=%s", summary.Samples[0].OccurredAt, want)
			}
		}
		if queries := counter.queries.Load(); queries != 1 {
			t.Fatalf("%d-row preview executed %d SQL statements, want 1", target, queries)
		}

		planArgs := append(append([]any(nil), baseArgs...), maxActivityCleanupPreviewEvents+1)
		var rawPlan []byte
		if err = pool.QueryRow(ctx, `explain (analyze,buffers,format json) `+query, planArgs...).Scan(&rawPlan); err != nil {
			t.Fatalf("explain %d events: %v", target, err)
		}
		plan, executionMS, planErr := activityPreviewPlanFacts(rawPlan)
		if planErr != nil {
			t.Fatal(planErr)
		}
		if plan.ActualRows > float64(maxActivityCleanupPreviewEvents+1) || plan.ActualLoops != 1 || plan.RowsRemoved > float64(maxActivityCleanupPreviewEvents+1) {
			t.Fatalf("%d-row base scan exceeded cap: rows=%.0f loops=%.0f removed=%.0f",
				target, plan.ActualRows, plan.ActualLoops, plan.RowsRemoved)
		}
		t.Logf("rows=%d base=%s actual=%.0f removed=%.0f execution_ms=%.3f", target,
			plan.NodeType, plan.ActualRows, plan.RowsRemoved, executionMS)
	}
}

type activityPreviewBasePlan struct {
	NodeType    string
	ActualRows  float64
	ActualLoops float64
	RowsRemoved float64
}

func activityPreviewPlanFacts(raw []byte) (activityPreviewBasePlan, float64, error) {
	var documents []map[string]any
	if err := json.Unmarshal(raw, &documents); err != nil {
		return activityPreviewBasePlan{}, 0, fmt.Errorf("decode activity preview plan: %w", err)
	}
	if len(documents) != 1 {
		return activityPreviewBasePlan{}, 0, fmt.Errorf("decode activity preview plan: got %d documents, want 1", len(documents))
	}
	executionMS, _ := documents[0]["Execution Time"].(float64)
	root, _ := documents[0]["Plan"].(map[string]any)
	var found activityPreviewBasePlan
	var walk func(map[string]any)
	walk = func(node map[string]any) {
		if node["Relation Name"] == "user_activity_events" {
			found = activityPreviewBasePlan{
				NodeType: fmt.Sprint(node["Node Type"]), ActualRows: numberFact(node["Actual Rows"]),
				ActualLoops: numberFact(node["Actual Loops"]), RowsRemoved: numberFact(node["Rows Removed by Filter"]),
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			if childNode, ok := child.(map[string]any); ok {
				walk(childNode)
			}
		}
	}
	walk(root)
	if found.NodeType == "" {
		return found, executionMS, fmt.Errorf("activity preview plan did not scan user_activity_events")
	}
	return found, executionMS, nil
}

func numberFact(value any) float64 {
	number, _ := value.(float64)
	return number
}
