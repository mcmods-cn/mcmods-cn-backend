package httpapi

import (
	"context"
	"net/http"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAuditGR001RawDiagnosticIsExactScopedReadOnlyAndRetainsLegacyUnknownIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	raw := []byte("normal owned diagnostic input")
	ticket := f.ticket(t, f.editor, "/api/v1/users/me/oss/uploads/presign", "diagnostic.zip", "application/zip", "test037-retention", raw)
	f.put(t, ticket, raw)
	file := decodeTEST022Data[map[string]any](t, auditOSSRequire(t, f, f.editor, http.MethodPost, "/api/v1/users/me/oss/uploads/complete", ticket.completion(), 201))
	auditOSSRequire(t, f, f.editor, http.MethodDelete, "/api/v1/users/me/files", map[string]any{"objectKey": ticket.ObjectKey}, 200)
	var legacy string
	if err := f.db.QueryRow(f.ctx, `insert into oss_files(bucket,endpoint,region,object_key,status) values('owned-legacy','https://example.invalid','cn-owned','synthetic-legacy-terminal-no-provider-object','deleted') returning public_id`).Scan(&legacy); err != nil {
		t.Fatal(err)
	}
	cfg := f.db.Config().Copy()
	cfg.MaxConns, cfg.MinConns = 1, 1
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "on"
	readonly, err := pgxpool.NewWithConfig(f.ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer readonly.Close()
	before := f.facts(t)
	snapshotCaps := func() string {
		var value string
		if e := f.db.QueryRow(f.ctx, `select coalesce(jsonb_agg(to_jsonb(c) order by bucket,endpoint,object_key),'[]'::jsonb)::text from oss_upload_capabilities c`).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	caps := snapshotCaps()
	unknown := randomCatalogPublicID()
	first, err := InspectOSSRawRetirements(f.ctx, readonly, []string{file["id"].(string), legacy, unknown})
	if err != nil || !first.ReadOnly || first.ProviderBytesObserved || len(first.Files) != 3 || first.Counts["DB_INTENT_PENDING"] != 1 || first.Counts["HOLD_NO_TYPED_RECEIPT"] != 1 || first.Counts["UNKNOWN_FILE"] != 1 {
		t.Fatalf("bounded read-only stock diagnosis failed: counts=%v err=%v", first.Counts, err)
	}
	second, err := InspectOSSRawRetirements(f.ctx, readonly, []string{file["id"].(string), legacy, unknown})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatal("repeat read-only diagnosis changed results without a business change")
	}
	for _, ids := range [][]string{nil, {"not-public"}, {legacy, legacy}} {
		if _, e := InspectOSSRawRetirements(context.Background(), readonly, ids); e == nil {
			t.Fatal("unbounded/invalid/duplicate public-ID diagnosis scope was accepted")
		}
	}
	f.unchanged(t, before)
	if snapshotCaps() != caps {
		t.Fatal("read-only diagnosis invented historical capability or changed actual deadlines")
	}
}
