package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestGeneratedOSSObjectRegistrationUsesDurableCompensation(t *testing.T) {
	raw, err := os.ReadFile("oss_access.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	for _, forbidden := range []string{
		"client.DeleteObject",
		"delete unregistered generated object",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("generated object registration still uses one-shot cleanup %q", forbidden)
		}
	}
	for _, required := range []string{
		"deleteOSSObjectIfUnregistered",
		"generated-object-registration-failed",
		"queue generated OSS cleanup",
		"errors.Join",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("generated object compensation is missing %q", required)
		}
	}
}
