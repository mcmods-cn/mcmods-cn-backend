package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/activity"
)

func TestCommentCollectionAndFloorActivityIdentifyActualViewedCommentIntegration(t *testing.T) {
	f := newTEST038Fixture(t)
	first := f.comment(t, f.editor, f.modCode, "first-floor", "first floor", "")
	second := f.comment(t, f.editor, f.modCode, "second-floor", "second floor", "")
	if first.FloorNumber == nil || second.FloorNumber == nil {
		t.Fatal("both root comments must have a floor")
	}
	base := "/api/v1/comment-targets/mod/" + f.modCode + "/comments"
	for _, view := range []struct{ path, expectedID string }{
		{base, ""},
		{fmt.Sprintf("%s/floors/%d", base, *first.FloorNumber), first.ID},
		{fmt.Sprintf("%s/floors/%d", base, *second.FloorNumber), second.ID},
		{"/api/v1/comments/" + second.ID + "/thread", second.ID},
	} {
		annotation := &requestActivity{}
		ctx := context.WithValue(f.ctx, requestActivityContextKey, annotation)
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, view.path, nil)
		req.Header.Set("Authorization", "Bearer "+f.editor)
		response := httptest.NewRecorder()
		f.origin.Config.Handler.(*Server).mux.ServeHTTP(response, req.Clone(ctx))
		event, valid := requestActivityEvent(req, annotation)
		if response.Code != http.StatusOK || !valid || event.ActionID != activity.ActionView || event.ObjectTypeID != activity.ObjectComment ||
			event.ObjectPublicID != view.expectedID || event.ObjectInternalID != 0 || event.UserID != f.userIDs[f.editor] {
			t.Errorf("%s returned HTTP %d, activity %+v, valid=%t; expected comment %q", view.path, response.Code, event, valid, view.expectedID)
		}
	}
}
