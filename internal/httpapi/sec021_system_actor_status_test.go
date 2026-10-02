package httpapi

import (
	"testing"

	"mcmods-cn-backend/internal/systemactor"
)

func TestSEC021SystemActorStatusHasAnExplicitNonInteractiveRecoveryPath(t *testing.T) {
	for _, test := range []struct {
		name     string
		username string
		email    string
		status   string
		want     bool
	}{
		{name: "restore system actor", username: systemactor.AutobotUsername, email: systemactor.AutobotEmail, status: systemactor.AutobotStatus, want: true},
		{name: "disable system actor", username: systemactor.AutobotUsername, email: systemactor.AutobotEmail, status: "disabled", want: true},
		{name: "delete system actor", username: systemactor.AutobotUsername, email: systemactor.AutobotEmail, status: "deleted", want: true},
		{name: "never make system actor interactive", username: systemactor.AutobotUsername, email: systemactor.AutobotEmail, status: "active", want: false},
		{name: "never ban system actor as a user", username: systemactor.AutobotUsername, email: systemactor.AutobotEmail, status: "banned", want: false},
		{name: "ordinary user cannot become system", username: "ordinary", email: "ordinary@example.test", status: systemactor.AutobotStatus, want: false},
		{name: "ordinary active user", username: "ordinary", email: "ordinary@example.test", status: "active", want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validSecurityManagedUserStatus(test.username, test.email, test.status); got != test.want {
				t.Fatalf("validSecurityManagedUserStatus(%q,%q,%q)=%v want %v", test.username, test.email, test.status, got, test.want)
			}
		})
	}
}
