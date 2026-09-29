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
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// OperationRecordFilename marks a mutating operation in progress on disk.
// Written before the first change and removed on return; a leftover means the process ended mid-operation.
const OperationRecordFilename = ".takt-operation.json"

// OperationRecord describes the mutating operation that was in progress.
type OperationRecord struct {
	// Action is the operation that was running: install, sync, uninstall, correct-drift, or reassign-models.
	Action string `json:"action"`
	// Started is when the operation began, shown in the recovery notice.
	Started time.Time `json:"started"`
	// TaktVersion is the build that ran the operation, for diagnosing version skew.
	TaktVersion string `json:"takt_version"`
}

// BeginOperation records action as in progress under rootDir and returns the
// func that clears the record once the operation has returned.
func BeginOperation(rootDir, action string) (func(), error) {
	data, err := json.Marshal(OperationRecord{Action: action, Started: time.Now(), TaktVersion: BuildVersion})
	if err != nil {
		return nil, err
	}
	file := filepath.Join(rootDir, OperationRecordFilename)
	if err := os.MkdirAll(rootDir, ManagedDirectoryMode); err != nil {
		return nil, err
	}
	// A plain write is enough: a torn record still reads as "an operation did not finish".
	if err := os.WriteFile(file, data, ManagedFileMode); err != nil {
		return nil, err
	}
	return func() { _ = os.Remove(file) }, nil
}

// IncompleteOperation returns the record an abruptly ended operation left
// under rootDir. An unreadable record still reports an unfinished operation.
func IncompleteOperation(rootDir string) (OperationRecord, bool) {
	data, err := os.ReadFile(filepath.Join(rootDir, OperationRecordFilename))
	if err != nil {
		return OperationRecord{}, false
	}
	var record OperationRecord
	_ = json.Unmarshal(data, &record)
	return record, true
}

// Notice explains an unfinished operation: what stopped, that files are checked first, and how to recover replaced content.
func (record OperationRecord) Notice(rootDir string) []string {
	action := map[string]string{"install": "installation", "sync": "configuration change", "uninstall": "uninstall", "correct-drift": "drift correction", "reassign-models": "model assignment"}[record.Action]
	if action == "" {
		action = "operation"
	}
	started := ""
	if !record.Started.IsZero() {
		started = " (started " + record.Started.Local().Format("2006-01-02 15:04") + ")"
	}
	recovery := "Rollback is not automatic and no replaced content was backed up."
	backups := filepath.Join(rootDir, BackupDir)
	if _, err := os.Stat(backups); err == nil {
		recovery = "Run 'takt-ai restore --root " + rootDir + "' to restore the files it replaced (backed up under " + backups + ")."
	}
	return []string{"A previous " + action + " did not finish" + started + ". Takt will check the actual files before any change.", recovery}
}

// BackupsSince returns rootDir's backup directory when it holds a copy
// written at or after since, or "" otherwise.
func BackupsSince(rootDir string, since time.Time) string {
	dir := filepath.Join(rootDir, BackupDir)
	found := false
	since = since.Truncate(time.Second) // filesystem mtimes are only second-precise
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || found || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil && !info.ModTime().Before(since) {
			found = true
		}
		return nil
	})
	if !found {
		return ""
	}
	return dir
}
