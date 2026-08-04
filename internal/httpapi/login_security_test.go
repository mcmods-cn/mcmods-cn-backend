package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/security"
)

func TestLoginDummyPasswordHashUsesCurrentArgon2Parameters(t *testing.T) {
	if !security.VerifyPassword("not-a-real-password", loginDummyPasswordHash) {
		t.Fatal("login dummy password hash is not a valid current Argon2id hash")
	}
	if security.VerifyPassword("a-user-supplied-password", loginDummyPasswordHash) {
		t.Fatal("login dummy password unexpectedly matched an arbitrary password")
	}
}
