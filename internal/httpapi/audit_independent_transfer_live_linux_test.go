package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Optional immutable API service for the independent FID-001 real UI check.
// Setup/cleanup hold the same task PG lock as all other full-schema tests;
// the browser's normal HTTP use does not hold that lock for its duration.
func TestIndependentP1FID001LiveBrowserFixture(t *testing.T) {
	if os.Getenv("MCMODS_RUN_LIVE_TRANSFER") != "1" {
		t.Skip("opt-in independent ordinary-user transfer browser fixture")
	}
	dir, authPath, lockPath := os.Getenv("MCMODS_AUDIT_TRANSFER_DIR"), os.Getenv("MCMODS_AUDIT_TRANSFER_AUTH"), os.Getenv("MCMODS_AUDIT_PG_LOCK")
	if !filepath.IsAbs(dir) || !filepath.IsAbs(authPath) || !filepath.IsAbs(lockPath) {
		t.Fatal("requires absolute task runtime, private auth and PG lock paths")
	}
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal("cannot identify source commit")
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil || len(strings.TrimSpace(string(status))) != 0 {
		t.Fatal("live fixture requires clean immutable checkout")
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal("runtime must be a new task-owned directory")
	}
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() })
	t.Log("Waiting for shared owned PostgreSQL fixture lock")
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	f := newAuditFixGRFixture(t, 10*time.Minute)
	// LIFO cleanup reacquires the lock before the fixture drops its owned
	// schema; the first registered cleanup releases it after every DB cleanup.
	t.Cleanup(func() {
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
			t.Error(err)
		}
	})
	if f.server.cfg.FrontendOrigin != "http://127.0.0.1:3311" {
		t.Fatal("requires exact frontend origin http://127.0.0.1:3311")
	}
	sender := f.userIDs[f.editor]
	grantTEST044Permissions(t, f.ctx, f.db, sender, "economy.transfer")
	permissions, err := resolveUsersRootPermissionsQuery(f.ctx, f.db, []int64{sender})
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range permissions[sender].Permissions {
		if rule.Allow && (rule.Code == "*" || rule.Code == "admin.*" || rule.Code == "admin") {
			t.Fatal("sender must be an ordinary user")
		}
	}
	var currency, target, decoy int64
	var senderPublic string
	if err := f.db.QueryRow(f.ctx, `select public_id from users where id=$1`, sender).Scan(&senderPublic); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `update currencies set transfer_tax_bps=0 where code='diamond' returning id`).Scan(&currency); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into user_currency_balances(user_id,currency_id,balance) values($1,$2,1000)`, sender, currency); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into users(id,public_id,username,email,password_hash,status) values(810001,'222333444','independent-live-target','target@independent.test','unused','active') returning id`).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into users(id,public_id,username,email,password_hash,status) values(222333444,'decoyfid1','222333444','decoy@independent.test','unused','active') returning id`).Scan(&decoy); err != nil {
		t.Fatal(err)
	}
	// Demonstrate the ambiguous legacy field rejects without funding either
	// target. The browser must use explicit public identity to succeed.
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/users/me/economy/transfer", map[string]any{"recipient": "222333444", "currency": "diamond", "amount": 100}, 409)
	if !strings.Contains(string(raw), "TRANSFER_RECIPIENT_AMBIGUOUS") {
		t.Fatal("legacy lookup did not produce exact ambiguity code")
	}
	assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{sender: 1000})
	assertTEST044TransferLedger(t, f.ctx, f.db, currency, 0)
	auth, _ := json.Marshal(map[string]any{"senderToken": f.editor})
	private, err := os.OpenFile(authPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal("auth file must be new and private")
	}
	if _, err = private.Write(auth); err != nil {
		private.Close()
		t.Fatal("private auth write failed")
	}
	if err = private.Close(); err != nil {
		t.Fatal(err)
	}
	runtime, _ := json.MarshalIndent(map[string]any{"apiOrigin": f.origin.URL, "frontendOrigin": "http://127.0.0.1:3311", "authPath": authPath, "authCookieName": authSessionCookieName, "senderPublicId": senderPublic, "recipientPublicId": "222333444", "recipientUsername": "independent-live-target", "decoyPublicId": "decoyfid1", "currency": "diamond", "amount": 100, "expectedSenderBalance": 900, "expectedRecipientBalance": 100, "expectedDecoyBalance": 0, "expectedLedgerRows": 2, "sourceCommit": strings.TrimSpace(string(head)), "sourceTreeClean": true, "scope": "Real API/PostgreSQL, ordinary sender and synthetic numeric-public-ID/internal-ID+username collision; no production balances touched"}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "runtime.json"), runtime, 0600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	t.Logf("TRANSFER_READY runtime=%s source=%s ordinarySender=true initialBalance=1000; credentials private", filepath.Join(dir, "runtime.json"), strings.TrimSpace(string(head)))
	wait, cancel := context.WithTimeout(f.ctx, 8*time.Minute)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-wait.Done():
			t.Fatal("browser completion absent; no transfer acceptance inferred")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "done.flag")); os.IsNotExist(err) {
				continue
			} else if err != nil {
				t.Fatal("completion marker inspection failed")
			}
			assertTEST044Balances(t, f.ctx, f.db, currency, map[int64]int64{sender: 900, target: 100})
			assertTEST044TransferLedger(t, f.ctx, f.db, currency, 2)
			var decoyBalance int64
			var exactLedger int
			if err := f.db.QueryRow(f.ctx, `select coalesce((select balance from user_currency_balances where user_id=$1 and currency_id=$2),0)`, decoy, currency).Scan(&decoyBalance); err != nil {
				t.Fatal(err)
			}
			if err := f.db.QueryRow(f.ctx, `select count(*) from currency_transactions where currency_id=$1 and ((user_id=$2 and counterparty_user_id=$3 and transaction_type='transfer_out' and amount_delta=-100) or (user_id=$3 and counterparty_user_id=$2 and transaction_type='transfer_in' and amount_delta=100))`, currency, sender, target).Scan(&exactLedger); err != nil {
				t.Fatal(err)
			}
			if decoyBalance != 0 || exactLedger != 2 {
				t.Fatalf("wrong account or ledger: decoyBalance=%d exactLedger=%d", decoyBalance, exactLedger)
			}
			report, _ := json.MarshalIndent(map[string]any{"result": "PASS", "sourceCommit": strings.TrimSpace(string(head)), "senderBalance": 900, "recipientBalance": 100, "decoyBalance": decoyBalance, "exactLedgerRows": exactLedger, "recipientPublicId": "222333444", "legacyAmbiguity": "409_TRANSFER_RECIPIENT_AMBIGUOUS_ZERO_WRITES", "productionDataExecuted": false}, "", "  ")
			if err := os.WriteFile(filepath.Join(dir, "backend-postconditions.json"), report, 0600); err != nil {
				t.Fatal(err)
			}
			t.Log("TRANSFER_PASS browser completed; exact recipient balance100/sender900/decoy0, two reciprocal ledger rows; closing owned fixture")
			return
		}
	}
}
