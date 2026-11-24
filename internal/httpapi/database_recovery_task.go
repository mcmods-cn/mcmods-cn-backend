package httpapi

import (
	"context"
	"time"

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

func startDatabaseJobHeartbeat(parent context.Context, interval time.Duration, renew func(context.Context) error) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := renew(ctx); err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return ctx, func() {
		cancel(context.Canceled)
		<-done
	}
}
