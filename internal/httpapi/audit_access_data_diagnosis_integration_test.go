package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"mcmods-cn-backend/internal/auditdata"
)

func TestAuditKnownDataDiagnosisIsReadOnlyAndOmitsPrivateDataIntegration(t *testing.T) {
	f := newTEST039Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "economy.transfer")
	for _, q := range []string{
		`insert into users(id,public_id,username,email,password_hash,status) values(222333444,'diagdecoy','synthetic-private-recipient','synthetic-private-recipient@example.test','unused','active')`,
		`insert into users(id,public_id,username,email,password_hash,status) values(810001,'222333444','synthetic-private-possible','synthetic-private-possible@example.test','unused','active')`,
	} {
		if _, err := f.db.Exec(f.ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) select $1,id,1000 from currencies where code='diamond'`, f.userIDs[f.editor]); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/economy/transfer", map[string]any{"recipientPublicId": "diagdecoy", "currency": "diamond", "amount": 100}, 200)
	if _, err := f.db.Exec(f.ctx, `insert into oss_files(bucket,endpoint,region,object_key,status,sha256) values('fixture','https://example.test','fixture','synthetic-private-path','active',repeat('a',64))`); err != nil {
		t.Fatal(err)
	}
	file := f.upload(t, f.editor, "diagnosis.png", test039PNG(t, 64, 64, 99))
	f.asset(t, f.editor, file, "synthetic-private-title", "private")
	var before, after string
	snapshot := `select jsonb_build_object('ledger',(select count(*) from currency_transactions),'files',(select jsonb_agg(jsonb_build_array(id,status,source_hash_verified,sha256) order by id) from oss_files))::text`
	if err := f.db.QueryRow(f.ctx, snapshot).Scan(&before); err != nil {
		t.Fatal(err)
	}
	report, err := auditdata.Inspect(f.ctx, f.db, time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 100)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ReadOnly || report.Sections["transfer_numeric_identity_collision_candidates"].Count != 1 || report.Sections["active_unverified_source_files"].Count < 1 || report.Sections["private_skin_historical_exposure_candidates"].Count != 1 {
		t.Fatalf("unexpected report sections: %+v", report.Sections)
	}
	raw, _ := json.Marshal(report)
	for _, private := range []string{"synthetic-private-path", "synthetic-private-title", "synthetic-private-recipient@example.test", "synthetic-private-possible"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("diagnosis disclosed private field %q", private)
		}
	}
	if err := f.db.QueryRow(f.ctx, snapshot).Scan(&after); err != nil || before != after {
		t.Fatalf("diagnosis mutated facts: err=%v", err)
	}
	if _, err := auditdata.Inspect(f.ctx, f.db, time.Time{}, time.Now(), 100); err == nil {
		t.Fatal("unbounded time window accepted")
	}
}
