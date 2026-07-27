package mailer

import "testing"

func TestParseMailboxRejectsHeaderInjection(t *testing.T) {
	for _, value := range []string{
		"user@example.com\r\nBcc: victim@example.com",
		"user@example.com\nBcc: victim@example.com",
		"",
		"not-an-address",
	} {
		if _, err := parseMailbox(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestParseMailboxAcceptsAddressAndDisplayName(t *testing.T) {
	for _, value := range []string{"user@example.com", "Mcmods-cn <noreply@example.com>"} {
		address, err := parseMailbox(value)
		if err != nil {
			t.Fatalf("parse %q: %v", value, err)
		}
		if address.Address == "" {
			t.Fatalf("parse %q returned an empty address", value)
		}
	}
}
