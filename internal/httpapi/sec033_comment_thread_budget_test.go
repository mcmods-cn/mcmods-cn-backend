package httpapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSEC033CommentThreadResponseTrimsToActualByteBudget(t *testing.T) {
	items := make([]commentResponse, 0, maxCommentThreadNodes)
	for index := 0; index < maxCommentThreadPathNodes; index++ {
		items = append(items, sec033LargeComment("ancestor"+string(rune('a'+index))))
	}
	items = append(items, sec033LargeComment("focus0001"))
	children := make([]commentThreadPageRow, 0, maxCommentThreadNodes-maxCommentThreadPathNodes-1)
	for index := 0; index < cap(children); index++ {
		row := commentThreadPageRow{id: int64(index + 1), createdAt: time.Unix(int64(index+1), 0).UTC()}
		children = append(children, row)
		items = append(items, sec033LargeComment("child"+string(rune('a'+index))))
	}
	response := commentThreadPageResponse{
		Items:   items,
		FocusID: "focus0001",
		Target:  commentTargetInfo{Type: "mod", Key: "example01", Title: "Example", URL: "/mod/example01"},
	}
	payload, err := marshalBoundedCommentThreadResponse(
		response,
		maxCommentThreadPathNodes,
		children,
		false,
		commentReplyCursorScope(1000, 0),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > maxCommentThreadResponseBytes {
		t.Fatalf("encoded response=%d exceeds budget=%d", len(payload), maxCommentThreadResponseBytes)
	}
	var wrapped struct {
		Data commentThreadPageResponse `json:"data"`
	}
	if err = json.Unmarshal(payload, &wrapped); err != nil {
		t.Fatal(err)
	}
	if len(wrapped.Data.Items) >= maxCommentThreadNodes || wrapped.Data.NextCursor == "" {
		t.Fatalf("oversized children were not cursor-trimmed: items=%d cursor=%q", len(wrapped.Data.Items), wrapped.Data.NextCursor)
	}
	if !containsSEC033Comment(wrapped.Data.Items, "focus0001") {
		t.Fatal("byte trimming removed the focus comment")
	}
}

func TestSEC033CommentThreadHandlerDoesNotRequestWholeTree(t *testing.T) {
	source, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	body := goFunctionBody(t, string(source), "commentThread")
	for _, required := range []string{"loadCommentThreadPage", "marshalBoundedCommentThreadResponse", "writeJSONBytes"} {
		if !strings.Contains(body, required) {
			t.Fatalf("commentThread is missing bounded neighborhood contract %q", required)
		}
	}
	if strings.Contains(body, "includeTree") || strings.Contains(body, "visible.RootID") {
		t.Fatal("commentThread still requests an entire root tree")
	}
}

func sec033LargeComment(id string) commentResponse {
	return commentResponse{
		ID:            id,
		Body:          strings.Repeat("x", maxCommentMarkdownRunes),
		Author:        commentAuthor{ID: "author001", Username: "author"},
		Reactions:     map[string]int{},
		UserReactions: []string{},
		Attachments:   []commentAttachment{},
		CreatedAt:     time.Unix(1, 0).UTC(),
		UpdatedAt:     time.Unix(1, 0).UTC(),
	}
}

func containsSEC033Comment(items []commentResponse, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}
