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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/setup"
)

const invalidRequestExitStatus = 2

// exitCode is an error that already reported itself and only carries the process exit status.
type exitCode int

// Error renders the process exit status this error carries.
func (c exitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

// runMemory drives the memory core for the OpenCode plugin: one strict JSON request in, one JSON line out.
func runMemory(args []string, stdin io.Reader, stdout io.Writer) error {
	result, err := dispatchMemory(args, stdin)
	var invalid *memory.ValidationError
	switch {
	case err == nil:
		return writeMemoryLine(stdout, map[string]any{"ok": true, "result": result}, 0)
	case errors.As(err, &invalid):
		return writeMemoryLine(stdout, map[string]any{"ok": false, "error": invalid.Reason}, invalidRequestExitStatus)
	default:
		return writeMemoryLine(stdout, map[string]any{"ok": false, "error": err.Error()}, 1)
	}
}

func dispatchMemory(args []string, stdin io.Reader) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("usage: takt-ai memory record|continue|close < request.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home directory: %w", err)
	}
	cfg := memory.Config{Root: home}
	cfg.EngramBinary, _ = engram.Resolve(home)
	ctx := context.Background()
	switch args[0] {
	case "record":
		req, err := decodeMemory[memory.RecordRequest](stdin)
		if err != nil {
			return nil, err
		}
		return memory.Record(ctx, cfg, req)
	case "continue":
		req, err := decodeMemory[memory.ContinueRequest](stdin)
		if err != nil {
			return nil, err
		}
		return struct{}{}, memory.Continue(ctx, cfg, req)
	case "close":
		req, err := decodeMemory[memory.CloseRequest](stdin)
		if err != nil {
			return nil, err
		}
		return memory.Close(ctx, cfg, req)
	}
	return nil, fmt.Errorf("unknown memory command %q (valid: record, continue, close)", args[0])
}

// decodeMemory treats a malformed request as a contract rejection so the caller sees why (exit 2).
func decodeMemory[T any](stdin io.Reader) (T, error) {
	req, err := decodeStrict[T](stdin)
	if err != nil {
		return req, &memory.ValidationError{Reason: "invalid request: " + err.Error()}
	}
	return req, nil
}

func writeMemoryLine(stdout io.Writer, line map[string]any, code int) error {
	if err := json.NewEncoder(stdout).Encode(line); err != nil {
		return exitCode(1)
	}
	if code != 0 {
		return exitCode(code)
	}
	return nil
}

// runDefaultRequest prints the default setup request so scripts can feed `setup install --input`.
func runDefaultRequest(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("setup default-request", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("invalid usage: %w", err)
	}
	if flags.NArg() != 0 {
		return errors.New("usage: takt-ai setup default-request")
	}
	request, err := setup.DefaultPlanRequest()
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(request)
}
