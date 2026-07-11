package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

type startConversationRequest struct {
	UserID int64 `json:"userId"`
}

type sendDirectMessageRequest struct {
	Body string `json:"body"`
}

type directConversationSummary struct {
	ID          int64      `json:"id"`
	PartnerID   int64      `json:"partnerId"`
	Username    string     `json:"username"`
	DisplayName string     `json:"displayName"`
	LastMessage string     `json:"lastMessage"`
	LastAt      *time.Time `json:"lastAt,omitempty"`
	UnreadCount int64      `json:"unreadCount"`
}

type directMessageItem struct {
	ID             int64      `json:"id"`
	ConversationID int64      `json:"conversationId"`
	SenderID       int64      `json:"senderId"`
	RecipientID    int64      `json:"recipientId"`
	Body           string     `json:"body"`
	ReadAt         *time.Time `json:"readAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	rows, err := s.db.Query(
		r.Context(),
		`select c.id, partner.id, partner.username, partner.display_name,
		        coalesce(last_message.body, ''), last_message.created_at,
		        (select count(*) from direct_messages unread
		         where unread.conversation_id = c.id and unread.recipient_id = $1 and unread.read_at is null)
		 from direct_conversations c
		 join users partner on partner.id = case when c.user_low_id = $1 then c.user_high_id else c.user_low_id end
		 left join lateral (
			select body, created_at from direct_messages m
			where m.conversation_id = c.id order by m.created_at desc limit 1
		 ) last_message on true
		 where c.user_low_id = $1 or c.user_high_id = $1
		 order by coalesce(last_message.created_at, c.updated_at) desc`,
		claims.Subject,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊会话失败")
		return
	}
	defer rows.Close()
	items := make([]directConversationSummary, 0)
	for rows.Next() {
		var item directConversationSummary
		if err := rows.Scan(&item.ID, &item.PartnerID, &item.Username, &item.DisplayName, &item.LastMessage, &item.LastAt, &item.UnreadCount); err != nil {
			writeError(w, http.StatusInternalServerError, "读取私聊会话失败")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) startConversation(w http.ResponseWriter, r *http.Request) {
	var req startConversationRequest
	if err := decodeJSON(r, &req); err != nil || req.UserID <= 0 {
		writeError(w, http.StatusBadRequest, "目标用户不正确")
		return
	}
	claims := currentClaims(r)
	if req.UserID == claims.Subject {
		writeError(w, http.StatusBadRequest, "不能与自己创建私聊")
		return
	}
	if !s.userHasPermission(r.Context(), req.UserID, "user.message.receive") {
		writeError(w, http.StatusForbidden, "对方没有接收私聊的权限")
		return
	}
	low, high := orderedUserIDs(claims.Subject, req.UserID)
	var conversationID int64
	err := s.db.QueryRow(
		r.Context(),
		`insert into direct_conversations (user_low_id, user_high_id)
		 select $1, $2 where exists(select 1 from users where id = $2 and status = 'active')
		 on conflict (user_low_id, user_high_id) do update set updated_at = direct_conversations.updated_at
		 returning id`,
		low,
		high,
	).Scan(&conversationID)
	if err != nil {
		writeError(w, http.StatusNotFound, "目标用户不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": conversationID})
}

func (s *Server) conversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := pathConversationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	if !s.isConversationMember(r, conversationID, claims.Subject) {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	_, _ = s.db.Exec(
		r.Context(),
		`update direct_messages set read_at = coalesce(read_at, now())
		 where conversation_id = $1 and recipient_id = $2 and read_at is null`,
		conversationID,
		claims.Subject,
	)
	rows, err := s.db.Query(
		r.Context(),
		`select id, conversation_id, sender_id, recipient_id, body, read_at, created_at
		 from (
			select id, conversation_id, sender_id, recipient_id, body, read_at, created_at
			from direct_messages where conversation_id = $1 order by created_at desc limit 100
		 ) recent order by created_at`,
		conversationID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊消息失败")
		return
	}
	defer rows.Close()
	items := make([]directMessageItem, 0)
	for rows.Next() {
		var item directMessageItem
		if err := rows.Scan(&item.ID, &item.ConversationID, &item.SenderID, &item.RecipientID, &item.Body, &item.ReadAt, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "读取私聊消息失败")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) sendConversationMessage(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := pathConversationID(w, r)
	if !ok {
		return
	}
	var req sendDirectMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if req.Body == "" || len([]rune(req.Body)) > 4000 {
		writeError(w, http.StatusBadRequest, "私聊消息长度需要在 1 到 4000 个字符之间")
		return
	}
	claims := currentClaims(r)
	var low, high int64
	if err := s.db.QueryRow(r.Context(), `select user_low_id, user_high_id from direct_conversations where id = $1`, conversationID).Scan(&low, &high); err != nil {
		writeError(w, http.StatusNotFound, "私聊会话不存在")
		return
	}
	if claims.Subject != low && claims.Subject != high {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	recipientID := low
	if claims.Subject == low {
		recipientID = high
	}
	if !s.userHasPermission(r.Context(), recipientID, "user.message.receive") {
		writeError(w, http.StatusForbidden, "对方没有接收私聊的权限")
		return
	}
	active := false
	_ = s.db.QueryRow(
		r.Context(),
		`select exists(select 1 from user_chat_presence where user_id = $1 and conversation_id = $2 and expires_at > now())`,
		recipientID,
		conversationID,
	).Scan(&active)
	var item directMessageItem
	err := s.db.QueryRow(
		r.Context(),
		`insert into direct_messages (conversation_id, sender_id, recipient_id, body, read_at)
		 values ($1, $2, $3, $4, case when $5 then now() else null end)
		 returning id, conversation_id, sender_id, recipient_id, body, read_at, created_at`,
		conversationID,
		claims.Subject,
		recipientID,
		req.Body,
		active,
	).Scan(&item.ID, &item.ConversationID, &item.SenderID, &item.RecipientID, &item.Body, &item.ReadAt, &item.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发送私聊消息失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update direct_conversations set updated_at = now() where id = $1`, conversationID)
	notificationQueued := false
	if !active && s.queue != nil {
		var senderName string
		_ = s.db.QueryRow(r.Context(), `select coalesce(nullif(display_name, ''), username) from users where id = $1`, claims.Subject).Scan(&senderName)
		preview := req.Body
		if len([]rune(preview)) > 160 {
			preview = string([]rune(preview)[:160]) + "..."
		}
		err := s.queue.PublishTask(r.Context(), notificationTaskCode, notificationEvent{
			Action: "email", RecipientID: recipientID,
			Title: senderName + " 给你发来一条私聊", Body: preview,
		})
		notificationQueued = err == nil
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": item, "notificationQueued": notificationQueued, "suppressed": active})
}

func (s *Server) updateConversationPresence(w http.ResponseWriter, r *http.Request) {
	conversationID, ok := pathConversationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	if !s.isConversationMember(r, conversationID, claims.Subject) {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	_, err := s.db.Exec(
		r.Context(),
		`insert into user_chat_presence (user_id, conversation_id, expires_at, updated_at)
		 values ($1, $2, now() + interval '30 seconds', now())
		 on conflict (user_id) do update
		 set conversation_id = excluded.conversation_id, expires_at = excluded.expires_at, updated_at = now()`,
		claims.Subject,
		conversationID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新会话状态失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"active": true})
}

func (s *Server) isConversationMember(r *http.Request, conversationID int64, userID int64) bool {
	var exists bool
	_ = s.db.QueryRow(
		r.Context(),
		`select exists(select 1 from direct_conversations where id = $1 and (user_low_id = $2 or user_high_id = $2))`,
		conversationID,
		userID,
	).Scan(&exists)
	return exists
}

func orderedUserIDs(first int64, second int64) (int64, int64) {
	if first < second {
		return first, second
	}
	return second, first
}

func pathConversationID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	conversationID, err := strconv.ParseInt(strings.TrimSpace(r.PathValue("id")), 10, 64)
	if err != nil || conversationID <= 0 {
		writeError(w, http.StatusBadRequest, "会话 ID 不正确")
		return 0, false
	}
	return conversationID, true
}
