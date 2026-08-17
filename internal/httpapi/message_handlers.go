package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type startConversationRequest struct {
	UserID string `json:"userId"`
}

type sendDirectMessageRequest struct {
	Body string `json:"body"`
}

type directConversationSummary struct {
	ID                string             `json:"id"`
	PartnerID         string             `json:"partnerId"`
	Username          string             `json:"username"`
	AvatarURL         string             `json:"avatarUrl"`
	OnlineStatus      publicOnlineStatus `json:"onlineStatus"`
	LastMessage       string             `json:"lastMessage"`
	LastAt            *time.Time         `json:"lastAt,omitempty"`
	UnreadCount       int64              `json:"unreadCount"`
	CanMessage        bool               `json:"canMessage"`
	partnerInternalID int64
	showOnline        bool
}

type directMessageItem struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversationId"`
	SenderID       string     `json:"senderId"`
	RecipientID    string     `json:"recipientId"`
	Body           string     `json:"body"`
	ReadAt         *time.Time `json:"readAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	rows, err := s.db.Query(r.Context(), `select c.public_id,partner.id,partner.public_id,partner.username,partner.avatar_url,
		partner.show_online_status,
		not exists(select 1 from user_blocks block where
			(block.blocker_id=$1 and block.blocked_id=partner.id) or
			(block.blocker_id=partner.id and block.blocked_id=$1)),
		coalesce(last_message.body,''),last_message.created_at,
		(select count(*) from direct_messages unread
		 where unread.conversation_id=c.id and unread.recipient_id=$1 and unread.read_at is null)
		from direct_conversations c
		join users partner on partner.id=case when c.user_low_id=$1 then c.user_high_id else c.user_low_id end
		left join lateral (
			select body,created_at from direct_messages message
			where message.conversation_id=c.id order by message.id desc limit 1
		) last_message on true
		where c.user_low_id=$1 or c.user_high_id=$1
		order by coalesce(last_message.created_at,c.updated_at) desc,c.id desc`, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊会话失败")
		return
	}
	defer rows.Close()
	items := make([]directConversationSummary, 0)
	ossCfg := s.ossConfigFromSettings(r.Context())
	for rows.Next() {
		var item directConversationSummary
		if err = rows.Scan(&item.ID, &item.partnerInternalID, &item.PartnerID, &item.Username, &item.AvatarURL, &item.showOnline, &item.CanMessage, &item.LastMessage, &item.LastAt, &item.UnreadCount); err != nil {
			writeError(w, http.StatusInternalServerError, "读取私聊会话失败")
			return
		}
		item.AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, item.AvatarURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate conversation avatar URL")
			return
		}
		items = append(items, item)
	}
	partnerIDs := make([]int64, len(items))
	for index := range items {
		partnerIDs[index] = items[index].partnerInternalID
	}
	online := s.cache.UsersOnline(r.Context(), partnerIDs, time.Now(), s.cache.Config().PresenceTTL)
	for index := range items {
		items[index].OnlineStatus = mapPublicOnlineVisibility(items[index].showOnline, online[items[index].partnerInternalID])
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) startConversation(w http.ResponseWriter, r *http.Request) {
	var request startConversationRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "目标用户不正确")
		return
	}
	request.UserID = strings.ToLower(strings.TrimSpace(request.UserID))
	var targetUserID int64
	if err := s.db.QueryRow(r.Context(), `select id from users where public_id=$1 and status='active'`, request.UserID).Scan(&targetUserID); err != nil {
		writeError(w, http.StatusNotFound, "目标用户不存在")
		return
	}
	claims := currentClaims(r)
	if targetUserID == claims.Subject {
		writeError(w, http.StatusBadRequest, "不能与自己创建私聊")
		return
	}
	blocked, err := s.usersBlockEachOther(r.Context(), claims.Subject, targetUserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "检查私聊权限失败")
		return
	}
	if blocked {
		writeError(w, http.StatusForbidden, "当前无法与该用户私聊")
		return
	}
	if !s.userHasPermission(r.Context(), targetUserID, "user.message.receive") {
		writeError(w, http.StatusForbidden, "对方没有接收私聊的权限")
		return
	}
	low, high := orderedUserIDs(claims.Subject, targetUserID)
	var conversationPublicID string
	err = s.db.QueryRow(r.Context(), `insert into direct_conversations(user_low_id,user_high_id)
		values($1,$2) on conflict(user_low_id,user_high_id)
		do update set updated_at=direct_conversations.updated_at returning public_id`, low, high).Scan(&conversationPublicID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "创建私聊失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": conversationPublicID})
}

func (s *Server) conversationMessages(w http.ResponseWriter, r *http.Request) {
	conversationID, conversationPublicID, ok := s.pathConversationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	if !s.isConversationMember(r, conversationID, claims.Subject) {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	readTag, _ := s.db.Exec(r.Context(), `update direct_messages set read_at=now()
		where conversation_id=$1 and recipient_id=$2 and read_at is null`, conversationID, claims.Subject)
	if readTag.RowsAffected() > 0 {
		s.cache.AdjustUnread(r.Context(), claims.Subject, "messages", -readTag.RowsAffected())
		s.publishRealtimeUser(claims.Subject, "unread.changed", map[string]string{"kind": "messages"})
	}
	afterID := strings.TrimSpace(r.URL.Query().Get("after"))
	messageWhere := "conversation_id=$1"
	queryArgs := []any{conversationID, claims.Subject}
	if afterID != "" {
		messageWhere += " and id>(select id from direct_messages where public_id=$3 and conversation_id=$1)"
		queryArgs = append(queryArgs, afterID)
	}
	rows, err := s.db.Query(r.Context(), `select recent.public_id,sender.public_id,recipient.public_id,
		recent.body,case when recent.sender_id=$2 and not recipient.show_online_status then null else recent.read_at end,recent.created_at
		from (
			select public_id,sender_id,recipient_id,body,read_at,created_at,id
			from direct_messages where `+messageWhere+` order by id desc limit 100
		) recent
		join users sender on sender.id=recent.sender_id
		join users recipient on recipient.id=recent.recipient_id
		order by recent.id`, queryArgs...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊消息失败")
		return
	}
	defer rows.Close()
	items := make([]directMessageItem, 0)
	for rows.Next() {
		var item directMessageItem
		item.ConversationID = conversationPublicID
		if err = rows.Scan(&item.ID, &item.SenderID, &item.RecipientID, &item.Body, &item.ReadAt, &item.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "读取私聊消息失败")
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) sendConversationMessage(w http.ResponseWriter, r *http.Request) {
	conversationID, conversationPublicID, ok := s.pathConversationID(w, r)
	if !ok {
		return
	}
	var request sendDirectMessageRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "请求格式不正确")
		return
	}
	request.Body = strings.TrimSpace(request.Body)
	if request.Body == "" || len([]rune(request.Body)) > 4000 {
		writeError(w, http.StatusBadRequest, "私聊消息长度需要在 1 到 4000 个字符之间")
		return
	}
	claims := currentClaims(r)
	var low, high int64
	if err := s.db.QueryRow(r.Context(), `select user_low_id,user_high_id from direct_conversations where id=$1`, conversationID).Scan(&low, &high); err != nil {
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
	blocked, err := s.usersBlockEachOther(r.Context(), claims.Subject, recipientID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "检查私聊权限失败")
		return
	}
	if blocked {
		writeError(w, http.StatusForbidden, "当前无法与该用户私聊")
		return
	}
	if !s.userHasPermission(r.Context(), recipientID, "user.message.receive") {
		writeError(w, http.StatusForbidden, "对方没有接收私聊的权限")
		return
	}
	activeConversation, active := s.cache.ChatPresence(r.Context(), recipientID)
	active = active && activeConversation == conversationID
	var recipientShowsOnline bool
	_ = s.db.QueryRow(r.Context(), `select show_online_status from users where id=$1`, recipientID).Scan(&recipientShowsOnline)
	var item directMessageItem
	item.ConversationID = conversationPublicID
	err = s.db.QueryRow(r.Context(), `insert into direct_messages(conversation_id,sender_id,recipient_id,body,read_at)
		values($1,$2,$3,$4,case when $5 then now() else null end)
		returning public_id,body,read_at,created_at`,
		conversationID, claims.Subject, recipientID, request.Body, active).
		Scan(&item.ID, &item.Body, &item.ReadAt, &item.CreatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发送私聊消息失败")
		return
	}
	if !active {
		s.cache.AdjustUnread(r.Context(), recipientID, "messages", 1)
	}
	s.publishRealtimeUser(recipientID, "message.created", map[string]any{"conversationId": conversationPublicID, "messageId": item.ID})
	item.SenderID = claims.PublicSubject
	if !recipientShowsOnline {
		item.ReadAt = nil
	}
	if err = s.db.QueryRow(r.Context(), `select public_id from users where id=$1`, recipientID).Scan(&item.RecipientID); err != nil {
		writeError(w, http.StatusInternalServerError, "发送私聊消息失败")
		return
	}
	_, _ = s.db.Exec(r.Context(), `update direct_conversations set updated_at=now() where id=$1`, conversationID)
	notificationQueued := false
	if !active && (s.queue != nil || s.cfg.NATS.OutboxEnabled) {
		var senderName string
		_ = s.db.QueryRow(r.Context(), `select username from users where id=$1`, claims.Subject).Scan(&senderName)
		preview := request.Body
		if len([]rune(preview)) > 160 {
			preview = string([]rune(preview)[:160]) + "..."
		}
		err = s.enqueueNotificationTask(r.Context(), notificationEvent{
			Action: "email", RecipientID: recipientID,
			Title: senderName + " 给你发来一条私聊", Body: preview,
		})
		notificationQueued = err == nil
	}
	response := map[string]any{"message": item}
	if recipientShowsOnline {
		response["notificationQueued"] = notificationQueued
		response["suppressed"] = active
	}
	writeJSON(w, http.StatusCreated, response)
}

func (s *Server) updateConversationPresence(w http.ResponseWriter, r *http.Request) {
	conversationID, _, ok := s.pathConversationID(w, r)
	if !ok {
		return
	}
	claims := currentClaims(r)
	if !s.isConversationMember(r, conversationID, claims.Subject) {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	s.cache.TouchChatPresence(r.Context(), claims.Subject, conversationID, 30*time.Second)
	writeJSON(w, http.StatusOK, map[string]bool{"active": true})
}

func (s *Server) isConversationMember(r *http.Request, conversationID, userID int64) bool {
	var exists bool
	_ = s.db.QueryRow(r.Context(), `select exists(select 1 from direct_conversations
		where id=$1 and (user_low_id=$2 or user_high_id=$2))`, conversationID, userID).Scan(&exists)
	return exists
}

func orderedUserIDs(first, second int64) (int64, int64) {
	if first < second {
		return first, second
	}
	return second, first
}

func (s *Server) pathConversationID(w http.ResponseWriter, r *http.Request) (int64, string, bool) {
	publicID := strings.ToLower(strings.TrimSpace(r.PathValue("id")))
	var internalID int64
	err := s.db.QueryRow(r.Context(), `select id from direct_conversations where public_id=$1`, publicID).Scan(&internalID)
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "会话不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取会话失败")
		}
		return 0, "", false
	}
	return internalID, publicID, true
}
