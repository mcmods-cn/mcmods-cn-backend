package security

import (
	"strings"
	"testing"
)

func TestHashPasswordUsesArgon2ID(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "argon2id$") {
		t.Fatalf("unexpected password format: %q", hash)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Fatal("generated password hash did not verify")
	}
	if VerifyPassword("wrong password", hash) {
		t.Fatal("incorrect password verified")
	}
}

func TestVerifyPasswordRejectsExcessiveArgon2Parameters(t *testing.T) {
	hash := "argon2id$v=19$m=1048576,t=3,p=2$c2FsdHNhbHQ$MTIzNDU2Nzg5MDEyMzQ1Ng"
	if VerifyPassword("password", hash) {
		t.Fatal("unsafe Argon2 parameters were accepted")
	}
}
