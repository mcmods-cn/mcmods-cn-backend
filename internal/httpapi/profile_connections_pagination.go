package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const userConnectionCursorVersion = 1

type userConnectionPageRequest struct {
	Limit  int
	Scope  string
	Cursor *userConnectionPageCursor
}

type userConnectionPageCursor struct {
	Version          int       `json:"v"`
	Scope            string    `json:"s"`
	CreatedAt        time.Time `json:"createdAt"`
	ConnectionUserID int64     `json:"connectionUserId"`
}

type userConnectionPageRow struct {
	Item             userConnectionItem
	CreatedAt        time.Time
	ConnectionUserID int64
}

type userConnectionPageQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func parseUserConnectionPageRequest(values url.Values, userID int64, connectionType string) (userConnectionPageRequest, error) {
	if connectionType != "followers" && connectionType != "following" || values.Has("page") || values.Has("pageSize") {
		return userConnectionPageRequest{}, errors.New("invalid user connection cursor")
	}
	limit := 24
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 60 {
			return userConnectionPageRequest{}, errors.New("invalid user connection cursor")
		}
		limit = parsed
	}
	scope := userConnectionPageScope(userID, connectionType, limit)
	cursor, err := decodeUserConnectionPageCursor(values.Get("cursor"), scope)
	if err != nil {
		return userConnectionPageRequest{}, err
	}
	return userConnectionPageRequest{Limit: limit, Scope: scope, Cursor: cursor}, nil
}

func userConnectionPageScope(userID int64, connectionType string, limit int) string {
	material, _ := json.Marshal(struct {
		Version        int    `json:"version"`
		UserID         int64  `json:"userId"`
		ConnectionType string `json:"connectionType"`
		Limit          int    `json:"limit"`
	}{userConnectionCursorVersion, userID, connectionType, limit})
	digest := sha256.Sum256(material)
	return hex.EncodeToString(digest[:16])
}

func encodeUserConnectionPageCursor(cursor userConnectionPageCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeUserConnectionPageCursor(raw, scope string) (*userConnectionPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid user connection cursor")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid user connection cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(decoded))
	decoder.DisallowUnknownFields()
	var cursor userConnectionPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid user connection cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) || cursor.Version != userConnectionCursorVersion ||
		cursor.Scope != scope || cursor.CreatedAt.IsZero() || cursor.ConnectionUserID <= 0 {
		return nil, errors.New("invalid user connection cursor")
	}
	return &cursor, nil
}

func loadUserConnectionPage(
	ctx context.Context,
	queryer userConnectionPageQueryer,
	userID int64,
	connectionType string,
	request userConnectionPageRequest,
) ([]userConnectionPageRow, bool, string, error) {
	query, args := userConnectionPageQuery(userID, connectionType, request)
	rows, err := queryer.Query(ctx, query, args...)
	if err != nil {
		return nil, false, "", err
	}
	defer rows.Close()
	pageRows := make([]userConnectionPageRow, 0, request.Limit+1)
	for rows.Next() {
		var row userConnectionPageRow
		if err = rows.Scan(&row.Item.ID, &row.Item.Username, &row.Item.AvatarURL, &row.Item.Signature, &row.CreatedAt, &row.ConnectionUserID); err != nil {
			return nil, false, "", err
		}
		pageRows = append(pageRows, row)
	}
	if err = rows.Err(); err != nil {
		return nil, false, "", err
	}
	hasMore := len(pageRows) > request.Limit
	if hasMore {
		pageRows = pageRows[:request.Limit]
	}
	nextCursor := ""
	if hasMore {
		last := pageRows[len(pageRows)-1]
		nextCursor = encodeUserConnectionPageCursor(userConnectionPageCursor{
			Version: userConnectionCursorVersion, Scope: request.Scope,
			CreatedAt: last.CreatedAt, ConnectionUserID: last.ConnectionUserID,
		})
	}
	return pageRows, hasMore, nextCursor, nil
}

func userConnectionPageQuery(userID int64, connectionType string, request userConnectionPageRequest) (string, []any) {
	connectionID := "follow.follower_id"
	query := `select account.public_id,account.username,account.avatar_url,account.signature,follow.created_at,follow.follower_id
		from user_follows follow join users account on account.id=follow.follower_id
		where follow.followed_id=$1 and account.status='active'`
	if connectionType == "following" {
		connectionID = "follow.followed_id"
		query = `select account.public_id,account.username,account.avatar_url,account.signature,follow.created_at,follow.followed_id
			from user_follows follow join users account on account.id=follow.followed_id
			where follow.follower_id=$1 and account.status='active'`
	}
	args := []any{userID}
	if request.Cursor != nil {
		query += fmt.Sprintf(" and (follow.created_at,%s)<($2,$3)", connectionID)
		args = append(args, request.Cursor.CreatedAt, request.Cursor.ConnectionUserID)
	}
	args = append(args, request.Limit+1)
	query += fmt.Sprintf(" order by follow.created_at desc,%s desc limit $%d", connectionID, len(args))
	return query, args
}
