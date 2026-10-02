package httpapi

import (
	"os"
	"strings"
	"testing"
)

func TestUnreadReconciliationSamplesLiveCachedDerivativesInsteadOfSweepingAllUsers(t *testing.T) {
	raw, err := os.ReadFile("unread_reconciliation.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	periodicEnd := strings.Index(source, "func (s *Server) adminReconcileUnread")
	if periodicEnd < 0 {
		t.Fatal("periodic unread reconciliation boundary not found")
	}
	periodic := source[:periodicEnd]
	for _, forbidden := range []string{
		"var cursor int64",
		"u.status='active' and u.id>$1",
		"if processed == 0 {\n\t\tnext = 0",
	} {
		if strings.Contains(periodic, forbidden) {
			t.Fatalf("periodic reconciliation still walks the full active-user table via %q", forbidden)
		}
	}
	for _, required := range []string{
		"unreadReconcileMaxSampleSize",
		"UnreadReconciliationCandidates",
		"reconcileUnreadUsers",
	} {
		if !strings.Contains(periodic, required) {
			t.Errorf("periodic reconciliation is missing bounded cache sampling contract %q", required)
		}
	}
	for _, query := range []string{unreadTruthInternalUsersSQL, unreadTruthAnyInternalUsersSQL} {
		for _, required := range []string{
			"selected_users as materialized",
			"notification_broadcast_state",
			"direct_notification_counts",
			"message_counts",
			"broadcast_receipt_corrections",
		} {
			if !strings.Contains(query, required) {
				t.Errorf("set-based unread truth query is missing %q", required)
			}
		}
		if strings.Contains(query, "(n.recipient_id is null or n.recipient_id=target.user_id)") {
			t.Fatal("shared broadcasts are still joined once per selected user with the direct-notification stream")
		}
	}
}
