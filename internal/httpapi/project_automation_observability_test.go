package httpapi

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type simpleRowsFailureStub struct {
	next        bool
	valuesErr   error
	terminalErr error
	closed      bool
}

func (rows *simpleRowsFailureStub) Close()                                       { rows.closed = true }
func (rows *simpleRowsFailureStub) Err() error                                   { return rows.terminalErr }
func (rows *simpleRowsFailureStub) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *simpleRowsFailureStub) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *simpleRowsFailureStub) RawValues() [][]byte                          { return nil }
func (rows *simpleRowsFailureStub) Conn() *pgx.Conn                              { return nil }
func (rows *simpleRowsFailureStub) Scan(...any) error                            { return errors.New("unused") }
func (rows *simpleRowsFailureStub) Values() ([]any, error)                       { return nil, rows.valuesErr }
func (rows *simpleRowsFailureStub) Next() bool {
	if rows.next {
		rows.next = false
		return true
	}
	return false
}

func TestProjectAutomationAndSimpleRowsDoNotHideDatabaseFailures(t *testing.T) {
	workerRaw, err := os.ReadFile("project_automation_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	logRaw, err := os.ReadFile("log_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	seedRaw, err := os.ReadFile("seed_crawler_worker.go")
	if err != nil {
		t.Fatal(err)
	}
	workerSource, logSource, seedSource := string(workerRaw), string(logRaw), string(seedRaw)
	tickBody := goFunctionBody(t, workerSource, "tick")
	scheduleBody := goFunctionBody(t, workerSource, "scheduleDue")
	for _, forbidden := range []string{"_, _ =", "_ = tx.Commit", "rows.Scan(&id) == nil"} {
		if strings.Contains(tickBody+scheduleBody, forbidden) {
			t.Fatalf("automation scheduler still hides a failure through %q", forbidden)
		}
	}
	for _, required := range []string{"func (worker *ProjectAutomationWorker) tick(ctx context.Context) error", "func (worker *ProjectAutomationWorker) scheduleDue(ctx context.Context) error", "rows.Err()", "project automation tick"} {
		if !strings.Contains(workerSource, required) {
			t.Fatalf("automation scheduler observability is missing %q", required)
		}
	}

	queryBody := goFunctionBody(t, logSource, "querySimpleRowsWithContext") + goFunctionBody(t, logSource, "collectSimpleRows")
	for _, required := range []string{"([]map[string]any, error)", "rows.Values()", "rows.Err()", "return nil, err"} {
		if !strings.Contains(queryBody, required) {
			t.Fatalf("simple row query must propagate query/value/cursor errors; missing %q", required)
		}
	}
	if strings.Contains(queryBody, "continue") {
		t.Fatal("simple row query still drops a row whose Values call failed")
	}

	seedScheduleBody := goFunctionBody(t, seedSource, "schedule") + goFunctionBody(t, seedSource, "scheduleDueRun")
	for _, forbidden := range []string{"_, _ =", "_ = tx.Commit"} {
		if strings.Contains(seedScheduleBody, forbidden) {
			t.Fatalf("seed crawler scheduler still hides a failure through %q", forbidden)
		}
	}
	for _, required := range []string{
		"func (worker *SeedCrawlerWorker) schedule(ctx context.Context) error",
		"func (worker *SeedCrawlerWorker) scheduleDueRun(ctx context.Context) error",
		"seed crawler schedule",
		"func (worker *SeedCrawlerWorker) translateSeedDraft(ctx context.Context, candidateID int64, raw []byte, budget int64) (map[string]any, error)",
	} {
		if !strings.Contains(seedSource, required) {
			t.Fatalf("seed crawler failure observability is missing %q", required)
		}
	}
	for _, function := range []string{"executeRun", "createSeedDraft", "translateSeedDraft", "submitSeedDraft"} {
		body := goFunctionBody(t, seedSource, function)
		if strings.Contains(body, "_, _ = worker.server.db.Exec") || strings.Contains(body, "_ = worker.server.db.QueryRow") {
			t.Fatalf("%s still hides a required database state failure", function)
		}
	}
}

func TestSimpleRowsValueAndCursorFailuresAreObservable(t *testing.T) {
	wantErr := errors.New("simple rows failure")
	valuesRows := &simpleRowsFailureStub{next: true, valuesErr: wantErr}
	if items, err := collectSimpleRows(valuesRows); !errors.Is(err, wantErr) || items != nil || !valuesRows.closed {
		t.Fatalf("values failure=(%v,%v,closed=%t), want nil/%v/true", items, err, valuesRows.closed, wantErr)
	}
	terminalRows := &simpleRowsFailureStub{terminalErr: wantErr}
	if items, err := collectSimpleRows(terminalRows); !errors.Is(err, wantErr) || items != nil || !terminalRows.closed {
		t.Fatalf("terminal failure=(%v,%v,closed=%t), want nil/%v/true", items, err, terminalRows.closed, wantErr)
	}
}
