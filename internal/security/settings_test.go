package security

import (
	"bytes"
	"testing"
)

func TestEncryptedSettingRoundTrip(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	plaintext := []byte(`{"password":"do-not-store-in-plaintext"}`)
	encrypted, err := EncryptSetting(secret, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encrypted, []byte("do-not-store-in-plaintext")) {
		t.Fatal("encrypted setting leaked plaintext")
	}
	decrypted, err := DecryptSetting(secret, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("unexpected decrypted value: %s", decrypted)
	}
}

func TestDecryptSettingAcceptsLegacyJSON(t *testing.T) {
	legacy := []byte(`{"enabled":true}`)
	decrypted, err := DecryptSetting("0123456789abcdef0123456789abcdef", legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decrypted, legacy) {
		t.Fatalf("legacy setting changed: %s", decrypted)
	}
}
