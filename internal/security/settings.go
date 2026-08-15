package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const encryptedSettingVersion = "aes-gcm-v1"

type encryptedSettingEnvelope struct {
	Encryption string `json:"_encryption"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func EncryptSetting(masterSecret string, plaintext []byte) ([]byte, error) {
	aead, err := settingAEAD(masterSecret)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, []byte(encryptedSettingVersion))
	return json.Marshal(encryptedSettingEnvelope{
		Encryption: encryptedSettingVersion,
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ciphertext),
	})
}

func DecryptSetting(masterSecret string, stored []byte) ([]byte, error) {
	var envelope encryptedSettingEnvelope
	if err := json.Unmarshal(stored, &envelope); err != nil {
		return nil, err
	}
	if envelope.Encryption == "" {
		return nil, errors.New("unencrypted setting payload is not supported")
	}
	if envelope.Encryption != encryptedSettingVersion || envelope.Nonce == "" || envelope.Ciphertext == "" {
		return nil, errors.New("unsupported encrypted setting envelope")
	}
	aead, err := settingAEAD(masterSecret)
	if err != nil {
		return nil, err
	}
	nonce, err := base64.RawStdEncoding.DecodeString(envelope.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return nil, errors.New("invalid encrypted setting nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) < aead.Overhead() {
		return nil, errors.New("invalid encrypted setting ciphertext")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, []byte(encryptedSettingVersion))
	if err != nil {
		return nil, errors.New("encrypted setting authentication failed")
	}
	return plaintext, nil
}

func settingAEAD(masterSecret string) (cipher.AEAD, error) {
	masterSecret = strings.TrimSpace(masterSecret)
	if len(masterSecret) < 32 {
		return nil, fmt.Errorf("settings encryption key must contain at least 32 characters")
	}
	key := sha256.Sum256([]byte("mcmods-cn/system-settings/v1\x00" + masterSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
