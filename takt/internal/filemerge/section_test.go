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
	"strings"
	"testing"
)

func TestInjectMarkdownSection_EmptyFile(t *testing.T) {
	result := InjectMarkdownSection("", "sdd", "## SDD Config\nSome content here.\n")

	want := "<!-- takt-ai:sdd -->\n## SDD Config\nSome content here.\n<!-- /takt-ai:sdd -->\n"
	if result != want {
		t.Fatalf("empty file inject:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_AppendToExistingContent(t *testing.T) {
	existing := "# My Config\n\nSome existing content.\n"
	result := InjectMarkdownSection(existing, "persona", "You are a senior architect.\n")

	want := "# My Config\n\nSome existing content.\n\n<!-- takt-ai:persona -->\nYou are a senior architect.\n<!-- /takt-ai:persona -->\n"
	if result != want {
		t.Fatalf("append to existing:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_UpdateExistingSection(t *testing.T) {
	existing := "# Config\n\n<!-- takt-ai:sdd -->\nOld SDD content.\n<!-- /takt-ai:sdd -->\n\nOther stuff.\n"
	result := InjectMarkdownSection(existing, "sdd", "New SDD content.\n")

	want := "# Config\n\n<!-- takt-ai:sdd -->\nNew SDD content.\n<!-- /takt-ai:sdd -->\n\nOther stuff.\n"
	if result != want {
		t.Fatalf("update existing section:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_MultipleSectionsOnlyTargetedOneUpdated(t *testing.T) {
	existing := "# Config\n\n<!-- takt-ai:persona -->\nPersona content.\n<!-- /takt-ai:persona -->\n\n<!-- takt-ai:sdd -->\nOld SDD.\n<!-- /takt-ai:sdd -->\n\n<!-- takt-ai:skills -->\nSkills content.\n<!-- /takt-ai:skills -->\n"

	result := InjectMarkdownSection(existing, "sdd", "Updated SDD.\n")

	// persona and skills should be unchanged
	want := "# Config\n\n<!-- takt-ai:persona -->\nPersona content.\n<!-- /takt-ai:persona -->\n\n<!-- takt-ai:sdd -->\nUpdated SDD.\n<!-- /takt-ai:sdd -->\n\n<!-- takt-ai:skills -->\nSkills content.\n<!-- /takt-ai:skills -->\n"
	if result != want {
		t.Fatalf("multiple sections:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_PreserveUserContentBeforeAndAfter(t *testing.T) {
	existing := "# User's custom intro\n\nHand-written notes.\n\n<!-- takt-ai:persona -->\nAuto persona.\n<!-- /takt-ai:persona -->\n\n# User's custom footer\n\nMore hand-written content.\n"

	result := InjectMarkdownSection(existing, "persona", "Updated persona.\n")

	want := "# User's custom intro\n\nHand-written notes.\n\n<!-- takt-ai:persona -->\nUpdated persona.\n<!-- /takt-ai:persona -->\n\n# User's custom footer\n\nMore hand-written content.\n"
	if result != want {
		t.Fatalf("preserve user content:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_MalformedMarkersTreatedAsNotFound(t *testing.T) {
	// Only opening marker, no closing marker — treat as not found, append.
	existing := "# Config\n\n<!-- takt-ai:sdd -->\nOrphaned content.\n"
	result := InjectMarkdownSection(existing, "sdd", "New SDD content.\n")

	// Should append since closing marker is missing.
	if result == existing {
		t.Fatalf("malformed markers: expected content to be appended, but got unchanged result")
	}

	// Result should contain the new properly-formed section.
	wantOpen := "<!-- takt-ai:sdd -->\nNew SDD content.\n<!-- /takt-ai:sdd -->\n"
	if !strings.Contains(result, wantOpen) {
		t.Fatalf("malformed markers: result should contain proper section:\ngot: %q", result)
	}
}

func TestInjectMarkdownSection_CloseBeforeOpenTreatedAsNotFound(t *testing.T) {
	// Closing marker appears before opening — treat as not found.
	existing := "<!-- /takt-ai:sdd -->\nSome content.\n<!-- takt-ai:sdd -->\n"
	result := InjectMarkdownSection(existing, "sdd", "New content.\n")

	// Should append the section, not replace.
	wantSuffix := "<!-- takt-ai:sdd -->\nNew content.\n<!-- /takt-ai:sdd -->\n"
	if !strings.HasSuffix(result, wantSuffix) {
		t.Fatalf("close-before-open: expected appended section:\ngot: %q\nwant suffix: %q", result, wantSuffix)
	}
}

// TestInjectMarkdownSection_OrphanRepair covers the four scenarios from issue #301:
// infinite block accumulation caused by orphan closing markers being mishandled.
func TestInjectMarkdownSection_OrphanRepair(t *testing.T) {
	const sid = "engram-protocol"
	open := "<!-- takt-ai:" + sid + " -->"
	close := "<!-- /takt-ai:" + sid + " -->"
	newContent := "Engram protocol content.\n"

	oneBlock := open + "\n" + newContent + close + "\n"

	tests := []struct {
		name     string
		existing string
		// wantOnce is the result after the FIRST sync.
		// wantTwice is the result after running sync a SECOND time on wantOnce.
		// Both must equal oneBlock (possibly with surrounding user content).
		checkOnce  func(t *testing.T, result string)
		checkTwice func(t *testing.T, result string)
	}{
		{
			// Scenario 1: clean file with exactly one well-formed block.
			// Sync must replace content in-place — file must not grow.
			name:     "clean file with one block — idempotent replace",
			existing: oneBlock,
			checkOnce: func(t *testing.T, result string) {
				if result != oneBlock {
					t.Fatalf("clean-block: expected unchanged block\ngot:  %q\nwant: %q", result, oneBlock)
				}
			},
			checkTwice: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("clean-block idempotent: expected 1 open marker, got %d\n%q", count, result)
				}
			},
		},
		{
			// Scenario 2: orphan opener (opening marker exists but closing marker
			// was deleted). Sync must repair — not append — resulting in exactly
			// one complete block.
			name:     "orphan opener (no closing marker) — repaired to one block",
			existing: "# Preamble\n\n" + open + "\nOld content.\n",
			checkOnce: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("orphan-opener: expected exactly 1 open marker after repair, got %d\n%q", count, result)
				}
				if !strings.Contains(result, close) {
					t.Fatalf("orphan-opener: result must contain the close marker\n%q", result)
				}
				if !strings.Contains(result, newContent) {
					t.Fatalf("orphan-opener: result must contain new content\n%q", result)
				}
				if !strings.Contains(result, "# Preamble") {
					t.Fatalf("orphan-opener: user preamble must be preserved\n%q", result)
				}
			},
			checkTwice: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("orphan-opener idempotent: expected 1 open marker, got %d\n%q", count, result)
				}
			},
		},
		{
			// Scenario 3: file already has two blocks — one orphan opener from a
			// previous buggy sync run PLUS one full block appended afterwards.
			// A single sync must collapse to one block.
			name: "two blocks (orphan opener + appended block) — collapsed to one",
			existing: "# Preamble\n\n" + open + "\nOld content.\n" +
				"\n" + open + "\n" + newContent + close + "\n",
			checkOnce: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("two-blocks: expected exactly 1 open marker after collapse, got %d\n%q", count, result)
				}
				if !strings.Contains(result, close) {
					t.Fatalf("two-blocks: result must contain the close marker\n%q", result)
				}
			},
			checkTwice: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("two-blocks idempotent: expected 1 open marker, got %d\n%q", count, result)
				}
			},
		},
		{
			// Scenario 4: file has NO markers at all. First sync must add exactly
			// one complete block without duplicating anything.
			name:     "no markers — first sync adds exactly one block",
			existing: "# Clean file\n\nUser content only.\n",
			checkOnce: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("no-markers: expected 1 open marker after first sync, got %d\n%q", count, result)
				}
				if !strings.Contains(result, close) {
					t.Fatalf("no-markers: result must contain the close marker\n%q", result)
				}
				if !strings.Contains(result, "User content only.") {
					t.Fatalf("no-markers: user content must be preserved\n%q", result)
				}
			},
			checkTwice: func(t *testing.T, result string) {
				count := strings.Count(result, open)
				if count != 1 {
					t.Fatalf("no-markers idempotent: expected 1 open marker, got %d\n%q", count, result)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			once := InjectMarkdownSection(tc.existing, sid, newContent)
			tc.checkOnce(t, once)

			twice := InjectMarkdownSection(once, sid, newContent)
			tc.checkTwice(t, twice)
		})
	}
}

func TestInjectMarkdownSection_EmptyContentRemovesSection(t *testing.T) {
	existing := "# Config\n\n<!-- takt-ai:sdd -->\nSDD content here.\n<!-- /takt-ai:sdd -->\n\nOther stuff.\n"
	result := InjectMarkdownSection(existing, "sdd", "")

	want := "# Config\n\nOther stuff.\n"
	if result != want {
		t.Fatalf("empty content removes section:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_EmptyContentOnMissingSectionNoOp(t *testing.T) {
	existing := "# Config\n\nSome content.\n"
	result := InjectMarkdownSection(existing, "sdd", "")

	if result != existing {
		t.Fatalf("empty content on missing section should be no-op:\ngot:  %q\nwant: %q", result, existing)
	}
}

func TestInjectMarkdownSection_ContentWithoutTrailingNewline(t *testing.T) {
	result := InjectMarkdownSection("", "test", "no trailing newline")

	want := "<!-- takt-ai:test -->\nno trailing newline\n<!-- /takt-ai:test -->\n"
	if result != want {
		t.Fatalf("content without trailing newline:\ngot:  %q\nwant: %q", result, want)
	}
}

func TestInjectMarkdownSection_ExistingWithoutTrailingNewline(t *testing.T) {
	existing := "# Title"
	result := InjectMarkdownSection(existing, "test", "Content.\n")

	want := "# Title\n\n<!-- takt-ai:test -->\nContent.\n<!-- /takt-ai:test -->\n"
	if result != want {
		t.Fatalf("existing without trailing newline:\ngot:  %q\nwant: %q", result, want)
	}
}
