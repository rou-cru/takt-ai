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

package dispatch

import (
	"errors"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/history"
)

// StartActivity records non-work-unit orchestrator or maintenance activity.
// Activity identity stays in ActivityID, never in WorkUnitID; delegated work
// continues to use Admit and the ordinary unit lifecycle.
func StartActivity(h *history.History, session, activityID string, nodeKind history.NodeKind) error {
	if session == "" || activityID == "" {
		return errors.New("dispatch: activity start requires session and activity_id")
	}
	kind, err := activityRecordKind(nodeKind, true)
	if err != nil {
		return err
	}
	if prior, exists := h.Project().Activities[activityID]; exists {
		if prior.SessionID == session && prior.NodeKind == nodeKind && prior.State == history.StateInFlight {
			return nil
		}
		return fmt.Errorf("dispatch: activity identity %q is already recorded", activityID)
	}
	return h.Append(history.Entry{
		Author: history.AuthorOf(kind), Kind: kind, SessionID: session, ActivityID: activityID,
		NodeKind: nodeKind, Cause: history.CauseNone,
	})
}

// FinishActivity settles a prior non-work-unit activity without creating a
// work-unit identity or affecting delegated admission accounting.
func FinishActivity(h *history.History, session, activityID string, nodeKind history.NodeKind, outcome history.Outcome) error {
	if session == "" || activityID == "" {
		return errors.New("dispatch: activity finish requires session and activity_id")
	}
	prior, exists := h.Project().Activities[activityID]
	if !exists || prior.SessionID != session || prior.NodeKind != nodeKind {
		return fmt.Errorf("dispatch: activity %q is not active for this session and kind", activityID)
	}
	if prior.State == history.StateSettled {
		if prior.Outcome == outcome {
			return nil
		}
		return fmt.Errorf("dispatch: activity %q already finished with outcome %q", activityID, prior.Outcome)
	}
	kind, err := activityRecordKind(nodeKind, false)
	if err != nil {
		return err
	}
	return h.Append(history.Entry{
		Author: history.AuthorOf(kind), Kind: kind, SessionID: session, ActivityID: activityID,
		NodeKind: nodeKind, Cause: history.CauseNone, Outcome: outcome,
	})
}

func activityRecordKind(nodeKind history.NodeKind, start bool) (history.Kind, error) {
	switch {
	case nodeKind == history.NodeKindOrchestrator && start:
		return history.KindActivityStarted, nil
	case nodeKind == history.NodeKindOrchestrator:
		return history.KindActivityFinished, nil
	case nodeKind == history.NodeKindMaintenance && start:
		return history.KindMaintenanceStarted, nil
	case nodeKind == history.NodeKindMaintenance:
		return history.KindMaintenanceFinished, nil
	default:
		return "", fmt.Errorf("dispatch: node kind %q is not a non-work-unit activity", nodeKind)
	}
}
