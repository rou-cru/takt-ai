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

package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// Privacy modes keep memory session state owner-only.
const (
	// PrivateStateDirectoryMode protects the memory session directory.
	PrivateStateDirectoryMode os.FileMode = 0o700
	// PrivateStateFileMode protects the memory session index lock.
	PrivateStateFileMode os.FileMode = 0o600
)

type sessionIndex struct {
	Session     string              `json:"session"`
	Project     string              `json:"project"`
	Directory   string              `json:"directory"`
	StartAnchor int64               `json:"start_anchor"`
	EndAnchor   int64               `json:"end_anchor"`
	Continues   string              `json:"continues"`
	Entries     []sessionIndexEntry `json:"entries"`
}

type sessionIndexEntry struct {
	ID       int64  `json:"id"`
	Author   string `json:"author"`
	Nature   string `json:"nature"`
	Scope    string `json:"scope"`
	Title    string `json:"title"`
	Relation string `json:"relation"`
	Target   int64  `json:"target"`
	At       string `json:"at"`
}

func sessionsDir(root string) string {
	return filepath.Join(root, ".takt-ai", "memory", "sessions")
}

func sessionIndexPath(root, session string) string {
	return filepath.Join(sessionsDir(root), session+".json")
}

// lockSession takes an exclusive flock on <Session>.json.lock; closing the returned file releases it.
func lockSession(root, session string) (*os.File, error) {
	if err := os.MkdirAll(sessionsDir(root), PrivateStateDirectoryMode); err != nil {
		return nil, fmt.Errorf("create memory session dir: %w", err)
	}
	f, err := os.OpenFile(sessionIndexPath(root, session)+".lock", os.O_CREATE|os.O_RDWR, PrivateStateFileMode)
	if err != nil {
		return nil, fmt.Errorf("open session index lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return nil, fmt.Errorf("lock session index: %w", errors.Join(err, f.Close()))
	}
	return f, nil
}

// loadSessionIndex returns (nil, nil) when the session has no index yet.
func loadSessionIndex(root, session string) (*sessionIndex, error) {
	data, err := os.ReadFile(sessionIndexPath(root, session))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read session index: %w", err)
	}
	var l sessionIndex
	if err := json.Unmarshal(data, &l); err != nil {
		return nil, fmt.Errorf("parse session index %s: %w", session, err)
	}
	return &l, nil
}

func saveSessionIndex(root string, l *sessionIndex) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	if _, err := filemerge.WriteFileAtomic(sessionIndexPath(root, l.Session), append(data, '\n'), PrivateStateFileMode); err != nil {
		return fmt.Errorf("write session index: %w", err)
	}
	return nil
}

// EntryIDsForSession returns the ids of every memory entry created within
// session, so a caller can report them without the specialist declaring them
// itself (MEM-AUT-6). Returns an empty slice, not an error, when the session
// has no index yet.
func EntryIDsForSession(root, session string) ([]int64, error) {
	l, err := loadSessionIndex(root, session)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return []int64{}, nil
	}
	ids := make([]int64, len(l.Entries))
	for i, e := range l.Entries {
		ids[i] = e.ID
	}
	return ids, nil
}

// EntryIDsByAuthor returns the ids of the entries author recorded within
// session: what that author may present as its own delivered results.
func EntryIDsByAuthor(root, session, author string) ([]int64, error) {
	l, err := loadSessionIndex(root, session)
	if err != nil || l == nil {
		return []int64{}, err
	}
	ids := []int64{}
	for _, e := range l.Entries {
		if e.Author == author {
			ids = append(ids, e.ID)
		}
	}
	return ids, nil
}

func (l *sessionIndex) hasEntry(id int64) bool {
	return slices.ContainsFunc(l.Entries, func(e sessionIndexEntry) bool { return e.ID == id })
}
