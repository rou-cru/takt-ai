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

package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InstalledConfigFilename records the PlanRequest last applied, so later operations reapply real custom choices.
const InstalledConfigFilename = ".takt-installed-config.json"

// BuildVersion is the running Takt build's version, recorded as the installed version by RecordInstallation.
var BuildVersion = "dev"

// installedRecord is the on-disk shape: the applied PlanRequest plus the version that produced it.
type installedRecord struct {
	PlanRequest
	TaktVersion string `json:"takt_version,omitempty"`
}

// SaveInstalledConfig persists request as the installed snapshot, keeping the recorded version.
// Partial changes must not claim the whole installation matches this build, so the version stays.
func SaveInstalledConfig(rootDir string, request PlanRequest) error {
	return saveInstalledRecord(rootDir, installedRecord{PlanRequest: request, TaktVersion: InstalledVersion(rootDir)})
}

// RecordInstallation persists request as the installed configuration a full
// install or sync produced with this build's definitions.
func RecordInstallation(rootDir string, request PlanRequest) error {
	return saveInstalledRecord(rootDir, installedRecord{PlanRequest: request, TaktVersion: BuildVersion})
}

// saveInstalledRecord writes one installed record atomically so partial reads never occur.
func saveInstalledRecord(rootDir string, record installedRecord) error {
	return saveRecord(rootDir, InstalledConfigFilename, "installed config", record)
}

// LoadInstalledConfig reads the installed snapshot; a missing file wraps os.ErrNotExist for fallback logic.
func LoadInstalledConfig(rootDir string) (PlanRequest, error) {
	record, err := loadInstalledRecord(rootDir)
	return record.PlanRequest, err
}

// loadInstalledRecord reads and parses the installed record, preserving version history.
func loadInstalledRecord(rootDir string) (installedRecord, error) {
	raw, err := os.ReadFile(filepath.Join(rootDir, InstalledConfigFilename))
	if err != nil {
		return installedRecord{}, fmt.Errorf("load installed config: %w", err)
	}
	var record installedRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return installedRecord{}, fmt.Errorf("parse installed config: %w", err)
	}
	return record, nil
}

// InstalledVersion returns rootDir's recorded Takt version, or "" when absent or untracked.
func InstalledVersion(rootDir string) string {
	record, _ := loadInstalledRecord(rootDir)
	return record.TaktVersion
}

// IsCurrentVersion reports whether installed matches this build, so drift
// correction never restores another version's definitions.
func IsCurrentVersion(installed string) bool {
	return installed == BuildVersion
}

// ForgetInstallation drops rootDir's installed record and the risk acceptances
// recorded with it: uninstalling OpenCode leaves nothing installed.
func ForgetInstallation(rootDir string) error {
	for _, name := range []string{InstalledConfigFilename, RiskAcceptanceFilename} {
		if err := os.Remove(filepath.Join(rootDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove installed config: %w", err)
		}
	}
	return nil
}

// IsInstalled reports whether rootDir holds a prior installation; an unreadable
// record means "nothing installed".
func IsInstalled(rootDir string) bool {
	_, err := LoadInstalledConfig(rootDir)
	return err == nil
}

// NotInstalledMessage is the full sentence shown by screens (Diagnostics,
// Drift, Uninstall) that require a prior installation and found none.
const NotInstalledMessage = "Nothing installed yet — install first."
