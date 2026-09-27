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

package engram

import (
	"fmt"

	filemerge "github.com/rou-cru/takt-ai/takt/internal/filemerge"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Remove strips memory support so uninstalls leave no orphan keys or files, and merged user keys are never touched.
func Remove(homeDir string) (model.InjectionResult, error) {
	result, err := filemerge.RemoveMCPServer(model.OpenCodeConfigPath(homeDir), name)
	if err != nil {
		return model.InjectionResult{}, err
	}
	return removePromptSection(model.OpenCodePromptPath(homeDir), result)
}

// removePromptSection strips the section so the prompt file returns to its pre-install state.
func removePromptSection(promptPath string, result model.InjectionResult) (model.InjectionResult, error) {
	existing, err := filemerge.ReadFileOrEmpty(promptPath)
	if err != nil {
		return model.InjectionResult{}, err
	}
	updated := filemerge.InjectMarkdownSection(existing, model.SectionEngramProtocol, "")
	if updated != existing {
		if _, err := filemerge.WriteFileAtomic(promptPath, []byte(updated), filemerge.DefaultFileMode); err != nil {
			return model.InjectionResult{}, fmt.Errorf("clean engram protocol from %s: %w", promptPath, err)
		}
		result.Changed = true
		result.Files = append(result.Files, promptPath)
	}
	return result, nil
}
