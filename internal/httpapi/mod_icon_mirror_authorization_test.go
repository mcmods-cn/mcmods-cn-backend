package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestReusableExternalImageObjectRequiresPurposeAndOwnerAuthorization(t *testing.T) {
	source, err := os.ReadFile("mod_icon_mirror.go")
	if err != nil {
		t.Fatal(err)
	}
	querySource := string(source)
	for _, condition := range []string{
		"category=$2",
		"source=$3",
		"uploader_id=$4",
	} {
		if !strings.Contains(querySource, condition) {
			t.Errorf("OSS image reuse query is missing %q authorization", condition)
		}
	}
}
