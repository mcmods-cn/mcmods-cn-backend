package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestSEC031EveryCommentSubresourceUsesTheVisibleCommentBoundary(t *testing.T) {
	handlerRaw, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	detailRaw, err := os.ReadFile("comment_detail_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, detail := string(handlerRaw), string(detailRaw)
	for _, function := range []string{
		"commentThread", "commentReplies", "commentItem", "commentPin", "commentReaction", "commentWatch",
	} {
		if body := goFunctionBody(t, handlers, function); !strings.Contains(body, "visibleCommentForRequest") {
			t.Fatalf("%s bypasses the shared visible-comment boundary", function)
		}
	}
	if body := goFunctionBody(t, detail, "downloadCommentAttachment"); !strings.Contains(body, "resolveVisibleComment") {
		t.Fatal("comment attachment download bypasses the shared visible-comment boundary")
	}
	if body := goFunctionBody(t, detail, "commentWatchItem"); !strings.Contains(body, "resolveVisibleCommentWatch") {
		t.Fatal("comment watch-item mutation bypasses target visibility")
	}

	watchList := goFunctionBody(t, handlers, "myCommentWatches")
	targetIndex := strings.Index(watchList, "queryCommentTargetsByInternal")
	commentIndex := strings.Index(watchList, "queryCommentItems")
	if targetIndex < 0 || commentIndex < 0 || targetIndex > commentIndex {
		t.Fatal("watch list reads comment bodies before resolving target visibility")
	}
	if strings.Contains(watchList, "target = commentTargetInfo") {
		t.Fatal("watch list still downgrades an invisible target to a readable shell")
	}
}
