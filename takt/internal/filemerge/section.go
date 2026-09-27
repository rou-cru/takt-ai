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
)

const (
	// markerPrefix is the opening HTML comment prefix for takt-ai sections.
	markerPrefix = "<!-- takt-ai:"
	// markerSuffix is the closing HTML comment suffix for takt-ai sections.
	markerSuffix = " -->"
	// closePrefix is the opening HTML comment prefix for section closers.
	closePrefix = "<!-- /takt-ai:"
)

// openMarker returns the opening marker for a section ID.
func openMarker(sectionID string) string {
	return markerPrefix + sectionID + markerSuffix
}

// closeMarker returns the closing marker for a section ID.
func closeMarker(sectionID string) string {
	return closePrefix + sectionID + markerSuffix
}

// stripOrphanMarkers removes unpaired opening or closing markers for the
// given sectionID. Prevents duplicate blocks on re-sync (issue #301).
func stripOrphanMarkers(content, open, close string) string {
	for {
		openIdx := strings.Index(content, open)
		closeIdx := strings.Index(content, close)

		switch {
		case openIdx < 0 && closeIdx < 0:
			// Neither marker present — nothing to strip.
			return content

		case closeIdx >= 0 && (openIdx < 0 || closeIdx < openIdx):
			// Orphan closer: no opener before it. Remove this closer and loop;
			// the opener may then form a valid pair or become an orphan opener
			// that gets cleaned up in the next iteration.
			content = content[:closeIdx] + content[closeIdx+len(close):]

		case openIdx >= 0 && closeIdx < 0:
			// Orphan opener: opening marker exists but no closer follows.
			// Remove the orphan opener so the caller appends a fresh block.
			content = content[:openIdx] + content[openIdx+len(open):]

		default:
			// Both markers present and opener precedes closer — valid pair found.
			// Stop; the main injection logic will handle the replacement.
			return content
		}
	}
}

// InjectMarkdownSection replaces or appends the section marked with HTML
// comments <!-- takt-ai:SECTION_ID --> in a markdown file.
func InjectMarkdownSection(existing, sectionID, content string) string {
	open := openMarker(sectionID)
	close := closeMarker(sectionID)

	// Repair any orphan markers left by previous (buggy) sync runs before
	// attempting replacement. This is the core fix for issue #301.
	existing = stripOrphanMarkers(existing, open, close)

	openIdx := strings.Index(existing, open)
	closeIdx := strings.Index(existing, close)

	// If both markers are found and in the correct order, replace the section.
	if openIdx >= 0 && closeIdx > openIdx {
		before := existing[:openIdx]
		after := existing[closeIdx+len(close):]
		if content == "" {
			return withoutSection(before, after)
		}
		return before + sectionBlock(open, content, close) + after
	}

	// If content is empty and section doesn't exist, return existing unchanged.
	if content == "" {
		return existing
	}

	// Section not found — append at end.
	separator := ""
	if existing != "" {
		if !strings.HasSuffix(existing, "\n") {
			separator = "\n"
		}
		separator += "\n"
	}
	return existing + separator + sectionBlock(open, content, close) + "\n"
}

// sectionBlock is the marked section: the opening marker, the content on its
// own lines, and the closing marker.
func sectionBlock(open, content, close string) string {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return open + "\n" + content + close
}

// withoutSection joins what surrounded a removed section, dropping the newline
// after the close marker and the blank lines before the open marker.
func withoutSection(before, after string) string {
	after = strings.TrimPrefix(after, "\n")
	result := strings.TrimRight(before, "\n")
	if after != "" {
		if result != "" {
			result += "\n"
		}
		return result + after
	}
	if result != "" {
		result += "\n"
	}
	return result
}
