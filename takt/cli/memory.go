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
	"os/signal"
	"syscall"

	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// serveMemory runs the memory service on the resolved memory directory until SIGTERM or interrupt.
func serveMemory() error {
	dir, err := memory.ResolveDir()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return memory.Serve(ctx, dir, resolveVersion(version))
}

// runMemory runs the memory service commands. `mcp` writes only MCP messages to stdout and `start` prints
// the workspace key; `serve` runs until SIGTERM or interrupt.
func runMemory(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: takt-ai memory serve|mcp|start")
	}
	switch args[0] {
	case "serve":
		return serveMemory()
	case "mcp":
		return runMemoryMCP(args[1:], stdin, stdout)
	case "start":
		return runMemoryStart(args[1:], stdout)
	}
	return fmt.Errorf("unknown memory command %q (valid: serve, mcp, start)", args[0])
}

func runMemoryMCP(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("memory mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	var opts memory.AdapterOptions
	flags.BoolVar(&opts.Crew, "crew", false, "relay to the crew path")
	flags.StringVar(&opts.Workspace, "workspace", "", "workspace directory")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: takt-ai memory mcp [--crew] [--workspace <dir>]")
	}
	dir, err := memory.ResolveDir()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	return memory.RunAdapter(ctx, dir, opts, stdin, stdout)
}

func runMemoryStart(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("memory start", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	create := flags.Bool("init", false, "create the memory directory and store when absent")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: takt-ai memory start [--init]")
	}
	dir, err := memory.ResolveDir()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx := context.Background()
	if _, err := memory.Start(ctx, dir, *create); err != nil {
		return err
	}
	socket, err := memory.SocketPath(dir)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(memoryStartOutput{Workspace: memory.WorkspaceKey(ctx, cwd), Socket: socket})
}

// memoryStartOutput is the one JSON line `memory start` prints: the caller's workspace key and the socket
// of the owner that serves it, so a client learns the socket from the runtime instead of resolving it.
type memoryStartOutput struct {
	Workspace string `json:"workspace"`
	Socket    string `json:"socket"`
}

// exitCode is an error that already reported itself and only carries the process exit status.
type exitCode int

// Error renders the process exit status this error carries.
func (c exitCode) Error() string { return fmt.Sprintf("exit status %d", int(c)) }

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
