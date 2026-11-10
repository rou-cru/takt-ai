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
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	// maxTokens is the Model2Vec default sequence length; the pinned config sets none.
	maxTokens = 512
	// medianTokenRunes is the median vocabulary token length in characters; the
	// reference truncates input text to maxTokens times it before tokenizing.
	medianTokenRunes = 6
	// normEpsilon is added to the norm so an empty text maps to the zero vector.
	normEpsilon = 1e-32
	// float32Bytes is the width of one stored weight.
	float32Bytes = 4
	// headerLengthBytes is the width of the safetensors header length prefix.
	headerLengthBytes = 8
	// weightsTensor is the only tensor Model2Vec stores for this model.
	weightsTensor = "embeddings"
	// float32DType is the safetensors name of the 32-bit float type.
	float32DType = "F32"
)

// Model is a loaded static embedding model. It is read-only and safe for concurrent use.
type Model struct {
	tokenizer *tokenizer
	weights   []byte
	dim       int
}

// tensorInfo is one safetensors header entry.
type tensorInfo struct {
	DType       string   `json:"dtype"`
	Shape       []int    `json:"shape"`
	DataOffsets [2]int64 `json:"data_offsets"`
}

// Load reads the model files from dir, rejecting any that do not form a coherent model.
func Load(dir string) (*Model, error) {
	tok, err := loadTokenizer(filepath.Join(dir, tokenizerFile))
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, weightsFile))
	if err != nil {
		return nil, fmt.Errorf("read model weights: %w", err)
	}
	if len(raw) < headerLengthBytes {
		return nil, fmt.Errorf("model weights are truncated")
	}
	headerLength := binary.LittleEndian.Uint64(raw)
	if headerLength > uint64(len(raw)-headerLengthBytes) {
		return nil, fmt.Errorf("model weights header exceeds the file")
	}
	dataStart := headerLengthBytes + int(headerLength)
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw[headerLengthBytes:dataStart], &header); err != nil {
		return nil, fmt.Errorf("parse model weights header: %w", err)
	}
	var info tensorInfo
	if err := json.Unmarshal(header[weightsTensor], &info); err != nil {
		return nil, fmt.Errorf("model weights have no %s tensor: %w", weightsTensor, err)
	}
	data := raw[dataStart:]
	if info.DType != float32DType || len(info.Shape) != 2 || info.Shape[1] <= 0 {
		return nil, fmt.Errorf("model weights must be a rank-2 %s tensor", float32DType)
	}
	size := int64(info.Shape[0]) * int64(info.Shape[1]) * float32Bytes
	if info.DataOffsets[0] != 0 || info.DataOffsets[1] != size || size > int64(len(data)) {
		return nil, fmt.Errorf("model weights tensor does not match its shape")
	}
	if info.Shape[0] != len(tok.vocab) {
		return nil, fmt.Errorf("model has %d vectors for a %d-token vocabulary", info.Shape[0], len(tok.vocab))
	}
	for token, id := range tok.vocab {
		if id < 0 || id >= info.Shape[0] {
			return nil, fmt.Errorf("token %q has id %d outside the model's %d vectors", token, id, info.Shape[0])
		}
	}
	return &Model{tokenizer: tok, weights: data[:size], dim: info.Shape[1]}, nil
}

// Embed returns the unit-length mean of the text's token vectors, or the zero
// vector when no token remains, exactly as the Model2Vec reference does.
func (m *Model) Embed(text string) []float32 {
	runes := []rune(text)
	if len(runes) > maxTokens*medianTokenRunes {
		runes = runes[:maxTokens*medianTokenRunes]
	}
	ids := m.tokenizer.encode(string(runes))
	if len(ids) > maxTokens {
		ids = ids[:maxTokens]
	}
	sum := make([]float64, m.dim)
	count := 0
	for _, id := range ids {
		if id == m.tokenizer.unk {
			continue
		}
		row := m.weights[id*m.dim*float32Bytes:]
		for i := range sum {
			sum[i] += float64(math.Float32frombits(binary.LittleEndian.Uint32(row[i*float32Bytes:])))
		}
		count++
	}
	vector := make([]float32, m.dim)
	if count == 0 {
		return vector
	}
	var squares float64
	for i := range sum {
		mean := float64(float32(sum[i] / float64(count)))
		sum[i] = mean
		squares += mean * mean
	}
	norm := math.Sqrt(squares) + normEpsilon
	for i := range vector {
		vector[i] = float32(sum[i] / norm)
	}
	return vector
}
