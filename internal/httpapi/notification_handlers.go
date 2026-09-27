package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

func normalizeNotificationTranslationLocale(value string) (string, bool) {
	locale := normalizeContentLocale(value)
	_, supported := supportedEditableContentLocales[locale]
	return locale, supported
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
	InternalID         int64               `json:"-"`
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
	s.cache.BumpUnreadEpoch(r.Context())
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) notifications(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	request, err := parseNotificationPageRequest(r.URL.Query(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusBadRequest, "通知分页参数不正确")
		return
	}
	page, err := s.loadNotificationPage(r.Context(), claims.Subject, request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取通知失败")
		return
	}
	writeBoundedCatalogJSON(w, map[string]any{
		"items": page.Items, "limit": request.Limit, "hasMore": page.HasMore, "nextCursor": page.NextCursor,
	})
}

func (s *Server) markNotificationRead(w http.ResponseWriter, r *http.Request) {
	notificationID, ok := s.pathNotificationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	tag, err := s.db.Exec(r.Context(), markNotificationReadSQL, notificationID, claims.Subject)
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
	var readBefore time.Time
	err := s.db.QueryRow(r.Context(), markAllNotificationsReadSQL, claims.Subject).Scan(&readBefore)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新通知状态失败")
		return
	}
	s.cache.InvalidateUnread(r.Context(), claims.Subject)
	writeJSON(w, http.StatusOK, map[string]any{"read": true, "readBefore": readBefore})
}

func (s *Server) unreadSummary(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	summary, err := s.cache.LoadUnread(r.Context(), claims.Subject, func(ctx context.Context) (querycache.UnreadSummary, error) {
		items, loadErr := loadUnreadTruth(ctx, s.db, unreadTruthInternalUsersSQL, []int64{claims.Subject})
		if loadErr != nil {
			return querycache.UnreadSummary{}, loadErr
		}
		if len(items) != 1 {
			return querycache.UnreadSummary{}, pgx.ErrNoRows
		}
		return items[0].Summary, nil
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
	req.TargetLocale, ok = normalizeNotificationTranslationLocale(req.TargetLocale)
	if !ok {
		writeError(w, http.StatusBadRequest, "目标语言不正确")
		return
	}
	claims := currentClaims(r)
	var title, body, sourceLocale, kind string
	err := s.db.QueryRow(
		r.Context(),
		`select title, body, source_locale, kind from notifications
		 where id = $1 and (recipient_id is null or recipient_id = $2)`,
		notificationID,
		claims.Subject,
	).Scan(&title, &body, &sourceLocale, &kind)
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
	if err = enqueueAITaskTx(r.Context(), tx, "ai.notification_translation.requested", taskID, taskUID, aiTaskNotificationTranslation, r.Header.Get("X-Request-ID")); err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "创建翻译任务失败")
		return
	}
	s.writeAITaskLog(r.Context(), taskID, "info", "task_queued", "Notification translation committed to the reliable outbox", payload)
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
	var resultRaw, payloadRaw []byte
	var inputTokens, outputTokens int64
	err := s.db.QueryRow(
		r.Context(),
		`select status, result, payload, error, input_tokens, output_tokens
		 from ai_tasks where task_uid = $1 and created_by = $2 and task_type = $3`,
		taskUID,
		claims.Subject,
		aiTaskNotificationTranslation,
	).Scan(&status, &resultRaw, &payloadRaw, &errorMessage, &inputTokens, &outputTokens)
	if err == pgx.ErrNoRows {
		writeError(w, http.StatusNotFound, "翻译任务不存在")
		return
	}
	if err != nil {
		logAITranslationFailure("notification_result_task", taskUID, err)
		writeError(w, http.StatusInternalServerError, "读取翻译任务失败")
		return
	}
	response := map[string]any{"status": status, "error": errorMessage, "inputTokens": inputTokens, "outputTokens": outputTokens}
	if status == "completed" {
		translated, decodeErr := decodeStoredNotificationTranslation(payloadRaw, resultRaw)
		if decodeErr != nil {
			logAITranslationFailure("notification_result_decode", taskUID, decodeErr)
			writeError(w, http.StatusInternalServerError, "翻译任务数据损坏")
			return
		}
		response["translation"] = translated
	}
	writeJSON(w, http.StatusOK, response)
}

func decodeStoredNotificationTranslation(payloadRaw, resultRaw []byte) (map[string]string, error) {
	var result map[string]any
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return nil, fmt.Errorf("decode stored notification translation result: %w", err)
	}
	_, translated, err := decodeNotificationTranslation(payloadRaw, result)
	return translated, err
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
