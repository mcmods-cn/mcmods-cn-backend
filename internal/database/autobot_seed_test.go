package database

import (
	"testing"

	"mcmods-cn-backend/internal/systemactor"
)

func TestAutobotSeedUsesNonLoginServiceIdentityAndTrustedAutomationPermissions(t *testing.T) {
	autobot := autobotSeed
	if autobot.Email != systemactor.AutobotEmail || autobot.Status != systemactor.AutobotStatus {
		t.Fatalf("unexpected autobot identity: %+v", autobot)
	}
	permissions := make(map[string]bool, len(autobot.Permissions))
	for _, permission := range autobot.Permissions {
		permissions[permission] = true
	}
	for _, required := range []string{
		"project.create", "project.edit", "project.no-review", "content.no-review",
		"security.anti-abuse.rate_multiplier.1000",
		"security.anti-abuse.rate_multiplier.review_submit.1000",
	} {
		if !permissions[required] {
			t.Fatalf("autobot is missing permission %q", required)
		}
	}

}

func TestAutobotNumericPermissionsHaveConcreteDefinitions(t *testing.T) {
	definitions := make(map[string]bool, len(seedPermissions))
	for _, permission := range seedPermissions {
		definitions[permission.Code] = true
	}
	for _, required := range []string{
		"security.anti-abuse.rate_multiplier.1000",
		"security.anti-abuse.rate_multiplier.review_submit.1000",
	} {
		if !definitions[required] {
			t.Fatalf("permission definition %q is missing", required)
		}
	}
}
