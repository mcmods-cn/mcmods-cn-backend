package httpapi

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

const (
	commentReplyNoticeKind = "reply_mention"
	commentWatchNoticeKind = "comment_watch_reply"
	commentNoticeBody      = "打开评论查看回复"
)

// A comment, its watch counters and its durable delivery intents have one
// transaction boundary; no post-commit best-effort sender may lose an intent.
func enqueueCommentDeliveryNotificationsTx(ctx context.Context, tx pgx.Tx, actorID int64, publicID string, parentID *int64, directRecipientID int64, pendingWatches []pendingWatchNotification) error {
	// Notification history may outlive access to its source. The thread
	// endpoint resolves the target and body against the current viewer.
	data := map[string]any{"commentId": publicID, "url": "/comments/" + publicID}
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
			if err = enqueueNotificationTaskTx(ctx, tx, "notification.direct", "user", strconv.FormatInt(directRecipientID, 10), "", notificationEvent{
				Action: "direct", RecipientID: directRecipientID, ActorID: actorID, Kind: commentReplyNoticeKind,
				Title: "评论收到回复", Body: commentNoticeBody, SourceLocale: "zh-CN", Data: data,
			}); err != nil {
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
		if err = enqueueNotificationTaskTx(ctx, tx, "notification.comment_watch", "user", strconv.FormatInt(pending.RecipientID, 10), "", notificationEvent{
			Action: "comment_watch", RecipientID: pending.RecipientID, ActorID: actorID, Kind: commentWatchNoticeKind,
			Title: "插眼的评论有了新回复", Body: commentNoticeBody, SourceLocale: "zh-CN", Data: watchData,
		}); err != nil {
			return err
		}
	}
	return nil
}

func commentNotificationRecipientAllowedTx(ctx context.Context, tx pgx.Tx, event notificationEvent) (bool, error) {
	commentID, _ := event.Data["commentId"].(string)
	var target commentTargetInfo
	err := tx.QueryRow(ctx, `select comment.target_type,comment.target_id,comment.target_version_id
		from comments comment join users author on author.id=comment.author_id
		join users recipient on recipient.id=$2
		where comment.public_id=$1 and comment.author_id=$3 and comment.status='published'
			and author.status='active' and recipient.status='active'
			and not exists(select 1 from user_blocks block where block.blocker_id=recipient.id and block.blocked_id=author.id)
		for share of comment,author,recipient`, commentID, event.RecipientID, event.ActorID).
		Scan(&target.Type, &target.InternalID, &target.VersionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	rules, err := resolveUsersRootPermissionsQuery(ctx, tx, []int64{event.RecipientID})
	if err != nil {
		return false, err
	}
	claims := security.Claims{Subject: event.RecipientID, PermissionRules: rules[event.RecipientID].Permissions}
	if claimsExplicitlyAllow(claims, "account.banned") {
		return false, nil
	}
	_, err = resolveCommentTargetByInternalWithQueryer(ctx, tx, target.Type, target.InternalID, target.VersionID, claims)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
