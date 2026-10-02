package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestSkinAssetJSONMatchesSkinAdministrationAuthorization(t *testing.T) {
	t.Parallel()
	record := skinAssetRecord{
		OwnerID:      86,
		Status:       "active",
		ReviewStatus: "pending",
		Visibility:   "private",
	}
	for name, testCase := range map[string]struct {
		claims  security.Claims
		canEdit bool
		canUse  bool
	}{
		"owner": {
			claims:  security.Claims{Subject: 86},
			canEdit: true,
			canUse:  true,
		},
		"ordinary viewer": {
			claims: security.Claims{Subject: 862},
		},
		"skin administrator": {
			claims: security.Claims{Subject: 860, PermissionRules: []security.PermissionRule{{
				Code: "skin.admin", Allow: true, Priority: 100,
			}}},
			canEdit: true,
		},
		"global administrator": {
			claims: security.Claims{Subject: 861, PermissionRules: []security.PermissionRule{{
				Code: "admin.*", Allow: true, Priority: 100,
			}}},
			canEdit: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			payload := skinAssetJSON(record, testCase.claims)
			if payload["canEdit"] != testCase.canEdit {
				t.Fatalf("canEdit mismatch: got %#v, want %t", payload, testCase.canEdit)
			}
			if payload["canUse"] != testCase.canUse {
				t.Fatalf("canUse mismatch: got %#v, want %t", payload, testCase.canUse)
			}
		})
	}
}
