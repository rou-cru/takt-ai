// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package setup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// providerActionTimeout is the maximum duration for a single provider action.
	providerActionTimeout = 2 * time.Minute
	// providerOutputLimit caps the captured output from a provider action.
	providerOutputLimit = 8 * 1024
)

// ProviderAction is a trusted external integration command executed without a shell.
type ProviderAction struct {
	// ID names the action in results and not-applied lists.
	ID string
	// Program is the executable resolved without a shell.
	Program string
	// Args are the exact arguments passed to the program.
	Args []string
}

// ProviderRuntime supplies executable lookup and command execution seams.
type ProviderRuntime struct {
	// LookPath resolves the program; defaults to exec.LookPath.
	LookPath func(string) (string, error)
	// Run executes the program; defaults to a bounded-output runner.
	Run func(context.Context, string, ...string) ([]byte, error)
	// Timeout caps one action; non-positive means the default timeout.
	Timeout time.Duration
}

// normalized fills unset runtime seams so callers can pass a zero value.

func (runtime ProviderRuntime) normalized() ProviderRuntime {
	if runtime.LookPath == nil {
		runtime.LookPath = exec.LookPath
	}
	if runtime.Run == nil {
		runtime.Run = runProviderCommand
	}
	if runtime.Timeout <= 0 {
		runtime.Timeout = providerActionTimeout
	}
	return runtime
}

// preflightProviderActions validates action identity and executable presence before any file is written.
func preflightProviderActions(plans []TargetPlan, runtime ProviderRuntime) ([]ProviderAction, error) {
	runtime = runtime.normalized()
	actions := make([]ProviderAction, 0)
	seen := make(map[string]struct{})
	for _, plan := range plans {
		for _, action := range plan.Actions {
			if err := validateProviderAction(action, runtime, seen); err != nil {
				return nil, err
			}
			actions = append(actions, action)
		}
	}
	return actions, nil
}

func validateProviderAction(action ProviderAction, runtime ProviderRuntime, seen map[string]struct{}) error {
	if err := validateProviderActionFields(action); err != nil {
		return err
	}
	if _, exists := seen[action.ID]; exists {
		return fmt.Errorf("duplicate provider action %q", action.ID)
	}
	seen[action.ID] = struct{}{}
	if _, err := runtime.LookPath(action.Program); err != nil {
		return fmt.Errorf("preflight provider action %q: executable %q not found: %w", action.ID, action.Program, err)
	}
	return nil
}

func validateProviderActionFields(action ProviderAction) error {
	if strings.TrimSpace(action.ID) == "" {
		return fmt.Errorf("provider action ID is required")
	}
	if strings.TrimSpace(action.ID) != action.ID || strings.ContainsRune(action.ID, '\x00') {
		return fmt.Errorf("provider action ID %q is invalid", action.ID)
	}
	if strings.TrimSpace(action.Program) == "" {
		return fmt.Errorf("provider action %q program is required", action.ID)
	}
	if strings.TrimSpace(action.Program) != action.Program || strings.ContainsRune(action.Program, '\x00') {
		return fmt.Errorf("provider action %q program %q is invalid", action.ID, action.Program)
	}
	for _, arg := range action.Args {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("provider action %q contains an invalid argument", action.ID)
		}
	}
	return nil
}

// executeProviderActions runs validated actions after deployment and records completed IDs.
func executeProviderActions(result DeploymentResult, actions []ProviderAction, runtime ProviderRuntime, progress func(DeploymentProgress)) (DeploymentResult, error) {
	runtime = runtime.normalized()
	for index, action := range actions {
		if progress != nil {
			progress(DeploymentProgress{Stage: "preparing", Message: "Running integration actions", Path: action.ID, Completed: index, Total: len(actions)})
		}
		ctx, cancel := context.WithTimeout(context.Background(), runtime.Timeout)
		output, err := runtime.Run(ctx, action.Program, action.Args...)
		cancel()
		if err != nil {
			detail := strings.TrimSpace(string(output))
			if detail != "" {
				return result, fmt.Errorf("provider action %q failed: %w: %s", action.ID, err, detail)
			}
			return result, fmt.Errorf("provider action %q failed: %w", action.ID, err)
		}
		result.Actions = append(result.Actions, action.ID)
	}
	return result, nil
}

// runProviderCommand runs one action with bounded combined output, preferring ctx errors on timeout.
func runProviderCommand(ctx context.Context, program string, args ...string) ([]byte, error) {
	output := &boundedOutput{remaining: providerOutputLimit}
	command := exec.CommandContext(ctx, program, args...)
	command.Stdout = output
	command.Stderr = output
	err := command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	return output.bytes, err
}

// boundedOutput keeps only the first bytes of output so failures stay readable.
type boundedOutput struct {
	bytes     []byte
	remaining int
}

// Write records bytes up to the cap while reporting full acceptance to the writer.
func (output *boundedOutput) Write(value []byte) (int, error) {
	written := len(value)
	if output.remaining > 0 {
		keep := min(output.remaining, len(value))
		output.bytes = append(output.bytes, value[:keep]...)
		output.remaining -= keep
	}
	return written, nil
}
