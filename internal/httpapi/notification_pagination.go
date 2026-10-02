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
	notificationCursorVersion      = 1
	notificationDefaultPageLimit   = 50
	notificationMaximumPageLimit   = 100
	notificationMaximumCursorBytes = 2048
)

type notificationPageRequest struct {
	Kind   string
	Limit  int
	Scope  string
	Cursor *notificationPageCursor
}

type notificationPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	UpdatedAt time.Time `json:"updatedAt"`
	ID        int64     `json:"id"`
}

type notificationPage struct {
	Items      []notificationItem
	HasMore    bool
	NextCursor string
}

func parseNotificationPageRequest(values url.Values, userID int64) (notificationPageRequest, error) {
	if values.Has("page") || values.Has("offset") {
		return notificationPageRequest{}, errors.New("offset pagination is not supported")
	}
	kind := strings.TrimSpace(values.Get("kind"))
	if kind != "" && !notificationKinds[kind] {
		return notificationPageRequest{}, errors.New("invalid notification kind")
	}
	limit := notificationDefaultPageLimit
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > notificationMaximumPageLimit {
			return notificationPageRequest{}, errors.New("invalid notification page limit")
		}
		limit = parsed
	}
	scope := notificationPageScope(userID, kind, limit)
	cursor, err := decodeNotificationPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return notificationPageRequest{}, err
	}
	return notificationPageRequest{Kind: kind, Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func notificationPageScope(userID int64, kind string, limit int) string {
	material, _ := json.Marshal(struct {
		Version int    `json:"version"`
		UserID  int64  `json:"userId"`
		Kind    string `json:"kind"`
		Limit   int    `json:"limit"`
	}{notificationCursorVersion, userID, kind, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeNotificationPageCursor(cursor notificationPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeNotificationPageCursor(raw, scope string) (*notificationPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > notificationMaximumCursorBytes {
		return nil, errors.New("invalid notification cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid notification cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor notificationPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid notification cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != notificationCursorVersion ||
		cursor.Scope != scope || cursor.UpdatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid notification cursor")
	}
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

func notificationPageSQL(userID int64, request notificationPageRequest) (string, []any) {
	args := []any{userID}
	kindPredicate := ""
	if request.Kind != "" {
		args = append(args, request.Kind)
		kindPredicate = ` and n.kind=$2`
	}
	cursorPredicate := ""
	if request.Cursor != nil {
		start := len(args) + 1
		cursorPredicate = ` and (n.updated_at,n.id)<($` + strconv.Itoa(start) + `,$` + strconv.Itoa(start+1) + `)`
		args = append(args, request.Cursor.UpdatedAt, request.Cursor.ID)
	}
	limitParameter := "$" + strconv.Itoa(len(args)+1)
	args = append(args, request.Limit+1)
	query := `with candidate as (
		(select n.id,n.updated_at from notifications n
			where n.recipient_id=$1` + kindPredicate + cursorPredicate + `
			order by n.updated_at desc,n.id desc limit ` + limitParameter + `)
		union all
		(select n.id,n.updated_at from notifications n
			where n.recipient_id is null` + kindPredicate + cursorPredicate + `
			order by n.updated_at desc,n.id desc limit ` + limitParameter + `)
	), selected as (
		select id,updated_at from candidate order by updated_at desc,id desc limit ` + limitParameter + `
	)
	select n.id,n.public_id,n.kind,n.title,n.body,n.source_locale,n.data,
		coalesce(actor_page.items,'[]'::jsonb),
		case when receipt.notification_id is not null then receipt.read_at is not null
			else n.id<=coalesce(watermark.max_notification_id,0)
				and n.updated_at<=coalesce(watermark.read_at,'-infinity'::timestamptz) end,
		n.created_at,n.updated_at,n.kind<>'system'
	from selected page
	join notifications n on n.id=page.id
	left join notification_receipts receipt on receipt.notification_id=n.id and receipt.user_id=$1
	left join notification_read_watermarks watermark on watermark.user_id=$1
	left join lateral (select jsonb_agg(jsonb_build_object('id',selected_actor.public_id,'username',selected_actor.username)
		order by selected_actor.id desc) items
		from (select actor.id,actor.public_id,actor.username
			from notification_actors na join users actor on actor.id=na.actor_id
			where na.notification_id=n.id order by actor.id desc limit 3) selected_actor) actor_page on true
	order by n.updated_at desc,n.id desc`
	return query, args
}

func (s *Server) loadNotificationPage(ctx context.Context, userID int64, request notificationPageRequest) (notificationPage, error) {
	query, args := notificationPageSQL(userID, request)
	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return notificationPage{}, err
	}
	defer rows.Close()
	items := make([]notificationItem, 0, request.Limit+1)
	for rows.Next() {
		var item notificationItem
		var rawData, rawActors []byte
		if err = rows.Scan(
			&item.InternalID, &item.ID, &item.Kind, &item.Title, &item.Body, &item.SourceLocale,
			&rawData, &rawActors, &item.Read, &item.CreatedAt, &item.UpdatedAt, &item.TranslationAllowed,
		); err != nil {
			return notificationPage{}, err
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
		return notificationPage{}, err
	}
	hasMore := len(items) > request.Limit
	if hasMore {
		items = items[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		last := items[len(items)-1]
		nextCursor = encodeNotificationPageCursor(notificationPageCursor{
			Version: notificationCursorVersion, Scope: request.Scope, UpdatedAt: last.UpdatedAt, ID: last.InternalID,
		})
	}
	return notificationPage{Items: items, HasMore: hasMore, NextCursor: nextCursor}, nil
}

const markAllNotificationsReadSQL = `insert into notification_read_watermarks(user_id,max_notification_id,read_at,updated_at)
	values($1,coalesce((select max(id) from notifications),0),clock_timestamp(),clock_timestamp())
	on conflict(user_id) do update set
		max_notification_id=greatest(notification_read_watermarks.max_notification_id,excluded.max_notification_id),
		read_at=greatest(notification_read_watermarks.read_at,excluded.read_at),updated_at=excluded.updated_at
returning read_at`

const markNotificationReadSQL = `insert into notification_receipts(notification_id,user_id,read_at)
select n.id,$2,clock_timestamp() from notifications n
left join notification_receipts receipt on receipt.notification_id=n.id and receipt.user_id=$2
left join notification_read_watermarks watermark on watermark.user_id=$2
where n.id=$1 and (n.recipient_id is null or n.recipient_id=$2) and ` + notificationUnreadPredicateSQL + `
on conflict(notification_id,user_id) do update set read_at=excluded.read_at
where notification_receipts.read_at is null`

const notificationUnreadPredicateSQL = `(
	(receipt.notification_id is not null and receipt.read_at is null)
	or (receipt.notification_id is null and not (
		n.id<=coalesce(watermark.max_notification_id,0)
		and n.updated_at<=coalesce(watermark.read_at,'-infinity'::timestamptz)
	))
)`
