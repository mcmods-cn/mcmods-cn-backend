package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRepresentativeCatalogAndActivityQueryPlansIntegration(t *testing.T) {
	ctx, pool, _ := isolatedAITestDatabase(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var userID int64
	if err = tx.QueryRow(ctx, `insert into users(username,email,password_hash) values('plan-synthetic','plan@example.invalid','synthetic') returning id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into mods(project_code,slug,primary_name,review_status,submitted_by,updated_at)
		select new_public_id(),'plan-synthetic-'||n,'Synthetic mod '||n,case when n%10=0 then 'pending' else 'approved' end,$1,now()-n*interval '1 minute'
		from generate_series(1,10000) n`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `insert into user_activity_events(user_id,action_id,object_type_id,occurred_at)
		select $1,3,7,now()-n*interval '1 minute' from generate_series(1,10000) n`, userID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `analyze mods; analyze user_activity_events; analyze public_routes`); err != nil {
		t.Fatal(err)
	}
	args := []any{int64(0), "", false, []int64{}, []string{}, []string{}, []string{}, []string{}, "any", []string{}, []string{}, []string{}, []string{}, []string{}, 0}
	plans := make(map[string]json.RawMessage)
	queries := []struct {
		name, sql string
		args      []any
	}{
		{"catalog_count", `select count(*) from mods m ` + publicModCatalogFilter, args},
		{"catalog_updated_page", `select m.id,m.primary_name,m.summary,m.body_markdown from mods m
			left join public_routes popularity_route on popularity_route.entity_type='mod' and popularity_route.internal_id=m.id
			left join content_popularity_stats popularity on popularity.object_route_id=popularity_route.id ` + publicModCatalogFilter + `
			order by m.updated_at desc,m.id desc limit 24 offset 0`, args},
		{"activity_user_page", `select id,action_id,object_type_id,occurred_at from user_activity_events where user_id=$1 order by occurred_at desc,id desc limit 50`, []any{userID}},
	}
	for _, q := range queries {
		var raw []byte
		if err = tx.QueryRow(ctx, `explain (analyze,buffers,format json) `+q.sql, q.args...).Scan(&raw); err != nil {
			t.Fatalf("%s: %v", q.name, err)
		}
		if !json.Valid(raw) {
			t.Fatalf("%s returned invalid plan", q.name)
		}
		plans[q.name] = json.RawMessage(raw)
	}
	// Optional artifact contains only execution plans from this newly created
	// database. The catalog projection is representative, not the entire DTO.
	if output := os.Getenv("MCMODS_AUDIT_PLAN_OUTPUT"); output != "" {
		if !filepath.IsAbs(output) {
			t.Fatal("plan evidence path must be absolute")
		}
		raw, err := json.MarshalIndent(map[string]any{"mod_rows": 10000, "approved_fraction": 0.9, "activity_rows": 10000, "environment": "new owned PostgreSQL database; synthetic data; rolled back", "plans": plans}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(output, append(raw, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
