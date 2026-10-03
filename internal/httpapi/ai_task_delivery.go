package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// t is an application-owned ai_tasks alias. Read only the latest event: an old
// dead event must not advertise recovery after a newer delivery was accepted.
const aiTaskDeliveryFailureProjectionSQL = `(select case when event.status='dead' then jsonb_build_object(
	'deadLetterId',dead.id::text,'stage','publish','retryable',
	t.status='queued' and t.started_at is null and t.input_tokens=0 and t.output_tokens=0
	and event.published_at is null and dead.id is not null and dead.replayed_at is null
	and not exists(select 1 from ai_task_logs log where
	 (log.task_id=t.id and log.event='task_started') or
	 (log.event='provider_request_usage' and log.payload->>'taskId'=t.id::text))) else null end
	from nats_outbox event left join dead_letter_events dead on dead.event_id=event.event_id
	 and dead.failure_stage='publish'
	where event.aggregate_type='ai_task' and event.aggregate_id=t.task_uid
	order by event.id desc limit 1) as delivery_failure`

var errAITaskDeliveryRetryUnavailable = errors.New("AI task publisher delivery cannot be retried")

func retryAITaskDeliveryTx(ctx context.Context, tx pgx.Tx, taskUID string, actorID int64) (string, error) {
	var taskID int64
	var eligible bool
	if err := tx.QueryRow(ctx, `select id,status='queued' and started_at is null and input_tokens=0 and output_tokens=0
	 from ai_tasks where task_uid=$1 for update`, taskUID).Scan(&taskID, &eligible); err != nil {
		return "", err
	}
	if !eligible {
		return "", errAITaskDeliveryRetryUnavailable
	}
	if err := tx.QueryRow(ctx, `select not exists(select 1 from ai_task_logs where
	 (task_id=$1 and event='task_started') or
	 (event=$2 and payload->>'taskId'=$1::bigint::text))`, taskID, aiRequestBudgetEvent).Scan(&eligible); err != nil {
		return "", err
	}
	if !eligible {
		return "", errAITaskDeliveryRetryUnavailable
	}
	var eventID, status string
	var published bool
	if err := tx.QueryRow(ctx, `select event_id,status,published_at is not null from nats_outbox
	 where aggregate_type='ai_task' and aggregate_id=$1 order by id desc limit 1`, taskUID).Scan(&eventID, &status, &published); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errAITaskDeliveryRetryUnavailable
		}
		return "", err
	}
	if status != "dead" || published {
		return "", errAITaskDeliveryRetryUnavailable
	}
	// Match the existing administrator replay lock order (dead letter, outbox).
	// Recheck the outbox atomically after obtaining the dead-letter lock.
	var deadID int64
	if err := tx.QueryRow(ctx, `select id from dead_letter_events where event_id=$1 and failure_stage='publish'
	 and replayed_at is null for update`, eventID).Scan(&deadID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errAITaskDeliveryRetryUnavailable
		}
		return "", err
	}
	tag, err := tx.Exec(ctx, `update nats_outbox set status='pending',attempts=0,last_error='',available_at=now(),
	 locked_at=null,locked_by='',updated_at=now() where event_id=$1 and status='dead' and published_at is null
	 and id=(select id from nats_outbox where aggregate_type='ai_task' and aggregate_id=$2 order by id desc limit 1)`, eventID, taskUID)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", errAITaskDeliveryRetryUnavailable
	}
	if _, err = tx.Exec(ctx, `update dead_letter_events set replayed_at=now() where id=$1`, deadID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `update ai_tasks set queued_at=now(),updated_at=now() where id=$1`, taskID); err != nil {
		return "", err
	}
	_, err = tx.Exec(ctx, `insert into app_logs(category,level,action,target,payload)
	 values('security','warning','ai_delivery_retried',$1,jsonb_build_object(
	 'actorUserId',$2::bigint,'eventId',$3::text,'deadLetterId',$4::bigint))`, taskUID, actorID, eventID, deadID)
	return eventID, err
}

func (s *Server) retryAITaskDelivery(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	if claims.Subject <= 0 || !claimsAllow(claims, "ai.read") || !claimsAllow(claims, "ai.task.enqueue") {
		writeAPIError(w, http.StatusForbidden, "AI_DELIVERY_RETRY_FORBIDDEN", "permission denied", 0, nil)
		return
	}
	taskUID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if taskUID == "" || len(taskUID) > 80 {
		writeAPIError(w, http.StatusBadRequest, "AI_TASK_ID_INVALID", "invalid AI task id", 0, nil)
		return
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "AI_DELIVERY_RETRY_FAILED", "failed to retry AI delivery", 0, nil)
		return
	}
	defer tx.Rollback(r.Context())
	eventID, err := retryAITaskDeliveryTx(r.Context(), tx, taskUID, claims.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		writeAPIError(w, http.StatusNotFound, "AI_TASK_NOT_FOUND", "AI task not found", 0, nil)
		return
	}
	if errors.Is(err, errAITaskDeliveryRetryUnavailable) {
		writeAPIError(w, http.StatusConflict, "AI_DELIVERY_RETRY_UNAVAILABLE", "AI task delivery is not eligible for retry", 0, nil)
		return
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeAPIError(w, http.StatusInternalServerError, "AI_DELIVERY_RETRY_FAILED", "failed to retry AI delivery", 0, nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"id": taskUID, "status": "queued", "deliveryQueued": true, "eventId": eventID})
}
