package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/querycache"
	"mcmods-cn-backend/internal/security"
)

func TestOCT02CreatorIdentityClaimRequiresIndependentReviewerIntegration(t *testing.T) {
	f := newTEST013Fixture(t)
	var creatorID int64
	var claimPublicID string
	if err := f.db.QueryRow(f.ctx, `insert into creators(kind,name,normalized_name,review_status)
	 values('author','Synthetic independent author','synthetic independent author','approved') returning id`).Scan(&creatorID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(f.ctx, `insert into content_creator_bindings(subject_type,subject_id,creator_id,status,permission_granting)
	 values('mod',$1,$2,'approved',true)`, f.modID, creatorID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `insert into creator_claims(creator_id,user_id,proof_markdown) values($1,$2,'Synthetic proof') returning public_id`, creatorID, f.userIDs[f.editor]).Scan(&claimPublicID); err != nil {
		t.Fatal(err)
	}
	cache := querycache.New(config.RedisConfig{})
	t.Cleanup(func() { _ = cache.Close() })
	s := &Server{db: f.db, cache: cache}
	review := func(actor int64) int {
		r := httptest.NewRequest(http.MethodPatch, "/api/v1/admin/creator-claims/"+claimPublicID, bytes.NewBufferString(`{"status":"approved","note":"Synthetic independent review"}`))
		r.SetPathValue("id", claimPublicID)
		r = r.WithContext(context.WithValue(f.ctx, claimsContextKey, security.Claims{Subject: actor, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}, {Code: "creator.claim.review", Allow: true}}}))
		w := httptest.NewRecorder()
		s.reviewCreatorClaim(w, r)
		return w.Code
	}
	if status := review(f.userIDs[f.editor]); status != http.StatusForbidden {
		t.Errorf("claimant administrative self-review status=%d, want403", status)
	}
	var state string
	var derived bool
	if err := f.db.QueryRow(f.ctx, `select status from creator_claims where public_id=$1`, claimPublicID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and source_type='author_claim' and project_id=$2)`, f.userIDs[f.editor], f.modID).Scan(&derived); err != nil {
		t.Fatal(err)
	}
	if state != "pending" || derived {
		t.Fatalf("self-review changed claim=%s derivedaccess=%v", state, derived)
	}
	if status := review(f.userIDs[f.reviewer]); status != http.StatusOK {
		t.Fatalf("independent reviewer status=%d, want200", status)
	}
	if err := f.db.QueryRow(f.ctx, `select exists(select 1 from effective_project_access where user_id=$1 and source_type='author_claim' and project_id=$2)`, f.userIDs[f.editor], f.modID).Scan(&derived); err != nil || !derived {
		t.Fatalf("independent approved claim missing derivedaccess=%v err=%v", derived, err)
	}
}
