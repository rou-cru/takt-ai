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

package filemerge_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

func TestMergeJSONObjectsKeepsUserInstructions(t *testing.T) {
	merged, err := filemerge.MergeJSONObjects(
		[]byte(`{"instructions": ["~/rules/mine.md"]}`),
		[]byte(`{"instructions": ["~/.config/opencode/takt/shared/GUIDELINES.md"]}`),
	)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}
	var got struct {
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := []string{"~/rules/mine.md", "~/.config/opencode/takt/shared/GUIDELINES.md"}
	if !reflect.DeepEqual(got.Instructions, want) {
		t.Fatalf("instructions = %v, want %v", got.Instructions, want)
	}
}
