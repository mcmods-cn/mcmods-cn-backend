package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/mailer"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/queue"
	"mcmods-cn-backend/internal/security"
)

const notificationTaskCode = "notifications"

type notificationEvent struct {
	Action       string         `json:"action"`
	RecipientID  int64          `json:"recipientId,omitempty"`
	ActorID      int64          `json:"actorId,omitempty"`
	Title        string         `json:"title,omitempty"`
	Body         string         `json:"body,omitempty"`
	Kind         string         `json:"kind,omitempty"`
	SourceLocale string         `json:"sourceLocale,omitempty"`
	SendEmail    bool           `json:"sendEmail,omitempty"`
	Data         map[string]any `json:"data,omitempty"`
}

type NotificationWorker struct {
	db                    *pgxpool.Pool
	queue                 *queue.Client
	fallback              config.SMTPConfig
	settingsEncryptionKey string
	cache                 *querycache.Cache
}

func NewNotificationWorker(db *pgxpool.Pool, queueClient *queue.Client, cache *querycache.Cache, fallback config.SMTPConfig, settingsEncryptionKey string) *NotificationWorker {
	return &NotificationWorker{db: db, queue: queueClient, cache: cache, fallback: fallback, settingsEncryptionKey: settingsEncryptionKey}
}

func (worker *NotificationWorker) Start() error {
	if worker == nil || worker.queue == nil {
		return queue.ErrUnavailable
	}
	return worker.queue.SubscribeTask(notificationTaskCode, worker.handleEvent)
}

func (worker *NotificationWorker) handleEvent(ctx context.Context, raw []byte) error {
	var event notificationEvent
	if err := json.Unmarshal(raw, &event); err != nil {
		return err
	}
	switch event.Action {
	case "system":
		return worker.createSystemNotification(ctx, event)
	case "direct":
		return worker.createDirectNotification(ctx, event)
	case "comment_watch":
		return worker.aggregateCommentWatchNotification(ctx, event)
	case "follow":
		return worker.aggregateFollowerNotification(ctx, event)
	case "email":
		worker.sendUserEmail(ctx, event.RecipientID, event.Title, event.Body)
		return nil
	default:
		return fmt.Errorf("unsupported notification action: %s", event.Action)
	}
}

func (worker *NotificationWorker) createSystemNotification(ctx context.Context, event notificationEvent) error {
	event.Title = strings.TrimSpace(event.Title)
	event.Body = strings.TrimSpace(event.Body)
	if event.Title == "" || event.Body == "" {
		return fmt.Errorf("system notification title and body are required")
	}
	if event.SourceLocale == "" {
		event.SourceLocale = "zh-CN"
	}
	rawData, _ := json.Marshal(event.Data)
	tag, err := worker.db.Exec(
		ctx,
		`insert into notifications (recipient_id,kind,title,body,source_locale,data,source_event_id)
		 values (null,'system',$1,$2,$3,$4::jsonb,nullif($5,'')) on conflict(source_event_id) where source_event_id is not null do nothing`,
		event.Title,
		event.Body,
		event.SourceLocale,
		string(rawData),
		queue.EventIDFromContext(ctx),
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	worker.cache.BumpUnreadEpoch(ctx)
	if event.SendEmail {
		worker.sendBroadcastEmail(ctx, event.Title, event.Body)
	}
	return nil
}

func (worker *NotificationWorker) createDirectNotification(ctx context.Context, event notificationEvent) error {
	if event.RecipientID <= 0 || strings.TrimSpace(event.Kind) == "" {
		return fmt.Errorf("direct notification recipient and kind are required")
	}
	rawData, _ := json.Marshal(event.Data)
	var notificationID int64
	if err := worker.db.QueryRow(
		ctx,
		`insert into notifications (recipient_id,kind,title,body,source_locale,data,source_event_id)
		 values ($1,$2,$3,$4,$5,$6::jsonb,nullif($7,'')) on conflict(source_event_id) where source_event_id is not null do nothing
		 returning id`,
		event.RecipientID,
		event.Kind,
		strings.TrimSpace(event.Title),
		strings.TrimSpace(event.Body),
		defaultString(event.SourceLocale, "zh-CN"),
		string(rawData),
		queue.EventIDFromContext(ctx),
	).Scan(&notificationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	if event.ActorID > 0 {
		_, _ = worker.db.Exec(ctx, `insert into notification_actors (notification_id, actor_id) values ($1, $2) on conflict do nothing`, notificationID, event.ActorID)
	}
	worker.cache.AdjustUnread(ctx, event.RecipientID, "notifications", 1)
	worker.publishRealtime(event.RecipientID, "notification.created", map[string]any{"kind": event.Kind})
	if event.SendEmail {
		worker.enqueueUserEmail(ctx, event.RecipientID, event.Title, event.Body)
	}
	return nil
}

func (worker *NotificationWorker) aggregateCommentWatchNotification(ctx context.Context, event notificationEvent) error {
	if event.RecipientID <= 0 {
		return fmt.Errorf("comment watch notification recipient is required")
	}
	watchID := fmt.Sprint(event.Data["watchId"])
	if watchID == "" || watchID == "<nil>" {
		return fmt.Errorf("comment watch notification watch ID is required")
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	claimed, err := claimProcessedEventTx(ctx, tx, "notifications.comment_watch")
	if err != nil || !claimed {
		return err
	}
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, event.RecipientID); err != nil {
		return err
	}
	var notificationID int64
	var replyCount int
	err = tx.QueryRow(ctx, `select id,coalesce((data->>'replyCount')::integer,1)
		from notifications where recipient_id=$1 and kind='comment_watch_reply'
		  and data->>'watchId'=$2 and updated_at>now()-interval '5 minutes'
		order by updated_at desc,id desc limit 1 for update`, event.RecipientID, watchID).
		Scan(&notificationID, &replyCount)
	rawData, _ := json.Marshal(event.Data)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `insert into notifications(recipient_id,kind,title,body,source_locale,data)
			values($1,'comment_watch_reply',$2,$3,$4,$5::jsonb) returning id`,
			event.RecipientID, strings.TrimSpace(event.Title), strings.TrimSpace(event.Body),
			defaultString(event.SourceLocale, "zh-CN"), string(rawData)).Scan(&notificationID)
	} else if err == nil {
		replyCount++
		event.Data["replyCount"] = replyCount
		rawData, _ = json.Marshal(event.Data)
		_, err = tx.Exec(ctx, `update notifications set title=$2,
			body=$3,data=data||$4::jsonb,updated_at=now() where id=$1`,
			notificationID, fmt.Sprintf("插眼的评论有了 %d 条新回复", replyCount),
			strings.TrimSpace(event.Body), string(rawData))
		if err == nil {
			_, err = tx.Exec(ctx, `delete from notification_receipts where notification_id=$1 and user_id=$2`,
				notificationID, event.RecipientID)
		}
	}
	if err != nil {
		return err
	}
	if event.ActorID > 0 {
		_, _ = tx.Exec(ctx, `insert into notification_actors(notification_id,actor_id)
			values($1,$2) on conflict do nothing`, notificationID, event.ActorID)
	}
	if err = tx.Commit(ctx); err == nil {
		worker.cache.InvalidateUnread(ctx, event.RecipientID)
		worker.publishRealtime(event.RecipientID, "notification.changed", map[string]any{"kind": "comment_watch_reply"})
	}
	return err
}

func (worker *NotificationWorker) aggregateFollowerNotification(ctx context.Context, event notificationEvent) error {
	if event.RecipientID <= 0 || event.ActorID <= 0 || event.RecipientID == event.ActorID {
		return fmt.Errorf("follower notification users are invalid")
	}
	tx, err := worker.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	claimed, err := claimProcessedEventTx(ctx, tx, "notifications.follow")
	if err != nil || !claimed {
		return err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, event.RecipientID); err != nil {
		return err
	}

	var notificationID int64
	err = tx.QueryRow(
		ctx,
		`select n.id
		 from notifications n
		 left join notification_receipts r on r.notification_id = n.id and r.user_id = $1
		 where n.recipient_id = $1 and n.kind = 'new_follower' and r.read_at is null
		 order by n.updated_at desc
		 limit 1
		 for update of n`,
		event.RecipientID,
	).Scan(&notificationID)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	if err == pgx.ErrNoRows {
		if err := tx.QueryRow(
			ctx,
			`insert into notifications (recipient_id, kind, title, body, source_locale)
			 values ($1, 'new_follower', '新增粉丝', '', 'zh-CN') returning id`,
			event.RecipientID,
		).Scan(&notificationID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `insert into notification_actors (notification_id, actor_id) values ($1, $2) on conflict do nothing`, notificationID, event.ActorID); err != nil {
		return err
	}

	rows, err := tx.Query(
		ctx,
		`select u.username
		 from notification_actors a
		 join users u on u.id = a.actor_id
		 where a.notification_id = $1
		 order by a.created_at desc
		 limit 3`,
		notificationID,
	)
	if err != nil {
		return err
	}
	names := make([]string, 0, 3)
	for rows.Next() {
		var username string
		if err := rows.Scan(&username); err == nil {
			names = append(names, username)
		}
	}
	rows.Close()
	var count int
	if err := tx.QueryRow(ctx, `select count(*) from notification_actors where notification_id = $1`, notificationID).Scan(&count); err != nil {
		return err
	}
	body := followerNotificationBody(names, count)
	if _, err := tx.Exec(ctx, `update notifications set body = $2, updated_at = now() where id = $1`, notificationID, body); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	worker.cache.InvalidateUnread(ctx, event.RecipientID)
	worker.publishRealtime(event.RecipientID, "notification.changed", map[string]any{"kind": "new_follower"})
	worker.enqueueUserEmail(ctx, event.RecipientID, "你有新的粉丝", body)
	return nil
}

func (worker *NotificationWorker) publishRealtime(userID int64, eventType string, data any) {
	if worker == nil || worker.queue == nil || userID <= 0 {
		return
	}
	_ = worker.queue.PublishBroadcast(context.Background(), "user."+strconv.FormatInt(userID, 10), realtimeEvent{ID: randomHex(12), Type: eventType, Data: data})
}

func claimProcessedEventTx(ctx context.Context, tx pgx.Tx, consumer string) (bool, error) {
	eventID := queue.EventIDFromContext(ctx)
	if eventID == "" {
		return true, nil
	}
	tag, err := tx.Exec(ctx, `insert into processed_events(consumer,event_id,event_type)
		values($1,$2,$1) on conflict do nothing`, consumer, eventID)
	return err == nil && tag.RowsAffected() > 0, err
}

func followerNotificationBody(names []string, count int) string {
	if len(names) == 0 || count <= 0 {
		return "有用户关注了你"
	}
	if count == 1 {
		return names[0] + " 关注了你"
	}
	if count == 2 && len(names) >= 2 {
		return names[0] + "、" + names[1] + " 关注了你"
	}
	return fmt.Sprintf("%s、%s 等 %d 人关注了你", names[0], names[1], count)
}

func (worker *NotificationWorker) sendBroadcastEmail(ctx context.Context, subject string, body string) {
	rows, err := worker.db.Query(
		ctx,
		`select u.id
		 from user_notification_settings s
		 join users u on u.id = s.user_id
		 where s.email_enabled = true and u.email_verified = true and u.status = 'active'`,
	)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err == nil {
			worker.enqueueUserEmail(ctx, userID, subject, body)
		}
	}
}

func (worker *NotificationWorker) enqueueUserEmail(ctx context.Context, userID int64, subject string, body string) {
	if worker.queue == nil || userID <= 0 {
		return
	}
	if err := worker.queue.PublishTask(ctx, notificationTaskCode, notificationEvent{
		Action: "email", RecipientID: userID, Title: subject, Body: body,
	}); err != nil {
		slog.Warn("queue notification email", "user_id", userID, "error", err)
	}
}

func (worker *NotificationWorker) sendUserEmail(ctx context.Context, userID int64, subject string, body string) {
	if userID <= 0 {
		return
	}
	var email string
	err := worker.db.QueryRow(
		ctx,
		`select u.email
		 from users u
		 join user_notification_settings s on s.user_id = u.id
		 where u.id = $1 and u.status = 'active' and u.email_verified = true and s.email_enabled = true`,
		userID,
	).Scan(&email)
	if err != nil {
		return
	}
	if err = worker.activeMailer(ctx).Send(email, subject, body); err != nil {
		slog.Warn("send notification email", "user_id", userID, "error", err)
	}
}

func (worker *NotificationWorker) activeMailer(ctx context.Context) mailer.Mailer {
	payload := mailConfigPayload{
		Enabled:  strings.TrimSpace(worker.fallback.Host) != "" && strings.TrimSpace(worker.fallback.From) != "",
		Host:     worker.fallback.Host,
		Port:     worker.fallback.Port,
		Username: worker.fallback.Username,
		Password: worker.fallback.Password,
		From:     worker.fallback.From,
		UseTLS:   worker.fallback.UseTLS,
	}
	var raw []byte
	if err := worker.db.QueryRow(ctx, `select value from system_settings where key = 'mail.smtp'`).Scan(&raw); err == nil {
		if decrypted, decryptErr := security.DecryptSetting(worker.settingsEncryptionKey, raw); decryptErr == nil {
			_ = json.Unmarshal(decrypted, &payload)
		}
	}
	return mailer.New(config.SMTPConfig{
		Host: payload.Host, Port: payload.Port, Username: payload.Username,
		Password: payload.Password, From: payload.From, UseTLS: payload.UseTLS,
	})
}
