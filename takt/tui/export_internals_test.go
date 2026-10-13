package tui

import tea "charm.land/bubbletea/v2"

// WithStack opens flows over the menu under the install route, as a route
// would, so black-box tests drive the controller with stand-in flows. With no
// flows it leaves the install route over an empty stack.
func WithStack(flows ...tea.Model) Model {
	m := New("/nonexistent")
	m.stack = flows
	m.routes = make([]Route, len(flows))
	for i := range m.routes {
		m.routes[i] = RouteInstall
	}
	m.route = RouteInstall
	return m
}
