package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/activity"
	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/progression"
)

const auditCommentProcessConfig = "MCMODS_AUDIT_COMMENT_PROCESS_CONFIG"

type auditCommentProcessInput struct {
	Connection  string
	Database    string
	Parameters  map[string]string
	Application string
	Config      config.Config
	Consume     bool
}

type auditCommentProcess struct {
	URL     string
	name    string
	control io.WriteCloser
	done    chan error
	logs    *bytes.Buffer
	output  *bytes.Buffer
	waited  bool
}

// This child serves only the parent's exact owned test database. EXIT deliberately
// bypasses Go defers and all graceful HTTP/consumer cleanup. No signal is sent to
// another process; stdin is a private pipe owned by this test's exec.Cmd.
func auditRunCommentProcess(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var input auditCommentProcessInput
	if json.Unmarshal(raw, &input) != nil {
		t.Fatal("invalid private child configuration")
	}
	poolConfig, err := pgxpool.ParseConfig(input.Connection)
	if err == nil {
		// ConnString retains the original parsed DSN even when a fixture changes
		// ConnConfig.Database. Preserve its actual owned target explicitly.
		poolConfig.ConnConfig.Database = input.Database
		poolConfig.ConnConfig.RuntimeParams = input.Parameters
	}
	if err != nil || poolConfig == nil || (poolConfig.ConnConfig.Host != "127.0.0.1" && poolConfig.ConnConfig.Host != "localhost" && poolConfig.ConnConfig.Host != "::1") || !strings.HasPrefix(poolConfig.ConnConfig.Database, "test018_project_update_") {
		t.Fatal("child requires the exact owned TEST018 loopback database")
	}
	poolConfig.ConnConfig.RuntimeParams["application_name"] = input.Application
	poolConfig.MaxConns, poolConfig.MinConns = 2, 1
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	// A private self-exit guard bounds a broken driver without killing any other
	// process or requiring an administrator/service-manager cleanup path.
	guard := time.AfterFunc(45*time.Second, func() { os.Exit(24) })
	defer guard.Stop()
	db, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	api := NewServer(ctx, input.Config, db, nil, nil, nil, nil)
	defer api.cache.Close()
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := api.Shutdown(stop); err != nil {
			t.Error(err)
		}
	}()
	if input.Consume {
		consumer := activity.NewMonitor(db, progression.NewService(db, api.cache), activity.Options{BatchSize: 16, QueueCapacity: 32, FlushInterval: 20 * time.Millisecond, RetryMinDelay: 20 * time.Millisecond, RetryMaxDelay: 50 * time.Millisecond, WriteTimeout: time.Second, DurableEnqueueTimeout: time.Second})
		api.activity = consumer
		defer func() {
			stop, done := context.WithTimeout(context.Background(), 3*time.Second)
			defer done()
			if err := consumer.Close(stop); err != nil {
				t.Error(err)
			}
		}()
	}
	origin := httptest.NewServer(api)
	defer origin.Close()
	fmt.Printf("AUDIT_COMMENT_PROCESS_READY %s\n", origin.URL)
	control := bufio.NewScanner(os.Stdin)
	if !control.Scan() {
		t.Fatal("private child control pipe closed without an explicit stop")
	}
	switch control.Text() {
	case "EXIT":
		os.Exit(23)
	case "STOP":
		return
	default:
		t.Fatal("invalid private child control command")
	}
}

func auditStartCommentProcess(t *testing.T, f test013Fixture, consume bool, suffix string) *auditCommentProcess {
	t.Helper()
	name := fmt.Sprintf("audit_cmp003_comment_%s_%d", suffix, time.Now().UnixNano())
	connection := f.db.Config().ConnConfig
	input := auditCommentProcessInput{Connection: connection.ConnString(), Database: connection.Database, Parameters: connection.RuntimeParams, Application: name, Config: f.origin.Config.Handler.(*Server).cfg, Consume: consume}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "private-child-config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=^TestAuditCMP003CommentSurvivesActualProcessExitBeforeAndAfterCommitIntegration$", "-test.v")
	command.Env = append(os.Environ(), auditCommentProcessConfig+"="+path)
	control, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child := &auditCommentProcess{name: name, control: control, done: make(chan error, 1), logs: new(bytes.Buffer), output: new(bytes.Buffer)}
	command.Stderr = child.logs
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if address, ok := strings.CutPrefix(scanner.Text(), "AUDIT_COMMENT_PROCESS_READY "); ok {
				ready <- address
			} else {
				fmt.Fprintln(child.output, scanner.Text())
			}
		}
		child.done <- command.Wait()
	}()
	t.Cleanup(func() {
		if !child.waited {
			child.stop(t, false)
		}
	})
	select {
	case child.URL = <-ready:
		if !strings.HasPrefix(child.URL, "http://127.0.0.1:") {
			t.Fatal("child did not bind an owned loopback HTTP socket")
		}
	case err := <-child.done:
		child.waited = true
		t.Fatalf("owned child failed before readiness: %v %s %s", err, child.logs.String(), child.output.String())
	case <-f.ctx.Done():
		t.Fatal("owned child readiness exceeded the original fixture budget")
	}
	return child
}

func (child *auditCommentProcess) stop(t *testing.T, abrupt bool) {
	t.Helper()
	command := "STOP\n"
	if abrupt {
		command = "EXIT\n"
	}
	if _, err := io.WriteString(child.control, command); err != nil {
		t.Errorf("write exact owned child control: %v", err)
	}
	_ = child.control.Close()
	select {
	case err := <-child.done:
		child.waited = true
		if abrupt {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Errorf("owned child did not exit at the explicit crash boundary: %v %s", err, child.logs.String())
			}
		} else if err != nil {
			t.Errorf("owned recovery child shutdown failed: %v %s", err, child.logs.String())
		}
	case <-time.After(8 * time.Second):
		t.Error("owned child did not finish its private control command")
	}
}

func TestAuditCMP003CommentSurvivesActualProcessExitBeforeAndAfterCommitIntegration(t *testing.T) {
	if path := os.Getenv(auditCommentProcessConfig); path != "" {
		auditRunCommentProcess(t, path)
		return
	}
	fixture := newTEST038Fixture(t)
	f := fixture.test013Fixture
	task := auditFixActivityTask(t, f)
	beforeFacts := fixture.facts(t)
	actor := f.userIDs[f.editor]
	body := map[string]any{"body": "Owned process-exit comment", "idempotencyKey": "audit-cmp003-process-exit"}
	path := "/api/v1/comment-targets/mod/" + f.modCode + "/comments"
	request := func(child *auditCommentProcess) <-chan auditCompletionHTTPResult {
		out := make(chan auditCompletionHTTPResult, 1)
		go func() {
			raw, err := json.Marshal(body)
			if err != nil {
				out <- auditCompletionHTTPResult{err: err}
				return
			}
			req, err := http.NewRequestWithContext(f.ctx, http.MethodPost, child.URL+path, bytes.NewReader(raw))
			if err != nil {
				out <- auditCompletionHTTPResult{err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+f.editor)
			response, err := f.origin.Client().Do(req)
			if err != nil {
				out <- auditCompletionHTTPResult{err: err}
				return
			}
			defer response.Body.Close()
			raw, err = io.ReadAll(io.LimitReader(response.Body, 32768))
			out <- auditCompletionHTTPResult{status: response.StatusCode, raw: raw, err: err}
		}()
		return out
	}
	assertZero := func() {
		t.Helper()
		c, e, p, r, b, x, l := auditFixActivityFacts(t, f, actor, task)
		var queued, xpRows int
		if err := f.db.QueryRow(f.ctx, `select (select count(*) from activity_event_outbox where user_id=$1),(select count(*) from experience_transactions where user_id=$1)`, actor).Scan(&queued, &xpRows); err != nil || c+e+p+r+b+x+l+int64(queued+xpRows) != 0 {
			t.Fatalf("precommit process exit left business/event/projection/ledger state: %d/%d/%d/%d/%d/%d/%d queue%d xpRows%d err%v", c, e, p, r, b, x, l, queued, xpRows, err)
		}
	}

	// An AFTER INSERT trigger holds this real business transaction before its
	// commit. The observer proves the exact child backend waits on this parent's
	// specific advisory owner, then the child exits without graceful defers.
	barrier, err := f.db.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer barrier.Rollback(context.Background())
	lock := time.Now().UnixNano()
	var blocker int
	if err := barrier.QueryRow(f.ctx, `select pg_backend_pid() from (select pg_advisory_xact_lock($1)) held`, lock).Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, fmt.Sprintf(`create function audit_comment_process_barrier() returns trigger language plpgsql as $body$ begin perform pg_advisory_xact_lock(%d); return new; end $body$; create trigger audit_comment_process_barrier after insert on comments for each row when(new.author_id=%d) execute function audit_comment_process_barrier()`, lock, actor)); err != nil {
		t.Fatal(err)
	}
	before := auditStartCommentProcess(t, f, false, "before")
	pending := request(before)
	auditFixWaitActivity(t, func() bool {
		var count int
		if err := f.db.QueryRow(f.ctx, `select count(*) from pg_stat_activity where application_name=$1 and $2::int=any(pg_blocking_pids(pid))`, before.name, blocker).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count == 1
	})
	before.stop(t, true)
	if result := <-pending; result.err == nil {
		t.Fatalf("precommit process exit returned an HTTP response: %d", result.status)
	}
	if err := barrier.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `drop trigger audit_comment_process_barrier on comments; drop function audit_comment_process_barrier()`); err != nil {
		t.Fatal(err)
	}
	assertZero()
	fixture.unchanged(t, beforeFacts)

	// This process intentionally has no activity consumer: the producer's
	// committed intent must survive independently of any in-memory queue or
	// graceful Monitor.Close. The normal comment permission/route is unchanged.
	after := auditStartCommentProcess(t, f, false, "after")
	result := <-request(after)
	if result.err != nil || result.status != http.StatusCreated {
		t.Fatalf("actual producer HTTP%d err%v body%s", result.status, result.err, result.raw)
	}
	var queued int
	if err := f.db.QueryRow(f.ctx, `select count(*) from activity_event_outbox where user_id=$1`, actor).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("producer did not commit exactly one original intent: %d %v", queued, err)
	}
	after.stop(t, true)
	c, e, p, r, b, x, l := auditFixActivityFacts(t, f, actor, task)
	if c != 1 || e+p+r+b+x+l != 0 {
		t.Fatalf("postcommit crash lost business or prematurely processed reward: %d/%d/%d/%d/%d/%d/%d", c, e, p, r, b, x, l)
	}

	recovered := auditStartCommentProcess(t, f, true, "recovery")
	for range 2 {
		result := <-request(recovered)
		if result.err != nil || result.status != http.StatusOK {
			t.Fatalf("actual recovered same-key replay HTTP%d err%v body%s", result.status, result.err, result.raw)
		}
	}
	auditFixWaitActivity(t, func() bool {
		_, events, _, _, currency, _, _ := auditFixActivityFacts(t, f, actor, task)
		return events == 1 && currency == 3
	})
	auditFixRequireRewardFacts(t, f, actor, task)
	recovered.stop(t, false)
	var xpRows int
	if err := f.db.QueryRow(f.ctx, `select (select count(*) from activity_event_outbox where user_id=$1),(select count(*) from experience_transactions where user_id=$1 and amount_delta=25 and reference_type='task')`, actor).Scan(&queued, &xpRows); err != nil || queued != 0 || xpRows != 1 {
		t.Fatalf("settled queue or XP ledger mismatch: queued%d xpRows%d err%v", queued, xpRows, err)
	}
	t.Log("actual child self-exit23 beforecommit→zero facts; after201→one durable intent; separate restarted HTTP/Monitor/Progression process + two same-key200→one comment/event/progress/25XP ledger/3coin ledger; no graceful consumer close on crashed producers")
}
