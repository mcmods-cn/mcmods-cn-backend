package httpapi

import (
	"context"

	"mcmods-cn-backend/internal/queue"
)

// Standalone database workers can still operate without an injected client.
// The application shares the active policy with its broker and outbox paths.
func runDatabaseRecoveryTask(ctx context.Context, client *queue.Client, code string, run func(context.Context) error) error {
	if client == nil {
		return run(ctx)
	}
	return client.RunTask(ctx, code, run)
}
