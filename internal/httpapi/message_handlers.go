package httpapi

import (
	"context"
	"errors"
	"fmt"
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
	internalID        int64
	partnerInternalID int64
	showOnline        bool
	cursorUpdatedAt   time.Time
}

type directMessageItem struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversationId"`
	SenderID       string     `json:"senderId"`
	RecipientID    string     `json:"recipientId"`
	Body           string     `json:"body"`
	ReadAt         *time.Time `json:"readAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	internalID     int64
}

const advanceDirectConversationSQL = `update direct_conversations set
	updated_at=case when last_message_id is null or last_message_id<$2 then clock_timestamp() else updated_at end,
	last_message_id=case when last_message_id is null or last_message_id<$2 then $2 else last_message_id end
	where id=$1`

func createDirectMessageTx(ctx context.Context, tx pgx.Tx, conversationID, senderID, recipientID int64, body string, read bool, emailEvent *notificationEvent, traceID string) (directMessageItem, error) {
	var item directMessageItem
	var messageID int64
	if err := tx.QueryRow(ctx, `insert into direct_messages(conversation_id,sender_id,recipient_id,body,read_at)
		values($1,$2,$3,$4,case when $5 then now() else null end)
		returning id,public_id,body,read_at,created_at`, conversationID, senderID, recipientID, body, read).
		Scan(&messageID, &item.ID, &item.Body, &item.ReadAt, &item.CreatedAt); err != nil {
		return directMessageItem{}, err
	}
	tag, err := tx.Exec(ctx, advanceDirectConversationSQL, conversationID, messageID)
	if err != nil {
		return directMessageItem{}, err
	}
	if tag.RowsAffected() != 1 {
		return directMessageItem{}, fmt.Errorf("direct conversation %d disappeared while sending", conversationID)
	}
	if emailEvent != nil {
		if emailEvent.Action != "email" || emailEvent.RecipientID != recipientID || emailEvent.TemplateKey == "" {
			return directMessageItem{}, errors.New("direct message email event is invalid")
		}
		if err = enqueueNotificationTaskTx(ctx, tx, "notification.direct_message_email.requested", "direct_message", item.ID, traceID, *emailEvent); err != nil {
			return directMessageItem{}, err
		}
	}
	return item, nil
}

func (s *Server) conversations(w http.ResponseWriter, r *http.Request) {
	claims := currentClaims(r)
	request, err := parseConversationPageRequest(r.URL.Query(), claims.Subject)
	if err != nil {
		writeError(w, http.StatusBadRequest, "私聊会话分页参数不正确")
		return
	}
	page, err := s.loadConversationPage(r.Context(), claims.Subject, request)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊会话失败")
		return
	}
	ossCfg := s.ossConfigFromSettings(r.Context())
	for index := range page.Items {
		page.Items[index].AvatarURL, err = s.resolveStoredOSSObjectAccessURLWithConfig(r.Context(), ossCfg, page.Items[index].AvatarURL)
		if err != nil {
			writeError(w, http.StatusBadGateway, "failed to generate conversation avatar URL")
			return
		}
	}
	partnerIDs := make([]int64, len(page.Items))
	for index := range page.Items {
		partnerIDs[index] = page.Items[index].partnerInternalID
	}
	online := s.cache.UsersOnline(r.Context(), partnerIDs, time.Now(), s.cache.Config().PresenceTTL)
	for index := range page.Items {
		page.Items[index].OnlineStatus = mapPublicOnlineVisibility(page.Items[index].showOnline, online[page.Items[index].partnerInternalID])
	}
	writeBoundedCatalogJSON(w, map[string]any{
		"items": page.Items, "limit": request.Limit, "hasMore": page.HasMore, "nextCursor": page.NextCursor,
	})
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
	member, err := s.isConversationMember(r.Context(), conversationID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊成员失败")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	request, err := parseDirectMessagePageRequest(r.URL.Query(), claims.Subject, conversationID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "私聊消息分页参数不正确")
		return
	}
	page, err := s.loadDirectMessagePage(r.Context(), conversationID, conversationPublicID, claims.Subject, request)
	if err != nil {
		if errors.Is(err, errInvalidDirectMessageAfterAnchor) {
			writeError(w, http.StatusBadRequest, "私聊消息增量锚点不正确")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取私聊消息失败")
		return
	}
	readTag, err := s.db.Exec(r.Context(), `update direct_messages set read_at=now()
		where conversation_id=$1 and recipient_id=$2 and read_at is null`, conversationID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "更新私聊已读状态失败")
		return
	}
	if readTag.RowsAffected() > 0 {
		s.cache.AdjustUnread(r.Context(), claims.Subject, "messages", -readTag.RowsAffected())
		s.publishRealtimeUser(claims.Subject, "unread.changed", map[string]string{"kind": "messages"})
	}
	writeBoundedCatalogJSON(w, map[string]any{
		"items": page.Items, "limit": request.Limit, "hasMore": page.HasMore, "nextCursor": page.NextCursor,
	})
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
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "私聊会话不存在")
		} else {
			writeError(w, http.StatusInternalServerError, "读取私聊成员失败")
		}
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
	var senderName, recipientPublicID string
	if err = s.db.QueryRow(r.Context(), `select sender.username,recipient.public_id from users sender cross join users recipient
		where sender.id=$1 and recipient.id=$2`, claims.Subject, recipientID).Scan(&senderName, &recipientPublicID); err != nil {
		writeError(w, http.StatusInternalServerError, "发送私聊消息失败")
		return
	}
	var emailEvent *notificationEvent
	if !active {
		preview := request.Body
		if len([]rune(preview)) > 160 {
			preview = string([]rune(preview)[:160]) + "..."
		}
		event := notificationEvent{
			Action: "email", RecipientID: recipientID, TemplateKey: "direct_message_email",
			TemplateValues: map[string]string{"sender": senderName, "preview": preview},
		}
		emailEvent = &event
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发送私聊消息失败")
		return
	}
	defer tx.Rollback(r.Context())
	item, err := createDirectMessageTx(r.Context(), tx, conversationID, claims.Subject, recipientID, request.Body, active, emailEvent, r.Header.Get("X-Request-ID"))
	if err == nil {
		err = tx.Commit(r.Context())
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发送私聊消息失败")
		return
	}
	if !active {
		s.cache.AdjustUnread(r.Context(), recipientID, "messages", 1)
	}
	s.publishRealtimeUser(recipientID, "message.created", map[string]any{"conversationId": conversationPublicID, "messageId": item.ID})
	item.ConversationID = conversationPublicID
	item.SenderID = claims.PublicSubject
	item.RecipientID = recipientPublicID
	if !recipientShowsOnline {
		item.ReadAt = nil
	}
	notificationQueued := emailEvent != nil
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
	member, err := s.isConversationMember(r.Context(), conversationID, claims.Subject)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取私聊成员失败")
		return
	}
	if !member {
		writeError(w, http.StatusForbidden, "无权访问该私聊")
		return
	}
	s.cache.TouchChatPresence(r.Context(), claims.Subject, conversationID, 30*time.Second)
	writeJSON(w, http.StatusOK, map[string]bool{"active": true})
}

func (s *Server) isConversationMember(ctx context.Context, conversationID, userID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `select exists(select 1 from direct_conversations
		where id=$1 and (user_low_id=$2 or user_high_id=$2))`, conversationID, userID).Scan(&exists)
	return exists, err
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
