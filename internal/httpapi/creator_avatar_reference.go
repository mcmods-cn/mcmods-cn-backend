package httpapi

import (
	"net/url"
	"path"
	"strings"
)

// A temporary capability is presentation data, never a durable avatar
// reference. Recognition does not imply that the issuer or object is trusted.
func temporaryCreatorAvatarReference(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	for _, pair := range strings.Split(parsed.RawQuery, "&") {
		key, _, _ := strings.Cut(pair, "=")
		decoded, err := url.QueryUnescape(key)
		if err != nil {
			continue
		}
		switch strings.ToLower(decoded) {
		case "x-oss-signature", "x-amz-signature", "signature", "auth_key":
			return true
		}
	}
	return false
}

// This is only an identity proof for an already-bound, separately validated
// file. It grants no upload/read permission and performs no network request.
// Never infer a trusted endpoint or a file key from the untrusted reference.
func creatorAvatarReferenceMatchesFile(cfg ossConfigPayload, reference, objectKey string) bool {
	if objectKey == "" || path.Clean(objectKey) != objectKey || strings.HasPrefix(objectKey, "/") || strings.Contains(objectKey, `\`) {
		return false
	}
	if reference == ossStoredObjectURL(cfg, objectKey) {
		return true
	}
	if !temporaryCreatorAvatarReference(reference) {
		return false
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.User != nil || parsed.Fragment != "" {
		return false
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return false
	}
	seen := make(map[string]bool, len(query))
	for key, values := range query {
		key = strings.ToLower(key)
		if len(values) != 1 || seen[key] {
			return false
		}
		seen[key] = true
		switch key {
		case "x-oss-signature-version", "x-oss-date", "x-oss-expires", "x-oss-credential", "x-oss-signature", "x-oss-security-token", "x-oss-additional-headers",
			"ossaccesskeyid", "expires", "signature", "security-token", "auth_key",
			"x-amz-algorithm", "x-amz-credential", "x-amz-date", "x-amz-expires", "x-amz-signedheaders", "x-amz-security-token", "x-amz-signature",
			"response-content-type", "response-content-language", "response-content-disposition", "response-content-encoding", "response-cache-control", "response-expires":
			// Only registered access/header parameters can be discarded. Business
			// transformations such as size or variant require a separate contract.
		default:
			return false
		}
	}
	for _, endpoint := range []string{cfg.PublicEndpoint, cfg.Endpoint} {
		base, err := url.Parse(endpoint)
		if err != nil || base.User != nil || base.RawQuery != "" || base.Fragment != "" ||
			(base.Scheme != "https" && base.Scheme != "http") || base.Host == "" ||
			!strings.EqualFold(parsed.Scheme, base.Scheme) || !strings.EqualFold(parsed.Host, base.Host) {
			continue
		}
		basePath := strings.TrimRight(base.EscapedPath(), "/") + "/"
		if !strings.HasPrefix(parsed.EscapedPath(), basePath) {
			continue
		}
		key, err := url.PathUnescape(strings.TrimPrefix(parsed.EscapedPath(), basePath))
		if err != nil || key == "" || path.Clean(key) != key || strings.Contains(key, `\`) {
			continue
		}
		if key == objectKey || (cfg.Bucket != "" && key == cfg.Bucket+"/"+objectKey) {
			return true
		}
	}
	return false
}
