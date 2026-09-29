package ui

import (
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// Verification shows availability checks from a verify report.
func Verification(report verify.Report) string {
	var b strings.Builder
	b.WriteString(theme.Title.Render(TextFunctionalTitle) + "\n")
	for _, check := range report.Checks {
		style := theme.Label.Foreground(theme.WarningFg)
		switch check.State {
		case verify.Verified:
			style = theme.Label.Foreground(theme.SuccessFg)
		case verify.NotVerified:
			style = theme.Label.Foreground(theme.DangerFg)
		}
		fmt.Fprintf(&b, "%s %s\n  %s\n", style.Render("["+string(check.State)+"]"), theme.Label.Render(check.ID), theme.Label.Render(check.Explanation))
	}
	b.WriteString("\n" + theme.Label.Render(report.FinalNote))
	return b.String()
}
