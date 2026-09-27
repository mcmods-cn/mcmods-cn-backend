package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	conversationCursorVersion     = 1
	conversationDefaultPageLimit  = 30
	conversationMaximumPageLimit  = 100
	directMessageCursorVersion    = 1
	directMessageDefaultPageLimit = 100
	directMessageMaximumPageLimit = 100
	messageMaximumCursorBytes     = 2048
)

var errInvalidDirectMessageAfterAnchor = errors.New("direct-message incremental anchor does not belong to the conversation")

type conversationPageRequest struct {
	Limit  int
	Scope  string
	Cursor *conversationPageCursor
}

type conversationPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	UpdatedAt time.Time `json:"updatedAt"`
	ID        int64     `json:"id"`
}

type conversationPage struct {
	Items      []directConversationSummary
	HasMore    bool
	NextCursor string
}

type directMessagePageRequest struct {
	Limit   int
	Scope   string
	Cursor  *directMessagePageCursor
	AfterID string
}

type directMessagePageCursor struct {
	Version int    `json:"v"`
	Scope   string `json:"s"`
	ID      int64  `json:"id"`
}

type directMessagePage struct {
	Items      []directMessageItem
	HasMore    bool
	NextCursor string
}

func parseConversationPageRequest(values url.Values, userID int64) (conversationPageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return conversationPageRequest{}, errors.New("offset pagination is not supported")
	}
	limit, err := parseMessagePageLimit(values, conversationDefaultPageLimit, conversationMaximumPageLimit)
	if err != nil {
		return conversationPageRequest{}, err
	}
	rawCursor, err := singleMessageQueryValue(values, "cursor")
	if err != nil {
		return conversationPageRequest{}, err
	}
	scope := conversationPageScope(userID, limit)
	cursor, err := decodeConversationPageCursor(rawCursor, scope)
	if err != nil {
		return conversationPageRequest{}, err
	}
	return conversationPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func parseDirectMessagePageRequest(values url.Values, userID, conversationID int64) (directMessagePageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return directMessagePageRequest{}, errors.New("offset pagination is not supported")
	}
	limit, err := parseMessagePageLimit(values, directMessageDefaultPageLimit, directMessageMaximumPageLimit)
	if err != nil {
		return directMessagePageRequest{}, err
	}
	rawCursor, err := singleMessageQueryValue(values, "cursor")
	if err != nil {
		return directMessagePageRequest{}, err
	}
	afterID, err := singleMessageQueryValue(values, "after")
	if err != nil {
		return directMessagePageRequest{}, err
	}
	afterID = strings.ToLower(strings.TrimSpace(afterID))
	if afterID != "" && !validCatalogPublicID(afterID) {
		return directMessagePageRequest{}, errInvalidDirectMessageAfterAnchor
	}
	if strings.TrimSpace(rawCursor) != "" && afterID != "" {
		return directMessagePageRequest{}, errors.New("history cursor cannot be combined with an incremental anchor")
	}
	scope := directMessagePageScope(userID, conversationID, limit)
	cursor, err := decodeDirectMessagePageCursor(rawCursor, scope)
	if err != nil {
		return directMessagePageRequest{}, err
	}
	return directMessagePageRequest{Limit: limit, Scope: scope, Cursor: cursor, AfterID: afterID}, nil
}

func parseMessagePageLimit(values url.Values, fallback, maximum int) (int, error) {
	raw, err := singleMessageQueryValue(values, "limit")
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit < 1 || limit > maximum {
		return 0, errors.New("invalid message page limit")
	}
	return limit, nil
}

func singleMessageQueryValue(values url.Values, key string) (string, error) {
	entries, exists := values[key]
	if !exists {
		return "", nil
	}
	if len(entries) != 1 {
		return "", errors.New("duplicate message page parameter")
	}
	return entries[0], nil
}

func conversationPageScope(userID int64, limit int) string {
	return messagePageScope(struct {
		Version int   `json:"version"`
		UserID  int64 `json:"userId"`
		Limit   int   `json:"limit"`
	}{conversationCursorVersion, userID, limit})
}

func directMessagePageScope(userID, conversationID int64, limit int) string {
	return messagePageScope(struct {
		Version        int   `json:"version"`
		UserID         int64 `json:"userId"`
		ConversationID int64 `json:"conversationId"`
		Limit          int   `json:"limit"`
	}{directMessageCursorVersion, userID, conversationID, limit})
}

func messagePageScope(value any) string {
	material, _ := json.Marshal(value)
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeConversationPageCursor(cursor conversationPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeConversationPageCursor(raw, scope string) (*conversationPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	decoded, err := decodeMessageCursor(raw)
	if err != nil {
		return nil, errors.New("invalid conversation cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor conversationPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid conversation cursor")
	}
	if err = requireMessageCursorEOF(decoder); err != nil || cursor.Version != conversationCursorVersion ||
		cursor.Scope != scope || cursor.UpdatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid conversation cursor")
	}
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

func encodeDirectMessagePageCursor(cursor directMessagePageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeDirectMessagePageCursor(raw, scope string) (*directMessagePageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	decoded, err := decodeMessageCursor(raw)
	if err != nil {
		return nil, errors.New("invalid direct-message cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor directMessagePageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid direct-message cursor")
	}
	if err = requireMessageCursorEOF(decoder); err != nil || cursor.Version != directMessageCursorVersion ||
		cursor.Scope != scope || cursor.ID <= 0 {
		return nil, errors.New("invalid direct-message cursor")
	}
	return &cursor, nil
}

func decodeMessageCursor(raw string) ([]byte, error) {
	if len(raw) > messageMaximumCursorBytes {
		return nil, errors.New("message cursor is too large")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(decoded) == 0 || len(decoded) > messageMaximumCursorBytes {
		return nil, errors.New("invalid message cursor")
	}
	return decoded, nil
}

func requireMessageCursorEOF(decoder *json.Decoder) error {
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("message cursor contains trailing data")
	}
	return nil
}

func conversationPageSQL(userID int64, request conversationPageRequest) (string, []any) {
	args := []any{userID}
	cursorPredicate := ""
	if request.Cursor != nil {
		cursorPredicate = " and (c.updated_at,c.id)<($2,$3)"
		args = append(args, request.Cursor.UpdatedAt, request.Cursor.ID)
	}
	limitParameter := "$" + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit+1)
	query := `with candidate as (
		(select c.id,c.public_id,c.user_low_id,c.user_high_id,c.last_message_id,c.updated_at
		 from direct_conversations c where c.user_low_id=$1` + cursorPredicate + `
		 order by c.updated_at desc,c.id desc limit ` + limitParameter + `)
		union all
		(select c.id,c.public_id,c.user_low_id,c.user_high_id,c.last_message_id,c.updated_at
		 from direct_conversations c where c.user_high_id=$1` + cursorPredicate + `
		 order by c.updated_at desc,c.id desc limit ` + limitParameter + `)
	), selected as materialized (
		select c.id,c.public_id,c.user_low_id,c.user_high_id,c.last_message_id,c.updated_at
		from candidate c order by c.updated_at desc,c.id desc limit ` + limitParameter + `
	)
	select c.id,c.public_id,partner.id,partner.public_id,partner.username,partner.avatar_url,
		partner.show_online_status,
		not exists(select 1 from user_blocks block where
			(block.blocker_id=$1 and block.blocked_id=partner.id) or
			(block.blocker_id=partner.id and block.blocked_id=$1)),
		coalesce(last_message.body,''),last_message.created_at,coalesce(unread.unread_count,0),c.updated_at
	from selected c
	join users partner on partner.id=case when c.user_low_id=$1 then c.user_high_id else c.user_low_id end
	left join direct_messages last_message on last_message.id=c.last_message_id
	left join direct_conversation_unread_counts unread on unread.conversation_id=c.id and unread.user_id=$1
	order by c.updated_at desc,c.id desc`
	return query, args
}

func (s *Server) loadConversationPage(ctx context.Context, userID int64, request conversationPageRequest) (conversationPage, error) {
	query, args := conversationPageSQL(userID, request)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return conversationPage{}, err
	}
	defer rows.Close()
	items := make([]directConversationSummary, 0, request.Limit+1)
	for rows.Next() {
		var item directConversationSummary
		if err = rows.Scan(
			&item.internalID, &item.ID, &item.partnerInternalID, &item.PartnerID, &item.Username, &item.AvatarURL,
			&item.showOnline, &item.CanMessage, &item.LastMessage, &item.LastAt, &item.UnreadCount, &item.cursorUpdatedAt,
		); err != nil {
			return conversationPage{}, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return conversationPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		nextCursor = encodeConversationPageCursor(conversationPageCursor{
			Version: conversationCursorVersion, Scope: request.Scope, UpdatedAt: last.cursorUpdatedAt, ID: last.internalID,
		})
	}
	return conversationPage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

func directMessagePageSQL(conversationID, userID int64, request directMessagePageRequest) (string, []any) {
	args := []any{conversationID, userID}
	messagePredicate := "message.conversation_id=$1"
	messageOrder := "message.id desc"
	if request.Cursor != nil {
		messagePredicate += " and message.id<$3"
		args = append(args, request.Cursor.ID)
	} else if request.AfterID != "" {
		messagePredicate += " and message.id>(select id from direct_messages where public_id=$3 and conversation_id=$1)"
		messageOrder = "message.id"
		args = append(args, request.AfterID)
	}
	limitParameter := "$" + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit+1)
	query := `select recent.id,recent.public_id,sender.public_id,recipient.public_id,
		recent.body,case when recent.sender_id=$2 and not recipient.show_online_status then null else recent.read_at end,recent.created_at
	from (
		select message.id,message.public_id,message.sender_id,message.recipient_id,message.body,message.read_at,message.created_at
		from direct_messages message where ` + messagePredicate + ` order by ` + messageOrder + ` limit ` + limitParameter + `
	) recent
	join users sender on sender.id=recent.sender_id
	join users recipient on recipient.id=recent.recipient_id
	order by recent.id`
	return query, args
}

func (s *Server) loadDirectMessagePage(ctx context.Context, conversationID int64, conversationPublicID string, userID int64, request directMessagePageRequest) (directMessagePage, error) {
	query, args := directMessagePageSQL(conversationID, userID, request)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return directMessagePage{}, err
	}
	defer rows.Close()
	items := make([]directMessageItem, 0, request.Limit+1)
	for rows.Next() {
		var item directMessageItem
		item.ConversationID = conversationPublicID
		if err = rows.Scan(&item.internalID, &item.ID, &item.SenderID, &item.RecipientID, &item.Body, &item.ReadAt, &item.CreatedAt); err != nil {
			return directMessagePage{}, err
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return directMessagePage{}, err
	}
	if request.AfterID != "" && len(items) == 0 {
		var anchorBelongs bool
		if err = s.db.QueryRow(ctx, `select exists(select 1 from direct_messages where public_id=$1 and conversation_id=$2)`,
			request.AfterID, conversationID).Scan(&anchorBelongs); err != nil {
			return directMessagePage{}, err
		}
		if !anchorBelongs {
			return directMessagePage{}, errInvalidDirectMessageAfterAnchor
		}
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		if request.AfterID == "" {
			items = items[1:]
		} else {
			items = items[:request.Limit]
		}
	}
	nextCursor := ""
	if hasMore && request.AfterID == "" && len(items) > 0 {
		nextCursor = encodeDirectMessagePageCursor(directMessagePageCursor{
			Version: directMessageCursorVersion, Scope: request.Scope, ID: items[0].internalID,
		})
	}
	return directMessagePage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}
