package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

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

func main() {
	durationSeconds, _ := strconv.Atoi(os.Getenv("MCMODS_OBSERVER_SECONDS"))
	if durationSeconds <= 0 || durationSeconds > 3600 { durationSeconds = 60 }
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(durationSeconds+10)*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.Load().DB.ConnString())
	if err != nil { panic(err) }
	defer pool.Close()
	result := report{StartedAt: time.Now().UTC(), DurationSeconds: durationSeconds}
	deadline := time.Now().Add(time.Duration(durationSeconds) * time.Second)
	for time.Now().Before(deadline) {
		var connections, active, waiting, slow int
		err = pool.QueryRow(ctx, `select count(*),count(*) filter(where state='active'),count(*) filter(where wait_event_type='Lock'),
			count(*) filter(where state='active' and query_start<now()-interval '1 second') from pg_stat_activity where datname=current_database()`).Scan(&connections, &active, &waiting, &slow)
		if err != nil { result.Errors++ } else {
			result.Samples++
			result.MaxConnections = max(result.MaxConnections, connections)
			result.MaxActive = max(result.MaxActive, active)
			result.MaxLockWaiters = max(result.MaxLockWaiters, waiting)
			result.MaxSlowQueries = max(result.MaxSlowQueries, slow)
		}
		time.Sleep(250 * time.Millisecond)
	}
	payload, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(payload))
	if output := os.Getenv("MCMODS_OBSERVER_OUTPUT"); output != "" { _ = os.WriteFile(output, append(payload, '\n'), 0o600) }
}
