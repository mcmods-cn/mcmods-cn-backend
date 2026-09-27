package httpapi

import (
	"bytes"
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
)

const (
	ratingReviewPageCursorVersion = 1
	defaultRatingReviewPageLimit  = 20
	maximumRatingReviewPageLimit  = 100
)

type ratingReviewPageRequest struct {
	Target rateableTarget
	Limit  int
	Scope  string
	Cursor *ratingReviewPageCursor
}

type ratingReviewPageCursor struct {
	Version   int       `json:"v"`
	Scope     string    `json:"scope"`
	UpdatedAt time.Time `json:"updatedAt"`
	ID        int64     `json:"id"`
}

type ratingReviewPageRow struct {
	ID           int64
	PublicID     string
	AuthorID     string
	AuthorName   string
	AuthorAvatar string
	OverallScore int
	Message      string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func parseRatingReviewPageRequest(values url.Values, target rateableTarget) (ratingReviewPageRequest, error) {
	if target.RouteID <= 0 || target.Type == "" || target.PublicID == "" {
		return ratingReviewPageRequest{}, errors.New("invalid rating target")
	}
	allowed := map[string]bool{"limit": true, "cursor": true}
	for key, items := range values {
		if !allowed[key] {
			return ratingReviewPageRequest{}, fmt.Errorf("unsupported rating query parameter %q", key)
		}
		if len(items) != 1 {
			return ratingReviewPageRequest{}, fmt.Errorf("rating query parameter %q must appear exactly once", key)
		}
	}
	request := ratingReviewPageRequest{Target: target, Limit: defaultRatingReviewPageLimit}
	if rawLimit := strings.TrimSpace(values.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > maximumRatingReviewPageLimit {
			return ratingReviewPageRequest{}, errors.New("invalid rating page size")
		}
		request.Limit = parsed
	}
	request.Scope = ratingReviewPageScope(request)
	cursor, err := decodeRatingReviewPageCursor(values.Get("cursor"), request.Scope)
	if err != nil {
		return ratingReviewPageRequest{}, err
	}
	request.Cursor = cursor
	return request, nil
}

func ratingReviewPageScope(request ratingReviewPageRequest) string {
	payload, _ := json.Marshal(struct {
		Version  int    `json:"version"`
		RouteID  int64  `json:"routeId"`
		Type     string `json:"type"`
		PublicID string `json:"publicId"`
		Limit    int    `json:"limit"`
	}{
		ratingReviewPageCursorVersion,
		request.Target.RouteID,
		request.Target.Type,
		request.Target.PublicID,
		request.Limit,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:16])
}

func encodeRatingReviewPageCursor(cursor ratingReviewPageCursor) string {
	payload, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(payload)
}

func decodeRatingReviewPageCursor(raw, scope string) (*ratingReviewPageCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 2048 {
		return nil, errors.New("invalid rating cursor")
	}
	payload, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(payload) == 0 || len(payload) > 2048 {
		return nil, errors.New("invalid rating cursor")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var cursor ratingReviewPageCursor
	if err = decoder.Decode(&cursor); err != nil {
		return nil, errors.New("invalid rating cursor")
	}
	var extra json.RawMessage
	if err = decoder.Decode(&extra); !errors.Is(err, io.EOF) ||
		cursor.Version != ratingReviewPageCursorVersion || cursor.Scope != scope ||
		cursor.UpdatedAt.IsZero() || cursor.ID <= 0 {
		return nil, errors.New("invalid rating cursor")
	}
	cursor.UpdatedAt = cursor.UpdatedAt.UTC()
	return &cursor, nil
}

func ratingReviewPageSQL(request ratingReviewPageRequest) (string, []any) {
	query := `select rating.id,rating.public_id,author.public_id,author.username,author.avatar_url,
		rating.overall_score,rating.message,rating.created_at,rating.updated_at
		from content_ratings rating join users author on author.id=rating.author_id
		where rating.object_route_id=$1 and rating.status='published'`
	arguments := []any{request.Target.RouteID}
	if request.Cursor != nil {
		arguments = append(arguments, request.Cursor.UpdatedAt, request.Cursor.ID)
		query += fmt.Sprintf(" and (rating.updated_at,rating.id)<($%d,$%d)", len(arguments)-1, len(arguments))
	}
	arguments = append(arguments, request.Limit+1)
	query += fmt.Sprintf(" order by rating.updated_at desc,rating.id desc limit $%d", len(arguments))
	return query, arguments
}

func ratingReviewNextCursor(request ratingReviewPageRequest, row ratingReviewPageRow) string {
	return encodeRatingReviewPageCursor(ratingReviewPageCursor{
		Version:   ratingReviewPageCursorVersion,
		Scope:     request.Scope,
		UpdatedAt: row.UpdatedAt.UTC(),
		ID:        row.ID,
	})
}

func (row ratingReviewPageRow) response() ratingItemResponse {
	return ratingItemResponse{
		ID:           row.ID,
		PublicID:     row.PublicID,
		AuthorID:     row.AuthorID,
		AuthorName:   row.AuthorName,
		AuthorAvatar: row.AuthorAvatar,
		OverallScore: row.OverallScore,
		Message:      row.Message,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
