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

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

const usage = "usage: takt-ai setup install|sync|uninstall [--root <dir>] [--input <json-file-or->]"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) < 2 || args[0] != "setup" || (args[1] != "install" && args[1] != "sync" && args[1] != "uninstall") {
		return report(stderr, errors.New(usage))
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return report(stderr, fmt.Errorf("resolve home directory: %w", err))
	}
	flags := flag.NewFlagSet("setup "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", home, "")
	inputPath := flags.String("input", "-", "")
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return report(stderr, errors.New(usage))
		}
		return report(stderr, fmt.Errorf("invalid usage: %w", err))
	}
	if flags.NArg() != 0 {
		return report(stderr, fmt.Errorf("invalid usage: unexpected argument %q", flags.Arg(0)))
	}

	input := stdin
	if *inputPath != "-" {
		file, err := os.Open(*inputPath)
		if err != nil {
			return report(stderr, fmt.Errorf("open input: %w", err))
		}
		defer file.Close()
		input = file
	}
	request, err := decodeRequest(input)
	if err != nil {
		return report(stderr, fmt.Errorf("invalid input: %w", err))
	}

	if args[1] == "uninstall" {
		targets, err := ownershipTargets(request.Targets)
		if err != nil {
			return report(stderr, fmt.Errorf("setup %s: %w", args[1], err))
		}
		result, err := setup.Uninstall(*root, targets...)
		if err != nil {
			return report(stderr, fmt.Errorf("setup %s: %w", args[1], err))
		}
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			return report(stderr, fmt.Errorf("write output: %w", err))
		}
		return nil
	}

	plans, err := setup.BuildTargetPlans(request)
	if err != nil {
		return report(stderr, fmt.Errorf("setup %s: %w", args[1], err))
	}
	var result setup.DeploymentResult
	if args[1] == "sync" {
		result, err = setup.Sync(*root, plans)
	} else {
		result, err = setup.Apply(*root, plans)
	}
	if err != nil {
		return report(stderr, fmt.Errorf("setup %s: %w", args[1], err))
	}
	if err := json.NewEncoder(stdout).Encode(result); err != nil {
		return report(stderr, fmt.Errorf("write output: %w", err))
	}
	return nil
}

// ownershipTargets maps plan agent ids onto ownership target ids for
// manifest-scoped operations.
func ownershipTargets(ids []model.AgentID) ([]setup.OwnershipTarget, error) {
	targets := make([]setup.OwnershipTarget, 0, len(ids))
	for _, id := range ids {
		target, err := setup.OwnershipTargetFor(string(id))
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

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

func report(stderr io.Writer, err error) error {
	_, _ = fmt.Fprintln(stderr, err)
	return err
}
