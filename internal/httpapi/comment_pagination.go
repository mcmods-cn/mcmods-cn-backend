package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"
)

const (
	commentPageCursorVersion  = 1
	maxCommentPageCursorBytes = 2048
)

type commentRootPageCursor struct {
	Version         int        `json:"v"`
	Scope           string     `json:"s"`
	Sort            string     `json:"o"`
	Pinned          bool       `json:"p"`
	PinnedAt        *time.Time `json:"pa,omitempty"`
	CreatedAt       time.Time  `json:"c"`
	HotScore        string     `json:"h,omitempty"`
	DescendantCount int        `json:"d,omitempty"`
	ID              int64      `json:"id"`
}

type commentReplyPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"s"`
	CreatedAt time.Time `json:"c"`
	ID        int64     `json:"id"`
}

type commentWatchPageCursor struct {
	Version        int       `json:"v"`
	Scope          string    `json:"s"`
	Sort           string    `json:"o"`
	UnreadCount    int       `json:"u,omitempty"`
	LastActivityAt time.Time `json:"a"`
	CreatedAt      time.Time `json:"c"`
	ID             int64     `json:"id"`
}

func encodeCommentRootPageCursor(cursor commentRootPageCursor) string {
	return encodeCommentPageCursor(cursor)
}

func encodeCommentReplyPageCursor(cursor commentReplyPageCursor) string {
	return encodeCommentPageCursor(cursor)
}

func encodeCommentWatchPageCursor(cursor commentWatchPageCursor) string {
	return encodeCommentPageCursor(cursor)
}

func encodeCommentPageCursor(cursor any) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeCommentRootPageCursor(raw, scope, sortName string) (*commentRootPageCursor, error) {
	var cursor commentRootPageCursor
	if err := decodeCommentPageCursor(raw, &cursor); err != nil || cursor.Version != commentPageCursorVersion ||
		cursor.Scope != scope || cursor.Sort != sortName || cursor.ID <= 0 || cursor.CreatedAt.IsZero() ||
		cursor.Pinned != (cursor.PinnedAt != nil) {
		return nil, errors.New("评论游标不正确")
	}
	switch sortName {
	case "latest", "oldest":
	case "hot":
		score, err := strconv.ParseFloat(cursor.HotScore, 64)
		if err != nil || score < 0 || math.IsNaN(score) || math.IsInf(score, 0) {
			return nil, errors.New("评论游标不正确")
		}
	case "replies":
		if cursor.DescendantCount < 0 {
			return nil, errors.New("评论游标不正确")
		}
	default:
		return nil, errors.New("评论游标不正确")
	}
	return &cursor, nil
}

func decodeCommentReplyPageCursor(raw, scope string) (*commentReplyPageCursor, error) {
	var cursor commentReplyPageCursor
	if err := decodeCommentPageCursor(raw, &cursor); err != nil || cursor.Version != commentPageCursorVersion ||
		cursor.Scope != scope || cursor.ID <= 0 || cursor.CreatedAt.IsZero() {
		return nil, errors.New("回复游标不正确")
	}
	return &cursor, nil
}

func decodeCommentWatchPageCursor(raw, scope, sortName string) (*commentWatchPageCursor, error) {
	var cursor commentWatchPageCursor
	if err := decodeCommentPageCursor(raw, &cursor); err != nil || cursor.Version != commentPageCursorVersion ||
		cursor.Scope != scope || cursor.Sort != sortName || cursor.ID <= 0 || cursor.CreatedAt.IsZero() ||
		cursor.LastActivityAt.IsZero() || cursor.UnreadCount < 0 {
		return nil, errors.New("插眼游标不正确")
	}
	return &cursor, nil
}

func decodeCommentPageCursor(raw string, destination any) error {
	if raw == "" || len(raw) > maxCommentPageCursorBytes*2 {
		return errors.New("游标不正确")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > maxCommentPageCursorBytes {
		return errors.New("游标不正确")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(destination); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("游标不正确")
	}
	return nil
}

func commentRootCursorScope(target commentTargetInfo, viewerID int64) string {
	versionID := int64(0)
	if target.VersionID != nil {
		versionID = *target.VersionID
	}
	return fmt.Sprintf("%s:%d:%d:%d", target.Type, target.InternalID, versionID, viewerID)
}

func commentReplyCursorScope(commentID, viewerID int64) string {
	return fmt.Sprintf("comment:%d:%d", commentID, viewerID)
}

func commentWatchCursorScope(userID int64, filter string) string {
	return fmt.Sprintf("user:%d:%s", userID, filter)
}

func (s *Server) queryCommentTargetVisibleTotal(ctx context.Context, target commentTargetInfo, viewerID int64) (int64, error) {
	return queryCommentTargetVisibleTotalWithQueryer(ctx, s.db, target, viewerID)
}

func queryCommentTargetVisibleTotalWithQueryer(ctx context.Context, queryer commentTargetQueryer, target commentTargetInfo, viewerID int64) (int64, error) {
	versionKey := int64(0)
	if target.VersionID != nil {
		versionKey = *target.VersionID
	}
	var total int64
	err := queryer.QueryRow(ctx, `select greatest(
		coalesce((select target.visible_count from comment_target_counts target
			where target.target_type=$1 and target.target_id=$2 and target.target_version_key=$3),0)-
		case when $4::bigint=0 then 0 else coalesce((
			select sum(author.visible_count)
			from user_blocks block
			join comment_target_author_counts author
			  on author.author_id=block.blocked_id and author.target_type=$1
			 and author.target_id=$2 and author.target_version_key=$3
			where block.blocker_id=$4
		),0) end,0)`, target.Type, target.InternalID, versionKey, viewerID).Scan(&total)
	return total, err
}

func buildCommentRootPageQuery(target commentTargetInfo, viewerID int64, limit int, sortName string, cursor *commentRootPageCursor) (string, []any) {
	order := "c.created_at desc,c.id desc"
	switch sortName {
	case "oldest":
		order = "c.created_at,c.id"
	case "hot":
		order = "c.hot_score desc,c.created_at desc,c.id desc"
	case "replies":
		order = "c.descendant_count desc,c.created_at desc,c.id desc"
	}
	args := []any{target.Type, target.InternalID, target.VersionID, viewerID, limit + 1}
	predicate := ""
	if cursor != nil {
		switch sortName {
		case "latest":
			if cursor.Pinned {
				predicate = ` and ((c.pinned_at is not null and (c.pinned_at,c.created_at,c.id)<
					($6::timestamptz,$7::timestamptz,$8::bigint)) or c.pinned_at is null)`
				args = append(args, *cursor.PinnedAt, cursor.CreatedAt, cursor.ID)
			} else {
				predicate = ` and c.pinned_at is null and (c.created_at,c.id)<($6::timestamptz,$7::bigint)`
				args = append(args, cursor.CreatedAt, cursor.ID)
			}
		case "oldest":
			if cursor.Pinned {
				predicate = ` and ((c.pinned_at is not null and (c.pinned_at<$6::timestamptz or
					(c.pinned_at=$6::timestamptz and (c.created_at,c.id)>($7::timestamptz,$8::bigint))))
					or c.pinned_at is null)`
				args = append(args, *cursor.PinnedAt, cursor.CreatedAt, cursor.ID)
			} else {
				predicate = ` and c.pinned_at is null and (c.created_at,c.id)>($6::timestamptz,$7::bigint)`
				args = append(args, cursor.CreatedAt, cursor.ID)
			}
		case "hot":
			if cursor.Pinned {
				predicate = ` and ((c.pinned_at is not null and (c.pinned_at,c.hot_score,c.created_at,c.id)<
					($6::timestamptz,$7::numeric,$8::timestamptz,$9::bigint)) or c.pinned_at is null)`
				args = append(args, *cursor.PinnedAt, cursor.HotScore, cursor.CreatedAt, cursor.ID)
			} else {
				predicate = ` and c.pinned_at is null and (c.hot_score,c.created_at,c.id)<
					($6::numeric,$7::timestamptz,$8::bigint)`
				args = append(args, cursor.HotScore, cursor.CreatedAt, cursor.ID)
			}
		case "replies":
			if cursor.Pinned {
				predicate = ` and ((c.pinned_at is not null and (c.pinned_at,c.descendant_count,c.created_at,c.id)<
					($6::timestamptz,$7::integer,$8::timestamptz,$9::bigint)) or c.pinned_at is null)`
				args = append(args, *cursor.PinnedAt, cursor.DescendantCount, cursor.CreatedAt, cursor.ID)
			} else {
				predicate = ` and c.pinned_at is null and (c.descendant_count,c.created_at,c.id)<
					($6::integer,$7::timestamptz,$8::bigint)`
				args = append(args, cursor.DescendantCount, cursor.CreatedAt, cursor.ID)
			}
		}
	}
	query := `select c.id,c.pinned_at,c.created_at,c.hot_score::text,c.descendant_count
		from comments c
		where c.target_type=$1 and c.target_id=$2 and coalesce(c.target_version_id,0)=coalesce($3::bigint,0)
		  and c.parent_id is null and c.status in ('published','deleted')
		  and ($4::bigint=0 or not exists(select 1 from user_blocks block
			where block.blocker_id=$4 and block.blocked_id=c.author_id))` + predicate + `
		order by (c.pinned_at is not null) desc,c.pinned_at desc,` + order + ` limit $5`
	return query, args
}

func buildCommentReplyPageQuery(commentID, viewerID int64, limit int, cursor *commentReplyPageCursor) (string, []any) {
	args := []any{commentID, viewerID, limit + 1}
	predicate := ""
	if cursor != nil {
		predicate = " and (created_at,id)>($4::timestamptz,$5::bigint)"
		args = append(args, cursor.CreatedAt, cursor.ID)
	}
	query := `select id,created_at from comments where parent_id=$1
		and status in ('published','deleted')
		and ($2::bigint=0 or not exists(select 1 from user_blocks block
			where block.blocker_id=$2 and block.blocked_id=comments.author_id))` + predicate + `
		order by created_at,id limit $3`
	return query, args
}

func buildCommentWatchPageQuery(userID int64, limit int, filter, sortName string, cursor *commentWatchPageCursor) (string, []any) {
	where := "watch.user_id=$1 and watch.status='active'"
	switch filter {
	case "unread":
		where += " and watch.unread_count>0"
	case "muted":
		where += " and (watch.muted_forever or watch.muted_until>now())"
	}
	order := "watch.last_activity_at desc,watch.id desc"
	switch sortName {
	case "created":
		order = "watch.created_at desc,watch.id desc"
	case "unread":
		order = "watch.unread_count desc,watch.last_activity_at desc,watch.id desc"
	}
	args := []any{userID, limit + 1}
	predicate := ""
	if cursor != nil {
		switch sortName {
		case "activity":
			predicate = " and (watch.last_activity_at,watch.id)<($3::timestamptz,$4::bigint)"
			args = append(args, cursor.LastActivityAt, cursor.ID)
		case "created":
			predicate = " and (watch.created_at,watch.id)<($3::timestamptz,$4::bigint)"
			args = append(args, cursor.CreatedAt, cursor.ID)
		case "unread":
			predicate = " and (watch.unread_count,watch.last_activity_at,watch.id)<($3::integer,$4::timestamptz,$5::bigint)"
			args = append(args, cursor.UnreadCount, cursor.LastActivityAt, cursor.ID)
		}
	}
	query := `select watch.id,watch.public_id,watch.comment_id,watch.muted_until,
		watch.muted_forever,watch.unread_count,watch.watched_reply_count,watch.created_at,watch.last_activity_at,
		comment.target_type,comment.target_id,comment.target_version_id
		from comment_watches watch join comments comment on comment.id=watch.comment_id
		where ` + where + predicate + ` order by ` + order + ` limit $2`
	return query, args
}
