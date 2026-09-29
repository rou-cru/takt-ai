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

// Package engram keeps memory-server wiring in one place so every agent target shares the same setup and checks.
package engram

import (
	"fmt"
	"os/exec"
	"strings"
)

var (
	// lookPath finds binaries so tests can swap in a fake path.
	lookPath = exec.LookPath
	// execCommand runs binaries so tests can stub command execution.
	execCommand = exec.Command
)

// VerifyVersion runs `<binary> version` so broken installs fail fast with a clear error.
func VerifyVersion(binary string) (string, error) {
	out, err := execCommand(binary, "version").Output()
	if err != nil {
		return "", fmt.Errorf("engram version command failed: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", fmt.Errorf("engram version returned empty output")
	}
	return version, nil
}
