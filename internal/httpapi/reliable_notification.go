package httpapi

import (
	"context"
	"strconv"

	"mcmods-cn-backend/internal/queue"
)

// enqueueNotificationTask persists the delivery intent before asking NATS to
// wake a worker. When Outbox is enabled, a NATS outage cannot lose the task.
// Callers that already own a business transaction should use queue.EnqueueTx
// directly so the business fact and delivery intent commit atomically.
func (s *Server) enqueueNotificationTask(ctx context.Context, event notificationEvent) error {
	if !s.cfg.NATS.OutboxEnabled {
		return s.queue.PublishTask(ctx, notificationTaskCode, event)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	aggregateType := "site"
	aggregateID := "all"
	if event.RecipientID > 0 {
		aggregateType = "user"
		aggregateID = strconv.FormatInt(event.RecipientID, 10)
	}
	if _, err = queue.EnqueueTx(ctx, tx, notificationTaskCode, "notification."+event.Action, aggregateType, aggregateID, "", event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
