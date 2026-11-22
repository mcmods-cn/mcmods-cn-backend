package httpapi

import (
	"net/http"
	"testing"
)

func TestFileLogShareBatchReturnsEveryItemAndReusesCompletedSourcesIntegration(t *testing.T) {
	f := newTEST037Fixture(t)
	first := uploadTestLogFile(t, f, "one.log", []byte("first complete log\n"))
	second := uploadTestLogFile(t, f, "two.log", []byte("second complete log\n"))
	type batchResponse struct {
		Items []struct {
			FileID     string `json:"fileId"`
			PublicCode string `json:"publicCode"`
			Status     string `json:"status"`
			Error      string `json:"error"`
			Reused     bool   `json:"reused"`
		} `json:"items"`
	}
	share := func(ids ...string) batchResponse {
		return decodeTEST022Data[batchResponse](t, f.require(t, f.editor, http.MethodPost, "/api/v1/log-shares/files",
			createFileLogSharesRequest{FileIDs: ids, RetentionDays: 30}, http.StatusMultiStatus))
	}
	initial := share(first, second).Items
	if len(initial) != 2 || initial[0].PublicCode == "" || initial[1].PublicCode == "" || initial[0].PublicCode == initial[1].PublicCode {
		t.Fatalf("two file shares were not created: %+v", initial)
	}
	for attempt := 0; attempt < 2; attempt++ {
		items := share(first, "missing001", second).Items
		if len(items) != 3 || items[0].FileID != first || items[0].PublicCode != initial[0].PublicCode || items[0].Status != "ready" || !items[0].Reused ||
			items[1].FileID != "missing001" || items[1].Status != "failed" || items[1].Error == "" ||
			items[2].FileID != second || items[2].PublicCode != initial[1].PublicCode || items[2].Status != "ready" || !items[2].Reused {
			t.Fatalf("attempt %d items=%+v", attempt, items)
		}
	}
	var shares int
	if err := f.db.QueryRow(f.ctx, `select count(*) from log_shares where comment_scope_id is null`).Scan(&shares); err != nil || shares != 2 {
		t.Fatalf("standalone file shares=%d err=%v", shares, err)
	}
}
