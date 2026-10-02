package httpapi

import (
	"os"
	"strings"
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestSkinCreationReviewPolicyRequiresAnExplicitBypass(t *testing.T) {
	t.Parallel()
	config := defaultReviewConfig()
	for name, testCase := range map[string]struct {
		claims security.Claims
		want   bool
	}{
		"ordinary uploader":    {claims: security.Claims{Subject: 1}, want: true},
		"skin administrator":   {claims: security.Claims{Subject: 2, PermissionRules: []security.PermissionRule{{Code: "skin.admin", Allow: true}}}, want: true},
		"no-review permission": {claims: security.Claims{Subject: 3, PermissionRules: []security.PermissionRule{{Code: "content.no-review", Allow: true}}}},
		"global administrator": {claims: security.Claims{Subject: 4, PermissionRules: []security.PermissionRule{{Code: "admin.*", Allow: true}}}},
	} {
		t.Run(name, func(t *testing.T) {
			got := catalogMutationReviewRequired(config, "create") && !catalogMutationBypassesReview(testCase.claims)
			if got != testCase.want {
				t.Fatalf("reviewRequired = %t, want %t", got, testCase.want)
			}
		})
	}
	config.CatalogCreate = false
	if catalogMutationReviewRequired(config, "create") {
		t.Fatal("disabled CatalogCreate must not require review absent a forced moderation boundary")
	}
}

func TestSkinCreationUsesCatalogCreateRevisionWorkflow(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("skin_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Server) createSkin(")
	end := strings.Index(source, "func (s *Server) skinDetail(")
	if start < 0 || end <= start {
		t.Fatal("createSkin source boundary was not found")
	}
	createSource := source[start:end]
	for _, required := range []string{
		`catalogMutationReviewRequired(`,
		`catalogMutationBypassesReview(claims)`,
		`createContentRevisionTx(`,
		`"operation": "create"`,
		`ReplaceLocalizations: true`,
	} {
		if !strings.Contains(createSource, required) {
			t.Errorf("createSkin does not use the required review boundary %q", required)
		}
	}
	for _, forbidden := range []string{
		`values($1,$2,$3,$4,$5,$6,$7,$8,'approved','active')`,
		`'human',true,'approved',$6`,
	} {
		if strings.Contains(createSource, forbidden) {
			t.Errorf("createSkin still hard-codes approval through %q", forbidden)
		}
	}
	updateStart := strings.Index(source, "func (s *Server) updateSkinDetail(")
	applyStart := strings.Index(source, "func applySkinAssetSnapshotTx(")
	if updateStart < 0 || applyStart <= updateStart {
		t.Fatal("updateSkinDetail source boundary was not found")
	}
	updateSource := source[updateStart:applyStart]
	for _, required := range []string{`if baseRevisionID == nil`, `operation = "create"`, `catalogMutationReviewRequired(reviewConfig, operation)`} {
		if !strings.Contains(updateSource, required) {
			t.Errorf("an unpublished skin can bypass CatalogCreate on resubmission; missing %q", required)
		}
	}
}
