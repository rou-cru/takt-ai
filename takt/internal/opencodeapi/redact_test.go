package opencodeapi_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
)

func TestRedactErrorNil(t *testing.T) {
	if got := opencodeapi.RedactError(nil); got != nil {
		t.Errorf("RedactError(nil) = %v, want nil", got)
	}
}

func TestRedactErrorUnchangedWhenNothingSensitive(t *testing.T) {
	original := errors.New("connection refused")
	got := opencodeapi.RedactError(original)
	if got != original {
		t.Errorf("RedactError with no sensitive content should return the original error unchanged, got %v", got)
	}
}

func TestRedactErrorMasksSensitiveContent(t *testing.T) {
	tests := []struct {
		name    string
		message string
		wantNot string
	}{
		{"api key field", `request failed: {"api_key": "sk-ant-abcdef123456"}`, "sk-ant-abcdef123456"},
		{"authorization header", "authorization: abcdef123456token", "abcdef123456token"},
		{"cookie header", "set-cookie: session=abcdef123456; Path=/", "abcdef123456"},
		{"prompt flag", `opencode run --prompt "do something secret"`, "do something secret"},
		{"password field", `config: {"password": "hunter2"}`, "hunter2"},
		{"github token", "using token ghp_abcdefghij1234567890", "ghp_abcdefghij1234567890"},
		{"aws key", "found key AKIAABCDEFGHIJKLMNOP in logs", "AKIAABCDEFGHIJKLMNOP"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			original := fmt.Errorf("%s", tc.message)
			got := opencodeapi.RedactError(original)
			if got == nil {
				t.Fatal("RedactError returned nil for a non-nil error")
			}
			if strings.Contains(got.Error(), tc.wantNot) {
				t.Errorf("RedactError(%q) = %q, still contains sensitive value %q", tc.message, got.Error(), tc.wantNot)
			}
			if !strings.Contains(got.Error(), "[REDACTED]") {
				t.Errorf("RedactError(%q) = %q, want it to contain [REDACTED]", tc.message, got.Error())
			}
		})
	}
}

func TestRedactErrorPreservesErrorChain(t *testing.T) {
	sentinel := errors.New("sentinel")
	wrapped := fmt.Errorf(`request failed with api_key: "sk-ant-secret123" cause: %w`, sentinel)
	got := opencodeapi.RedactError(wrapped)
	if !errors.Is(got, sentinel) {
		t.Errorf("RedactError should preserve the error chain via errors.Is, got %v", got)
	}
}
