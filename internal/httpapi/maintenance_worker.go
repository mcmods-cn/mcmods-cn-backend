package httpapi

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const maintenanceBatchSize = 1000

// MaintenanceWorker keeps expiration cleanup outside user-facing request
// transactions. Each delete is bounded so a large backlog cannot hold a long
// table lock or delay API responses.
type MaintenanceWorker struct {
	db *pgxpool.Pool
}

func NewMaintenanceWorker(db *pgxpool.Pool) *MaintenanceWorker {
	return &MaintenanceWorker{db: db}
}

func (worker *MaintenanceWorker) Start(ctx context.Context) {
	if worker == nil || worker.db == nil {
		return
	}
	go worker.run(ctx)
}

func (worker *MaintenanceWorker) run(ctx context.Context) {
	worker.prune(ctx)
	ticker := time.NewTicker(10 * time.Minute)
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

func (worker *MaintenanceWorker) prune(ctx context.Context) {
	pruneCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for _, statement := range []string{
		`delete from user_drafts where id in (
			select id from user_drafts where expires_at<=now() order by expires_at limit $1
		)`,
		`delete from yggdrasil_join_sessions where server_id in (
			select server_id from yggdrasil_join_sessions where expires_at<=now() order by expires_at limit $1
		)`,
	} {
		for pruneCtx.Err() == nil {
			command, err := worker.db.Exec(pruneCtx, statement, maintenanceBatchSize)
			if err != nil {
				log.Printf("background expiration cleanup failed: %v", err)
				break
			}
			if command.RowsAffected() < maintenanceBatchSize {
				break
			}
		}
	}
}
