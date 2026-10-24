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
// then applied as corrections to the baseline. Snapshot-invisible candidates
// are only the active top-level XIDs recorded in that snapshot or XIDs at/above
// its xmax. The stored revision_xid is stamped with pg_current_xact_id(), so
// indexing these exact candidates preserves late commits without rescanning
// every historical broadcast, even while an unrelated old transaction is open.
// Time-changed, post-snapshot and snapshot-active counts are disjoint. The
// candidate subqueries keep the useful time/XID range as the inner access key;
// outer ID/time checks must not turn it back into a scan of the entire ID prefix.
const unreadTruthSQLTail = `,
targets as materialized (
	select selected.id user_id,selected.public_id,watermark.max_notification_id,watermark.read_at,watermark.visibility_snapshot,
        case when watermark.visibility_snapshot is not null then pg_snapshot_xmax(watermark.visibility_snapshot) end visibility_xmax,
        (select coalesce(array_agg(active.xid),'{}'::xid8[]) from pg_snapshot_xip(watermark.visibility_snapshot) active(xid)) visibility_inflight
	from selected_users selected
	left join notification_read_watermarks watermark on watermark.user_id=selected.id
),
direct_notification_counts as (
	select target.user_id,count(*)::bigint unread
	from notifications n
	join targets target on target.user_id=n.recipient_id
	left join notification_receipts receipt on receipt.notification_id=n.id and receipt.user_id=target.user_id
	where n.recipient_id=any($1::bigint[]) and (
		(receipt.notification_id is not null and (receipt.read_at is null or not coalesce(receipt.read_revision_xid=n.revision_xid,true)))
		or (receipt.notification_id is null and not (
			n.id<=coalesce(target.max_notification_id,0)
			and n.updated_at<=coalesce(target.read_at,'-infinity'::timestamptz)
            and (target.visibility_snapshot is null or pg_visible_in_snapshot(n.revision_xid,target.visibility_snapshot))
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
broadcast_baselines as materialized (
	select target.user_id,case when target.max_notification_id is null then state.live_count else
		(case when target.max_notification_id<=state.max_notification_id/2 then
			state.live_count-(select count(*) from notifications prefix
				where prefix.recipient_id is null and prefix.id<=target.max_notification_id)
		else (select count(*) from notifications suffix
			where suffix.recipient_id is null and suffix.id>target.max_notification_id) end)
        +(select count(*) from (
            select changed_time.id from notifications changed_time
            where changed_time.recipient_id is null and changed_time.updated_at>target.read_at
            order by changed_time.updated_at desc,changed_time.id desc
            offset 0
        ) time_candidates where time_candidates.id<=target.max_notification_id)
        +(select count(*) from (
            select changed_xid.id,changed_xid.updated_at,changed_xid.revision_xid from notifications changed_xid
            where changed_xid.recipient_id is null and changed_xid.revision_xid>=target.visibility_xmax
            order by changed_xid.revision_xid,changed_xid.id
            offset 0
        ) new_xids where target.visibility_snapshot is not null
            and new_xids.id<=target.max_notification_id and new_xids.updated_at<=target.read_at
            and not pg_visible_in_snapshot(new_xids.revision_xid,target.visibility_snapshot))
        +(select count(*) from unnest(target.visibility_inflight) active(xid)
            cross join lateral (
                select changed_active.id,changed_active.updated_at,changed_active.revision_xid
                from notifications changed_active where changed_active.recipient_id is null
                    and changed_active.revision_xid>=active.xid and changed_active.revision_xid<=active.xid
                order by changed_active.revision_xid,changed_active.id
                offset 0
            ) active_xids
            where target.visibility_snapshot is not null and active_xids.id<=target.max_notification_id
                and active_xids.updated_at<=target.read_at
                and not pg_visible_in_snapshot(active_xids.revision_xid,target.visibility_snapshot))
	end::bigint baseline
	from targets target cross join notification_broadcast_state state
	where state.singleton
),
broadcast_receipt_corrections as (
	select target.user_id,coalesce(sum(case
		when (receipt.read_at is null or not coalesce(receipt.read_revision_xid=notification.revision_xid,true)) and not (
			target.max_notification_id is null or notification.id>target.max_notification_id or notification.updated_at>target.read_at or (target.visibility_snapshot is not null and not pg_visible_in_snapshot(notification.revision_xid,target.visibility_snapshot))
		) then 1
		when receipt.read_at is not null and coalesce(receipt.read_revision_xid=notification.revision_xid,true) and (
			target.max_notification_id is null or notification.id>target.max_notification_id or notification.updated_at>target.read_at or (target.visibility_snapshot is not null and not pg_visible_in_snapshot(notification.revision_xid,target.visibility_snapshot))
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
