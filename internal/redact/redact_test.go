package redact

import (
	"strings"
	"testing"
)

func TestTextRemovesCredentials(t *testing.T) {
	t.Parallel()
	input := "DATABASE_URL=postgres://" + "tripgo:tripgo" + `@localhost/db password="` + "hunter2" + `" token=` + "abc" + ` Authorization: Bearer ` + "xyz" // #nosec G101 -- synthetic redaction fixtures.
	got := Text(input)
	for _, secret := range []string{"tripgo:tripgo", "hunter2", "abc", "xyz"} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted text contains %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "localhost/db") || strings.Count(got, "[REDACTED]") < 4 {
		t.Fatalf("redacted text lost context: %s", got)
	}
}
