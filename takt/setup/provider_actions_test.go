package setup_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestApplyContextRunsProviderActionsAfterDeployment(t *testing.T) {
	root := t.TempDir()
	var ran []string
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("hello")}},
		Actions:      []setup.ProviderAction{{ID: "notify", Program: "notify-cmd", Args: []string{"--flag"}}},
	}}
	runtime := setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/usr/bin/" + program, nil },
		Run: func(_ context.Context, program string, args ...string) ([]byte, error) {
			ran = append(ran, program)
			return nil, nil
		},
	}

	result, err := setup.ApplyContext(context.Background(), root, plans, runtime)
	if err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if len(result.Actions) != 1 || result.Actions[0] != "notify" {
		t.Fatalf("ApplyContext() Actions = %v, want [notify]", result.Actions)
	}
	if len(ran) != 1 || ran[0] != "notify-cmd" {
		t.Fatalf("provider commands run = %v, want [notify-cmd]", ran)
	}
}

func TestApplyContextFailsWhenActionExecutableIsMissing(t *testing.T) {
	root := t.TempDir()
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("hello")}},
		Actions:      []setup.ProviderAction{{ID: "notify", Program: "does-not-exist"}},
	}}
	runtime := setup.ProviderRuntime{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
	}

	if _, err := setup.ApplyContext(context.Background(), root, plans, runtime); err == nil {
		t.Fatal("ApplyContext() with a missing action executable error = nil, want an error")
	}
}

func TestApplyContextFailsOnDuplicateActionID(t *testing.T) {
	root := t.TempDir()
	action := setup.ProviderAction{ID: "notify", Program: "notify-cmd"}
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt", "b.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("hello")}, {Path: "b.txt", Content: []byte("world")}},
		Actions:      []setup.ProviderAction{action, action},
	}}
	runtime := setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/usr/bin/" + program, nil },
	}

	if _, err := setup.ApplyContext(context.Background(), root, plans, runtime); err == nil {
		t.Fatal("ApplyContext() with a duplicate action ID error = nil, want an error")
	}
}

func TestApplyContextPropagatesActionFailure(t *testing.T) {
	root := t.TempDir()
	plans := []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{"a.txt"},
		Artifacts:    []setup.Artifact{{Path: "a.txt", Content: []byte("hello")}},
		Actions:      []setup.ProviderAction{{ID: "notify", Program: "notify-cmd"}},
	}}
	runtime := setup.ProviderRuntime{
		LookPath: func(program string) (string, error) { return "/usr/bin/" + program, nil },
		Run: func(context.Context, string, ...string) ([]byte, error) {
			return []byte("boom detail"), errors.New("action failed")
		},
	}

	_, err := setup.ApplyContext(context.Background(), root, plans, runtime)
	if err == nil {
		t.Fatal("ApplyContext() with a failing action error = nil, want an error")
	}
}
