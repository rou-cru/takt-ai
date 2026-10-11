// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package modelpicker

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
	// detailMinWidth is the narrowest detail pane worth drawing: the width of
	// the full price table. Below it the list keeps the whole width.
	detailMinWidth = 54
	// listMinWidth is the narrowest list column beside the detail pane.
	listMinWidth = 20
	// paneGap separates the list from the detail pane; paneDivider is the line
	// between them.
	paneGap     = 2
	paneDivider = "│"
	// tabGap separates the provider tabs; tabMore marks tabs scrolled out of view.
	tabGap  = "   "
	tabMore = "…"
	// detailLabelWidth aligns the detail values in one column.
	detailLabelWidth = 11
	// priceGap separates the price table's columns.
	priceGap = 2
	// Money shows at least minMoneyDecimals and at most maxMoneyDecimals; the
	// cap keeps float noise out of a price.
	minMoneyDecimals = 2
	maxMoneyDecimals = 4
	// tokensPerK and tokensPerM convert token counts to the k and M suffixes.
	tokensPerK = 1000
	tokensPerM = 1000 * tokensPerK
	// providerAccountSep splits the account OpenCode prefixes to a provider
	// name ("Default / OpenCode") from the provider itself.
	providerAccountSep = " / "
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

// activeTab is the provider whose models are listed: the chosen tab while it
// still has models, else the first provider.
func (p Model) activeTab() string {
	ids := p.providerIDs()
	if slices.Contains(ids, p.tab) || len(ids) == 0 {
		return p.tab
	}
	return ids[0]
}

// providerLabel is the name OpenCode gives the provider without its account
// prefix, or its id when it has none. A short name that two providers share
// keeps its account prefix so the tabs stay distinguishable.
func (p Model) providerLabel(id string) string {
	short := func(id string) string {
		name := p.Providers[id]
		if name == "" {
			return id
		}
		if index := strings.LastIndex(name, providerAccountSep); index >= 0 {
			return name[index+len(providerAccountSep):]
		}
		return name
	}
	label := short(id)
	for _, other := range p.providerIDs() {
		if other != id && short(other) == label && p.Providers[id] != "" {
			return p.Providers[id]
		}
	}
	return label
}

// moveTab switches to the neighbouring provider tab, wrapping around.
func (p *Model) moveTab(delta int) {
	tabs := p.providerIDs()
	if len(tabs) == 0 {
		return
	}
	p.tab = tabs[ui.MoveCursor(max(0, slices.Index(tabs, p.activeTab())), len(tabs), delta)]
	p.modelCursor = 0
}

// tabsView draws the provider tabs with the count of models each lists under
// the current search, scrolled so the active tab stays visible. One provider
// needs no tabs.
func (p Model) tabsView(width int) string {
	tabs := p.providerIDs()
	if len(tabs) < 2 {
		return ""
	}
	labels := make([]string, len(tabs))
	for index, id := range tabs {
		labels[index] = fmt.Sprintf(ui.TextPickerTabFmt, p.providerLabel(id), len(p.modelsIn(id)))
	}
	active := max(0, slices.Index(tabs, p.activeTab()))
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

// formatTokens writes a token count as 128k or 1M.
func formatTokens(tokens int) string {
	if tokens >= tokensPerM {
		return strings.TrimSuffix(strconv.FormatFloat(float64(tokens)/tokensPerM, 'f', 1, 64), ".0") + "M"
	}
	return strconv.Itoa((tokens+tokensPerK/2)/tokensPerK) + "k"
}

// moneyDecimals is the precision one amount needs, within the money bounds.
func moneyDecimals(amount float64) int {
	_, decimals, _ := strings.Cut(strconv.FormatFloat(amount, 'f', -1, 64), ".")
	return min(max(len(decimals), minMoneyDecimals), maxMoneyDecimals)
}

// priceTable lays out the price tiers per 1M tokens: one row per tier, every
// amount at the precision the most precise one needs, right-aligned so the
// decimal points line up. A cache column that is zero in every tier is left
// out; input and output always show, since a zero there is a stated price.
func priceTable(tiers []opencodeapi.CostTier) []string {
	type column struct {
		header string
		amount func(opencodeapi.CostTier) float64
	}
	columns := []column{
		{ui.TextPickerPriceInput, func(t opencodeapi.CostTier) float64 { return t.Input }},
		{ui.TextPickerPriceOutput, func(t opencodeapi.CostTier) float64 { return t.Output }},
	}
	cache := []column{
		{ui.TextPickerCacheRead, func(t opencodeapi.CostTier) float64 { return t.CacheRead }},
		{ui.TextPickerCacheWrite, func(t opencodeapi.CostTier) float64 { return t.CacheWrite }},
	}
	for _, c := range cache {
		if slices.ContainsFunc(tiers, func(t opencodeapi.CostTier) bool { return c.amount(t) > 0 }) {
			columns = append(columns, c)
		}
	}
	decimals := minMoneyDecimals
	for _, tier := range tiers {
		for _, c := range columns {
			decimals = max(decimals, moneyDecimals(c.amount(tier)))
		}
	}
	cells := make([][]string, len(tiers))
	widths := make([]int, len(columns))
	for index, c := range columns {
		widths[index] = lipgloss.Width(c.header)
	}
	for row, tier := range tiers {
		for index, c := range columns {
			cell := "$" + strconv.FormatFloat(c.amount(tier), 'f', decimals, 64)
			cells[row] = append(cells[row], cell)
			widths[index] = max(widths[index], len(cell))
		}
	}
	line := func(label string, values []string, style lipgloss.Style) string {
		var b strings.Builder
		b.WriteString(theme.Secondary.Render(fmt.Sprintf("%-*s", detailLabelWidth, label)))
		for index, value := range values {
			if index > 0 {
				b.WriteString(strings.Repeat(" ", priceGap))
			}
			b.WriteString(style.Render(fmt.Sprintf("%*s", widths[index], value)))
		}
		return b.String()
	}
	headers := make([]string, len(columns))
	for index, c := range columns {
		headers[index] = c.header
	}
	lines := []string{theme.Secondary.Render(ui.TextPickerPriceTitle), line("", headers, theme.Caption)}
	for row, tier := range tiers {
		lines = append(lines, line(tierLabel(tiers, row, tier), cells[row], theme.Label))
	}
	return lines
}

// tierLabel names a price tier by the context size it applies to; a single
// tier applies everywhere and needs no name.
func tierLabel(tiers []opencodeapi.CostTier, row int, tier opencodeapi.CostTier) string {
	switch {
	case len(tiers) < 2:
		return ""
	case tier.AboveContext > 0:
		return fmt.Sprintf(ui.TextPickerTierAboveFmt, formatTokens(tier.AboveContext))
	case row+1 < len(tiers) && tiers[row+1].AboveContext > 0:
		return fmt.Sprintf(ui.TextPickerTierUpToFmt, formatTokens(tiers[row+1].AboveContext))
	}
	return ""
}

// detailView describes one model with the facts OpenCode states; a fact the
// API leaves out gets no line.
func (p Model) detailView(ref string, width int) string {
	info, ok := p.Info[ref]
	name := p.label(ref)
	if ok && info.Name != "" {
		name = info.Name
	}
	var b strings.Builder
	b.WriteString(theme.Title.Render(name) + "\n" + theme.Caption.Render(ref) + "\n")
	if !ok {
		return strings.TrimRight(b.String(), "\n")
	}
	b.WriteString("\n")
	if len(info.Cost) > 0 {
		b.WriteString(strings.Join(priceTable(info.Cost), "\n") + "\n\n")
	}
	line := func(label, value string) {
		// Long values wrap under their own column, not under the label.
		wrapped := lipgloss.NewStyle().Width(max(1, width-detailLabelWidth)).Render(theme.Label.Render(value))
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, theme.Secondary.Render(fmt.Sprintf("%-*s", detailLabelWidth, label)), wrapped) + "\n")
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
