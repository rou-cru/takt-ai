// Package runtime provides Bubble Tea lifecycle action boundaries.
package runtime

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/common"
)

// Action identifies a setup lifecycle operation.
type Action string

const (
	ActionInstall   Action = "install"
	ActionSync      Action = "sync"
	ActionUninstall Action = "uninstall"
)

// ActionRequest is the asynchronous boundary between a lifecycle screen and setup.
type ActionRequest struct {
	Action     Action
	RootDir    string
	Targets    []model.AgentID
	Components []string
}

// ActionResult reports the paths affected by an action.
type ActionResult struct {
	Action    Action
	Changed   []string
	Unchanged []string
	Removed   []string
	Preserved []string
}

// ActionResultMsg returns an action result to Bubble Tea's event loop.
type ActionResultMsg struct {
	Request ActionRequest
	Result  ActionResult
	Err     error
}

// TickMsg advances shared busy-state animation.
type TickMsg struct{ Frame int }

// Tick schedules the next spinner frame.
func Tick(frame int) tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return TickMsg{Frame: frame + 1} })
}

// Adapter invokes setup's lifecycle APIs. It owns no planning or filesystem logic.
type Adapter struct{}

// Command returns a Bubble Tea command that performs request after Update returns.
func (Adapter) Command(request ActionRequest) tea.Cmd {
	return func() tea.Msg {
		result, err := (Adapter{}).Execute(request)
		return ActionResultMsg{Request: request, Result: result, Err: err}
	}
}

// Execute invokes the requested setup lifecycle operation.
func (Adapter) Execute(request ActionRequest) (ActionResult, error) {
	planRequest := setup.PlanRequest{Targets: request.Targets}
	if request.Action == ActionInstall || request.Action == ActionSync {
		defaultRequest, err := setup.DefaultPlanRequest(request.Targets)
		if err != nil {
			return ActionResult{}, err
		}
		planRequest = defaultRequest
		planRequest.Components = request.Components
	}
	result, err := lifecycle.RunLifecycle(string(request.Action), request.RootDir, planRequest)
	return ActionResult{
		Action:    request.Action,
		Changed:   result.Changed,
		Unchanged: result.Unchanged,
		Removed:   result.Removed,
		Preserved: result.Preserved,
	}, err
}

// Model is the minimal Bubble Tea runtime state shared by lifecycle screens.
type Model struct {
	Busy         bool
	Presentation common.Presentation
	adapter      Adapter
}

// NewModel creates a runtime model.
func NewModel() Model {
	return Model{}
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// Update starts actions asynchronously and records their eventual result.
func (runtime Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case ActionRequest:
		runtime.Busy = true
		runtime.Presentation = common.Presentation{}
		return runtime, runtime.adapter.Command(message)
	case ActionResultMsg:
		runtime.Busy = false
		if message.Err != nil {
			runtime.Presentation = common.Failure(message.Err)
		} else {
			runtime.Presentation = common.Success(string(message.Result.Action))
		}
	}
	return runtime, nil
}

// View is intentionally empty until a lifecycle screen owns visible content.
func (Model) View() string { return "" }
