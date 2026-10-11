// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package modelpicker

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
	// detailMinWidth is the narrowest detail pane worth drawing; below it the
	// list keeps the whole width.
	detailMinWidth = 40
	// paneGap separates the list from the detail pane; paneDivider is the line
	// between them.
	paneGap     = 2
	paneDivider = "│"
	// tabGap separates the provider tabs; tabMore marks tabs scrolled out of view.
	tabGap  = "   "
	tabMore = "…"
	// detailLabelWidth aligns the detail values in one column.
	detailLabelWidth = 10
	// tokensPerK and tokensPerM convert token counts to the k and M suffixes.
	tokensPerK = 1000
	tokensPerM = 1000 * tokensPerK
)

// providerOf returns the provider id of a provider/model reference.
func providerOf(ref string) string {
	id, _, _ := strings.Cut(ref, "/")
	return id
}

// providerIDs lists the providers that have models, in list order.
func (p Model) providerIDs() []string {
	var ids []string
	for _, ref := range p.Available {
		if id := providerOf(ref); !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// providerLabel is the name OpenCode gives the provider, or its id when none.
func (p Model) providerLabel(id string) string {
	if name := p.Providers[id]; name != "" {
		return name
	}
	return id
}

// moveTab switches to the neighbouring provider tab, wrapping around. The
// All tab is the empty provider id.
func (p *Model) moveTab(delta int) {
	tabs := append([]string{""}, p.providerIDs()...)
	p.tab = tabs[ui.MoveCursor(max(0, slices.Index(tabs, p.tab)), len(tabs), delta)]
	p.modelCursor = 0
}

// tabsView draws the provider tabs with their model counts, scrolled so the
// active tab stays visible. One provider needs no tabs.
func (p Model) tabsView(width int) string {
	ids := p.providerIDs()
	if len(ids) < 2 {
		return ""
	}
	tabs := append([]string{""}, ids...)
	labels := make([]string, len(tabs))
	for index, id := range tabs {
		count, name := len(p.Available), ui.TextPickerAllTab
		if id != "" {
			count = p.countIn(id)
			name = p.providerLabel(id)
		}
		labels[index] = fmt.Sprintf(ui.TextPickerTabFmt, name, count)
	}
	active := max(0, slices.Index(tabs, p.tab))
	// Drop leading tabs until the active one fits.
	start := 0
	for start < active && lipgloss.Width(strings.Join(labels[start:active+1], tabGap)) > width-lipgloss.Width(tabMore+tabGap) {
		start++
	}
	var out strings.Builder
	if start > 0 {
		out.WriteString(theme.Caption.Render(tabMore + tabGap))
	}
	for index := start; index < len(labels); index++ {
		style := theme.Secondary
		if index == active {
			style = theme.Focus.Bold(true)
		}
		out.WriteString(style.Render(labels[index]))
		if index < len(labels)-1 {
			out.WriteString(tabGap)
		}
	}
	return out.String()
}

func (p Model) countIn(provider string) int {
	count := 0
	for _, ref := range p.Available {
		if providerOf(ref) == provider {
			count++
		}
	}
	return count
}

// formatTokens writes a token count as 128k or 1M.
func formatTokens(tokens int) string {
	if tokens >= tokensPerM {
		return strings.TrimSuffix(strconv.FormatFloat(float64(tokens)/tokensPerM, 'f', 1, 64), ".0") + "M"
	}
	return strconv.Itoa((tokens+tokensPerK/2)/tokensPerK) + "k"
}

// formatMoney writes a dollar amount with at least two decimals.
func formatMoney(amount float64) string {
	text := strconv.FormatFloat(amount, 'f', -1, 64)
	if _, decimals, found := strings.Cut(text, "."); !found {
		return text + ".00"
	} else if len(decimals) < 2 {
		return text + "0"
	}
	return text
}

// detailView describes one model with the facts OpenCode states; a fact the
// API leaves out gets no line.
func (p Model) detailView(ref string, width int) string {
	info, ok := p.Info[ref]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString(theme.Title.Render(info.Name) + "\n" + theme.Caption.Render(ref) + "\n\n")
	line := func(label, value string) {
		// Long values wrap under their own column, not under the label.
		wrapped := lipgloss.NewStyle().Width(max(1, width-detailLabelWidth)).Render(theme.Label.Render(value))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, theme.Secondary.Render(fmt.Sprintf("%-*s", detailLabelWidth, label)), wrapped) + "\n")
	}
	for index, tier := range info.Cost {
		value := "$" + formatMoney(tier.Input) + " in · $" + formatMoney(tier.Output) + " out"
		label := ""
		if index == 0 {
			label = ui.TextPickerCostLabel
		}
		if tier.AboveContext > 0 {
			value = ">" + formatTokens(tier.AboveContext) + " ctx: $" + formatMoney(tier.Input) + " / $" + formatMoney(tier.Output)
		}
		line(label, value)
	}
	if len(info.Cost) > 0 && (info.Cost[0].CacheRead > 0 || info.Cost[0].CacheWrite > 0) {
		line(ui.TextPickerCacheLabel, "read $"+formatMoney(info.Cost[0].CacheRead)+" · write $"+formatMoney(info.Cost[0].CacheWrite))
	}
	limits := formatTokens(info.ContextLimit) + " context · " + formatTokens(info.OutputLimit) + " output"
	if info.InputLimit > 0 {
		limits += " · " + formatTokens(info.InputLimit) + " input"
	}
	line(ui.TextPickerLimitsLabel, limits)
	if len(info.Input) > 0 {
		line(ui.TextPickerInputLabel, strings.Join(info.Input, "  "))
	}
	if len(info.Variants) > 0 {
		line(ui.TextPickerVariantLabel, strings.Join(info.Variants, "  "))
	}
	if !info.Released.IsZero() {
		line(ui.TextPickerReleaseLabel, info.Released.Format("2006-01-02"))
	}
	return strings.TrimRight(b.String(), "\n")
}
