// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package opencode

import (
	"context"
	"errors"
	"fmt"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
)

// AvailableModels returns the provider/model identifiers exposed by the
// local OpenCode V2 API, preserving the server's order.
func AvailableModels(ctx context.Context) ([]string, error) {
	models, err := opencodeapi.New().Models(ctx)
	if err != nil {
		return nil, fmt.Errorf("opencode models: %w", err)
	}
	if len(models) == 0 {
		return nil, errors.New("opencode reported no models")
	}
	identifiers := make([]string, 0, len(models))
	for _, model := range models {
		identifiers = append(identifiers, model.Ref.String())
	}
	return identifiers, nil
}

// Handshake proves that the local OpenCode installation is functional for
// Takt before setup mutates anything (PR-ART-2).
func Handshake(ctx context.Context) (opencodeapi.Handshake, error) {
	return opencodeapi.New().Handshake(ctx)
}
