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
package embed

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// Component types of the pinned tokenizer.json this code implements.
	normalizerType   = "BertNormalizer"
	preTokenizerType = "BertPreTokenizer"
	wordPieceType    = "WordPiece"
	// replacementRune is dropped by BERT text cleaning together with NUL.
	replacementRune = '�'
)

// chineseRanges are the code point ranges BERT pads with spaces so each ideograph is a word.
var chineseRanges = unicode.RangeTable{R32: []unicode.Range32{
	{Lo: 0x3400, Hi: 0x4DBF, Stride: 1},
	{Lo: 0x4E00, Hi: 0x9FFF, Stride: 1},
	{Lo: 0xF900, Hi: 0xFAFF, Stride: 1},
	{Lo: 0x20000, Hi: 0x2A6DF, Stride: 1},
	{Lo: 0x2A700, Hi: 0x2B73F, Stride: 1},
	{Lo: 0x2B740, Hi: 0x2B81F, Stride: 1},
	{Lo: 0x2B920, Hi: 0x2CEAF, Stride: 1},
	{Lo: 0x2F800, Hi: 0x2FA1F, Stride: 1},
}}

// tokenizer is the BERT WordPiece tokenizer described by a tokenizer.json.
type tokenizer struct {
	vocab         map[string]int
	unk           int
	prefix        string
	maxWordRunes  int
	specialTokens []string
}

// tokenizerFileJSON holds the tokenizer.json fields the implementation honors.
type tokenizerFileJSON struct {
	AddedTokens []struct {
		Content string `json:"content"`
		Special bool   `json:"special"`
	} `json:"added_tokens"`
	Normalizer struct {
		Type string `json:"type"`
	} `json:"normalizer"`
	PreTokenizer struct {
		Type string `json:"type"`
	} `json:"pre_tokenizer"`
	Model struct {
		Type         string         `json:"type"`
		Vocab        map[string]int `json:"vocab"`
		UnkToken     string         `json:"unk_token"`
		Prefix       string         `json:"continuing_subword_prefix"`
		MaxWordRunes int            `json:"max_input_chars_per_word"`
	} `json:"model"`
}

// loadTokenizer parses path and rejects any configuration other than BERT WordPiece.
func loadTokenizer(path string) (*tokenizer, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model tokenizer: %w", err)
	}
	var file tokenizerFileJSON
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("parse model tokenizer: %w", err)
	}
	if file.Normalizer.Type != normalizerType || file.PreTokenizer.Type != preTokenizerType || file.Model.Type != wordPieceType {
		return nil, fmt.Errorf("model tokenizer is not a BERT WordPiece tokenizer")
	}
	unk, known := file.Model.Vocab[file.Model.UnkToken]
	if !known || file.Model.MaxWordRunes <= 0 {
		return nil, fmt.Errorf("model tokenizer has no usable unknown token or word limit")
	}
	tok := &tokenizer{vocab: file.Model.Vocab, unk: unk, prefix: file.Model.Prefix, maxWordRunes: file.Model.MaxWordRunes}
	for _, added := range file.AddedTokens {
		if _, known := file.Model.Vocab[added.Content]; added.Special && !known {
			return nil, fmt.Errorf("special token %q is not in the vocabulary", added.Content)
		}
		if added.Special {
			tok.specialTokens = append(tok.specialTokens, added.Content)
		}
	}
	return tok, nil
}

// encode returns token ids without adding special tokens. Special-token text
// in the input maps to its id before any normalization, as in the reference.
func (t *tokenizer) encode(text string) []int {
	var ids []int
	for {
		at, special := t.nextSpecial(text)
		if at < 0 {
			return append(ids, t.encodeText(text)...)
		}
		ids = append(ids, t.encodeText(text[:at])...)
		ids = append(ids, t.vocab[special])
		text = text[at+len(special):]
	}
}

// nextSpecial finds the leftmost, then longest, special token in text.
func (t *tokenizer) nextSpecial(text string) (int, string) {
	best, found := -1, ""
	for _, special := range t.specialTokens {
		at := strings.Index(text, special)
		if at >= 0 && (best < 0 || at < best || (at == best && len(special) > len(found))) {
			best, found = at, special
		}
	}
	return best, found
}

// encodeText normalizes, pre-tokenizes, and WordPiece-encodes text free of special tokens.
func (t *tokenizer) encodeText(text string) []int {
	var ids []int
	for _, word := range preTokenize(normalize(text)) {
		ids = append(ids, t.wordPiece(word)...)
	}
	return ids
}

// normalize cleans control characters, pads ideographs, strips accents, and lowercases.
func normalize(text string) string {
	var cleaned strings.Builder
	for _, r := range text {
		switch {
		case r == 0 || r == replacementRune || isControl(r):
			continue
		case unicode.IsSpace(r):
			cleaned.WriteRune(' ')
		case unicode.Is(&chineseRanges, r):
			cleaned.WriteString(" " + string(r) + " ")
		default:
			cleaned.WriteRune(r)
		}
	}
	var out strings.Builder
	for _, r := range norm.NFD.String(cleaned.String()) {
		if !unicode.Is(unicode.Mn, r) {
			out.WriteRune(unicode.ToLower(r))
		}
	}
	return out.String()
}

// isControl matches BERT cleaning: control, format, and private-use characters other than tab, newline, and carriage return.
func isControl(r rune) bool {
	return r != '\t' && r != '\n' && r != '\r' && unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co)
}

// preTokenize splits on whitespace and isolates each punctuation character.
func preTokenize(text string) []string {
	var words []string
	var word []rune
	flush := func() {
		if len(word) > 0 {
			words = append(words, string(word))
			word = word[:0]
		}
	}
	for _, r := range text {
		switch {
		case unicode.IsSpace(r):
			flush()
		case isPunctuation(r):
			flush()
			words = append(words, string(r))
		default:
			word = append(word, r)
		}
	}
	flush()
	return words
}

// isPunctuation is BERT's definition: all ASCII non-alphanumerics plus Unicode category P.
func isPunctuation(r rune) bool {
	return (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) || unicode.IsPunct(r)
}

// wordPiece greedily matches the longest vocabulary pieces; a word that cannot be
// fully matched, or that is too long, becomes the single unknown token.
func (t *tokenizer) wordPiece(word string) []int {
	runes := []rune(word)
	if len(runes) > t.maxWordRunes {
		return []int{t.unk}
	}
	var ids []int
	for start := 0; start < len(runes); {
		end, id := len(runes), -1
		for ; end > start; end-- {
			piece := string(runes[start:end])
			if start > 0 {
				piece = t.prefix + piece
			}
			if found, ok := t.vocab[piece]; ok {
				id = found
				break
			}
		}
		if id < 0 {
			return []int{t.unk}
		}
		ids = append(ids, id)
		start = end
	}
	return ids
}
