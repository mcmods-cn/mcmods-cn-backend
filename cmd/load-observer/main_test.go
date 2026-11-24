package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeObserverPool struct {
	queryErr error
	closed   bool
}

func (pool *fakeObserverPool) QueryRow(context.Context, string, ...any) pgx.Row {
	return fakeObserverRow{err: pool.queryErr}
}

func (pool *fakeObserverPool) Close() { pool.closed = true }

type fakeObserverRow struct{ err error }

func (row fakeObserverRow) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	for index, value := range []int{4, 2, 1, 1} {
		*destinations[index].(*int) = value
	}
	return nil
}

func TestLoadObserverPropagatesDatabaseSamplingEncodingAndOutputFailures(t *testing.T) {
	baseTime := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	newRuntime := func(pool *fakeObserverPool) observerRuntime {
		current := baseTime
		return observerRuntime{
			getenv: func(key string) string {
				if key == "MCMODS_OBSERVER_SECONDS" {
					return "1"
				}
				return ""
			},
			openPool: func(context.Context) (observerPool, error) { return pool, nil },
			marshal:  jsonMarshalIndent,
			writeFile: func(string, []byte, os.FileMode) error {
				return nil
			},
			stdout: io.Discard,
			now:    func() time.Time { return current },
			wait: func(context.Context, time.Duration) error {
				current = current.Add(250 * time.Millisecond)
				return nil
			},
		}
	}

	connectionFailure := newRuntime(nil)
	unavailable := errors.New("database unavailable")
	connectionFailure.openPool = func(context.Context) (observerPool, error) { return nil, unavailable }
	if err := runLoadObserver(context.Background(), connectionFailure); !errors.Is(err, unavailable) {
		t.Fatalf("database failure = %v", err)
	}

	samplingFailure := newRuntime(&fakeObserverPool{queryErr: errors.New("sample timeout")})
	if err := runLoadObserver(context.Background(), samplingFailure); err == nil || !strings.Contains(err.Error(), "sample timeout") {
		t.Fatalf("sampling failure = %v", err)
	}

	encodingFailure := newRuntime(&fakeObserverPool{})
	encodingFailure.marshal = func(any, string, string) ([]byte, error) {
		return nil, errors.New("encode failed")
	}
	if err := runLoadObserver(context.Background(), encodingFailure); err == nil || !strings.Contains(err.Error(), "encode failed") {
		t.Fatalf("encoding failure = %v", err)
	}

	outputFailure := newRuntime(&fakeObserverPool{})
	outputFailure.getenv = func(key string) string {
		switch key {
		case "MCMODS_OBSERVER_SECONDS":
			return "1"
		case "MCMODS_OBSERVER_OUTPUT":
			return "missing/report.json"
		default:
			return ""
		}
	}
	outputFailure.writeFile = func(string, []byte, os.FileMode) error {
		return errors.New("output denied")
	}
	if err := runLoadObserver(context.Background(), outputFailure); err == nil || !strings.Contains(err.Error(), "output denied") {
		t.Fatalf("output failure = %v", err)
	}
}

func TestLoadObserverWritesSuccessfulReportAndClosesPool(t *testing.T) {
	pool := &fakeObserverPool{}
	current := time.Date(2026, 8, 22, 0, 0, 0, 0, time.UTC)
	var stdout bytes.Buffer
	runtime := observerRuntime{
		getenv: func(key string) string {
			if key == "MCMODS_OBSERVER_SECONDS" {
				return "1"
			}
			return ""
		},
		openPool: func(context.Context) (observerPool, error) { return pool, nil },
		marshal:  jsonMarshalIndent,
		writeFile: func(string, []byte, os.FileMode) error {
			return nil
		},
		stdout: &stdout,
		now:    func() time.Time { return current },
		wait: func(context.Context, time.Duration) error {
			current = current.Add(250 * time.Millisecond)
			return nil
		},
	}
	if err := runLoadObserver(context.Background(), runtime); err != nil {
		t.Fatal(err)
	}
	if !pool.closed {
		t.Fatal("observer pool was not closed")
	}
	for _, expected := range []string{`"samples": 4`, `"maxConnections": 4`, `"maxLockWaiters": 1`} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("report %s does not contain %s", stdout.String(), expected)
		}
	}
}

func TestLoadObserverDoesNotIncludeDatabaseCredentialsInConfigurationErrors(t *testing.T) {
	t.Setenv("MCMODS_SKIP_DOTENV", "true")
	const user = "synthetic-observer-user"
	const password = "synthetic-observer-secret"
	t.Setenv("DATABASE_URL", "postgres://"+user+":"+password+"@127.0.0.1:invalid/fixture")
	err := runLoadObserver(context.Background(), defaultObserverRuntime())
	if err == nil || !strings.Contains(err.Error(), "parse database configuration") {
		t.Fatal("expected a classified configuration failure")
	}
	for _, private := range []string{user, password, "postgres://"} {
		if strings.Contains(err.Error(), private) {
			t.Fatal("observer reported database connection data")
		}
	}
}

func TestLoadObserverConfigurationAndFailureExitAreDocumented(t *testing.T) {
	environment, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	documentation, err := os.ReadFile("../../docs/load-observer.md")
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"DATABASE_URL=", "MCMODS_OBSERVER_SECONDS=60", "MCMODS_OBSERVER_OUTPUT="} {
		if !bytes.Contains(environment, []byte(required)) {
			t.Fatalf(".env.example is missing %q", required)
		}
	}
	for _, required := range []string{"DATABASE_URL", "read-only", "exit nonzero", "MCMODS_OBSERVER_OUTPUT"} {
		if !bytes.Contains(documentation, []byte(required)) {
			t.Fatalf("load observer documentation is missing %q", required)
		}
	}
	for _, required := range []string{"if err := runLoadObserver", "log.Fatal(err)"} {
		if !bytes.Contains(source, []byte(required)) {
			t.Fatalf("main does not convert run failure to a nonzero exit: missing %q", required)
		}
	}
}
