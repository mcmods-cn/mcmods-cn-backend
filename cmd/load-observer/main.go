package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
)

type report struct {
	StartedAt       time.Time `json:"startedAt"`
	DurationSeconds int       `json:"durationSeconds"`
	Samples         int       `json:"samples"`
	MaxConnections  int       `json:"maxConnections"`
	MaxActive       int       `json:"maxActive"`
	MaxLockWaiters  int       `json:"maxLockWaiters"`
	MaxSlowQueries  int       `json:"maxSlowQueries"`
	Errors          int       `json:"errors"`
}

type observerPool interface {
	Ping(context.Context) error
	QueryRow(context.Context, string, ...any) pgx.Row
	Close()
}

type observerRuntime struct {
	getenv    func(string) string
	openPool  func(context.Context, string) (observerPool, error)
	marshal   func(any, string, string) ([]byte, error)
	writeFile func(string, []byte, os.FileMode) error
	stdout    io.Writer
	now       func() time.Time
	wait      func(context.Context, time.Duration) error
}

func main() {
	if err := runLoadObserver(context.Background(), defaultObserverRuntime()); err != nil {
		log.Fatal(err)
	}
}

func defaultObserverRuntime() observerRuntime {
	return observerRuntime{
		getenv: os.Getenv,
		openPool: func(ctx context.Context, connectionString string) (observerPool, error) {
			return pgxpool.New(ctx, connectionString)
		},
		marshal:   jsonMarshalIndent,
		writeFile: os.WriteFile,
		stdout:    os.Stdout,
		now:       time.Now,
		wait:      waitForObserverInterval,
	}
}

func jsonMarshalIndent(value any, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(value, prefix, indent)
}

func waitForObserverInterval(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runLoadObserver(parent context.Context, runtime observerRuntime) error {
	durationSeconds, _ := strconv.Atoi(runtime.getenv("MCMODS_OBSERVER_SECONDS"))
	if durationSeconds <= 0 || durationSeconds > 3600 {
		durationSeconds = 60
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(durationSeconds+10)*time.Second)
	defer cancel()
	pool, err := runtime.openPool(ctx, config.Load().DB.ConnString())
	if err != nil {
		return fmt.Errorf("create load observer database pool: %w", err)
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect load observer database: %w", err)
	}

	startedAt := runtime.now()
	result := report{StartedAt: startedAt.UTC(), DurationSeconds: durationSeconds}
	deadline := startedAt.Add(time.Duration(durationSeconds) * time.Second)
	var samplingErr error
	for runtime.now().Before(deadline) {
		var connections, active, waiting, slow int
		err = pool.QueryRow(ctx, `select count(*),count(*) filter(where state='active'),count(*) filter(where wait_event_type='Lock'),
			count(*) filter(where state='active' and query_start<now()-interval '1 second') from pg_stat_activity where datname=current_database()`).Scan(&connections, &active, &waiting, &slow)
		if err != nil {
			result.Errors++
			if samplingErr == nil {
				samplingErr = err
			}
		} else {
			result.Samples++
			result.MaxConnections = max(result.MaxConnections, connections)
			result.MaxActive = max(result.MaxActive, active)
			result.MaxLockWaiters = max(result.MaxLockWaiters, waiting)
			result.MaxSlowQueries = max(result.MaxSlowQueries, slow)
		}
		remaining := deadline.Sub(runtime.now())
		if remaining <= 0 {
			break
		}
		interval := min(250*time.Millisecond, remaining)
		if waitErr := runtime.wait(ctx, interval); waitErr != nil {
			result.Errors++
			if samplingErr == nil {
				samplingErr = waitErr
			}
			break
		}
	}
	payload, err := runtime.marshal(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode load observer report: %w", err)
	}
	if output := runtime.getenv("MCMODS_OBSERVER_OUTPUT"); output != "" {
		if err = runtime.writeFile(output, append(payload, '\n'), 0o600); err != nil {
			return fmt.Errorf("write load observer report %q: %w", output, err)
		}
	}
	if _, err = fmt.Fprintln(runtime.stdout, string(payload)); err != nil {
		return fmt.Errorf("write load observer stdout: %w", err)
	}
	if samplingErr != nil {
		return fmt.Errorf("load observer sampling failed after %d successful samples and %d errors: %w",
			result.Samples, result.Errors, samplingErr)
	}
	return nil
}
