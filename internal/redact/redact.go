// Package redact removes credentials from user-visible diagnostic and log text.
package redact

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	uriCredentials  = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)
	credentialField = regexp.MustCompile(`(?i)("?(?:password|passwd|token|secret|authorization|api[_-]?key)"?\s*[:=]\s*)("[^"]*"|'[^']*'|[^,\s}]+)`)
	bearer          = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
	ansiEscape      = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
)

// Text preserves surrounding context while replacing common credential forms.
func Text(value string) string {
	value = uriCredentials.ReplaceAllString(value, `${1}[REDACTED]@`)
	value = bearer.ReplaceAllString(value, `${1}[REDACTED]`)
	return credentialField.ReplaceAllString(value, `${1}[REDACTED]`)
}

// TerminalText additionally removes terminal controls and bounds externally
// sourced text before it can reach a terminal or diagnostic file.
func TerminalText(value string, maxBytes int) string {
	// Normalize terminal controls before matching secrets. Otherwise an
	// attacker-controlled stream could split "password" or "Bearer" with an
	// ANSI sequence and reconstruct the secret only after redaction.
	value = ansiEscape.ReplaceAllString(value, "")
	value = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' || character >= ' ' {
			return character
		}
		return -1
	}, value)
	value = Text(value)
	if maxBytes <= 0 || len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "…"
}
