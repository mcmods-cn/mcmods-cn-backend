package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"mcmods-cn-backend/internal/serverprobe"
)

func TestServerSubmissionReportsChangedReviewRequirementAfterProbe(t *testing.T) {
	f := newTEST015ServerFixture(t)
	proof := f.proof(t, f.editor, "active", "clean", 100)
	const address = "review-policy-change.invalid:25565"
	// The administrator disables review while an ordinary server submission
	// is being checked. The submitter originally agreed to enter the queue.
	f.server.serverProbe = func(ctx context.Context, address string) (serverprobe.Result, error) {
		_, err := f.db.Exec(ctx, "update system_settings set value=jsonb_set(value,'{serverCreate}','false') where key=$1", reviewConfigSettingKey)
		return test015ProbeResult(address), err
	}
	raw := f.require(t, f.editor, http.MethodPost, "/api/v1/servers", test015Submission(address, proof), http.StatusConflict)
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(raw, &problem); err != nil || problem.Code != "SERVER_REVIEW_REQUIREMENT_CHANGED" {
		t.Fatalf("unexpected conflict response %s: %v", raw, err)
	}
	var exists bool
	if err := f.db.QueryRow(f.ctx, "select exists(select 1 from minecraft_servers where address=$1)", address).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("changed review requirement persisted the submission")
	}
}
