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
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

const (
	// sequenceLimit is the number of tokens the model keeps from a text (the Model2Vec default sequence length).
	sequenceLimit = 512
	// runeLimit is more characters than the model reads from a text: the sequence limit times the median token length.
	runeLimit = sequenceLimit * 6
)

// oneHotVocab lists the tokens of the test model; a token's id is its position.
var oneHotVocab = []string{unknownToken, "[M", "[MASK]", "a", "b", "c", "ab", "##d", "i", "中", ","}

// oneHotModel loads a model whose vector for token n is the n-th unit vector, so the
// non-zero components of Embed(text) are exactly the tokens the text produced.
func oneHotModel(t *testing.T, mutate func(doc map[string]any)) *embed.Model {
	t.Helper()
	ids := map[string]int{}
	for id, token := range oneHotVocab {
		ids[token] = id
	}
	doc := tokenizerDoc(ids)
	doc["added_tokens"] = []any{
		map[string]any{"content": "[M", "special": true},
		map[string]any{"content": "[MASK]", "special": true},
	}
	if mutate != nil {
		mutate(doc)
	}
	size := len(oneHotVocab)
	data := make([]byte, 0, size*size*float32Width)
	for row := range size {
		for column := range size {
			value := float32(0)
			if row == column {
				value = 1
			}
			data = binary.LittleEndian.AppendUint32(data, math.Float32bits(value))
		}
	}
	weights := safetensors(t, map[string]any{"embeddings": tensor("F32", []int{size, size}, len(data))}, data)
	model, err := embed.Load(writeModelFiles(t, doc, weights))
	if err != nil {
		t.Fatal(err)
	}
	return model
}

// tokensOf returns the vocabulary entries text produced, in vocabulary order.
func tokensOf(t *testing.T, model *embed.Model, text string) []string {
	t.Helper()
	vector := model.Embed(text)
	if len(vector) != len(oneHotVocab) {
		t.Fatalf("dimension %d, want %d", len(vector), len(oneHotVocab))
	}
	var tokens []string
	for id, component := range vector {
		if component != 0 {
			tokens = append(tokens, oneHotVocab[id])
		}
	}
	return tokens
}

func TestEmbedTokenizesLikeBERT(t *testing.T) {
	model := oneHotModel(t, nil)
	cases := []struct {
		name, text string
		want       []string
	}{
		{"empty text has no tokens", "", nil},
		{"a special token between words", "a [MASK] b", []string{"[MASK]", "a", "b"}},
		{"the longest special token wins at the same position", "[MASK]", []string{"[MASK]"}},
		{"the shorter special token still matches alone", "[Mc", []string{"[M", "c"}},
		{"special tokens are matched before lowercasing", "A [mask]", []string{"a"}},
		{"letters are lowercased and accents stripped", "Á B", []string{"a", "b"}},
		{"a dotted capital I becomes i", "İ", []string{"i"}},
		{"NUL is dropped", "a\x00b", []string{"ab"}},
		{"a format character is dropped", "a\u200bb", []string{"ab"}},
		{"the replacement character is dropped", "a\ufffdb", []string{"ab"}},
		{"a tab separates words", "a\tb", []string{"a", "b"}},
		{"a newline separates words", "a\nb", []string{"a", "b"}},
		{"a no-break space separates words", "a\u00a0b", []string{"a", "b"}},
		{"an ideograph is a word of its own", "a中b", []string{"a", "b", "中"}},
		{"punctuation is isolated", "a,b", []string{"a", "b", ","}},
		{"a word continues with a prefixed piece", "cd", []string{"c", "##d"}},
		{"a word that cannot be matched is the unknown token", "cx", nil},
		{"the unknown token adds nothing to its neighbors", "c cx", []string{"c"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tokensOf(t, model, c.text); !slices.Equal(got, c.want) {
				t.Fatalf("Embed(%q) produced %q, want %q", c.text, got, c.want)
			}
		})
	}
}

func TestEmbedTreatsAWordOverTheLimitAsUnknown(t *testing.T) {
	model := oneHotModel(t, func(doc map[string]any) { tokenizerModel(doc)["max_input_chars_per_word"] = 3 })
	if got := tokensOf(t, model, "ab abab"); !slices.Equal(got, []string{"ab"}) {
		t.Fatalf("got %q, want only the word within the limit", got)
	}
}

func TestEmbedKeepsOnlyTheFirstTokensOfALongText(t *testing.T) {
	model := oneHotModel(t, nil)
	cases := []struct {
		name, text string
		want       []string
	}{
		{"the last token that fits is kept", strings.Repeat("a ", sequenceLimit-1) + "b", []string{"a", "b"}},
		{"the first token past the limit is dropped", strings.Repeat("a ", sequenceLimit) + "b", []string{"a"}},
		{"characters past the limit are not read", strings.Repeat("a ", runeLimit) + "b", []string{"a"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tokensOf(t, model, c.text); !slices.Equal(got, c.want) {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
