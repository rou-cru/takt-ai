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

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/history"
)

// usageDag lists valid dag commands so callers can recover after a mistake.
const usageDag = "usage: takt-ai dag status --workspace <dir> --state <private-dir> [--session <root-session>] --format json"

// runDag serves the read-only DAG snapshot; it never selects, admits or
// mutates work (PRD_DAG_TUI.md §2).
func runDag(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "status" {
		return errors.New(usageDag)
	}
	flags := flag.NewFlagSet("dag status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	// dispatch/vfs/gc all take --workspace and --state as two independent,
	// caller-managed paths (the private state directory is not derived from
	// the workspace path anywhere in this codebase); dag status reuses that
	// same pair instead of inventing a fixed "state lives under workspace"
	// convention the rest of the CLI does not follow.
	workspace := flags.String("workspace", "", "workspace governed by the execution history")
	state := flags.String("state", "", "private state directory holding the execution history")
	// The state directory is per workspace, so its history spans every session
	// that ever worked there; --session narrows the projection to the one
	// root session the caller is showing.
	session := flags.String("session", "", "root session whose plan to project (default: every session in the history)")
	format := flags.String("format", "json", "output format; only json is supported")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *workspace == "" || *state == "" {
		return errors.New("dag: --workspace and --state are required; no positional arguments")
	}
	if *format != "json" {
		return fmt.Errorf("dag: unsupported --format %q (only json)", *format)
	}
	snap, err := readSnapshot(*workspace, *state, *session)
	if err != nil {
		// A control-plane query failure MUST NOT be rendered as an empty or
		// completed DAG (PRD_DAG_TUI.md §11): the TUI plugin only has this
		// process's combined stdout+stderr to parse as JSON, so a read
		// failure here still writes one clean JSON value and exits 0, rather
		// than the nonzero-exit/stderr-message convention the rest of this
		// CLI uses for a caller's usage mistake. A missing --workspace/--state
		// flag above is still a hard usage error: that is a caller mistake,
		// not a control-plane read failure.
		return json.NewEncoder(stdout).Encode(map[string]string{"capture": history.CaptureUnavailable})
	}
	return json.NewEncoder(stdout).Encode(snap)
}

// readSnapshot opens the workspace's execution history the same way
// dispatch/vfs/gc do and derives its DAG snapshot. It never initializes a store
// for a mistyped path (mirroring gc.go's and vfs.go's "existing store
// required" guard): an absent history is a read failure, not an empty plan, and
// is checked before history.Open, which would create one.
func readSnapshot(workspace, state, session string) (snap history.Snapshot, err error) {
	if info, statErr := os.Stat(workspace); statErr != nil || !info.IsDir() {
		return history.Snapshot{}, fmt.Errorf("dag: workspace %q is not an existing directory", workspace)
	}
	if _, statErr := os.Stat(filepath.Join(state, "history.sqlite")); statErr != nil {
		return history.Snapshot{}, fmt.Errorf("dag: existing execution history required: %w", statErr)
	}
	h, err := history.Open(state)
	if err != nil {
		return history.Snapshot{}, err
	}
	defer func() { err = errors.Join(err, h.Close()) }()
	entries := h.Entries()
	if session != "" {
		entries = slices.DeleteFunc(slices.Clone(entries), func(e history.Entry) bool { return e.SessionID != session })
	}
	snap = history.BuildSnapshot(entries)
	// The wire names each admitted agent by its catalog label, the form the
	// sidebar shows; an instance the catalog does not know keeps its own name.
	crew, err := catalog.LoadNativeContent()
	if err != nil {
		return history.Snapshot{}, err
	}
	for i, n := range snap.Nodes {
		if member, ok := crew[n.Agent]; ok {
			snap.Nodes[i].Agent = member.Label
		}
	}
	return snap, nil
}
