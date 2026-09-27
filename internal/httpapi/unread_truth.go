package httpapi

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/querycache"
)

type unreadTruthItem struct {
	UserID   int64
	PublicID string
	Summary  querycache.UnreadSummary
}

const unreadTruthInternalUsersSQL = `with selected_users as materialized (
	select id,public_id from users where status='active' and id=any($1::bigint[])
)` + unreadTruthSQLTail

const unreadTruthAnyInternalUsersSQL = `with selected_users as materialized (
	select id,public_id from users where id=any($1::bigint[])
)` + unreadTruthSQLTail

// unreadTruthSQLTail separates user-specific facts from the shared broadcast
// baseline. New users read the singleton count in O(1). Users with a read
// watermark count the smaller live ID range, plus the normally empty set of
// older broadcasts changed after that watermark. Sparse explicit receipts are
// then applied as corrections to the baseline.
const unreadTruthSQLTail = `,
targets as materialized (
	select selected.id user_id,selected.public_id,watermark.max_notification_id,watermark.read_at
	from selected_users selected
	left join notification_read_watermarks watermark on watermark.user_id=selected.id
),
direct_notification_counts as (
	select target.user_id,count(*)::bigint unread
	from notifications n
	join targets target on target.user_id=n.recipient_id
	left join notification_receipts receipt on receipt.notification_id=n.id and receipt.user_id=target.user_id
	where n.recipient_id=any($1::bigint[]) and (
		(receipt.notification_id is not null and receipt.read_at is null)
		or (receipt.notification_id is null and not (
			n.id<=coalesce(target.max_notification_id,0)
			and n.updated_at<=coalesce(target.read_at,'-infinity'::timestamptz)
		))
	)
	group by target.user_id
),
message_counts as (
	select message.recipient_id user_id,count(*)::bigint unread
	from direct_messages message
	where message.read_at is null and message.recipient_id=any($1::bigint[])
	group by message.recipient_id
),
broadcast_baselines as (
	select target.user_id,case when target.max_notification_id is null then state.live_count else
		(case when target.max_notification_id<=state.max_notification_id/2 then
			state.live_count-(select count(*) from notifications prefix
				where prefix.recipient_id is null and prefix.id<=target.max_notification_id)
		else (select count(*) from notifications suffix
			where suffix.recipient_id is null and suffix.id>target.max_notification_id) end)
		+(select count(*) from notifications changed
			where changed.recipient_id is null and changed.id<=target.max_notification_id
				and changed.updated_at>target.read_at)
	end::bigint baseline
	from targets target cross join notification_broadcast_state state
	where state.singleton
),
broadcast_receipt_corrections as (
	select target.user_id,coalesce(sum(case
		when receipt.read_at is null and not (
			target.max_notification_id is null or notification.id>target.max_notification_id or notification.updated_at>target.read_at
		) then 1
		when receipt.read_at is not null and (
			target.max_notification_id is null or notification.id>target.max_notification_id or notification.updated_at>target.read_at
		) then -1
		else 0 end),0)::bigint correction
	from targets target
	join notification_receipts receipt on receipt.user_id=target.user_id
	join notifications notification on notification.id=receipt.notification_id and notification.recipient_id is null
	group by target.user_id
)
select target.user_id,target.public_id,
	greatest(coalesce(direct.unread,0)+coalesce(broadcast.baseline,0)+coalesce(correction.correction,0),0)::bigint notifications,
	coalesce(message.unread,0)::bigint messages
from targets target
left join direct_notification_counts direct on direct.user_id=target.user_id
left join broadcast_baselines broadcast on broadcast.user_id=target.user_id
left join broadcast_receipt_corrections correction on correction.user_id=target.user_id
left join message_counts message on message.user_id=target.user_id
order by target.user_id`

func loadUnreadTruth(ctx context.Context, db *pgxpool.Pool, query string, userIDs any) ([]unreadTruthItem, error) {
	rows, err := db.Query(ctx, query, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]unreadTruthItem, 0)
	for rows.Next() {
		var item unreadTruthItem
		if err = rows.Scan(&item.UserID, &item.PublicID, &item.Summary.Notifications, &item.Summary.Messages); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
