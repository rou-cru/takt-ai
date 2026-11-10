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
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

const (
	// float32Width is the size in bytes of one stored weight.
	float32Width = 4
	// unknownToken is the vocabulary entry every test tokenizer uses for unknown words.
	unknownToken = "[UNK]"
)

// tokenizerDoc returns a valid BERT WordPiece tokenizer.json document for vocab.
func tokenizerDoc(vocab map[string]int) map[string]any {
	return map[string]any{
		"added_tokens":  []any{},
		"normalizer":    map[string]any{"type": "BertNormalizer"},
		"pre_tokenizer": map[string]any{"type": "BertPreTokenizer"},
		"model": map[string]any{
			"type": "WordPiece", "vocab": vocab, "unk_token": unknownToken,
			"continuing_subword_prefix": "##", "max_input_chars_per_word": 100,
		},
	}
}

// tokenizerModel returns the "model" object of a tokenizer document.
func tokenizerModel(doc map[string]any) map[string]any { return doc["model"].(map[string]any) }

// tensor returns a safetensors header entry.
func tensor(dtype string, shape []int, end int) map[string]any {
	return map[string]any{"dtype": dtype, "shape": shape, "data_offsets": []int{0, end}}
}

// safetensors builds a model.safetensors file: the header length, the JSON header and the tensor bytes.
func safetensors(t *testing.T, header map[string]any, data []byte) []byte {
	t.Helper()
	raw, err := json.Marshal(header)
	if err != nil {
		t.Fatal(err)
	}
	file := binary.LittleEndian.AppendUint64(nil, uint64(len(raw)))
	return append(append(file, raw...), data...)
}

// writeModelFiles writes the tokenizer and, unless weights is nil, the weights into a new directory.
func writeModelFiles(t *testing.T, tokenizer map[string]any, weights []byte) string {
	t.Helper()
	dir := t.TempDir()
	if tokenizer != nil {
		raw, err := json.Marshal(tokenizer)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if weights != nil {
		if err := os.WriteFile(filepath.Join(dir, "model.safetensors"), weights, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// twoVectorWeights is a valid 2x1 F32 tensor.
func twoVectorWeights(t *testing.T) []byte {
	t.Helper()
	return safetensors(t, map[string]any{"embeddings": tensor("F32", []int{2, 1}, 2*float32Width)}, make([]byte, 2*float32Width))
}

// writeModel writes a two-vector, one-dimension model whose vocabulary maps the
// unknown token to 0 and the word "a" to wordID.
func writeModel(t *testing.T, wordID int) string {
	t.Helper()
	return writeModelFiles(t, tokenizerDoc(map[string]int{unknownToken: 0, "a": wordID}), twoVectorWeights(t))
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

func TestLoadRejectsModelFilesThatAreMissing(t *testing.T) {
	valid := tokenizerDoc(map[string]int{unknownToken: 0, "a": 1})
	for name, dir := range map[string]string{
		"no tokenizer": writeModelFiles(t, nil, twoVectorWeights(t)),
		"no weights":   writeModelFiles(t, valid, nil),
	} {
		if _, err := embed.Load(dir); err == nil {
			t.Fatalf("%s: the model loaded", name)
		}
	}
}

// TestLoadRejectsABrokenTokenizer changes one thing at a time in a valid tokenizer.json.
func TestLoadRejectsABrokenTokenizer(t *testing.T) {
	cases := map[string]func(doc map[string]any){
		"not a normalizer BERT uses": func(doc map[string]any) { doc["normalizer"] = map[string]any{"type": "Lowercase"} },
		"not a pre-tokenizer BERT uses": func(doc map[string]any) {
			doc["pre_tokenizer"] = map[string]any{"type": "Whitespace"}
		},
		"not WordPiece":              func(doc map[string]any) { tokenizerModel(doc)["type"] = "BPE" },
		"unknown token not in vocab": func(doc map[string]any) { tokenizerModel(doc)["unk_token"] = "<missing>" },
		"no word limit":              func(doc map[string]any) { tokenizerModel(doc)["max_input_chars_per_word"] = 0 },
		"special token not in vocab": func(doc map[string]any) {
			doc["added_tokens"] = []any{map[string]any{"content": "[MASK]", "special": true}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			doc := tokenizerDoc(map[string]int{unknownToken: 0, "a": 1})
			mutate(doc)
			if _, err := embed.Load(writeModelFiles(t, doc, twoVectorWeights(t))); err == nil {
				t.Fatal("the model loaded")
			}
		})
	}
}

func TestLoadRejectsATokenizerThatIsNotJSON(t *testing.T) {
	dir := writeModelFiles(t, nil, twoVectorWeights(t))
	if err := os.WriteFile(filepath.Join(dir, "tokenizer.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := embed.Load(dir); err == nil {
		t.Fatal("the model loaded")
	}
}

func TestLoadAcceptsASpecialTokenInTheVocabulary(t *testing.T) {
	doc := tokenizerDoc(map[string]int{unknownToken: 0, "[MASK]": 1})
	doc["added_tokens"] = []any{
		map[string]any{"content": "[MASK]", "special": true},
		map[string]any{"content": "extra", "special": false},
	}
	if _, err := embed.Load(writeModelFiles(t, doc, twoVectorWeights(t))); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejectsBrokenWeights(t *testing.T) {
	oversizedHeader := binary.LittleEndian.AppendUint64(nil, 1000)
	notJSONHeader := append(binary.LittleEndian.AppendUint64(nil, 8), "not json"...)
	cases := map[string][]byte{
		"truncated before the header length": {1, 2, 3},
		"header longer than the file":        append(oversizedHeader, "{}"...),
		"header is not JSON":                 notJSONHeader,
		"no embeddings tensor":               safetensors(t, map[string]any{}, nil),
		"not float32": safetensors(t, map[string]any{"embeddings": tensor("F16", []int{2, 1}, 2*float32Width)},
			make([]byte, 2*float32Width)),
		"not rank two": safetensors(t, map[string]any{"embeddings": tensor("F32", []int{2}, 2*float32Width)},
			make([]byte, 2*float32Width)),
		"offsets do not match the shape": safetensors(t, map[string]any{"embeddings": tensor("F32", []int{2, 1}, float32Width)},
			make([]byte, 2*float32Width)),
		"data shorter than the shape": safetensors(t, map[string]any{"embeddings": tensor("F32", []int{2, 1}, 2*float32Width)},
			make([]byte, float32Width)),
		"more vectors than the vocabulary": safetensors(t, map[string]any{"embeddings": tensor("F32", []int{3, 1}, 3*float32Width)},
			make([]byte, 3*float32Width)),
	}
	for name, weights := range cases {
		t.Run(name, func(t *testing.T) {
			doc := tokenizerDoc(map[string]int{unknownToken: 0, "a": 1})
			if _, err := embed.Load(writeModelFiles(t, doc, weights)); err == nil {
				t.Fatal("the model loaded")
			}
		})
	}
}
