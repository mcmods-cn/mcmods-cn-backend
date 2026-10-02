package httpapi

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	logRetentionInterval  = 10 * time.Minute
	logRetentionRunBudget = 30 * time.Second
	logRetentionMaxRounds = 32
)

type LogRetentionWorker struct {
	db *pgxpool.Pool
}

func NewLogRetentionWorker(db *pgxpool.Pool) *LogRetentionWorker {
	return &LogRetentionWorker{db: db}
}

func (worker *LogRetentionWorker) Start(ctx context.Context) {
	if worker == nil || worker.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *LogRetentionWorker) run(ctx context.Context) {
	worker.prune(ctx)
	ticker := time.NewTicker(logRetentionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.prune(ctx)
		}
	}
}

func (worker *LogRetentionWorker) prune(ctx context.Context) map[string]int64 {
	deleted, err := worker.pruneWithError(ctx)
	if err != nil && ctx.Err() == nil {
		log.Printf("background log retention cleanup failed: %v", err)
	}
	return deleted
}

func (worker *LogRetentionWorker) pruneWithError(ctx context.Context) (map[string]int64, error) {
	deleted := map[string]int64{}
	if worker == nil || worker.db == nil {
		return deleted, nil
	}
	runCtx, cancel := context.WithTimeout(ctx, logRetentionRunBudget)
	defer cancel()
	connection, err := worker.db.Acquire(runCtx)
	if err != nil {
		return deleted, fmt.Errorf("acquire log retention connection: %w", err)
	}
	var locked bool
	if err = connection.QueryRow(runCtx, `select pg_try_advisory_lock(hashtext('mcmods-log-retention'))`).Scan(&locked); err != nil {
		connection.Release()
		return deleted, fmt.Errorf("acquire log retention lease: %w", err)
	}
	if !locked {
		connection.Release()
		return deleted, nil
	}
	defer releaseLogRetentionLock(connection)

	retention, err := loadLogRetentionConfig(runCtx, connection)
	if err != nil {
		return deleted, err
	}
	if !retention.Enabled {
		return deleted, nil
	}
	statements := logCleanupStatements(retention, logCleanupBatchSize)
	for round := 0; round < logRetentionMaxRounds && runCtx.Err() == nil; round++ {
		fullBatch := false
		for _, statement := range statements {
			tag, executeErr := connection.Exec(runCtx, statement.SQL, statement.Args...)
			if executeErr != nil {
				return deleted, fmt.Errorf("clean %s logs: %w", statement.Label, executeErr)
			}
			rows := tag.RowsAffected()
			deleted[statement.Label] += rows
			if rows == logCleanupBatchSize {
				fullBatch = true
			}
		}
		if !fullBatch {
			return deleted, nil
		}
	}
	if err = runCtx.Err(); err != nil {
		return deleted, fmt.Errorf("log retention run budget: %w", err)
	}
	return deleted, nil
}

func releaseLogRetentionLock(connection *pgxpool.Conn) {
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	err := connection.QueryRow(unlockCtx, `select pg_advisory_unlock(hashtext('mcmods-log-retention'))`).Scan(&unlocked)
	if err == nil && unlocked {
		connection.Release()
		return
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	_ = connection.Hijack().Close(closeCtx)
}
