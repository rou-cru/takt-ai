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

// name is the MCP server key engram is stored under.
const name = "engram"

// Inject adds memory support to OpenCode.
func Inject(homeDir, engramCommand string) (model.InjectionResult, error) {
	result, err := filemerge.InjectMCPServer(model.OpenCodeConfigPath(homeDir), name, append([]string{engramCommand}, mcpArgs()...))
	if err != nil {
		return model.InjectionResult{}, err
	}

	promptWrite, err := injectPromptSection(model.OpenCodePromptPath(homeDir))
	if err != nil {
		return model.InjectionResult{}, err
	}
	result.Changed = result.Changed || promptWrite.Changed
	result.Files = append(result.Files, model.OpenCodePromptPath(homeDir))
	return result, nil
}

// injectPromptSection writes the marked section so the prompt file stays idempotent.
func injectPromptSection(promptPath string) (filemerge.WriteResult, error) {
	existing, err := filemerge.ReadFileOrEmpty(promptPath)
	if err != nil {
		return filemerge.WriteResult{}, fmt.Errorf("read prompt file %q: %w", promptPath, err)
	}
	updated := filemerge.InjectMarkdownSection(existing, model.SectionEngramProtocol, bootstrapSection())
	write, err := filemerge.WriteFileAtomic(promptPath, []byte(updated), filemerge.DefaultFileMode)
	if err != nil {
		return filemerge.WriteResult{}, fmt.Errorf("write prompt file %q: %w", promptPath, err)
	}
	return write, nil
}
