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
	"errors"
	"fmt"
	"reflect"
	"slices"
)

// MergeJSONObjects deep-merges overlayJSON into baseJSON, overlay keys
// winning on conflicts. Empty or whitespace-only input reads as an empty
// object; comments and trailing commas are accepted. Input that does not parse
// as a JSON object, including a non-object root such as an array or null, is
// rejected with an *InvalidJSONError and is never coerced to an empty object,
// so no caller can mistake unreadable user content for content safe to replace.
func MergeJSONObjects(baseJSON, overlayJSON []byte) ([]byte, error) {
	base, err := ParseJSONObject(baseJSON)
	if err != nil {
		return nil, fmt.Errorf("read base json: %w", err)
	}

	overlay, err := ParseJSONObject(overlayJSON)
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

// InvalidJSONError locates why input cannot be read as a JSON object. The
// position refers to the original input bytes, comments included, and the
// error never carries the input's content.
type InvalidJSONError struct {
	// Line is the one-based line where reading failed.
	Line int
	// Column is the one-based byte column of the failure within Line.
	Column int
	// Reason is the parser's explanation, or why the root is not an object.
	Reason string
}

// Error reports the position and reason of the failure.
func (e *InvalidJSONError) Error() string {
	return fmt.Sprintf("invalid JSON at line %d, column %d: %s", e.Line, e.Column, e.Reason)
}

// ParseJSONObject reads raw as a JSON object under MergeJSONObjects' rules,
// returning an *InvalidJSONError when it is not one.
func ParseJSONObject(raw []byte) (map[string]any, error) {
	// NormalizeJSON keeps every byte offset, so parser positions map to raw.
	normalized := NormalizeJSON(raw)
	if len(bytes.TrimSpace(normalized)) == 0 {
		return map[string]any{}, nil
	}
	var root any
	if err := json.Unmarshal(normalized, &root); err != nil {
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, invalidJSONAt(raw, syntax.Offset-1, syntax.Error())
		}
		return nil, invalidJSONAt(raw, 0, err.Error())
	}
	object, isObject := root.(map[string]any)
	if !isObject {
		start := len(raw) - len(bytes.TrimLeft(normalized, " \t\r\n"))
		return nil, invalidJSONAt(raw, int64(start), fmt.Sprintf("root is %s, not an object", jsonKind(root)))
	}
	return object, nil
}

// invalidJSONAt builds an InvalidJSONError for offset, clamped into raw.
func invalidJSONAt(raw []byte, offset int64, reason string) *InvalidJSONError {
	offset = max(0, min(offset, int64(len(raw))-1))
	before := raw[:offset]
	line := bytes.Count(before, []byte{'\n'}) + 1
	column := len(before) - (bytes.LastIndexByte(before, '\n') + 1) + 1
	return &InvalidJSONError{Line: line, Column: column, Reason: reason}
}

// jsonKind names the JSON type of a decoded root value.
func jsonKind(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	default:
		return "a number"
	}
}

// NormalizeJSON blanks comments and trailing commas with spaces, keeping line
// breaks and every byte offset, so a user-edited JSONC file parses with
// encoding/json and parse positions still refer to the original bytes.
func NormalizeJSON(raw []byte) []byte {
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
			} else {
				out = append(out, ' ')
			}
		case block:
			if ch == '*' && next == '/' {
				out, block, i = append(out, ' ', ' '), false, i+1
			} else {
				out = append(out, blank(ch))
			}
		case str.consume(ch):
			out = append(out, ch)
		case ch == '/' && next == '/':
			out, line, i = append(out, ' ', ' '), true, i+1
		case ch == '/' && next == '*':
			out, block, i = append(out, ' ', ' '), true, i+1
		default:
			out = append(out, ch)
		}
	}

	return out
}

// blank replaces a removed byte with a space, keeping line breaks so line
// numbers survive normalization.
func blank(ch byte) byte {
	if ch == '\n' || ch == '\r' {
		return ch
	}
	return ' '
}

func stripTrailingCommas(raw []byte) []byte {
	out := make([]byte, 0, len(raw))
	var str jsonString
	for i, ch := range raw {
		if str.consume(ch) || ch != ',' || !closesContainer(raw[i+1:]) {
			out = append(out, ch)
		} else {
			out = append(out, ' ')
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
// registries (plugin, skill and instruction paths) rather than Takt-owned settings. For
// these keys the overlay must ADD entries instead of replacing the existing
// array: a user's cli.json may already list their own plugins, and wholesale
// replacement would silently drop them on every Takt install or upgrade
// (PR-CFG-1, PR-INS-3, PR-INS-46). Any other array key keeps the
// historical "overlay wins" behavior.
var arrayUnionKeys = map[string]bool{
	"plugins":      true,
	"skills":       true,
	"instructions": true,
}

// unionArrays returns the base entries first, then the overlay entries not
// already present — a stable, order-preserving union. User entries keep their
// order and precedence, so re-running an install (or the merge twice) yields
// the same array: idempotent by construction.
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

// safeCap returns a+b as an allocation size hint, or 0 on int overflow
// (lengths come from parsed JSON, so they are attacker-influenced). Zero only
// drops the capacity hint; append still grows the allocation, so correctness
// is unaffected.
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
