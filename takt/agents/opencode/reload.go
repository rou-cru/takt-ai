// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencode

import (
	"context"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
)

// Reload asks the user's OpenCode server to rebuild loaded locations after
// a deployment, using normal server resolution so the handoff affects the
// session the user will run.
func Reload(ctx context.Context) error {
	return opencodeapi.New().Reload(ctx)
}
