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

func TestVerifyPasswordRejectsLegacyPBKDF2(t *testing.T) {
	// This is a valid PBKDF2-HMAC-SHA256 hash for "legacy-password",
	// 100,000 iterations and the salt "legacy-salt". The development schema
	// has no legacy-password compatibility contract, so accepting it would
	// silently restore the removed authentication branch.
	legacy := "pbkdf2$sha256$100000$bGVnYWN5LXNhbHQ$9bFnZntVcT0mdppBMIIbUSDM/+6YmsHsW1Nx1Pytmro"
	if VerifyPassword("legacy-password", legacy) {
		t.Fatal("legacy PBKDF2 password was accepted")
	}
	if VerifyCode("legacy-password", legacy) {
		t.Fatal("legacy PBKDF2 verification remained reachable through VerifyCode")
	}
}
