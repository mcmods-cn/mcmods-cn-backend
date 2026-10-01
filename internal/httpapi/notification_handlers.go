package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

var notificationKinds = map[string]bool{
	"system": true, "reply_mention": true, "comment_watch_reply": true, "review": true, "new_follower": true,
}

type publishSystemNotificationRequest struct {
	Title        string `json:"title"`
	Body         string `json:"body"`
	SourceLocale string `json:"sourceLocale"`
	SendEmail    bool   `json:"sendEmail"`
}

type translateNotificationRequest struct {
	TargetLocale string `json:"targetLocale"`
}

type aiDailyBalancePayload struct {
	UsedTokens      int64 `json:"usedTokens"`
	ReservedTokens  int64 `json:"reservedTokens"`
	LimitTokens     int64 `json:"limitTokens"`
	RemainingTokens int64 `json:"remainingTokens"`
	Unlimited       bool  `json:"unlimited"`
}

type notificationActor struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type notificationItem struct {
	ID                 string              `json:"id"`
	Kind               string              `json:"kind"`
	Title              string              `json:"title"`
	Body               string              `json:"body"`
	SourceLocale       string              `json:"sourceLocale"`
	Data               map[string]any      `json:"data"`
	Actors             []notificationActor `json:"actors"`
	Read               bool                `json:"read"`
	CreatedAt          time.Time           `json:"createdAt"`
	UpdatedAt          time.Time           `json:"updatedAt"`
	TranslationAllowed bool                `json:"translationAllowed"`
}

func (s *Server) publishSystemNotification(w http.ResponseWriter, r *http.Request) {
	var req publishSystemNotificationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	req.Body = strings.TrimSpace(req.Body)
	req.SourceLocale = strings.TrimSpace(req.SourceLocale)
	if req.Title == "" || req.Body == "" {
		writeError(w, http.StatusBadRequest, "通知标题和正文不能为空")
		return
	}
	if len([]rune(req.Title)) > 200 || len([]rune(req.Body)) > 10000 {
		writeError(w, http.StatusBadRequest, "通知内容过长")
		return
	}
	if req.SourceLocale == "" {
		req.SourceLocale = "zh-CN"
	}
	if s.queue == nil && !s.cfg.NATS.OutboxEnabled {
		writeError(w, http.StatusServiceUnavailable, "通知任务队列不可用")
		return
	}
	publisherPublicID, err := s.publicIDForInternal(r.Context(), "user", currentClaims(r).Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve publisher")
		return
	}
	err = s.enqueueNotificationTask(r.Context(), notificationEvent{
		Action: "system", Title: req.Title, Body: req.Body,
		SourceLocale: req.SourceLocale, SendEmail: req.SendEmail,
		Data: map[string]any{"publishedBy": publisherPublicID},
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "通知任务队列不可用")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
}

func (s *Server) deleteSystemNotification(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := s.pathNotificationID(w, r)
	if !ok {
		return
	}
	tag, err := s.db.Exec(r.Context(), `delete from notifications where id = $1 and recipient_id is null and kind = 'system'`, notificationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "删除系统通知失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "系统通知不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kind != "" && !notificationKinds[kind] {
		writeError(w, http.StatusBadRequest, "通知类型不正确")
		return
	}
	limit := boundedLimit(r.URL.Query().Get("limit"), 100, 200)
	args := []any{claims.Subject}
	where := "(n.recipient_id is null or n.recipient_id = $1)"
	if kind != "" {
		args = append(args, kind)
		where += " and n.kind = $2"
	}
	args = append(args, limit)
	rows, err := s.db.Query(
		r.Context(),
		`select n.public_id, n.kind, n.title, n.body, n.source_locale, n.data,
		        coalesce(jsonb_agg(distinct jsonb_build_object(
		          'id', actor.public_id, 'username', actor.username
		        )) filter (where actor.id is not null), '[]'::jsonb),
		        (receipt.read_at is not null), n.created_at, n.updated_at, n.kind <> 'system'
		 from notifications n
		 left join notification_receipts receipt on receipt.notification_id = n.id and receipt.user_id = $1
		 left join notification_actors na on na.notification_id = n.id
		 left join users actor on actor.id = na.actor_id
		 where `+where+`
		 group by n.id, receipt.read_at
		 order by n.updated_at desc
		 limit $`+strconv.Itoa(len(args)),
		args...,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取通知失败")
		return
	}
	defer rows.Close()
	items := make([]notificationItem, 0)
	for rows.Next() {
		var item notificationItem
		var rawData, rawActors []byte
		if err := rows.Scan(
			&item.ID, &item.Kind, &item.Title, &item.Body, &item.SourceLocale,
			&rawData, &rawActors, &item.Read, &item.CreatedAt, &item.UpdatedAt, &item.TranslationAllowed,
		); err != nil {
			writeError(w, http.StatusInternalServerError, "读取通知失败")
			return
		}
		_ = json.Unmarshal(rawData, &item.Data)
		_ = json.Unmarshal(rawActors, &item.Actors)
		if item.Data == nil {
			item.Data = map[string]any{}
		}
		if item.Actors == nil {
			item.Actors = []notificationActor{}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "读取通知失败")
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := s.pathNotificationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	tag, err := s.db.Exec(
		r.Context(),
		`insert into notification_receipts (notification_id, user_id, read_at)
		 select id, $2, now() from notifications where id = $1 and (recipient_id is null or recipient_id = $2)
		 on conflict (notification_id, user_id) do update set read_at = now()
		 where notification_receipts.read_at is null`,
		notificationID,
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新通知状态失败")
		return
	}
	if tag.RowsAffected() == 0 {
		// Marking an already-read item is intentionally idempotent.
		writeJSON(w, http.StatusOK, map[string]bool{"read": true})
		return
	}
	s.cache.AdjustUnread(r.Context(), claims.Subject, "notifications", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"read": true})
}

func (s *Server) markAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	tag, err := s.db.Exec(
		r.Context(),
		`insert into notification_receipts (notification_id, user_id, read_at)
		 select id, $1, now() from notifications
		 where recipient_id is null or recipient_id = $1
		 on conflict (notification_id, user_id) do update set read_at = now()
		 where notification_receipts.read_at is null`,
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新通知状态失败")
		return
	}
	s.cache.InvalidateUnread(r.Context(), claims.Subject)
	writeJSON(w, http.StatusOK, map[string]any{"read": true, "updated": tag.RowsAffected()})
}

func (s *Server) unreadSummary(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	summary, err := s.cache.LoadUnread(r.Context(), claims.Subject, func(ctx context.Context) (querycache.UnreadSummary, error) {
		var value querycache.UnreadSummary
		err := s.db.QueryRow(ctx, `select
			(select count(*) from notifications n left join notification_receipts receipt
			 on receipt.notification_id=n.id and receipt.user_id=$1
			 where (n.recipient_id is null or n.recipient_id=$1) and receipt.read_at is null),
			(select count(*) from direct_messages where recipient_id=$1 and read_at is null)`, claims.Subject).
			Scan(&value.Notifications, &value.Messages)
		return value, err
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to load unread summary")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{
		"notifications": summary.Notifications,
		"messages":      summary.Messages,
		"total":         summary.Total(),
	})
}

func (s *Server) aiDailyBalance(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	writeJSON(w, http.StatusOK, s.userAIDailyBalance(r.Context(), claims.Subject, claims))
}

func (s *Server) translateNotification(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := s.pathNotificationID(w, r)
	if !ok {
		return
	}
	var req translateNotificationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.TargetLocale = normalizeContentLocale(req.TargetLocale)
	if !validContentLocaleTag(req.TargetLocale) {
		writeError(w, http.StatusBadRequest, "目标语言不正确")
		return
	}
	claims := currentClaims(r)
	var title, body, sourceLocale, kind string
	var sourceUpdatedAt time.Time
	err := s.db.QueryRow(
		r.Context(),
		`select title, body, source_locale, kind, updated_at from notifications
		 where id = $1 and (recipient_id is null or recipient_id = $2)`,
		notificationID,
		claims.Subject,
	).Scan(&title, &body, &sourceLocale, &kind, &sourceUpdatedAt)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "通知不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取通知失败")
		return
	}
	if kind == "system" {
		writeAPIError(w, http.StatusForbidden, "SYSTEM_NOTIFICATION_TRANSLATION_DISABLED", "系统通知已按接收语言生成，无需 AI 翻译", 0, nil)
		return
	}
	if req.TargetLocale == normalizeContentLocale(sourceLocale) {
		writeJSON(w, http.StatusOK, map[string]any{
			"cached": true, "translation": map[string]string{"title": title, "body": body},
		})
		return
	}
	var cachedTitle, cachedBody string
	err = s.db.QueryRow(
		r.Context(),
		`select title, body from notification_translations
		 where notification_id=$1 and user_id=$2 and locale=$3 and created_at >= $4`,
		notificationID,
		claims.Subject,
		req.TargetLocale,
		sourceUpdatedAt,
	).Scan(&cachedTitle, &cachedBody)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"cached": true, "translation": map[string]string{"title": cachedTitle, "body": cachedBody},
		})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusServiceUnavailable, "读取通知翻译失败")
		return
	}
	limit := int64(claimsNumericPermissionValue(claims, "user.ai.daily_token_limit"))
	if limit <= 0 {
		writeError(w, http.StatusForbidden, "没有可用的每日 AI Token 额度")
		return
	}
	cfg := s.aiConfigFromSettings(r.Context())
	binding, ok := findAITaskModel(cfg.TaskModels, aiTaskNotificationTranslation)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "通知翻译任务尚未配置模型")
		return
	}
	provider, model, ok := resolveAIModel(cfg, binding.ModelKey)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "通知翻译模型或供应商不可用")
		return
	}
	payload := map[string]any{
		"notificationId":  notificationID,
		"sourceLocale":    sourceLocale,
		"sourceUpdatedAt": sourceUpdatedAt,
		"targetLocale":    req.TargetLocale,
		"items":           []map[string]string{{"key": "title", "text": title}, {"key": "body", "text": body}},
	}
	if err = freezeAITranslationContext(payload, cfg); err != nil {
		writeError(w, http.StatusBadRequest, "翻译术语配置无效")
		return
	}
	rawPayload, _ := json.Marshal(payload)
	if _, err = buildTranslationPrompt(aiTaskNotificationTranslation, "", rawPayload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	digest := sha256.Sum256(rawPayload)
	concurrencyKey := "notification:" + strconv.FormatInt(claims.Subject, 10) + ":" + hex.EncodeToString(digest[:])
	reserved := estimatedAIReservation(rawPayload, model)

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `select pg_advisory_xact_lock(hashtextextended($1,0))`, concurrencyKey); err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	var existingUID, existingStatus string
	err = tx.QueryRow(r.Context(), `select task_uid,status from ai_tasks
		where concurrency_key=$1 and created_by=$2 and status in ('queued','running','retrying')
		order by id desc limit 1`, concurrencyKey, claims.Subject).Scan(&existingUID, &existingStatus)
	if err == nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"cached": false, "taskId": existingUID, "status": existingStatus})
		return
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取翻译任务失败")
		return
	}
	if err = reserveAISiteQuotaTx(r.Context(), tx, cfg, reserved); err != nil {
		if errors.Is(err, errAIQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日 AI Token 余额不足")
		} else {
			writeError(w, http.StatusServiceUnavailable, "读取 AI Token 额度失败")
		}
		return
	}
	if err = reserveAITaskQuotaTx(r.Context(), tx, claims.Subject, limit, reserved); err != nil {
		if errors.Is(err, errAIQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日 AI Token 余额不足")
		} else {
			writeError(w, http.StatusServiceUnavailable, "读取 AI Token 额度失败")
		}
		return
	}
	taskUID := "ai_" + randomHex(16)
	var taskID int64
	err = tx.QueryRow(
		r.Context(),
		`insert into ai_tasks (
		 task_uid, task_type, provider, model, status, concurrency_key, payload,
		 created_by, queued_at, quota_reserved_tokens
		 ) values ($1, $2, $3, $4, 'queued', $5, $6::jsonb, $7, now(), $8)
		 returning id`,
		taskUID,
		aiTaskNotificationTranslation,
		provider.Code,
		model.Model,
		concurrencyKey,
		string(rawPayload),
		claims.Subject,
		reserved,
	).Scan(&taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	if err = s.enqueueAIOutboxTx(r.Context(), tx, taskID, taskUID, aiTaskNotificationTranslation); err != nil {
		if errors.Is(err, errAIQuotaExceeded) {
			writeError(w, http.StatusTooManyRequests, "今日 AI 预算不足")
		} else {
			writeError(w, http.StatusServiceUnavailable, "AI 任务队列不可用")
		}
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	if !s.cfg.NATS.OutboxEnabled && (s.queue == nil || s.queue.PublishTask(r.Context(), "ai", aiTaskMessage{TaskID: taskID, TaskUID: taskUID, TaskType: aiTaskNotificationTranslation}) != nil) {
		_, _ = s.db.Exec(r.Context(), `update ai_tasks set status = 'failed', error = 'NATS unavailable', finished_at = now(), updated_at = now() where id = $1`, taskID)
		writeError(w, http.StatusServiceUnavailable, "AI 任务队列不可用")
		return
	}
	s.writeAITaskLog(r.Context(), taskID, "info", "task_queued", "Notification translation published to NATS", payload)
	writeJSON(w, http.StatusAccepted, map[string]any{"cached": false, "taskId": taskUID, "status": "queued"})
}

func (s *Server) notificationTranslationResult(w http.ResponseWriter, r *http.Request) {
	taskUID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if taskUID == "" || len(taskUID) > 80 {
		writeError(w, http.StatusBadRequest, "AI 任务 ID 不正确")
		return
	}
	claims := currentClaims(r)
	var status, errorMessage string
	var payloadRaw []byte
	var inputTokens, outputTokens int64
	err := s.db.QueryRow(
		r.Context(),
		`select status, payload, error, input_tokens, output_tokens
		 from ai_tasks where task_uid = $1 and created_by = $2 and task_type = $3`,
		taskUID,
		claims.Subject,
		aiTaskNotificationTranslation,
	).Scan(&status, &payloadRaw, &errorMessage, &inputTokens, &outputTokens)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "翻译任务不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取翻译任务失败")
		return
	}
	response := map[string]any{"status": status, "error": errorMessage, "inputTokens": inputTokens, "outputTokens": outputTokens}
	if status == "completed" {
		var payload struct {
			NotificationID int64  `json:"notificationId"`
			TargetLocale   string `json:"targetLocale"`
		}
		if err = json.Unmarshal(payloadRaw, &payload); err != nil || payload.NotificationID <= 0 || !validContentLocaleTag(payload.TargetLocale) {
			writeError(w, http.StatusInternalServerError, "读取翻译任务失败")
			return
		}
		var title, body string
		err = s.db.QueryRow(r.Context(), `select translation.title,translation.body from notification_translations translation
			join notifications notification on notification.id=translation.notification_id
			where translation.notification_id=$1 and translation.user_id=$2 and translation.locale=$3
			  and (notification.recipient_id is null or notification.recipient_id=$2)
			  and translation.created_at>=notification.updated_at`, payload.NotificationID, claims.Subject, payload.TargetLocale).Scan(&title, &body)
		if errors.Is(err, pgx.ErrNoRows) {
			response["status"] = "failed"
			response["error"] = "通知内容已更新或翻译不可用，请重新翻译"
		} else if err != nil {
			writeError(w, http.StatusServiceUnavailable, "读取通知翻译失败")
			return
		} else {
			response["translation"] = map[string]string{"title": title, "body": body}
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) userAIDailyUsage(ctx context.Context, userID int64) (int64, int64) {
	var used, reserved int64
	_ = s.db.QueryRow(
		ctx,
		`select
		 coalesce(sum(case when status = 'completed' then input_tokens + output_tokens else 0 end), 0),
		 coalesce(sum(case when status in ('queued', 'running', 'retrying') then quota_reserved_tokens else 0 end), 0)
		 from ai_tasks where created_by = $1 and created_at >= date_trunc('day', now())`,
		userID,
	).Scan(&used, &reserved)
	return used, reserved
}

func (s *Server) userAIDailyBalance(ctx context.Context, userID int64, claims security.Claims) aiDailyBalancePayload {
	limit := int64(claimsNumericPermissionValue(claims, "user.ai.daily_token_limit"))
	used, reserved := s.userAIDailyUsage(ctx, userID)
	return aiDailyBalancePayload{
		UsedTokens:      used,
		ReservedTokens:  reserved,
		LimitTokens:     limit,
		RemainingTokens: remainingTokens(limit, used+reserved),
		Unlimited:       limit == int64(maxPermissionValue),
	}
}

func remainingTokens(limit int64, used int64) int64 {
	if limit == int64(maxPermissionValue) {
		return int64(maxPermissionValue)
	}
	if used >= limit {
		return 0
	}
	return limit - used
}

func (s *Server) pathNotificationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	if !validCatalogPublicID(publicID) {
		writeError(w, http.StatusBadRequest, "通知 ID 不正确")
		return 0, false
	}
	var notificationID int64
	if err := s.db.QueryRow(r.Context(), `select id from notifications where public_id=$1`, publicID).Scan(&notificationID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "notification not found")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to load notification")
		}
		return 0, false
	}
	return notificationID, true
}
