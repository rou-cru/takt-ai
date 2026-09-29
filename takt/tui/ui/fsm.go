package ui

import tea "charm.land/bubbletea/v2"

// TransitionKey names one rule in a transition table.
type TransitionKey[S comparable] struct {
	From  S
	Event string
}

// Table holds a flow's transitions as data.
type Table[S comparable, M any] map[TransitionKey[S]]func(*M) (S, tea.Cmd)

// Apply runs the rule for (from, event).
func (t Table[S, M]) Apply(m *M, from S, event string) (next S, cmd tea.Cmd, ok bool) {
	rule, exists := t[TransitionKey[S]{From: from, Event: event}]
	if !exists {
		return from, nil, false
	}
	next, cmd = rule(m)
	return next, cmd, true
}
