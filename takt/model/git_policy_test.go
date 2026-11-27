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

package model_test

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
)

func TestGitPermissionRules(t *testing.T) {
	orch := model.GitPermissionRules(model.RoleOrchestrator)
	if len(orch) == 0 || orch[0] != (model.GitPermissionRule{Pattern: "git *", Effect: model.GitEffectAllow}) {
		t.Fatalf("orchestrator first rule = %+v, want git * allow", orch)
	}
	if !slices.Contains(orch, model.GitPermissionRule{Pattern: "git push", Effect: model.GitEffectAsk}) {
		t.Fatalf("orchestrator rules missing gated push: %+v", orch)
	}

	exec := model.GitPermissionRules(model.RoleExecution)
	if len(exec) == 0 || exec[0] != (model.GitPermissionRule{Pattern: "git *", Effect: model.GitEffectDeny}) {
		t.Fatalf("execution first rule = %+v, want git * deny", exec)
	}
	if !slices.Contains(exec, model.GitPermissionRule{Pattern: "git status", Effect: model.GitEffectAllow}) {
		t.Fatalf("execution rules missing read-only status allow: %+v", exec)
	}
	if !slices.Contains(exec, model.GitPermissionRule{Pattern: "git diff *--output*", Effect: model.GitEffectDeny}) {
		t.Fatalf("execution rules missing writing-option deny: %+v", exec)
	}
}

func TestReadOnlyGitSubcommandsAndWritingOptionsReturnsCopies(t *testing.T) {
	subs := model.ReadOnlyGitSubcommands()
	if !slices.Contains(subs, "status") {
		t.Fatalf("ReadOnlyGitSubcommands missing status: %v", subs)
	}
	subs[0] = "mutated"
	if model.ReadOnlyGitSubcommands()[0] == "mutated" {
		t.Fatal("ReadOnlyGitSubcommands returned backing slice instead of a clone")
	}

	opts := model.GitWritingOptions("diff")
	if !slices.Contains(opts, "--output") {
		t.Fatalf("GitWritingOptions(diff) = %v, want --output", opts)
	}
	opts[0] = "mutated"
	if model.GitWritingOptions("diff")[0] == "mutated" {
		t.Fatal("GitWritingOptions returned backing slice instead of a clone")
	}
}
