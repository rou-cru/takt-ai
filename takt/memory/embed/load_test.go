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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

// writeModel writes a two-vector, one-dimension model whose vocabulary maps the
// unknown token to 0 and the word "a" to wordID.
func writeModel(t *testing.T, wordID int) string {
	t.Helper()
	dir := t.TempDir()
	tokenizer := fmt.Sprintf(`{"added_tokens":[],"normalizer":{"type":"BertNormalizer"},"pre_tokenizer":{"type":"BertPreTokenizer"},`+
		`"model":{"type":"WordPiece","vocab":{"[UNK]":0,"a":%d},"unk_token":"[UNK]","continuing_subword_prefix":"##","max_input_chars_per_word":100}}`, wordID)
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte(tokenizer), 0o600); err != nil {
		t.Fatal(err)
	}
	header, err := json.Marshal(map[string]any{"embeddings": map[string]any{"dtype": "F32", "shape": []int{2, 1}, "data_offsets": []int{0, 8}}})
	if err != nil {
		t.Fatal(err)
	}
	weights := binary.LittleEndian.AppendUint64(nil, uint64(len(header)))
	weights = append(weights, header...)
	weights = append(weights, make([]byte, 8)...)
	if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), weights, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadAcceptsACoherentModel(t *testing.T) {
	model, err := embed.Load(writeModel(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if got := model.Embed("a"); len(got) != 1 {
		t.Fatalf("dimension %d, want 1", len(got))
	}
}

func TestLoadRejectsATokenIDOutsideTheVectors(t *testing.T) {
	for _, id := range []int{-1, 2, 7} {
		if _, err := embed.Load(writeModel(t, id)); err == nil {
			t.Fatalf("token id %d was accepted", id)
		}
	}
}
