package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestAuditFID001PublicRecipientNeverUsesInternalIDFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	sender := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, sender, "economy.transfer")
	var currencyID int64
	if err := f.db.QueryRow(f.ctx, `update currencies set transfer_tax_bps=0 where code='diamond' returning id`).Scan(&currencyID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$2,1000)`, sender, currencyID); err != nil {
		t.Fatal(err)
	}
	var target, decoy, numericName, zeroTarget int64
	for _, row := range []struct {
		id             int64
		publicID, name string
		out            *int64
	}{
		{810001, "222333444", "audit-fid-target", &target},
		{222333444, "decoyfid1", "audit-fid-decoy", &decoy},
		{810003, "numbernam", "000123456", &numericName},
		{810004, "000000123", "audit-fid-leading", &zeroTarget},
	} {
		if err := f.db.QueryRow(f.ctx, `insert into users(id,public_id,username,email,password_hash,status) values($1,$2,$3,$3||'@example.test','unused','active') returning id`, row.id, row.publicID, row.name).Scan(row.out); err != nil {
			t.Fatal(err)
		}
	}
	ledgerCount := 0
	for _, row := range []struct {
		body   string
		want   int
		target int64
	}{
		{`{"recipient":"222333444","currency":"diamond","amount":100}`, 200, target},
		{`{"recipient":"000123456","currency":"diamond","amount":100}`, 200, numericName},
		{`{"recipient":"000000123","currency":"diamond","amount":100}`, 200, zeroTarget},
		{`{"recipient":"222333445","currency":"diamond","amount":100}`, 404, 0},
		{`{"recipient":"test044-test013-editor","currency":"diamond","amount":100}`, 400, 0},
	} {
		f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/economy/transfer", mustAuditJSON(t, row.body), row.want)
		if row.want == 200 {
			ledgerCount += 2
			assertTEST044Balances(t, f.ctx, f.db, currencyID, map[int64]int64{row.target: 100})
		}
		assertTEST044TransferLedger(t, f.ctx, f.db, currencyID, ledgerCount)
	}
	var decoyBalance int64
	if err := f.db.QueryRow(f.ctx, `select coalesce((select balance from user_currency_balances where user_id=$1 and currency_id=$2),0)`, decoy, currencyID).Scan(&decoyBalance); err != nil || decoyBalance != 0 {
		t.Fatalf("wrong recipient balance=%d err=%v", decoyBalance, err)
	}
	if _, err := f.db.Exec(f.ctx, `update users set username='222333444' where id=$1`, decoy); err != nil {
		t.Fatal(err)
	}
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/economy/transfer", map[string]any{"recipient": "222333444", "currency": "diamond", "amount": 100}, 409)
	assertTEST044TransferLedger(t, f.ctx, f.db, currencyID, ledgerCount)
	f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/economy/transfer", map[string]any{"recipientPublicId": "222333444", "currency": "diamond", "amount": 100}, 200)
	assertTEST044Balances(t, f.ctx, f.db, currencyID, map[int64]int64{target: 200, sender: 600})
	assertTEST044TransferLedger(t, f.ctx, f.db, currencyID, ledgerCount+2)
}

func TestAuditF001ClaimCreationFullHTTPIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.reviewer], "admin.*")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.editor], "creator.claim", "creator.claim.review")
	grantTEST044Permissions(t, f.ctx, f.db, f.userIDs[f.otherEditor], "creator.claim.review")
	for index, token := range []string{f.editor, f.reviewer} {
		var publicID string
		if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status) values('author',$1,$1,'approved') returning public_id`, fmt.Sprintf("audit-claim-%d", index)).Scan(&publicID); err != nil {
			t.Fatal(err)
		}
		body := map[string]any{"proofMarkdown": "Synthetic independent proof", "proofFileIds": []string{}}
		result := decodeTEST022Data[struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}](t, f.require(t, token, http.MethodPost, "/api/v1/creators/"+publicID+"/claims", body, 201))
		if _, err := f.db.Exec(f.ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status,permission_granting) select 'mod',$1,id,'approved',true from creators where public_id=$2`, f.modID, publicID); err != nil {
			t.Fatal(err)
		}
		if result.Status != "pending" || result.ID == "" {
			t.Fatalf("claim=%+v", result)
		}
		f.require(t, token, http.MethodPost, "/api/v1/creators/"+publicID+"/claims", body, 409)
		var count int
		if err := f.db.QueryRow(f.ctx, `select count(*) from creator_claims claim join creators creator on creator.id=claim.creator_id where creator.public_id=$1 and claim.status='pending' and claim.reviewed_by is null and claim.reviewed_at is null`, publicID).Scan(&count); err != nil || count != 1 {
			t.Fatalf("pending count=%d err=%v", count, err)
		}
		reviewPath := "/api/v1/admin/creator-claims/" + result.ID
		f.require(t, token, http.MethodPatch, reviewPath, map[string]any{"status": "approved"}, 403)
		status := "approved"
		if index == 1 {
			status = "rejected"
		}
		f.require(t, f.otherEditor, http.MethodPatch, reviewPath, map[string]any{"status": status}, 200)
		f.require(t, f.otherEditor, http.MethodPatch, reviewPath, map[string]any{"status": status}, 409)
		var derived bool
		if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and project_id=$2 and source_type='author_claim')`, f.userIDs[token], f.modID).Scan(&derived); err != nil || derived != (index == 0) {
			t.Fatalf("claim access=%v err=%v", derived, err)
		}
		if index == 0 {
			f.require(t, f.otherEditor, http.MethodDelete, reviewPath, map[string]any{"reason": "Synthetic revoke"}, 200)
			if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and project_id=$2 and source_type='author_claim')`, f.userIDs[token], f.modID).Scan(&derived); err != nil || derived {
				t.Fatalf("revoked access=%v err=%v", derived, err)
			}
		}

	}
}

func TestAuditCMP002CommunityAuthorsCanReadOwnEditLockFullHTTPIntegration(t *testing.T) {
	f := newTEST042Fixture(t)
	for _, kind := range []string{"tutorial", "issue", "news", "discussion"} {
		item := f.create(t, f.editor, f.snapshot(kind, "Audit lock "+kind))
		path := "/api/v1/review-locks/community_post/" + item.ID
		result := decodeTEST022Data[reviewLockResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
		if !result.Locked || result.RequestID == "" {
			t.Fatalf("%s pending lock=%+v", kind, result)
		}
		f.require(t, f.otherEditor, http.MethodGet, path, nil, 403)
		f.require(t, f.reviewer, http.MethodGet, path, nil, 403)
		f.review(t, item, "approved")
		result = decodeTEST022Data[reviewLockResponse](t, f.require(t, f.editor, http.MethodGet, path, nil, 200))
		if result.Locked {
			t.Fatalf("%s approved remains locked", kind)
		}
	}
}

func mustAuditJSON(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
