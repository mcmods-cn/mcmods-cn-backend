// Package auditdiag contains bounded read-only audit queries, never repairs.
package auditdiag

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReviewResultScope struct {
	Kind     string    `json:"kind"`
	PublicID string    `json:"public_id"`
	Since    time.Time `json:"since_inclusive"`
	Until    time.Time `json:"until_exclusive"`
	Limit    int       `json:"limit"`
}

func (s ReviewResultScope) Validate() error {
	switch s.Kind {
	case "modpack", "plugin", "map", "resource_pack", "shader_pack", "datapack", "addon", "creator":
	default:
		return errors.New("unsupported review aggregate kind")
	}
	if !regexp.MustCompile(`^[a-z0-9]{9}$`).MatchString(s.PublicID) {
		return errors.New("require an exact nine-character public aggregate identity")
	}
	if s.Since.IsZero() || !s.Since.Before(s.Until) || s.Until.Sub(s.Since) > 31*24*time.Hour || s.Limit < 1 || s.Limit > 1000 {
		return errors.New("require a positive explicit time window <=31 days and limit 1..1000")
	}
	return nil
}

type ReviewResult struct {
	RevisionID              string    `json:"revision_public_id"`
	DecisionID              string    `json:"decision_public_id"`
	Decision                string    `json:"decision"`
	DecidedAt               time.Time `json:"decided_at"`
	RecipientExists         bool      `json:"recipient_exists"`
	ObservableIntents       int64     `json:"observable_result_intents"`
	ObservableNotifications int64     `json:"observable_result_notifications"`
	MissingCandidate        bool      `json:"missing_result_candidate"`
}

// Old result payloads identify their aggregate and recipient but not revision.
// Counts therefore describe matching rows in the decision's time interval,
// never proof of exact revision delivery or authority to reconstruct an event.
const ReviewResultsSQL = `with decisions as (
 select revision.public_id as revision_public_id,event.public_id as decision_public_id,
 event.id,event.event_type,event.created_at,request.submitted_by,
 lead(event.created_at) over(order by event.created_at,event.id) as next_decision_at
 from content_revisions revision
 join change_requests request on request.proposed_revision_id=revision.id
 join review_events event on event.change_request_id=request.id
 where revision.aggregate_key=$2
 and revision.aggregate_type=case when $1 in ('modpack','creator') then $1 else 'simple_project' end
 and ($1 in ('modpack','creator') or revision.snapshot->>'projectType'=$1)
 and event.event_type in ('approved','rejected')
), scoped as (
 select * from decisions where created_at>=$3 and created_at<$4
 order by created_at,id limit $5
)
 select revision_public_id,decision_public_id,event_type,created_at,
 exists(select 1 from users where id=scoped.submitted_by),
 (select count(*) from nats_outbox intent
  where intent.aggregate_type='user' and intent.aggregate_id=scoped.submitted_by::text
  and intent.event_type='notification.direct' and intent.subject='notifications'
  and intent.created_at>=scoped.created_at and intent.created_at<least(coalesce(next_decision_at,$4),$4)
  and intent.payload->>'templateKey'='review_'||scoped.event_type
  and intent.payload->>'recipientId'=scoped.submitted_by::text
  and intent.payload->'data'->>(case when $1='modpack' then 'modpackId' when $1='creator' then 'creatorId' else 'projectId' end)=$2),
 (select count(*) from notifications notification
  where notification.recipient_id=scoped.submitted_by and notification.template_key='review_'||scoped.event_type
  and notification.created_at>=scoped.created_at and notification.created_at<least(coalesce(next_decision_at,$4),$4)
  and notification.data->>(case when $1='modpack' then 'modpackId' when $1='creator' then 'creatorId' else 'projectId' end)=$2)
 from scoped order by created_at,id`

type reviewQuery interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func ReadReviewResults(ctx context.Context, q reviewQuery, scope ReviewResultScope) ([]ReviewResult, error) {
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	rows, err := q.Query(ctx, ReviewResultsSQL, scope.Kind, scope.PublicID, scope.Since, scope.Until, scope.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ReviewResult, 0)
	for rows.Next() {
		var item ReviewResult
		if err = rows.Scan(&item.RevisionID, &item.DecisionID, &item.Decision, &item.DecidedAt, &item.RecipientExists, &item.ObservableIntents, &item.ObservableNotifications); err != nil {
			return nil, err
		}
		item.MissingCandidate = item.RecipientExists && item.ObservableIntents == 0 && item.ObservableNotifications == 0
		items = append(items, item)
	}
	return items, rows.Err()
}
