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

package setup

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// Config preservation: when Takt deploys a config artifact onto a file the
// user already had before any Takt install (no ownership-manifest entry), the
// deployed content is merged over the existing bytes instead of replacing
// them. Files Takt owns keep replace semantics.

// isMergeableConfig reports whether a pre-existing unmanaged file is merged
// instead of replaced. OpenCode only deploys JSON configs, so JSON objects are
// the only merge format.
func isMergeableConfig(path string) bool {
	return strings.HasSuffix(path, ".json")
}

// mergePreexistingConfig returns artifact with its content merged over an
// existing unmanaged file beneath rootDir, or unchanged when there is nothing
// to merge.
func mergePreexistingConfig(rootDir string, artifact Artifact, manifest *OwnershipManifest) (Artifact, error) {
	if !isMergeableConfig(artifact.Path) {
		return artifact, nil
	}
	if _, managed := manifest.Entries[artifact.Path]; managed {
		return artifact, nil
	}
	existing, err := os.ReadFile(filepath.Join(rootDir, filepath.FromSlash(artifact.Path)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifact, nil
		}
		return Artifact{}, fmt.Errorf("read existing config %q: %w", artifact.Path, err)
	}
	if bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(artifact.Content)) || len(bytes.TrimSpace(existing)) == 0 {
		return artifact, nil
	}
	merged, err := filemerge.MergeJSONObjects(existing, artifact.Content)
	if err != nil {
		return Artifact{}, fmt.Errorf("merge config %q: %w", artifact.Path, err)
	}
	artifact.Content = merged
	return artifact, nil
}
