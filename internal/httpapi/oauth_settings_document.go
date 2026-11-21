package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Keep the stored document alongside the projection used by login and the admin
// form. A save can then preserve fields introduced by another application version.
type storedOAuthConfig struct {
	oauthConfigPayload
	document  map[string]any
	providers map[string]any
}

func newStoredOAuthConfig() storedOAuthConfig {
	providers := map[string]any{}
	return storedOAuthConfig{
		oauthConfigPayload: oauthConfigPayload{Providers: map[string]oauthProviderConfig{}},
		document:           map[string]any{"providers": providers},
		providers:          providers,
	}
}

func (c *storedOAuthConfig) UnmarshalJSON(raw []byte) error {
	if err := json.Unmarshal(raw, &c.oauthConfigPayload); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		return err
	}
	var providers map[string]any
	for name, value := range document {
		if strings.EqualFold(name, "providers") {
			var ok bool
			if providers != nil {
				return errors.New("duplicate OAuth provider groups")
			}
			if providers, ok = value.(map[string]any); !ok {
				return errors.New("OAuth providers must be an object")
			}
		}
	}
	if providers == nil {
		return errors.New("missing OAuth providers")
	}
	for name, value := range providers {
		if _, supported := supportedOAuthProviders[name]; !supported {
			continue
		}
		fields, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("OAuth provider %s must be an object", name)
		}
		for key, field := range fields {
			if field == nil && nonNullableSettingField(&oauthProviderConfig{}, key) {
				return fmt.Errorf("OAuth provider %s.%s cannot be null", name, key)
			}
		}
	}
	c.document, c.providers = document, providers
	return nil
}

func (c *storedOAuthConfig) setProvider(name string, value oauthProviderConfig) error {
	fields, ok := c.providers[name].(map[string]any)
	if !ok {
		fields = map[string]any{}
	}
	// Normalize historical casing for known fields. Preserve extension fields
	// verbatim, including null and large JSON numbers.
	for key := range fields {
		if nonNullableSettingField(&value, key) {
			delete(fields, key)
		}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	c.providers[name], c.Providers[name] = fields, value
	return nil
}
