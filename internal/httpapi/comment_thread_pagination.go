package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"mcmods-cn-backend/internal/security"
)

const (
	maxCommentThreadNodes         = 64
	maxCommentThreadPathNodes     = 16
	maxCommentThreadResponseBytes = 512 << 10
)

var errCommentThreadResponseBudget = errors.New("comment thread response exceeds byte budget")

type commentThreadPageRow struct {
	id        int64
	createdAt time.Time
}

type commentThreadPageResponse struct {
	Items         []commentResponse `json:"items"`
	FocusID       string            `json:"focusId"`
	Target        commentTargetInfo `json:"target"`
	NextCursor    string            `json:"nextCursor"`
	PathTruncated bool              `json:"pathTruncated"`
}

// queryCommentThreadNeighborhoodWithQueryer deliberately returns only the
// nearest ancestor path and one directly paginated reply level. The two
// supporting indexes are bounded by descendant_id and parent_id respectively;
// no request scans, decorates, or serializes an entire root tree.
func queryCommentThreadNeighborhoodWithQueryer(
	ctx context.Context,
	queryer commentQueryer,
	focusID int64,
	viewerID int64,
	cursor *commentReplyPageCursor,
) ([]int64, []commentThreadPageRow, bool, bool, error) {
	ancestorIDs := make([]int64, 0, maxCommentThreadPathNodes)
	pathTruncated := false
	if cursor == nil {
		rows, err := queryer.Query(ctx, `select path.ancestor_id
			from comment_closure path
			join comments ancestor on ancestor.id=path.ancestor_id
			where path.descendant_id=$1 and path.depth>0
			  and ancestor.status in ('published','deleted')
			  and ($2::bigint=0 or not exists(select 1 from user_blocks block
				where block.blocker_id=$2 and block.blocked_id=ancestor.author_id))
			order by path.depth,path.ancestor_id limit $3`, focusID, viewerID, maxCommentThreadPathNodes+1)
		if err != nil {
			return nil, nil, false, false, err
		}
		for rows.Next() {
			var ancestorID int64
			if err = rows.Scan(&ancestorID); err != nil {
				rows.Close()
				return nil, nil, false, false, err
			}
			ancestorIDs = append(ancestorIDs, ancestorID)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, nil, false, false, err
		}
		pathTruncated = len(ancestorIDs) > maxCommentThreadPathNodes
		if pathTruncated {
			ancestorIDs = ancestorIDs[:maxCommentThreadPathNodes]
		}
		for left, right := 0, len(ancestorIDs)-1; left < right; left, right = left+1, right-1 {
			ancestorIDs[left], ancestorIDs[right] = ancestorIDs[right], ancestorIDs[left]
		}
	}

	replyLimit := maxCommentThreadNodes
	if cursor == nil {
		replyLimit -= len(ancestorIDs) + 1 // Reserve one node for the focus comment.
	}
	query, args := buildCommentReplyPageQuery(focusID, viewerID, replyLimit, cursor)
	rows, err := queryer.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, false, false, err
	}
	replies := make([]commentThreadPageRow, 0, replyLimit+1)
	for rows.Next() {
		var row commentThreadPageRow
		if err = rows.Scan(&row.id, &row.createdAt); err != nil {
			rows.Close()
			return nil, nil, false, false, err
		}
		replies = append(replies, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, false, false, err
	}
	hasMore := len(replies) > replyLimit
	if hasMore {
		replies = replies[:replyLimit]
	}
	return ancestorIDs, replies, hasMore, pathTruncated, nil
}

func (s *Server) loadCommentThreadPage(
	ctx context.Context,
	visible visibleCommentInfo,
	focusPublicID string,
	claims security.Claims,
	cursor *commentReplyPageCursor,
) (commentThreadPageResponse, int, []commentThreadPageRow, bool, error) {
	ancestorIDs, replyRows, hasMore, pathTruncated, err := queryCommentThreadNeighborhoodWithQueryer(
		ctx, s.db, visible.ID, claims.Subject, cursor,
	)
	if err != nil {
		return commentThreadPageResponse{}, 0, nil, false, err
	}
	selectedIDs := make([]int64, 0, len(ancestorIDs)+len(replyRows)+1)
	selectedIDs = append(selectedIDs, ancestorIDs...)
	if cursor == nil {
		selectedIDs = append(selectedIDs, visible.ID)
	}
	for _, row := range replyRows {
		selectedIDs = append(selectedIDs, row.id)
	}
	loaded, err := s.queryCommentItems(ctx, selectedIDs, false, 0, claims)
	if err != nil {
		return commentThreadPageResponse{}, 0, nil, false, err
	}
	byNumericID := make(map[int64]commentResponse, len(loaded))
	for _, item := range loaded {
		byNumericID[item.internalID] = item
	}
	ordered := make([]commentResponse, 0, len(loaded))
	actualAncestorCount := 0
	for _, ancestorID := range ancestorIDs {
		if item, ok := byNumericID[ancestorID]; ok {
			ordered = append(ordered, item)
			actualAncestorCount++
		}
	}
	if cursor == nil {
		focus, ok := byNumericID[visible.ID]
		if !ok {
			return commentThreadPageResponse{}, 0, nil, false, errors.New("visible focus comment disappeared")
		}
		// The branch-level cursor owns this reply page. Per-node reply cursors
		// remain available for every other node in the returned neighborhood.
		focus.HasMoreReplies = false
		ordered = append(ordered, focus)
	}
	actualReplyRows := make([]commentThreadPageRow, 0, len(replyRows))
	for _, row := range replyRows {
		if item, ok := byNumericID[row.id]; ok {
			ordered = append(ordered, item)
			actualReplyRows = append(actualReplyRows, row)
		}
	}
	return commentThreadPageResponse{
		Items: ordered, FocusID: focusPublicID, Target: visible.Target, PathTruncated: pathTruncated,
	}, actualAncestorCount, actualReplyRows, hasMore, nil
}

// marshalBoundedCommentThreadResponse verifies the encoded API envelope, not
// an estimate. It trims only optional reply rows first, then far ancestors;
// the focus comment and at least one advancing reply remain intact.
func marshalBoundedCommentThreadResponse(
	response commentThreadPageResponse,
	ancestorCount int,
	childRows []commentThreadPageRow,
	sourceHasMore bool,
	cursorScope string,
) ([]byte, error) {
	childrenTrimmed := false
	for {
		hasMore := sourceHasMore || childrenTrimmed
		response.NextCursor = ""
		if hasMore {
			if len(childRows) == 0 {
				return nil, errCommentThreadResponseBudget
			}
			last := childRows[len(childRows)-1]
			response.NextCursor = encodeCommentReplyPageCursor(commentReplyPageCursor{
				Version: commentPageCursorVersion, Scope: cursorScope, CreatedAt: last.createdAt, ID: last.id,
			})
		}
		payload, err := json.Marshal(apiResponse{Data: response})
		if err != nil {
			return nil, err
		}
		if len(payload) <= maxCommentThreadResponseBytes {
			return payload, nil
		}
		if len(childRows) > 1 {
			childRows = childRows[:len(childRows)-1]
			response.Items = response.Items[:len(response.Items)-1]
			childrenTrimmed = true
			continue
		}
		if ancestorCount > 0 {
			response.Items = response.Items[1:]
			ancestorCount--
			response.PathTruncated = true
			continue
		}
		return nil, errCommentThreadResponseBudget
	}
}
