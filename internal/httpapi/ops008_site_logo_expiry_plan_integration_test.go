package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestOPS008SiteLogoExpiryUsesPartialIndexInLargeOSSHistoryIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pool := newTEST018IsolatedDatabase(t, ctx)
	// The full generation168 schema and its public-ID trigger remain enabled.
	if _, err := pool.Exec(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,content_type,size_bytes,status,scan_status,created_at)
		select 'owned-plan','https://owned.invalid','cn-test','ops008/history/'||n,'site','project-icon','history.png','image/png',1,'active','trusted_generated',now()-interval '2 days'
		from generate_series(1,100000) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `insert into oss_files(bucket,endpoint,region,object_key,category,source,original_name,content_type,size_bytes,status,scan_status,created_at)
		select 'owned-plan','https://owned.invalid','cn-test','ops008/staging/'||n,'site','site_logo_pending','staging.png','image/png',1,'active','trusted_generated',now()-interval '2 hours'
		from generate_series(1,100) n`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `analyze oss_files`); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `explain (analyze,buffers,format json) select id from oss_files
		where source='site_logo_pending' and status='active' and created_at<=now()-make_interval(secs=>$2)
		order by created_at,id for update skip locked limit $1`, maintenanceBatchSize, int(siteLogoRetention/time.Second)).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	t.Logf("actual 100100-row shared-logo expiry plan: %s", raw)
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("invalid measured plan: %v", err)
	}
	if plans[0].Plan["Actual Rows"] != float64(100) {
		t.Fatalf("wrong expiry rows: %s", raw)
	}
	usedIndex := false
	var check func(map[string]any)
	check = func(node map[string]any) {
		if strings.Contains(fmt.Sprint(node["Node Type"]), "Seq Scan") {
			t.Fatalf("history-wide scan: %s", raw)
		}
		if node["Index Name"] == "idx_oss_files_site_logo_pending_expiry" {
			usedIndex = true
		}
		if rows, ok := node["Actual Rows"].(float64); ok {
			loops, _ := node["Actual Loops"].(float64)
			removed, _ := node["Rows Removed by Filter"].(float64)
			if (rows+removed)*loops > 1000 {
				t.Fatalf("unbounded history reads: %s", raw)
			}
		}
		if blocks, ok := node["Shared Hit Blocks"].(float64); ok {
			reads, _ := node["Shared Read Blocks"].(float64)
			if blocks+reads > 1000 {
				t.Fatalf("unbounded history buffers: %s", raw)
			}
		}
		children, _ := node["Plans"].([]any)
		for _, child := range children {
			next, ok := child.(map[string]any)
			if !ok {
				t.Fatal("malformed measured child")
			}
			check(next)
		}
	}
	check(plans[0].Plan)
	if !usedIndex {
		t.Fatalf("expiry does not execute the production partial index: %s", raw)
	}
}
