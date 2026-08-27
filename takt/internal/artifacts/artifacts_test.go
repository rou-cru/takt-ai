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

package artifacts

import "testing"

func TestNormalizeRelPath(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "simple", input: "sub/agent.md", want: "sub/agent.md"},
		{name: "trailing slash", input: "sub/", want: "sub"},
		{name: "nested", input: "a/b/c", want: "a/b/c"},
		{name: "empty", input: "", wantErr: true},
		{name: "parent traversal", input: "../escape", wantErr: true},
		{name: "bare parent", input: "..", wantErr: true},
		{name: "backslash", input: "a\\b", wantErr: true},
		{name: "absolute", input: "/abs/path", wantErr: true},
		{name: "windows drive", input: "C:foo", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeRelPath(c.input)
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %q", c.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", c.input, err)
			}
			if got != c.want {
				t.Fatalf("NormalizeRelPath(%q) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestValidateID(t *testing.T) {
	cases := []struct {
		name    string
		label   string
		id      string
		wantErr bool
	}{
		{name: "valid", label: "Claude", id: "my-agent"},
		{name: "empty", label: "Claude", id: "", wantErr: true},
		{name: "untrimmed", label: "Claude", id: " my-agent ", wantErr: true},
		{name: "slash", label: "Claude", id: "a/b", wantErr: true},
		{name: "dot", label: "Claude", id: ".", wantErr: true},
		{name: "dotdot", label: "Claude", id: "..", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateID(c.label, c.id)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for id %q", c.id)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for id %q: %v", c.id, err)
			}
		})
	}
}

func TestSortUniqueByPath(t *testing.T) {
	type item struct{ p string }
	pathOf := func(i item) string { return i.p }

	t.Run("sorts by copy", func(t *testing.T) {
		out, err := SortUniqueByPath([]item{{p: "b"}, {p: "a"}, {p: "c"}}, pathOf, "Test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 3 || out[0].p != "a" || out[1].p != "b" || out[2].p != "c" {
			t.Fatalf("unexpected order: %+v", out)
		}
	})

	t.Run("duplicate path errors", func(t *testing.T) {
		_, err := SortUniqueByPath([]item{{p: "a"}, {p: "a"}}, pathOf, "Test")
		if err == nil {
			t.Fatal("expected duplicate-path error")
		}
	})

	t.Run("empty", func(t *testing.T) {
		out, err := SortUniqueByPath([]item{}, pathOf, "Test")
		if err != nil || len(out) != 0 {
			t.Fatalf("expected empty result, got %v err %v", out, err)
		}
	})
}

func TestEnsureTrailingNewline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "no newline", in: "hello", want: "hello\n"},
		{name: "has newline", in: "hello\n", want: "hello\n"},
		{name: "empty", in: "", want: "\n"},
		{name: "double newline preserved", in: "a\n\n", want: "a\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EnsureTrailingNewline(c.in); got != c.want {
				t.Fatalf("EnsureTrailingNewline(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
