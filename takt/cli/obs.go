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
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// usageObs lists valid obs commands so callers can recover after a mistake.
const usageObs = "usage: takt-ai obs ingest --workspace <dir> < event.json"

// obsIPCVersion is the wire contract version of obs ingest, independent of the vfs one so each can evolve alone.
const obsIPCVersion = 1

// obsIngestPlanes fixes which plane may emit each class the plugin is allowed to send; every other class
// (vfs_*, control, harness, lifecycle) is produced inside Takt and is rejected here.
var obsIngestPlanes = map[obs.EventClass]obs.SourcePlane{
	obs.EventDispatch:      obs.PlaneOrchestration,
	obs.EventUnitLifecycle: obs.PlaneOrchestration,
	obs.EventToolActivity:  obs.PlanePlatform,
	obs.EventModelUsage:    obs.PlanePlatform,
}

// obsIngestRequest is the strict stdin contract of obs ingest: identity and class plus content-free attributes.
type obsIngestRequest struct {
	IPCVersion int            `json:"ipc_version"`
	SessionID  string         `json:"session_id"`
	WorkUnitID string         `json:"work_unit_id"`
	AttemptID  string         `json:"attempt_id"`
	Agent      string         `json:"agent"`
	Source     string         `json:"source"`
	EventClass string         `json:"event_class"`
	Attributes map[string]any `json:"attributes"`
}

// runObs appends externally observed events to the workspace event store.
func runObs(args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	workspace, err := parseObsWorkspace(args, stderr)
	if err != nil {
		return err
	}
	// A typo must not create a new state directory somewhere else.
	if err := requireObsWorkspace(workspace); err != nil {
		return err
	}
	req, err := decodeStrict[obsIngestRequest](stdin)
	if err != nil {
		return fmt.Errorf("obs: invalid input: %w", err)
	}
	if req.IPCVersion != obsIPCVersion {
		return fmt.Errorf("obs: ipc_version %d unsupported (want %d)", req.IPCVersion, obsIPCVersion)
	}
	class := obs.EventClass(req.EventClass)
	plane, ok := obsIngestPlanes[class]
	if !ok {
		return fmt.Errorf("obs: event_class %q cannot be ingested (dispatch, unit_lifecycle, tool_activity, model_usage)", req.EventClass)
	}
	if obs.SourcePlane(req.Source) != plane {
		return fmt.Errorf("obs: source %q does not match class %q (want %q)", req.Source, req.EventClass, plane)
	}
	env := obs.Envelope{
		SchemaVersion: obs.SchemaVersion,
		WallTimestamp: time.Now(),
		Source:        plane,
		Authority:     req.Agent,
		EventClass:    class,
		Correlations:  obs.CorrelationIDs{SessionID: req.SessionID, WorkUnitID: req.WorkUnitID, AttemptID: req.AttemptID},
		Attributes:    req.Attributes,
	}
	// Envelope.Validate (called by AppendEvent) owns the attribute allowlist and the content ban.
	store, err := obs.OpenStore(workspace)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	id, err := store.AppendEvent(env)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"ok": true, "id": id})
}

func parseObsWorkspace(args []string, stderr io.Writer) (string, error) {
	if len(args) == 0 || args[0] != "ingest" {
		return "", errors.New(usageObs)
	}
	flags := flag.NewFlagSet("obs ingest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", "", "existing workspace directory holding the event store")
	if err := flags.Parse(args[1:]); err != nil {
		return "", err
	}
	if flags.NArg() != 0 || *workspace == "" {
		return "", errors.New("obs: --workspace is required; no positional arguments")
	}
	return *workspace, nil
}

func requireObsWorkspace(workspace string) error {
	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("obs: workspace %q is not an existing directory", workspace)
	}
	return nil
}
