package shared

import (
	"fmt"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// mustLoadOrchestrator finds the catalog's orchestrator agent, failing fast on broken builds.
func mustLoadOrchestrator() catalog.AgentDefinition {
	pkgs, err := catalog.LoadPackages()
	if err != nil {
		panic(err)
	}
	for _, agent := range pkgs.Agents {
		if agent.Role == model.RoleOrchestrator {
			return agent
		}
	}
	panic(fmt.Errorf("catalog: no agent with role %q", model.RoleOrchestrator))
}

// orchestratorDefinition is the single shared orchestrator identity for all adapters.
var orchestratorDefinition = mustLoadOrchestrator()

// OrchestratorID names the selectable top-level agent across harnesses.
var OrchestratorID = orchestratorDefinition.ID
