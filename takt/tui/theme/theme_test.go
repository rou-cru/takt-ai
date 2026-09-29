package theme_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

func TestRolesResolveToDarkTokens(t *testing.T) {
	if theme.TextPrimary != theme.Dark.TextPrimary || theme.SuccessFg != theme.Dark.SuccessFg {
		t.Fatal("role colors did not resolve to the dark palette")
	}
}
