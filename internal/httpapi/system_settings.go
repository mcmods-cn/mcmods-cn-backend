package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"mcmods-cn-backend/internal/security"
)

func (s *Server) sealSystemSetting(payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encrypted, err := security.EncryptSetting(s.cfg.SettingsEncryptionKey, raw)
	if err != nil {
		return "", err
	}
	return string(encrypted), nil
}

func (s *Server) openSystemSetting(stored []byte, target any) error {
	return openSettingJSON(s.cfg.SettingsEncryptionKey, stored, target)
}

func openSettingJSON(key string, stored []byte, target any) error {
	raw, err := security.DecryptSetting(key, stored)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

// Each encrypted write uses a fresh nonce. A key-only change therefore advances
// the revision without deriving a public fingerprint from plaintext.
func encryptedSettingRevision(stored []byte) string {
	digest := sha256.Sum256(stored)
	return hex.EncodeToString(digest[:])
}
