package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

type arch027Rows struct {
	remaining int
	scan      func(...any) error
	err       error
	closed    bool
}

func (rows *arch027Rows) Next() bool {
	if rows.remaining == 0 {
		return false
	}
	rows.remaining--
	return true
}

func (rows *arch027Rows) Scan(destinations ...any) error {
	if rows.scan == nil {
		return nil
	}
	return rows.scan(destinations...)
}

func (rows *arch027Rows) Err() error { return rows.err }

func (rows *arch027Rows) Close() { rows.closed = true }

func TestEconomyAndActivityCollectorsRejectTerminalRowErrors(t *testing.T) {
	databaseFailure := errors.New("ARCH-027 terminal database failure")
	balances := &arch027Rows{err: databaseFailure}
	if _, err := collectEconomyBalances(balances); !errors.Is(err, databaseFailure) {
		t.Fatalf("balance terminal error = %v", err)
	}
	if !balances.closed {
		t.Fatal("balance rows were not closed")
	}
	activity := &arch027Rows{err: databaseFailure}
	if _, err := collectAdminActivityEvents(activity); !errors.Is(err, databaseFailure) {
		t.Fatalf("activity terminal error = %v", err)
	}
	if !activity.closed {
		t.Fatal("activity rows were not closed")
	}
}

func TestStoredJSONObjectRejectsSilentEmptyFallbacks(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", `"text"`, "1", "{"} {
		if value, err := decodeStoredJSONObject([]byte(raw), "fixture"); err == nil {
			t.Fatalf("decodeStoredJSONObject(%q) = %#v, nil", raw, value)
		}
	}
	value, err := decodeStoredJSONObject([]byte(`{"zh-CN":{"name":"名称"}}`), "fixture")
	if err != nil || value == nil {
		t.Fatalf("valid object = %#v, %v", value, err)
	}
}

type arch027EconomyConfigReader struct {
	raw []byte
	err error
}

func (reader arch027EconomyConfigReader) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return arch027EconomyConfigRow(reader)
}

type arch027EconomyConfigRow arch027EconomyConfigReader

func (row arch027EconomyConfigRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*(destinations[0].(*[]byte)) = append([]byte(nil), row.raw...)
	return nil
}

func TestEconomyConfigDefaultsOnlyForMissingSetting(t *testing.T) {
	missing, err := loadEconomyConfig(context.Background(), arch027EconomyConfigReader{err: pgx.ErrNoRows})
	if err != nil || missing.Checkin.Currency != defaultEconomyConfig().Checkin.Currency {
		t.Fatalf("missing setting = %#v, %v", missing, err)
	}
	for _, fixture := range []arch027EconomyConfigReader{
		{err: errors.New("database unavailable")},
		{raw: []byte("null")},
		{raw: []byte("[]")},
	} {
		if value, loadErr := loadEconomyConfig(context.Background(), fixture); loadErr == nil {
			t.Fatalf("corrupt setting was accepted as %#v", value)
		}
	}
}

func TestEconomyAndTaskReadPathsDoNotDiscardErrors(t *testing.T) {
	for _, file := range []string{"economy_handlers.go", "progression_handlers.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		for _, discarded := range []string{"_ = json.Unmarshal", "raw, _ := json.Marshal"} {
			if strings.Contains(source, discarded) {
				t.Fatalf("%s still contains discarded JSON result %q", file, discarded)
			}
		}
	}
}
