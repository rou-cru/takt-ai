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

package vfs

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/rou-cru/takt-ai/takt/model"
)

// sensitiveSkipDirs are never descended into while looking for secrets: .git
// holds no workspace secret the rules name, and dependency trees are large
// enough to make every inspection pay for the walk.
var sensitiveSkipDirs = []string{".git", "node_modules"}

// sensitivePaths resolves model.SensitivePathGlobs to the existing absolute
// literal paths an inspection sandbox must deny for reading: every match at
// any depth of the workspace, and the home-relative ones (".ssh/**",
// ".aws/credentials", ...) under home. The sandbox accepts no globs, so a
// secret is denied only once it exists; a directory pattern ("x/**") denies
// the directory itself.
func sensitivePaths(root, home string) []string {
	var found []string
	seen := map[string]bool{}
	add := func(p string) {
		// The sandbox rejects glob metacharacters in a literal path.
		if !seen[p] && !strings.ContainsAny(p, "*?[]") {
			seen[p] = true
			found = append(found, p)
		}
	}
	_ = filepath.WalkDir(root, sensitiveWalkFn(root, add))
	addHomeSensitivePaths(home, add)
	return found
}

// sensitiveWalkFn builds the filepath.WalkDir callback that reports every
// sensitive match under root to add, skipping directories the walk never
// needs to descend into.
func sensitiveWalkFn(root string, add func(string)) fs.WalkDirFunc {
	return func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(p, root+string(filepath.Separator)))
		if d.IsDir() && isSkipDir(d.Name()) {
			return filepath.SkipDir
		}
		if matchesSensitive(rel, d.IsDir()) {
			add(p)
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	}
}

// addHomeSensitivePaths reports every home-relative sensitive glob match
// (".ssh/**", ".aws/credentials", ...) under home to add. A blank home is a
// no-op, since it means the caller could not resolve one.
func addHomeSensitivePaths(home string, add func(string)) {
	if home == "" {
		return
	}
	for _, glob := range model.SensitivePathGlobs {
		matches, _ := filepath.Glob(filepath.Join(home, filepath.FromSlash(strings.TrimSuffix(glob, "/**"))))
		for _, m := range matches {
			add(m)
		}
	}
}

func isSkipDir(name string) bool {
	for _, skip := range sensitiveSkipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

// matchesSensitive reports whether rel (slash-separated, workspace-relative)
// matches a sensitive glob at any depth, as the native "**/<glob>" rules do.
func matchesSensitive(rel string, dir bool) bool {
	parts := strings.Split(rel, "/")
	for _, glob := range model.SensitivePathGlobs {
		dirPattern := strings.HasSuffix(glob, "/**")
		if dirPattern != dir {
			continue
		}
		pattern := strings.TrimSuffix(glob, "/**")
		n := strings.Count(pattern, "/") + 1
		if len(parts) < n {
			continue
		}
		if ok, _ := path.Match(pattern, strings.Join(parts[len(parts)-n:], "/")); ok {
			return true
		}
	}
	return false
}

// userHome is the home directory whose secrets inspection must not read; an
// unresolvable home only drops the home-relative denials.
func userHome() string {
	home, _ := os.UserHomeDir()
	return home
}
