package queue

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"mcmods-cn-backend/internal/config"
)

// RedactURL keeps diagnostic endpoints while removing credentials. It also
// handles comma-separated servers supported by the NATS client.
func RedactURL(raw string) string {
	servers := strings.Split(raw, ",")
	for index, server := range servers {
		server = strings.TrimSpace(server)
		schemed := strings.Contains(server, "://")
		if !schemed {
			server = "nats://" + server
		}
		address, err := url.Parse(server)
		if err != nil || address.Hostname() == "" {
			servers[index] = "[invalid NATS endpoint]"
			continue
		}
		address.User, address.RawQuery, address.Fragment = nil, "", ""
		servers[index] = address.String()
		if !schemed {
			servers[index] = strings.TrimPrefix(servers[index], "nats://")
		}
	}
	return strings.Join(servers, ",")
}

func redactDiagnostic(message string, cfg config.NATSConfig) string {
	message = strings.ReplaceAll(message, cfg.URL, RedactURL(cfg.URL))
	secrets := []string{cfg.Password, cfg.Token}
	for _, server := range strings.Split(cfg.URL, ",") {
		message = strings.ReplaceAll(message, server, RedactURL(server))
		address, err := url.Parse(strings.TrimSpace(server))
		if err == nil && address.User != nil {
			secrets = append(secrets, address.User.String(), address.User.Username())
			if password, exists := address.User.Password(); exists {
				secrets = append(secrets, password)
			}
		}
	}
	for _, secret := range secrets {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return message
}

// PostgreSQL text and the broker metadata must use the same valid UTF-8
// prefix. A diagnostic must never prevent recording a failed delivery.
func boundedDiagnostic(message string) string {
	const maxBytes = 1000
	if len(message) <= maxBytes && utf8.ValidString(message) && !strings.ContainsRune(message, 0) {
		return message
	}
	var result strings.Builder
	result.Grow(min(len(message), maxBytes))
	for _, r := range message {
		if r == 0 {
			r = utf8.RuneError
		}
		if result.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		result.WriteRune(r)
	}
	return result.String()
}
