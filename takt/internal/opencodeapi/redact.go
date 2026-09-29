package opencodeapi

import (
	"regexp"
	"strings"
)

const redactedValue = "[REDACTED]"

// redactedError keeps errors.Is/errors.As useful while making the displayed
// message safe for doctor reports and verification diagnostics.
type redactedError struct {
	cause   error
	message string
}

// Error returns the redacted message.
func (e redactedError) Error() string { return e.message }

// Unwrap preserves the original error chain for errors.Is and errors.As.
func (e redactedError) Unwrap() error { return e.cause }

// RedactError removes credentials and prompt contents before an API error is
// included in user-facing diagnostics. It preserves the original error chain.
func RedactError(err error) error {
	if err == nil {
		return nil
	}
	message := redactSensitiveText(err.Error())
	if message == err.Error() {
		return err
	}
	return redactedError{cause: err, message: message}
}

var sensitivePatterns = []*regexp.Regexp{
	// Structured values are handled before generic key/value pairs so quotes
	// and JSON punctuation do not become part of the replacement.
	regexp.MustCompile(`(?i)(["']?(?:api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|id[-_ ]?token|client[-_ ]?secret|authorization|cookie|set-cookie|password|passwd|credential|credentials|prompt|system|user[-_ ]?prompt)["']?\s*:\s*)("[^"]*"|'[^']*'|[^,}\s]+)`),
	regexp.MustCompile(`(?i)(--prompt(?:=|\s+))("[^"]*"|'[^']*'|[^\r\n]+)`),
	regexp.MustCompile(`(?i)(\b(?:authorization)\s*[:=]\s*(?:bearer|basic|token)?\s*)[^\s,;]+`),
	regexp.MustCompile(`(?i)(\b(?:cookie|set-cookie)\s*[:=]\s*)[^\r\n]+`),
	regexp.MustCompile(`(?i)(\b(?:api[-_ ]?key|access[-_ ]?token|refresh[-_ ]?token|id[-_ ]?token|client[-_ ]?secret|password|passwd|credential|credentials|prompt|system|user[-_ ]?prompt)\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;&]+)`),
	regexp.MustCompile(`(?i)\b(?:sk|sk-ant|xai)-[a-z0-9_-]+\b`),
	regexp.MustCompile(`\b(?:AIza[a-z0-9_-]{20,}|(?:ghp|github_pat)_[a-z0-9_]{10,}|AKIA[0-9A-Z]{16})\b`),
}

// redactSensitiveText deliberately works on text rather than structured
// errors: the OpenCode CLI reports failures through human-readable stderr,
// where headers, cookies, request fields and prompts may all be mixed.
func redactSensitiveText(text string) string {
	for _, pattern := range sensitivePatterns[:5] {
		text = pattern.ReplaceAllString(text, `$1`+redactedValue)
	}
	// Prefix-only patterns have no capture group and therefore need a separate
	// replacement pass. Keeping this last prevents a redacted value from being
	// mistaken for another credential.
	for _, pattern := range sensitivePatterns[5:] {
		text = pattern.ReplaceAllString(text, redactedValue)
	}
	return strings.TrimSpace(text)
}
