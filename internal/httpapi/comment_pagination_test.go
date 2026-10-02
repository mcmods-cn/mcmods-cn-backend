package httpapi

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestCommentPageCursorsRoundTripAndRejectForeignScopes(t *testing.T) {
	t.Parallel()
	anchor := time.Date(2026, 8, 22, 10, 11, 12, 123456000, time.UTC)
	pinnedAt := anchor.Add(-time.Hour)

	rootRaw := encodeCommentRootPageCursor(commentRootPageCursor{
		Version: commentPageCursorVersion, Scope: "mod:42:0:7", Sort: "hot", Pinned: true,
		PinnedAt: &pinnedAt, CreatedAt: anchor, HotScore: "12.500000", DescendantCount: 31, ID: 99,
	})
	root, err := decodeCommentRootPageCursor(rootRaw, "mod:42:0:7", "hot")
	if err != nil {
		t.Fatalf("decode root cursor: %v", err)
	}
	if root.ID != 99 || root.HotScore != "12.500000" || root.PinnedAt == nil || !root.Pinned {
		t.Fatalf("unexpected root cursor: %+v", root)
	}
	if _, err = decodeCommentRootPageCursor(rootRaw, "mod:43:0:7", "hot"); err == nil {
		t.Fatal("root cursor crossed target scope")
	}
	if _, err = decodeCommentRootPageCursor(rootRaw, "mod:42:0:7", "latest"); err == nil {
		t.Fatal("root cursor crossed sort scope")
	}

	replyRaw := encodeCommentReplyPageCursor(commentReplyPageCursor{
		Version: commentPageCursorVersion, Scope: "comment:55:7", CreatedAt: anchor, ID: 101,
	})
	reply, err := decodeCommentReplyPageCursor(replyRaw, "comment:55:7")
	if err != nil || reply.ID != 101 {
		t.Fatalf("decode reply cursor: cursor=%+v err=%v", reply, err)
	}
	if _, err = decodeCommentReplyPageCursor(replyRaw, "comment:56:7"); err == nil {
		t.Fatal("reply cursor crossed comment scope")
	}

	watchRaw := encodeCommentWatchPageCursor(commentWatchPageCursor{
		Version: commentPageCursorVersion, Scope: "user:7:unread", Sort: "unread",
		UnreadCount: 8, LastActivityAt: anchor, CreatedAt: anchor.Add(-time.Hour), ID: 202,
	})
	watch, err := decodeCommentWatchPageCursor(watchRaw, "user:7:unread", "unread")
	if err != nil || watch.ID != 202 || watch.UnreadCount != 8 {
		t.Fatalf("decode watch cursor: cursor=%+v err=%v", watch, err)
	}
	if _, err = decodeCommentWatchPageCursor(watchRaw, "user:7:all", "unread"); err == nil {
		t.Fatal("watch cursor crossed filter scope")
	}
}

func TestCommentPageCursorsRejectMalformedAndUnknownFields(t *testing.T) {
	t.Parallel()
	unknown := base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"s":"comment:1:0","c":"2026-08-22T00:00:00Z","id":1,"extra":true}`))
	for name, test := range map[string]func() error{
		"root malformed": func() error {
			_, err := decodeCommentRootPageCursor("not-base64", "root", "latest")
			return err
		},
		"reply unknown": func() error {
			_, err := decodeCommentReplyPageCursor(unknown, "comment:1:0")
			return err
		},
		"watch empty": func() error {
			_, err := decodeCommentWatchPageCursor("", "user:1:all", "activity")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := test(); err == nil {
				t.Fatal("invalid cursor was accepted")
			}
		})
	}
}
