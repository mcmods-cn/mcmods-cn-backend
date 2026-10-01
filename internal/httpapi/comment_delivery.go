package httpapi

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
)

// A comment, its watch counters and its durable delivery intents have one
// transaction boundary; no post-commit best-effort sender may lose an intent.
func enqueueCommentDeliveryNotificationsTx(ctx context.Context, tx pgx.Tx, actorID int64, publicID, body string, target commentTargetInfo, parentID *int64, directRecipientID int64, pendingWatches []pendingWatchNotification) error {
	data := map[string]any{"commentId": publicID, "targetType": target.Type, "targetKey": target.Key, "targetLabel": target.Title, "url": target.URL + "#comment-" + publicID}
	blocked := func(recipientID int64) (bool, error) {
		var value bool
		err := tx.QueryRow(ctx, "select exists(select 1 from user_blocks where blocker_id=$1 and blocked_id=$2)", recipientID, actorID).Scan(&value)
		return value, err
	}
	if parentID != nil && directRecipientID != actorID {
		denied, err := blocked(directRecipientID)
		if err != nil {
			return err
		}
		if !denied {
			if err = enqueueNotificationTaskTx(ctx, tx, "notification.direct", "user", strconv.FormatInt(directRecipientID, 10), "", notificationEvent{Action: "direct", RecipientID: directRecipientID, ActorID: actorID, Kind: "reply_mention", Title: "评论收到回复", Body: truncateRunes(body, 160), SourceLocale: "zh-CN", Data: data}); err != nil {
				return err
			}
		}
	}
	notified := make(map[int64]bool)
	for _, pending := range pendingWatches {
		if pending.RecipientID == directRecipientID || pending.Muted || notified[pending.RecipientID] {
			continue
		}
		denied, err := blocked(pending.RecipientID)
		if err != nil {
			return err
		}
		if denied {
			continue
		}
		notified[pending.RecipientID] = true
		watchData := make(map[string]any, len(data)+2)
		for key, value := range data {
			watchData[key] = value
		}
		watchData["watchId"], watchData["replyCount"] = pending.WatchID, 1
		if err = enqueueNotificationTaskTx(ctx, tx, "notification.comment_watch", "user", strconv.FormatInt(pending.RecipientID, 10), "", notificationEvent{Action: "comment_watch", RecipientID: pending.RecipientID, ActorID: actorID, Kind: "comment_watch_reply", Title: "插眼的评论有了新回复", Body: truncateRunes(body, 160), SourceLocale: "zh-CN", Data: watchData}); err != nil {
			return err
		}
	}
	return nil
}
