package httpapi

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"mcmods-cn-backend/internal/security"
)

type commentReadQueryStub struct {
	rows pgx.Rows
	err  error
}

func (query commentReadQueryStub) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return query.rows, query.err
}

type commentTotalQueryStub struct{ row pgx.Row }

func (query commentTotalQueryStub) QueryRow(context.Context, string, ...any) pgx.Row {
	return query.row
}

type commentTotalRowStub struct {
	total int64
	err   error
}

func (row commentTotalRowStub) Scan(destinations ...any) error {
	if row.err != nil {
		return row.err
	}
	*destinations[0].(*int64) = row.total
	return nil
}

func TestCommentListReadsRejectScanCursorAndCountFailures(t *testing.T) {
	handlerRaw, err := os.ReadFile("comment_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	detailRaw, err := os.ReadFile("comment_detail_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	paginationRaw, err := os.ReadFile("comment_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	targetRaw, err := os.ReadFile("comment_target_batch.go")
	if err != nil {
		t.Fatal(err)
	}
	handlers, detail := string(handlerRaw), string(detailRaw)
	for _, function := range []string{"listTargetComments", "commentReplies", "myCommentWatches"} {
		body := goFunctionBody(t, handlers, function)
		for _, required := range []string{"rows.Scan", "rows.Err()", "rows.Close()", "http.StatusInternalServerError"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s does not reject partial row reads; missing %q", function, required)
			}
		}
	}
	for function, source := range map[string]string{
		"queryCommentItemsWithQueryer":             detail,
		"queryCommentTargetsByInternalWithQueryer": string(targetRaw),
	} {
		body := goFunctionBody(t, source, function)
		for _, required := range []string{"rows.Scan", "rows.Err()", "return nil, err"} {
			if !strings.Contains(body, required) {
				t.Fatalf("%s still permits partial assembly; missing %q", function, required)
			}
		}
	}
	totalBody := goFunctionBody(t, string(paginationRaw), "queryCommentTargetVisibleTotalWithQueryer")
	if !strings.Contains(totalBody, "return total, err") || strings.Contains(totalBody, "_ =") {
		t.Fatal("comment total read still converts a database failure to zero")
	}
	watchBody := goFunctionBody(t, handlers, "myCommentWatches")
	for _, required := range []string{"if err != nil", "读取插眼评论失败", "读取插眼目标失败"} {
		if !strings.Contains(watchBody, required) {
			t.Fatalf("watch list does not expose comment/target assembly failure; missing %q", required)
		}
	}
}

func TestCommentAssemblyTargetAndTotalFailuresAreObservable(t *testing.T) {
	wantErr := errors.New("comment row stream interrupted")
	itemRows := &simpleRowsFailureStub{terminalErr: wantErr}
	if items, err := queryCommentItemsWithQueryer(context.Background(), commentReadQueryStub{rows: itemRows}, []int64{1}, false, 0, 7); !errors.Is(err, wantErr) || items != nil || !itemRows.closed {
		t.Fatalf("comment item terminal failure = %#v/%v/closed=%t", items, err, itemRows.closed)
	}
	itemScanRows := &simpleRowsFailureStub{next: true}
	if items, err := queryCommentItemsWithQueryer(context.Background(), commentReadQueryStub{rows: itemScanRows}, []int64{1}, false, 0, 7); err == nil || items != nil || !itemScanRows.closed {
		t.Fatalf("comment item scan failure = %#v/%v/closed=%t", items, err, itemScanRows.closed)
	}

	targetRows := &simpleRowsFailureStub{terminalErr: wantErr}
	identities := []commentTargetIdentity{{Type: "mod", ID: 1}}
	if targets, err := queryCommentTargetsByInternalWithQueryer(context.Background(), commentReadQueryStub{rows: targetRows}, identities, security.Claims{}); !errors.Is(err, wantErr) || targets != nil || !targetRows.closed {
		t.Fatalf("comment target terminal failure = %#v/%v/closed=%t", targets, err, targetRows.closed)
	}
	targetScanRows := &simpleRowsFailureStub{next: true}
	if targets, err := queryCommentTargetsByInternalWithQueryer(context.Background(), commentReadQueryStub{rows: targetScanRows}, identities, security.Claims{}); err == nil || targets != nil || !targetScanRows.closed {
		t.Fatalf("comment target scan failure = %#v/%v/closed=%t", targets, err, targetScanRows.closed)
	}

	if total, err := queryCommentTargetVisibleTotalWithQueryer(context.Background(), commentTotalQueryStub{row: commentTotalRowStub{err: wantErr}}, commentTargetInfo{Type: "mod", InternalID: 1}, 7); !errors.Is(err, wantErr) || total != 0 {
		t.Fatalf("comment total failure = %d/%v", total, err)
	}
	if total, err := queryCommentTargetVisibleTotalWithQueryer(context.Background(), commentTotalQueryStub{row: commentTotalRowStub{total: 42}}, commentTargetInfo{Type: "mod", InternalID: 1}, 7); err != nil || total != 42 {
		t.Fatalf("comment total success = %d/%v", total, err)
	}
}
