package ui

import (
	"strings"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// capabilityNames maps a check's kind (and, for MCP servers, its server) to
// the capability a person recognizes; check identifiers never reach the UI.
var capabilityNames = map[string]string{
	"mcp:engram":    "Memory",
	"mcp:codegraph": "Code navigation",
	"mcp:context7":  "Library docs",
	"orchestrator":  "Takt orchestrator",
	"model":         "Default model",
	"skills":        "Skills",
	"plugins":       "Plugins",
	"reload":        "OpenCode reload",
}

// capabilityName resolves a check ID such as "mcp:opencode:engram".
func capabilityName(id string) string {
	parts := strings.Split(id, ":")
	key := parts[0]
	if key == "mcp" {
		key += ":" + parts[len(parts)-1]
	}
	if name, ok := capabilityNames[key]; ok {
		return name
	}
	return id
}

// Verification shows the verdict once, then one row per capability (PR-UX-27):
// verified rows are just their name; the others add state and explanation.
func Verification(report verify.Report) string {
	var b strings.Builder
	switch {
	case report.Failed():
		b.WriteString(Status(StateWarning, TextNotReady) + "\n\n")
	case !report.Ready && len(report.Checks) > 0:
		b.WriteString(Status(StateWarning, TextUnconfirmed) + "\n\n")
	}
	b.WriteString(theme.Title.Render(TextFunctionalTitle) + "\n")
	for _, check := range report.Checks {
		name := capabilityName(check.ID)
		switch check.State {
		case verify.Verified:
			b.WriteString(theme.SuccessText.Render(theme.Icon.Done) + " " + theme.Label.Render(name) + "\n")
		case verify.NotVerified:
			b.WriteString(theme.DangerText.Render(theme.Icon.Failed) + " " + theme.Label.Render(name) + theme.DangerText.Render(TextStateNotWorking) + "\n")
			b.WriteString("  " + theme.Caption.Render(check.Explanation) + "\n")
		default:
			b.WriteString(theme.WarningText.Render(theme.Icon.Unchecked) + " " + theme.Label.Render(name) + theme.WarningText.Render(TextStateNotChecked) + "\n")
			b.WriteString("  " + theme.Caption.Render(check.Explanation) + "\n")
		}
	}
	if report.FinalNote != "" {
		b.WriteString("\n" + theme.Caption.Render(report.FinalNote))
	}
	return strings.TrimRight(b.String(), "\n")
}
