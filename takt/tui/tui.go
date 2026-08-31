// Package tui composes Takt's interactive lifecycle flows.
package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/styles"
	syncflow "github.com/rou-cru/takt-ai/takt/tui/sync"
	"github.com/rou-cru/takt-ai/takt/tui/uninstall"
)

// Route identifies a screen in the top-level application.
type Route string

const (
	RouteMenu      Route = "menu"
	RouteInstall   Route = "install"
	RouteSync      Route = "sync"
	RouteUninstall Route = "uninstall"
)

var menuItems = []struct {
	route Route
	label string
}{
	{RouteInstall, "Install"},
	{RouteSync, "Sync"},
	{RouteUninstall, "Uninstall"},
	{"", "Quit"},
}

// Model owns navigation and the action boundary shared by lifecycle flows.
type Model struct {
	root    string
	cursor  int
	route   Route
	active  tea.Model
	runtime runtime.Model
}

// New creates the top-level TUI rooted at root.
func New(root string) Model {
	return Model{root: root, route: RouteMenu, runtime: runtime.NewModel()}
}

// Run starts the TUI with explicit streams so callers can control program I/O.
func Run(input io.Reader, output io.Writer) error {
	root, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	_, err = tea.NewProgram(New(root), tea.WithInput(input), tea.WithOutput(output)).Run()
	return err
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// CurrentRoute returns the visible route.
func (m Model) CurrentRoute() Route { return m.route }

// Update routes input to the visible screen and action messages through runtime.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message.(type) {
	case runtime.ActionRequest, runtime.ActionResultMsg:
		return m.updateAction(message)
	case syncflow.BackMsg:
		m.returnToMenu()
		return m, nil
	case tea.KeyMsg:
		if m.active != nil && !m.runtime.Busy && message.(tea.KeyMsg).String() == "ctrl+b" {
			m.returnToMenu()
			return m, nil
		}
	}

	if m.active == nil {
		return m.updateMenu(message)
	}
	next, command := m.active.Update(message)
	m.active = next
	if flow, ok := next.(uninstall.Model); ok && flow.BackRequested() {
		m.returnToMenu()
	}
	return m, command
}

func (m Model) updateAction(message tea.Msg) (tea.Model, tea.Cmd) {
	nextRuntime, actionCommand := m.runtime.Update(message)
	m.runtime = nextRuntime.(runtime.Model)
	if m.active == nil {
		return m, actionCommand
	}
	nextFlow, flowCommand := m.active.Update(message)
	m.active = nextFlow
	return m, tea.Batch(actionCommand, flowCommand)
}

func (m Model) updateMenu(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		m.cursor = (m.cursor + len(menuItems) - 1) % len(menuItems)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(menuItems)
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter":
		item := menuItems[m.cursor]
		if item.route == "" {
			return m, tea.Quit
		}
		m.open(item.route)
	}
	return m, nil
}

func (m *Model) open(route Route) {
	m.route = route
	switch route {
	case RouteInstall:
		m.active = install.New(m.root)
	case RouteSync:
		m.active = syncflow.New(m.root)
	case RouteUninstall:
		m.active = uninstall.New(m.root)
	}
}

func (m *Model) returnToMenu() {
	m.route = RouteMenu
	m.active = nil
}

// View renders the menu or the active lifecycle flow.
func (m Model) View() string {
	if m.active != nil {
		return m.active.View() + "\n\nStatus: " + m.status() + " · Ctrl+B returns to menu\n"
	}
	var view strings.Builder
	view.WriteString(styles.RenderLogo())
	view.WriteString("\n\n")
	view.WriteString("Takt AI\n\nChoose an operation:\n")
	for index, item := range menuItems {
		marker := " "
		if index == m.cursor {
			marker = ">"
		}
		fmt.Fprintf(&view, "%s %s\n", marker, item.label)
	}
	view.WriteString("\nStatus: Ready\nUp/Down choose · Enter open · q quit\n")
	return view.String()
}

func (m Model) status() string {
	if m.runtime.Busy {
		return "Working"
	}
	if m.runtime.Presentation.Err != nil {
		return "Failed"
	}
	if m.runtime.Presentation.Message != "" {
		return "Complete"
	}
	return "Ready"
}
