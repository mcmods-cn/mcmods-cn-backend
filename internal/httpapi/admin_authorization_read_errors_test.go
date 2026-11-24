package httpapi

import (
	"context"
	"errors"
	"testing"
)

func TestAdminAuthorizationReadsPropagateEveryDatabaseError(t *testing.T) {
	failure := errors.New("unavailable role permissions")
	terminal := &simpleRowsFailureStub{terminalErr: failure}
	malformed := &simpleRowsFailureStub{next: true}
	for _, test := range []struct {
		name  string
		query commentReadQueryStub
		rows  *simpleRowsFailureStub
	}{
		{"query", commentReadQueryStub{err: failure}, nil},
		{"cursor", commentReadQueryStub{rows: terminal}, terminal},
		{"scan", commentReadQueryStub{rows: malformed}, malformed},
	} {
		t.Run(test.name, func(t *testing.T) {
			entries, err := readRolePermissionEntries(context.Background(), test.query, []string{"moderator"})
			if err == nil || entries != nil || (test.rows != nil && !test.rows.closed) {
				t.Fatalf("failed read returned entries=%v err=%v rows=%+v", entries, err, test.rows)
			}
		})
	}
}
