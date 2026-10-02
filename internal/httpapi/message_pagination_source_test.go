package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestDirectMessageHandlersExposeBoundedCursorPages(t *testing.T) {
	handlerRaw, err := os.ReadFile("message_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	paginationRaw, err := os.ReadFile("message_pagination.go")
	if err != nil {
		t.Fatal(err)
	}
	combined := string(handlerRaw) + string(paginationRaw)
	for _, required := range []string{
		"parseConversationPageRequest", "loadConversationPage", "parseDirectMessagePageRequest", "loadDirectMessagePage",
		`"hasMore"`, `"nextCursor"`, "writeBoundedCatalogJSON",
	} {
		if !strings.Contains(combined, required) {
			t.Errorf("direct-message pagination is missing %q", required)
		}
	}
	handler := string(handlerRaw)
	conversationsStart := strings.Index(handler, "func (s *Server) conversations")
	conversationEnd := strings.Index(handler, "func (s *Server) startConversation")
	if conversationsStart < 0 || conversationEnd <= conversationsStart {
		t.Fatal("conversation handler boundary not found")
	}
	conversationHandler := strings.ToLower(handler[conversationsStart:conversationEnd])
	if strings.Contains(conversationHandler, "select count(*)") || strings.Contains(conversationHandler, "join lateral") {
		t.Fatal("conversation handler still embeds unbounded per-row message queries")
	}
}
