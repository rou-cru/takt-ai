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
