package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/queue"
)

// Commit the reservation before touching SMTP. A crash leaves the event
// inspectable and prevents a second worker from sending an uncertain delivery.
func deliverNotificationEmail(ctx context.Context, db *pgxpool.Pool, send func() error) error {
	eventID := queue.EventIDFromContext(ctx)
	if eventID == "" {
		return errors.New("notification email is missing its delivery event")
	}
	tag, err := db.Exec(ctx, `insert into processed_events(consumer,event_id,event_type)
		values('notification.email',$1,'smtp.attempt') on conflict(consumer,event_id) do nothing`, eventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var state string
		if err = db.QueryRow(ctx, `select event_type from processed_events where consumer='notification.email' and event_id=$1`, eventID).Scan(&state); err != nil {
			return err
		}
		if state == "smtp.accepted" {
			return nil
		}
		return mailer.ErrAcceptanceUnknown
	}

	sendErr := send()
	if errors.Is(sendErr, mailer.ErrAcceptanceUnknown) {
		return sendErr
	}
	settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if sendErr == nil {
		tag, err = db.Exec(settle, `update processed_events set event_type='smtp.accepted',processed_at=now()
			where consumer='notification.email' and event_id=$1 and event_type='smtp.attempt'`, eventID)
	} else {
		// A known rejection (including preflight) can be retried normally.
		tag, err = db.Exec(settle, `delete from processed_events
			where consumer='notification.email' and event_id=$1 and event_type='smtp.attempt'`, eventID)
	}
	if err == nil && tag.RowsAffected() != 1 {
		err = errors.New("notification email reservation changed before settlement")
	}
	return errors.Join(sendErr, err)
}
