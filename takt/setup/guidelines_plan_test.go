// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.
package setup_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestOpenCodePlanDeploysGuidelinesAndRegistersThem(t *testing.T) {
	request, err := setup.DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	plans, _, err := setup.BuildTargetPlans(request)
	if err != nil {
		t.Fatal(err)
	}
	const deployed = ".config/opencode/takt/shared/GUIDELINES.md"
	var guidelines, config []byte
	for _, artifact := range plans[0].Artifacts {
		switch artifact.Path {
		case deployed:
			guidelines = artifact.Content
		case ".config/opencode/opencode.json":
			config = artifact.Content
		}
	}
	if len(guidelines) == 0 {
		t.Fatalf("plan does not deploy %s", deployed)
	}
	var decoded struct {
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal(config, &decoded); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(decoded.Instructions, "~/"+deployed) {
		t.Fatalf("instructions = %v, want %q", decoded.Instructions, "~/"+deployed)
	}
}
