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

package filemerge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
)

// MergeJSONObjects deep-merges overlayJSON into baseJSON, overlay keys
// winning on conflicts; a malformed base is treated as an empty object.
func MergeJSONObjects(baseJSON, overlayJSON []byte) ([]byte, error) {
	base, err := unmarshalJSONObject(baseJSON)
	if err != nil {
		// Real user machines may have a malformed or non-JSON mcp.json (e.g. a file
		// that starts with "a" or contains arbitrary text). The installer backup step
		// already snapshots the existing file before apply, so proceeding with an
		// empty base is safe and far preferable to aborting the whole install.
		base = map[string]any{}
	}

	overlay, err := unmarshalJSONObject(overlayJSON)
	if err != nil {
		return nil, fmt.Errorf("unmarshal overlay json: %w", err)
	}

	merged := mergeObjects(base, overlay)
	encoded, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal merged json: %w", err)
	}

	return append(encoded, '\n'), nil
}

func unmarshalJSONObject(raw []byte) (map[string]any, error) {
	object := map[string]any{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return object, nil
	}

	if err := json.Unmarshal(raw, &object); err == nil {
		return object, nil
	}

	normalized := normalizeJSON(raw)
	if err := json.Unmarshal(normalized, &object); err != nil {
		return nil, err
	}

	return object, nil
}

func normalizeJSON(raw []byte) []byte {
	withoutComments := stripJSONComments(raw)
	return stripTrailingCommas(withoutComments)
}

// jsonString tracks whether a byte scan is inside a JSON string literal.
type jsonString struct{ in, escaped bool }

// consume reports whether ch belongs to a string literal, quotes included, and
// advances the state.
func (s *jsonString) consume(ch byte) bool {
	switch {
	case s.in && s.escaped:
		s.escaped = false
	case s.in && ch == '\\':
		s.escaped = true
	case s.in && ch == '"':
		s.in = false
	case s.in:
	case ch == '"':
		s.in = true
	default:
		return false
	}
	return true
}

func stripJSONComments(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	var str jsonString
	line, block := false, false

	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		var next byte
		if i+1 < len(raw) {
			next = raw[i+1]
		}
		switch {
		case line:
			if ch == '\n' {
				out, line = append(out, ch), false
			}
		case block:
			if ch == '*' && next == '/' {
				block, i = false, i+1
			}
		case str.consume(ch):
			out = append(out, ch)
		case ch == '/' && next == '/':
			line, i = true, i+1
		case ch == '/' && next == '*':
			block, i = true, i+1
		default:
			out = append(out, ch)
		}
	}

	return out
}

func stripTrailingCommas(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	var str jsonString
	for i, ch := range raw {
		if str.consume(ch) || ch != ',' || !closesContainer(raw[i+1:]) {
			out = append(out, ch)
		}
	}
	return out
}

// closesContainer reports whether the first non-whitespace byte of rest closes
// an object or array.
func closesContainer(rest []byte) bool {
	for _, next := range rest {
		switch next {
		case ' ', '\t', '\n', '\r':
			continue
		case '}', ']':
			return true
		}
		return false
	}
	return false
}

// ReplaceSentinel marks an overlay value for atomic replacement instead of
// deep merge: the sentinel's value is used verbatim.
const ReplaceSentinel = "__replace__"

// asSentinel checks if v is a map with exactly one key "__replace__".
// If so, it returns the replacement value and true. Otherwise it returns nil, false.
func asSentinel(v any) (any, bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return nil, false
	}
	if replacement, hasSentinel := m[ReplaceSentinel]; hasSentinel && len(m) == 1 {
		return replacement, true
	}
	return nil, false
}

// arrayUnionKeys names the config keys whose array values are user-extensible
// registries (plugin and skill paths) rather than Takt-owned settings. For
// these keys the overlay must ADD entries instead of replacing the existing
// array: a user's cli.json may already list their own plugins, and wholesale
// replacement would silently drop them on every Takt install or upgrade
// (R4: PR-CFG-1, PR-INS-3, PR-INS-46). Any other array key keeps the
// historical "overlay wins" behavior.
var arrayUnionKeys = map[string]bool{
	"plugins": true,
	"skills":  true,
}

// unionArrays returns the base entries first, then the overlay entries that
// are not already present — a stable, order-preserving union. User entries
// keep their original order and precedence, so re-running an install (or the
// merge itself twice) yields the same array: idempotent by construction.
func unionArrays(base, overlay []any) []any {
	merged := make([]any, 0, safeCap(len(base), len(overlay)))
	merged = append(merged, base...)

	for _, candidate := range overlay {
		if containsArrayEntry(merged, candidate) {
			continue
		}
		merged = append(merged, candidate)
	}

	return merged
}

// containsArrayEntry reports whether candidate already appears in entries.
func containsArrayEntry(entries []any, candidate any) bool {
	return slices.ContainsFunc(entries, func(entry any) bool { return reflect.DeepEqual(entry, candidate) })
}

// safeCap returns a+b as an allocation size hint, or 0 if the addition would
// overflow int (base/overlay lengths come from parsed JSON, so both are
// attacker-influenced). Zero just drops the capacity hint; append still
// grows the allocation as needed, so this never affects correctness.
func safeCap(a, b int) int {
	sum := a + b
	if sum < a {
		return 0
	}
	return sum
}

func mergeObjects(base, overlay map[string]any) map[string]any {
	result := make(map[string]any, safeCap(len(base), len(overlay)))
	for key, value := range base {
		result[key] = value
	}

	for key, overlayValue := range overlay {
		result[key] = mergeValue(result[key], overlayValue, arrayUnionKeys[key])
	}

	return result
}

func mergeValue(baseValue, overlayValue any, union bool) any {
	if replacement, ok := asSentinel(overlayValue); ok {
		return replacement
	}
	baseMap, baseIsMap := baseValue.(map[string]any)
	overlayMap, overlayIsMap := overlayValue.(map[string]any)
	if overlayIsMap && !baseIsMap {
		return mergeObjects(map[string]any{}, overlayMap)
	}
	if baseIsMap && overlayIsMap {
		return mergeObjects(baseMap, overlayMap)
	}
	if union {
		baseArray, baseOK := baseValue.([]any)
		overlayArray, overlayOK := overlayValue.([]any)
		if baseOK && overlayOK {
			return unionArrays(baseArray, overlayArray)
		}
	}
	return overlayValue
}
