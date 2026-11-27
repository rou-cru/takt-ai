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

// Package semver compares numeric MAJOR.MINOR.PATCH version strings.
package semver

import (
	"strconv"
	"strings"
)

// semverComponents is the number of numeric components in a semantic version.
const semverComponents = 3

// AtLeast reports whether the numeric MAJOR.MINOR.PATCH prefix of have is not
// older than want.
func AtLeast(have, want string) bool {
	parsedHave, ok := parse(have)
	if !ok {
		return false
	}
	parsedWant, _ := parse(want)
	for i := range parsedHave {
		if parsedHave[i] != parsedWant[i] {
			return parsedHave[i] > parsedWant[i]
		}
	}
	return true
}

// parse accepts numeric MAJOR.MINOR.PATCH; pre-release tags are rejected as not yet the pinned release.
func parse(value string) ([3]int, bool) {
	var parsed [3]int
	value, _, _ = strings.Cut(strings.TrimPrefix(value, "v"), "+")
	parts := strings.Split(value, ".")
	if len(parts) != semverComponents {
		return parsed, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return parsed, false
		}
		parsed[i] = n
	}
	return parsed, true
}
