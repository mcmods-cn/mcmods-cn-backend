package httpapi

import (
	"errors"
	"net/url"
	"strings"
)

func parseCatalogSlugExclusion(values url.Values) (string, error) {
	value := normalizeModSiteID(values.Get("excludeSiteId"))
	if value != "" && !validModSiteID(value) {
		return "", errors.New("invalid catalog exclusion")
	}
	return value, nil
}

func parseCatalogPublicIDExclusion(values url.Values) (string, error) {
	value := strings.ToLower(strings.TrimSpace(values.Get("excludeSiteId")))
	// Multi-type pickers send the current catalog slug to every participating
	// endpoint. A valid non-server slug is a harmless no-op here; rejecting it
	// would make one server branch fail the entire combined page.
	if value != "" && !validModSiteID(value) {
		return "", errors.New("invalid catalog exclusion")
	}
	return value, nil
}
