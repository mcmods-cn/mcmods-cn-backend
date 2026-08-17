package database

import (
	"testing"

	"mcmods-cn-backend/internal/systemactor"
)

func TestAutobotSeedUsesNonLoginServiceIdentityAndTrustedAutomationPermissions(t *testing.T) {
	var autobot *seedUser
	for index := range seedUsers {
		if seedUsers[index].Username == systemactor.AutobotUsername {
			autobot = &seedUsers[index]
			break
		}
	}
	if autobot == nil {
		t.Fatal("autobot seed user is missing")
	}
	if autobot.Email != systemactor.AutobotEmail || autobot.Status != "active" {
		t.Fatalf("unexpected autobot identity: %+v", *autobot)
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
	hash, updatePassword, err := seedPasswordHash(autobot.Username)
	if err != nil || !updatePassword || hash != "password-login-disabled" {
		t.Fatalf("autobot must not receive an interactive password: hash=%q update=%v err=%v", hash, updatePassword, err)
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
