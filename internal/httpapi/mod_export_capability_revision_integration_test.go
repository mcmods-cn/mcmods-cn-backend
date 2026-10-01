package httpapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestExportCapabilitiesDeduplicateRevisionAliasesIntegration(t *testing.T) {
	pool := openModExportConcurrentTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err = tx.Exec(ctx, `create temp table catalog_import_capabilities(
		revision_id text not null,capability_id text not null,status text not null,
		source text not null,data jsonb not null,primary key(revision_id,capability_id)
	) on commit drop`); err != nil {
		t.Fatal(err)
	}
	capabilities := []modExportCapability{
		{ID: "registries", Status: "available", Source: "exporter", Data: json.RawMessage(`{"id":"registries","status":"available"}`)},
		{ID: "worldgen", Status: "degraded", Source: "exporter", Data: json.RawMessage(`{"id":"worldgen","status":"degraded"}`)},
	}
	if err = importExportCapabilities(ctx, tx, map[string]string{"minecraft": "revision-main", "jei": "revision-main", "forge": "revision-main", "other": "revision-other"}, capabilities); err != nil {
		t.Fatal(err)
	}
	var total int
	if err = tx.QueryRow(ctx, `select count(*)::int from catalog_import_capabilities`).Scan(&total); err != nil || total != 4 {
		t.Fatalf("rows=%d err=%v", total, err)
	}
	for _, revision := range []string{"revision-main", "revision-other"} {
		for _, capability := range capabilities {
			var status, source string
			var data json.RawMessage
			if err = tx.QueryRow(ctx, `select status,source,data from catalog_import_capabilities where revision_id=$1 and capability_id=$2`, revision, capability.ID).Scan(&status, &source, &data); err != nil {
				t.Fatal(err)
			}
			var decoded map[string]string
			if err = json.Unmarshal(data, &decoded); err != nil || status != capability.Status || source != capability.Source || decoded["id"] != capability.ID || decoded["status"] != status {
				t.Fatalf("capability facts changed: status=%s source=%s data=%s err=%v", status, source, data, err)
			}
		}
	}
}
