package runtime

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// CancelledBody explains a cancelled result so users know what stayed applied.
func CancelledBody(result ActionResult) string {
	applied := slices.Concat(result.Changed, result.Removed, result.Restored, result.Retained)
	if !result.Partial() {
		return ui.Status(ui.StateWarning, ui.TextCancelledNone) + "\n\n" + theme.Label.Render(ui.TextOutcomeNothing)
	}
	var b strings.Builder
	b.WriteString(ui.Status(ui.StatePartial, fmt.Sprintf(ui.TextCancelledPartFmt, len(applied), len(applied)+len(result.NotApplied))))
	b.WriteString("\n\n" + theme.Label.Render(ui.TextOutcomeAppliedHead))
	for _, path := range applied {
		b.WriteString("\n" + theme.Label.Render("  "+path))
	}
	notApplied := fmt.Sprintf(ui.TextNotAppliedFmt, len(result.NotApplied))
	if len(result.NotApplied) == 1 {
		notApplied = ui.TextOneNotApplied
	}
	b.WriteString("\n\n" + theme.Label.Render(ui.TextAppliedStaysIntro+notApplied+"."))
	recovery := ui.TextNoRollback
	if result.BackupDir != "" {
		recovery += ui.TextBackupKeptIntro + result.BackupDir + "."
	}
	b.WriteString("\n" + theme.Label.Render(recovery))
	return b.String()
}

// LateCancelNote explains a result that finished before cancellation took effect.
func LateCancelNote(result ActionResult) string {
	if !result.CancelRequested || result.Cancelled() {
		return ""
	}
	return theme.Label.Render(ui.TextLateCancel)
}
