// Package redact removes credentials from user-visible diagnostic and log text.
package redact

import "regexp"

var (
	uriCredentials  = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]+@`)
	credentialField = regexp.MustCompile(`(?i)("?(?:password|passwd|token|secret|authorization|api[_-]?key)"?\s*[:=]\s*)("[^"]*"|'[^']*'|[^,\s}]+)`)
	bearer          = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._~+/=-]+`)
)

// Text preserves surrounding context while replacing common credential forms.
func Text(value string) string {
	value = uriCredentials.ReplaceAllString(value, `${1}[REDACTED]@`)
	value = bearer.ReplaceAllString(value, `${1}[REDACTED]`)
	return credentialField.ReplaceAllString(value, `${1}[REDACTED]`)
}
