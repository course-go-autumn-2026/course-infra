package redact

import (
	"strings"
	"testing"
)

func TestTerminalTextRemovesControlsAndBoundsUTF8(t *testing.T) {
	t.Parallel()
	got := TerminalText("\x1b[31mtoken=secret\x1b[0m\x00 café", 22)
	if strings.Contains(got, "\x1b") || strings.Contains(got, "secret") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("terminal-safe text = %q", got)
	}
	if !strings.Contains(TerminalText(strings.Repeat("é", 20), 7), "…") {
		t.Fatal("bounded UTF-8 text did not include truncation marker")
	}
}

func TestTerminalTextNormalizesControlsBeforeRedaction(t *testing.T) {
	t.Parallel()
	input := "pass\x1b[31mword=hunter2 api\x00_key=key-value Bear\x1b[0mer bearer-value postgres://user:pa\x1b[32mss@localhost/db"
	got := TerminalText(input, 4096)
	for _, secret := range []string{"hunter2", "key-value", "bearer-value", "user:pass"} {
		if strings.Contains(got, secret) {
			t.Fatalf("terminal-safe text contains %q: %q", secret, got)
		}
	}
	if strings.Count(got, "[REDACTED]") < 4 {
		t.Fatalf("terminal-safe text did not redact normalized credentials: %q", got)
	}
}

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
