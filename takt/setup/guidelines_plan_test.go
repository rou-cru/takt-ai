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
	"bytes"
	"encoding/json"
	"io/fs"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
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
	source, err := fs.ReadFile(catalog.AssetFS(), catalog.GuidelinesPath)
	if err != nil {
		t.Fatal(err)
	}
	deployed := opencode.DeployPath(catalog.GuidelinesPath)
	var guidelines []byte
	var instructions []string
	for _, artifact := range plans[0].Artifacts {
		if artifact.Path == deployed {
			guidelines = artifact.Content
		}
		var decoded struct {
			Instructions []string `json:"instructions"`
		}
		if json.Unmarshal(artifact.Content, &decoded) == nil {
			instructions = append(instructions, decoded.Instructions...)
		}
	}
	if !bytes.Equal(guidelines, source) {
		t.Fatalf("plan deploys %s = %q, want the catalog guidelines", deployed, guidelines)
	}
	if !slices.Contains(instructions, "~/"+deployed) {
		t.Fatalf("instructions = %v, want %q", instructions, "~/"+deployed)
	}
}
