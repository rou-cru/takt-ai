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
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/history"
)

// Read selectors an assignment may state. Anything else is an exact path.
const (
	// ReadAll covers every path; it is refused while another unit in flight holds one.
	ReadAll = "all"
	// ReadFree covers every path except those another unit in flight holds.
	ReadFree = "free"
	// recursiveSuffix marks a directory selector that includes its descendants.
	recursiveSuffix = "/**"
)

// ValidateReads rejects a selector that names nothing it can match.
func ValidateReads(selectors []string) error {
	for _, selector := range selectors {
		switch {
		case selector == ReadAll || selector == ReadFree:
		case strings.HasPrefix(selector, "*."):
			if strings.ContainsAny(selector[1:], "/*?[") || len(selector) == 2 {
				return fmt.Errorf("%w: read selector %q is not an extension pattern", ErrInvalidPath, selector)
			}
		default:
			dir := strings.TrimSuffix(strings.TrimSuffix(selector, "/"), "/**")
			if !fs.ValidPath(dir) || dir == "." || !lexicallyCanonical(dir) {
				return fmt.Errorf("%w: read selector %q", ErrInvalidPath, selector)
			}
		}
	}
	return nil
}

// readCovers reports whether selector names rel.
func readCovers(selector, rel string) bool {
	switch {
	case selector == ReadAll || selector == ReadFree:
		return true
	case strings.HasPrefix(selector, "*."):
		return strings.HasSuffix(path.Base(rel), selector[1:])
	case strings.HasSuffix(selector, recursiveSuffix):
		return strings.HasPrefix(rel, strings.TrimSuffix(selector, "**"))
	case strings.HasSuffix(selector, "/"):
		return path.Dir(rel) == strings.TrimSuffix(selector, "/")
	}
	return rel == selector
}

// readsCover reports whether any selector names rel.
func readsCover(selectors []string, rel string) bool {
	return slices.ContainsFunc(selectors, func(selector string) bool { return readCovers(selector, rel) })
}

// readsExplicitlyCover reports whether a selector other than free names rel:
// only those refuse a launch over work in flight.
func readsExplicitlyCover(selectors []string, rel string) bool {
	return slices.ContainsFunc(selectors, func(selector string) bool { return selector != ReadFree && readCovers(selector, rel) })
}

// liveLocked reports whether key's unit holds its paths as a mutex: it is
// awaiting launch or in flight. A holder that is neither left staged work with
// no one running over it.
func (f *FS) liveLocked(p history.Projection, key AgentID) bool {
	b, ok := f.bindings[key]
	if !ok {
		return false
	}
	if b.Prelaunch {
		return true
	}
	u, known := p.Unit(b.SessionID, b.WorkUnitID)
	return known && u.State == history.StateInFlight
}

func sameUnit(a, b Identity) bool { return a.SessionID == b.SessionID && a.WorkUnitID == b.WorkUnitID }

// admitReadsLocked is the reader's side of the mutex: an explicit selector over
// a path another unit in flight holds is refused to the orchestrator.
func (f *FS) admitReadsLocked(identity Identity) error {
	if len(identity.Reads) == 0 {
		return nil
	}
	if err := ValidateReads(identity.Reads); err != nil {
		return err
	}
	p, err := history.ReadProjection(f.stateDir)
	if err != nil {
		return err
	}
	var held []string
	for rel, owner := range f.owners {
		if sameUnit(f.bindings[owner], identity) || !readsExplicitlyCover(identity.Reads, rel) || !f.liveLocked(p, owner) {
			continue
		}
		held = append(held, fmt.Sprintf("%s (unit %s)", rel, f.bindings[owner].WorkUnitID))
	}
	if len(held) == 0 {
		return nil
	}
	slices.Sort(held)
	return fmt.Errorf("%w: reads over %s collide with work in flight; wait for it to deliver, narrow reads away from those paths, or state %q to read all that is not in flight", ErrCollision, strings.Join(held, ", "), ReadFree)
}

// guardReadersLocked is the writer's side of the mutex: an exclusive claim over
// a path a reader in flight covers is refused to the orchestrator.
func (f *FS) guardReadersLocked(identity Identity, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	p, err := history.ReadProjection(f.stateDir)
	if err != nil {
		return err
	}
	for key, reader := range f.bindings {
		if len(reader.Reads) == 0 || sameUnit(reader, identity) || !f.liveLocked(p, key) {
			continue
		}
		for _, rel := range paths {
			if readsCover(reader.Reads, rel) {
				return fmt.Errorf("%w: %q is being read by unit %s, which is in flight; wait for it to deliver, or narrow its reads away from that path", ErrCollision, rel, reader.WorkUnitID)
			}
		}
	}
	return nil
}

// readViewLocked resolves whose view serves agent's read of rel and whether
// its reads grant the path. A granted path another unit left staged is served
// from that work; the claim and its staged delta stay as they were.
func (f *FS) readViewLocked(agent AgentID, rel string) (AgentID, bool) {
	identity, ok := f.bindings[agent]
	if !ok || len(identity.Reads) == 0 || !readsCover(identity.Reads, rel) {
		return agent, false
	}
	owner, owned := f.owners[rel]
	if !owned || owner == agent {
		return agent, true
	}
	p, err := history.ReadProjection(f.stateDir)
	if err != nil || f.liveLocked(p, owner) {
		return agent, false
	}
	return owner, true
}
