package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

var notificationKinds = map[string]bool{
	"system": true, "reply_mention": true, "review": true, "new_follower": true,
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
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type notificationItem struct {
	ID           int64               `json:"id"`
	Kind         string              `json:"kind"`
	Title        string              `json:"title"`
	Body         string              `json:"body"`
	SourceLocale string              `json:"sourceLocale"`
	Data         map[string]any      `json:"data"`
	Actors       []notificationActor `json:"actors"`
	Read         bool                `json:"read"`
	CreatedAt    time.Time           `json:"createdAt"`
	UpdatedAt    time.Time           `json:"updatedAt"`
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
	if s.queue == nil {
		writeError(w, http.StatusServiceUnavailable, "通知任务队列不可用")
		return
	}
	err := s.queue.PublishTask(r.Context(), notificationTaskCode, notificationEvent{
		Action: "system", Title: req.Title, Body: req.Body,
		SourceLocale: req.SourceLocale, SendEmail: req.SendEmail,
		Data: map[string]any{"publishedBy": currentClaims(r).Subject},
	})
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "通知任务队列不可用")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"queued": true})
}

func (s *Server) deleteSystemNotification(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := pathNotificationID(w, r)
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
		`select n.id, n.kind, n.title, n.body, n.source_locale, n.data,
		        coalesce(jsonb_agg(distinct jsonb_build_object(
		          'id', actor.id, 'username', actor.username, 'displayName', actor.display_name
		        )) filter (where actor.id is not null), '[]'::jsonb),
		        (receipt.read_at is not null), n.created_at, n.updated_at
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
			&rawData, &rawActors, &item.Read, &item.CreatedAt, &item.UpdatedAt,
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
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := pathNotificationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	tag, err := s.db.Exec(
		r.Context(),
		`insert into notification_receipts (notification_id, user_id, read_at)
		 select id, $2, now() from notifications where id = $1 and (recipient_id is null or recipient_id = $2)
		 on conflict (notification_id, user_id) do update set read_at = now()`,
		notificationID,
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新通知状态失败")
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "通知不存在")
		return
	}
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
	writeJSON(w, http.StatusOK, map[string]any{"read": true, "updated": tag.RowsAffected()})
}

func (s *Server) unreadSummary(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	var notificationsCount, messagesCount int64
	_ = s.db.QueryRow(
		r.Context(),
		`select count(*) from notifications n
		 left join notification_receipts receipt on receipt.notification_id = n.id and receipt.user_id = $1
		 where (n.recipient_id is null or n.recipient_id = $1) and receipt.read_at is null`,
		claims.Subject,
	).Scan(&notificationsCount)
	_ = s.db.QueryRow(r.Context(), `select count(*) from direct_messages where recipient_id = $1 and read_at is null`, claims.Subject).Scan(&messagesCount)
	writeJSON(w, http.StatusOK, map[string]int64{
		"notifications": notificationsCount,
		"messages":      messagesCount,
		"total":         notificationsCount + messagesCount,
	})
}

func (s *Server) aiDailyBalance(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	writeJSON(w, http.StatusOK, s.userAIDailyBalance(r.Context(), claims.Subject, claims.Permissions))
}

func (s *Server) translateNotification(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := pathNotificationID(w, r)
	if !ok {
		return
	}
	var req translateNotificationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.TargetLocale = strings.TrimSpace(req.TargetLocale)
	if req.TargetLocale == "" || len(req.TargetLocale) > 20 {
		writeError(w, http.StatusBadRequest, "目标语言不正确")
		return
	}
	claims := currentClaims(r)
	var title, body, sourceLocale string
	err := s.db.QueryRow(
		r.Context(),
		`select title, body, source_locale from notifications
		 where id = $1 and (recipient_id is null or recipient_id = $2)`,
		notificationID,
		claims.Subject,
	).Scan(&title, &body, &sourceLocale)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "通知不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取通知失败")
		return
	}
	var cachedTitle, cachedBody string
	if err := s.db.QueryRow(
		r.Context(),
		`select title, body from notification_translations where notification_id = $1 and user_id = $2 and locale = $3`,
		notificationID,
		claims.Subject,
		req.TargetLocale,
	).Scan(&cachedTitle, &cachedBody); err == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"cached": true, "translation": map[string]string{"title": cachedTitle, "body": cachedBody},
		})
		return
	}
	limit := int64(numericPermissionValue(claims.Permissions, "user.ai.daily_token_limit"))
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
		"notificationId": notificationID,
		"sourceLocale":   sourceLocale,
		"targetLocale":   req.TargetLocale,
		"items":          []map[string]string{{"key": "title", "text": title}, {"key": "body", "text": body}},
	}
	rawPayload, _ := json.Marshal(payload)
	reserved := int64(utf8.RuneCountInString(title+body)*2 + 128)
	if reserved < 128 {
		reserved = 128
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	defer tx.Rollback(r.Context())
	_, _ = tx.Exec(r.Context(), `select pg_advisory_xact_lock($1)`, claims.Subject)
	var used, pending int64
	if err := tx.QueryRow(
		r.Context(),
		`select
		 coalesce(sum(case when status = 'completed' then input_tokens + output_tokens else 0 end), 0),
		 coalesce(sum(case when status in ('queued', 'running', 'retrying') then quota_reserved_tokens else 0 end), 0)
		 from ai_tasks where created_by = $1 and created_at >= date_trunc('day', now())`,
		claims.Subject,
	).Scan(&used, &pending); err != nil {
		writeError(w, http.StatusInternalServerError, "读取 AI Token 额度失败")
		return
	}
	if limit != int64(maxPermissionValue) && used+pending+reserved > limit {
		writeError(w, http.StatusTooManyRequests, "今日 AI Token 余额不足")
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
		"notification:"+strconv.FormatInt(notificationID, 10),
		string(rawPayload),
		claims.Subject,
		reserved,
	).Scan(&taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	if s.queue == nil || s.queue.PublishTask(r.Context(), "ai", aiTaskMessage{TaskID: taskID, TaskUID: taskUID, TaskType: aiTaskNotificationTranslation}) != nil {
		_, _ = s.db.Exec(r.Context(), `update ai_tasks set status = 'failed', error = 'NATS unavailable', finished_at = now(), updated_at = now() where id = $1`, taskID)
		writeError(w, http.StatusServiceUnavailable, "AI 任务队列不可用")
		return
	}
	s.writeAITaskLog(r.Context(), taskID, "info", "task_queued", "Notification translation published to NATS", payload)
	writeJSON(w, http.StatusAccepted, map[string]any{"cached": false, "taskId": taskID, "status": "queued"})
}

func (s *Server) notificationTranslationResult(w http.ResponseWriter, r *http.Request) {
	taskID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || taskID <= 0 {
		writeError(w, http.StatusBadRequest, "AI 任务 ID 不正确")
		return
	}
	claims := currentClaims(r)
	var status, errorMessage string
	var resultRaw, payloadRaw []byte
	var inputTokens, outputTokens int64
	err = s.db.QueryRow(
		r.Context(),
		`select status, result, payload, error, input_tokens, output_tokens
		 from ai_tasks where id = $1 and created_by = $2 and task_type = $3`,
		taskID,
		claims.Subject,
		aiTaskNotificationTranslation,
	).Scan(&status, &resultRaw, &payloadRaw, &errorMessage, &inputTokens, &outputTokens)
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
		var result struct {
			Items []struct {
				Key  string `json:"key"`
				Text string `json:"text"`
			} `json:"items"`
		}
		var payload struct {
			NotificationID int64  `json:"notificationId"`
			TargetLocale   string `json:"targetLocale"`
		}
		_ = json.Unmarshal(resultRaw, &result)
		_ = json.Unmarshal(payloadRaw, &payload)
		translated := map[string]string{}
		for _, item := range result.Items {
			translated[item.Key] = item.Text
		}
		_, _ = s.db.Exec(
			r.Context(),
			`insert into notification_translations (notification_id, user_id, locale, title, body)
			 values ($1, $2, $3, $4, $5)
			 on conflict (notification_id, user_id, locale) do update
			 set title = excluded.title, body = excluded.body, created_at = now()`,
			payload.NotificationID,
			claims.Subject,
			payload.TargetLocale,
			translated["title"],
			translated["body"],
		)
		response["translation"] = translated
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

func (s *Server) userAIDailyBalance(ctx context.Context, userID int64, permissions []string) aiDailyBalancePayload {
	limit := int64(numericPermissionValue(permissions, "user.ai.daily_token_limit"))
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

func pathNotificationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	notificationID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || notificationID <= 0 {
		writeError(w, http.StatusBadRequest, "通知 ID 不正确")
		return 0, false
	}
	return notificationID, true
}
