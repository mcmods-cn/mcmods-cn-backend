package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestProtectedMutationUsesBoundedSuccessQueue(t *testing.T) {
	source, err := os.ReadFile("anti_abuse_middleware.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if strings.Contains(text, "go s.antiAbuse.RecordSuccess") {
		t.Fatal("successful mutation still starts one goroutine per request")
	}
	if !strings.Contains(text, "EnqueueSuccess") {
		t.Fatal("successful mutation does not use the bounded recorder queue")
	}
}
