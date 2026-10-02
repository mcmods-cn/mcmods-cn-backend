package antiabuse

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type botRuleQueryStub struct {
	rows pgx.Rows
	err  error
}

func (query botRuleQueryStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return query.rows, query.err
}

type botRuleRowsStub struct {
	values      []botRule
	position    int
	scanErr     error
	terminalErr error
	closed      bool
}

func (rows *botRuleRowsStub) Close()                                       { rows.closed = true }
func (rows *botRuleRowsStub) Err() error                                   { return rows.terminalErr }
func (rows *botRuleRowsStub) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (rows *botRuleRowsStub) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (rows *botRuleRowsStub) Values() ([]any, error)                       { return nil, errors.New("unused") }
func (rows *botRuleRowsStub) RawValues() [][]byte                          { return nil }
func (rows *botRuleRowsStub) Conn() *pgx.Conn                              { return nil }
func (rows *botRuleRowsStub) Next() bool {
	if rows.position >= len(rows.values) {
		rows.closed = true
		return false
	}
	rows.position++
	return true
}
func (rows *botRuleRowsStub) Scan(destinations ...any) error {
	if rows.scanErr != nil {
		return rows.scanErr
	}
	value := rows.values[rows.position-1]
	*destinations[0].(*string) = value.Kind
	*destinations[1].(*string) = value.Matcher
	*destinations[2].(*string) = value.SecretHash
	return nil
}

type riskAggregateExecStub struct {
	err   error
	calls int
}

func (execer *riskAggregateExecStub) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	execer.calls++
	return pgconn.CommandTag{}, execer.err
}

func TestSecuritySideEffectsDoNotSilentlyDiscardFailures(t *testing.T) {
	serviceRaw, err := os.ReadFile("service.go")
	if err != nil {
		t.Fatal(err)
	}
	crawlerRaw, err := os.ReadFile("crawler.go")
	if err != nil {
		t.Fatal(err)
	}
	middlewareRaw, err := os.ReadFile("../httpapi/anti_abuse_middleware.go")
	if err != nil {
		t.Fatal(err)
	}
	serviceSource := string(serviceRaw)
	crawlerSource := string(crawlerRaw)
	middlewareSource := string(middlewareRaw)
	restrictionBody := antiAbuseFunctionBody(t, serviceSource, "applyAutomaticRestriction")
	for _, forbidden := range []string{"_, _ = s.db.Exec", "settings, _ := s.Settings"} {
		if strings.Contains(restrictionBody, forbidden) {
			t.Fatalf("automatic restriction still discards a critical failure through %q", forbidden)
		}
	}
	if !strings.Contains(restrictionBody, "tx.Commit") || !strings.Contains(restrictionBody, "return") {
		t.Fatal("automatic restriction and risk-state writes are not one observable transaction")
	}

	riskWriterBody := antiAbuseFunctionBody(t, serviceSource, "writeRiskEvents")
	if strings.Contains(riskWriterBody, "_, _ = s.insertEvent") || strings.Contains(riskWriterBody, "_, _ = s.db.Exec") {
		t.Fatal("risk event writer still discards persistence failures")
	}
	if strings.Contains(riskWriterBody, "delete(aggregates, key)") && !strings.Contains(serviceSource, "flushRiskAggregates") {
		t.Fatal("daily aggregate is still deleted from memory without a successful persistence boundary")
	}

	botRulesBody := antiAbuseFunctionBody(t, crawlerSource, "botRules")
	for _, forbidden := range []string{"rows.Scan(&value.Kind, &value.Matcher, &value.SecretHash) == nil", "_ = json.Unmarshal", "return nil\n\t}"} {
		if strings.Contains(botRulesBody, forbidden) {
			t.Fatalf("bot rule loading still hides a query/decoding failure through %q", forbidden)
		}
	}
	for _, required := range []string{
		"if persistErr := s.antiAbuse.RecordDecision",
		"anti_abuse_state_unavailable",
		"class, classifyErr := s.antiAbuse.ClassifyCrawler",
		"anti_abuse_bot_rules_degraded",
		"if recordErr := s.antiAbuse.RecordCrawler",
	} {
		if !strings.Contains(middlewareSource, required) {
			t.Fatalf("HTTP failure strategy is missing %q", required)
		}
	}
}

func TestBotRuleLoadingRejectsScanAndTerminalErrors(t *testing.T) {
	wantErr := errors.New("bot rule stream interrupted")
	rows := &botRuleRowsStub{values: []botRule{{Kind: "blocked_bot", Matcher: "badbot"}}, terminalErr: wantErr}
	if values, err := loadBotRules(context.Background(), botRuleQueryStub{rows: rows}); !errors.Is(err, wantErr) || values != nil || !rows.closed {
		t.Fatalf("terminal rule failure values=%v err=%v closed=%t", values, err, rows.closed)
	}
	rows = &botRuleRowsStub{values: []botRule{{Kind: "blocked_bot"}}, scanErr: wantErr}
	if values, err := loadBotRules(context.Background(), botRuleQueryStub{rows: rows}); !errors.Is(err, wantErr) || values != nil || !rows.closed {
		t.Fatalf("scan rule failure values=%v err=%v closed=%t", values, err, rows.closed)
	}
	rows = &botRuleRowsStub{values: []botRule{{Kind: "ip_block", Matcher: "192.0.2.0/24", SecretHash: "hash"}}}
	values, err := loadBotRules(context.Background(), botRuleQueryStub{rows: rows})
	if err != nil || len(values) != 1 || values[0] != rows.values[0] || !rows.closed {
		t.Fatalf("successful rule load values=%v err=%v closed=%t", values, err, rows.closed)
	}
}

func TestRiskAggregateIsRemovedOnlyAfterPersistence(t *testing.T) {
	aggregates := map[string]riskAggregate{"key": {date: "2026-08-23", action: "comment.create", outcome: "delay", crawler: "human", count: 7}}
	wantErr := errors.New("daily aggregate unavailable")
	execer := &riskAggregateExecStub{err: wantErr}
	if failed := flushRiskAggregates(context.Background(), execer, aggregates); failed != 1 || len(aggregates) != 1 || execer.calls != 1 {
		t.Fatalf("failed flush failed=%d retained=%d calls=%d", failed, len(aggregates), execer.calls)
	}
	execer.err = nil
	if failed := flushRiskAggregates(context.Background(), execer, aggregates); failed != 0 || len(aggregates) != 0 || execer.calls != 2 {
		t.Fatalf("successful retry failed=%d retained=%d calls=%d", failed, len(aggregates), execer.calls)
	}
	aggregates["a"] = riskAggregate{count: 2}
	aggregates["b"] = riskAggregate{count: 3}
	if dropped := discardRiskAggregates(aggregates); dropped != 5 || len(aggregates) != 0 {
		t.Fatalf("discarded aggregates=%d retained=%d; want 5/0", dropped, len(aggregates))
	}
}

func antiAbuseFunctionBody(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "func ")
	for start >= 0 {
		candidate := source[start:]
		lineEnd := strings.IndexByte(candidate, '\n')
		if lineEnd < 0 {
			break
		}
		if strings.Contains(candidate[:lineEnd], name+"(") {
			depth := 0
			opened := false
			for index, character := range candidate {
				switch character {
				case '{':
					depth++
					opened = true
				case '}':
					depth--
					if opened && depth == 0 {
						return candidate[:index+1]
					}
				}
			}
			break
		}
		next := strings.Index(candidate[lineEnd:], "func ")
		if next < 0 {
			break
		}
		start += lineEnd + next
	}
	t.Fatalf("function %s not found", name)
	return ""
}
