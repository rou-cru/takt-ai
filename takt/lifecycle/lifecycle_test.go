package lifecycle

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestRunLifecycleRejectsUnsupportedAction(t *testing.T) {
	request := setup.PlanRequest{Targets: []model.AgentID{model.AgentOpenCode}}
	result, err := RunLifecycle("remove", t.TempDir(), request)
	if err == nil || !strings.Contains(err.Error(), `unsupported action "remove"`) {
		t.Fatalf("RunLifecycle() error = %v, want unsupported action", err)
	}
	if len(result.Changed)+len(result.Unchanged)+len(result.Removed)+len(result.Preserved) != 0 {
		t.Fatalf("RunLifecycle() result = %+v, want zero value", result)
	}
}
