package httpapi

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata"
)

func normalizeTimezone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "Local" || len(value) > 128 {
		return "", fmt.Errorf("timezone must be a valid IANA timezone")
	}
	location, err := time.LoadLocation(value)
	if err != nil {
		return "", fmt.Errorf("timezone must be a valid IANA timezone")
	}
	return location.String(), nil
}
