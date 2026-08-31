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
	"runtime/debug"
	"strings"

	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui"
)

const usage = "usage: takt-ai version | doctor | setup install|sync|uninstall [--root <dir>] [--input <json-file-or->]"

// version is set by GoReleaser via ldflags at build time.
var version = "dev"

var runTUI = tui.Run

var buildInfoReader = debug.ReadBuildInfo

// resolveVersion determines the effective version string.
// Priority: ldflags override > BuildInfo.Main.Version > "dev".
func resolveVersion(ldflagsVersion string) string {
	if ldflagsVersion != "dev" {
		return ldflagsVersion
	}
	info, ok := buildInfoReader()
	if !ok {
		return "dev"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

// run starts the TUI without arguments; setup commands retain their JSON interface.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		if err := runTUI(stdin, stdout); err != nil {
			return report(stderr, err)
		}
		return nil
	}
	switch args[0] {
	case "version", "--version", "-v":
		_, _ = fmt.Fprintf(stdout, "takt-ai %s\n", resolveVersion(version))
		return nil
	case "doctor":
		return doctor.Run(stdout)
	}
	command, root, inputPath, err := parseSetupInvocation(args)
	if err != nil {
		return report(stderr, err)
	}
	input, closeInput, err := openRequestInput(inputPath, stdin)
	if err != nil {
		return report(stderr, fmt.Errorf("open input: %w", err))
	}
	defer closeInput()
	request, err := decodeRequest(input)
	if err != nil {
		return report(stderr, fmt.Errorf("invalid input: %w", err))
	}

	result, err := executeSetup(command, root, request)
	if err != nil {
		return report(stderr, fmt.Errorf("setup %s: %w", command, err))
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return report(stderr, fmt.Errorf("write output: %w", err))
	}
	return nil
}

var setupCommands = map[string]bool{"install": true, "sync": true, "uninstall": true}

// parseSetupInvocation validates the setup subcommand, parses its flags, and
// resolves the default deployment root.
func parseSetupInvocation(args []string) (string, string, string, error) {
	if len(args) < 2 || args[0] != "setup" || !setupCommands[args[1]] {
		return "", "", "", errors.New(usage)
	}
	flags := flag.NewFlagSet("setup "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "")
	inputPath := flags.String("input", "-", "")
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", "", "", errors.New(usage)
		}
		return "", "", "", fmt.Errorf("invalid usage: %w", err)
	}
	if flags.NArg() != 0 {
		return "", "", "", fmt.Errorf("invalid usage: unexpected argument %q", flags.Arg(0))
	}
	resolvedRoot, err := resolveRoot(*root)
	if err != nil {
		return "", "", "", err
	}
	return args[1], resolvedRoot, *inputPath, nil
}

func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

func openRequestInput(inputPath string, stdin io.Reader) (io.Reader, func(), error) {
	if inputPath == "-" {
		return stdin, func() {}, nil
	}
	file, err := os.Open(inputPath)
	if err != nil {
		return nil, nil, err
	}
	return file, func() { _ = file.Close() }, nil
}

// executeSetup dispatches one validated setup subcommand through the shared
// lifecycle and maps the outcome to the command's JSON result shape.
func executeSetup(command, root string, request setup.PlanRequest) (any, error) {
	result, err := lifecycle.RunLifecycle(command, root, request)
	if err != nil {
		return nil, err
	}
	if command == "uninstall" {
		return setup.UninstallResult{Removed: result.Removed, Preserved: result.Preserved}, nil
	}
	return setup.DeploymentResult{Changed: result.Changed, Unchanged: result.Unchanged}, nil
}

// decodeRequest decodes a single JSON object from input into a setup plan request.
// Unknown fields and additional JSON values cause an error.
func decodeRequest(input io.Reader) (setup.PlanRequest, error) {
	var request setup.PlanRequest
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return setup.PlanRequest{}, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return setup.PlanRequest{}, errors.New("multiple JSON values")
		}
		return setup.PlanRequest{}, err
	}
	return request, nil
}

// report writes the error message to stderr and returns the same error.
func report(stderr io.Writer, err error) error {
	_, _ = fmt.Fprintln(stderr, err)
	return err
}
