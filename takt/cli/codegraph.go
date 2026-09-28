// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/rou-cru/takt-ai/takt/codegraph"
)

// runCodegraph prepares the active OpenCode workspace before its MCP server
// can expose CodeGraph tools.
func runCodegraph(args []string) error {
	if len(args) != 1 || args[0] != "ensure-index" {
		return errors.New("usage: takt-ai codegraph ensure-index")
	}
	workspace, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve CodeGraph workspace: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	binary, found := codegraph.Resolve(home)
	if !found {
		return fmt.Errorf("CodeGraph %s is unavailable; reinstall Takt before opening this workspace", codegraph.CodegraphVersion)
	}
	return codegraph.EnsureIndexed(context.Background(), binary, workspace)
}
