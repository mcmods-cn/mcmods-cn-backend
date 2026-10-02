package httpapi

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/queue"
)

func enqueueNotificationTaskTx(ctx context.Context, tx pgx.Tx, eventType, aggregateType, aggregateID, traceID string, event notificationEvent) error {
	_, err := queue.EnqueueTx(ctx, tx, notificationTaskCode, eventType, aggregateType, aggregateID, traceID, event)
	return err
}

func enqueueTemplatedNotificationTx(
	ctx context.Context,
	tx pgx.Tx,
	eventType string,
	recipientID, actorID int64,
	templateKey string,
	values map[string]string,
	data map[string]any,
	traceID string,
) error {
	if recipientID <= 0 || templateKey == "" {
		return fmt.Errorf("templated notification recipient and template are required")
	}
	return enqueueNotificationTaskTx(ctx, tx, eventType, "user", strconv.FormatInt(recipientID, 10), traceID, notificationEvent{
		Action: "direct", RecipientID: recipientID, ActorID: actorID, Kind: "system",
		TemplateKey: templateKey, TemplateValues: values, Data: data,
	})
}

func enqueueUserEmailTx(ctx context.Context, tx pgx.Tx, userID int64, subject, body string) error {
	if userID <= 0 {
		return nil
	}
	return enqueueNotificationTaskTx(ctx, tx, "notification.email.requested", "user", strconv.FormatInt(userID, 10), "", notificationEvent{
		Action: "email", RecipientID: userID, Title: subject, Body: body,
	})
}

// enqueueNotificationTask persists every task through the authoritative
// PostgreSQL Outbox. Callers that own a business transaction use
// enqueueNotificationTaskTx so the business fact and delivery intent commit
// atomically.
func (s *Server) enqueueNotificationTask(ctx context.Context, event notificationEvent) error {
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
	if err = enqueueNotificationTaskTx(ctx, tx, "notification."+event.Action, aggregateType, aggregateID, "", event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
