// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package embed_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

// vectorTolerance bounds the per-component difference between Go and the
// reference float32 computation, which sums in a different order.
const vectorTolerance = 1e-5

// This fixture compares against vectors produced by the reference Model2Vec
// package (version recorded in the fixture) for the pinned model. It needs the
// writable model directory named by TAKT_E2E_MODEL; missing files are
// downloaded and verified against the pinned manifest.
func TestEmbedMatchesModel2VecReference(t *testing.T) {
	if testing.Short() {
		t.Skip("real model fixture")
	}
	dir := os.Getenv("TAKT_E2E_MODEL")
	if dir == "" {
		t.Skip("real model fixture requires a model directory")
	}
	if err := embed.Pinned().Acquire(context.Background(), http.DefaultClient, dir); err != nil {
		t.Fatalf("acquire model: %v", err)
	}
	model, err := embed.Load(dir)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "reference_vectors.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture struct {
		Cases []struct {
			Text   string    `json:"text"`
			Vector []float32 `json:"vector"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	for _, c := range fixture.Cases {
		got := model.Embed(c.Text)
		if len(got) != len(c.Vector) {
			t.Fatalf("dimension: got %d, reference %d", len(got), len(c.Vector))
		}
		for i := range got {
			if math.Abs(float64(got[i]-c.Vector[i])) > vectorTolerance {
				t.Errorf("text %.40q component %d: got %v, reference %v", c.Text, i, got[i], c.Vector[i])
				break
			}
		}
	}
}
