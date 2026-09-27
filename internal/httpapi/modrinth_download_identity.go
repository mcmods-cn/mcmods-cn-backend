package httpapi

import (
	"net/url"
	"regexp"
	"strings"
)

var modrinthCDNIdentitySegment = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func modrinthDownloadIdentity(raw string) (string, string, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "cdn.modrinth.com") ||
		parsed.Port() != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return "", "", false
	}
	segments := strings.Split(parsed.EscapedPath(), "/")
	if len(segments) != 6 || segments[0] != "" || segments[1] != "data" || segments[3] != "versions" ||
		!modrinthCDNIdentitySegment.MatchString(segments[2]) || !modrinthCDNIdentitySegment.MatchString(segments[4]) {
		return "", "", false
	}
	fileName, err := url.PathUnescape(segments[5])
	if err != nil || fileName == "" || fileName == "." || fileName == ".." || strings.ContainsAny(fileName, `/\`) {
		return "", "", false
	}
	return segments[2], segments[4], true
}

func consistentModrinthDownloadIdentity(downloads []string) (string, string, bool) {
	if len(downloads) == 0 {
		return "", "", false
	}
	projectID, versionID := "", ""
	for _, download := range downloads {
		candidateProjectID, candidateVersionID, ok := modrinthDownloadIdentity(download)
		if !ok {
			return "", "", false
		}
		if projectID == "" {
			projectID, versionID = candidateProjectID, candidateVersionID
			continue
		}
		if candidateProjectID != projectID || candidateVersionID != versionID {
			return "", "", false
		}
	}
	return projectID, versionID, true
}
