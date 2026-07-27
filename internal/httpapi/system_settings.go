package httpapi

import (
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
	raw, err := security.DecryptSetting(s.cfg.SettingsEncryptionKey, stored)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}
