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
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestInstalledConfigSaveLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	request.Components = []string{"theme", "opencode-takt-logo"}
	request.OpenCode = setup.OpenCodePlanOptions{Model: "custom/model"}
	request.OpenCodeModelOverrides = map[string]model.ModelAssignment{"one": {Model: "custom/override"}}

	if err := setup.SaveInstalledConfig(root, request); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	loaded, err := setup.LoadInstalledConfig(root)
	if err != nil {
		t.Fatalf("LoadInstalledConfig() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, request) {
		t.Fatalf("loaded = %+v, want %+v", loaded, request)
	}
}

func TestLoadInstalledConfigMissingIsNotExist(t *testing.T) {
	_, err := setup.LoadInstalledConfig(t.TempDir())
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want wrapping os.ErrNotExist", err)
	}
}
