package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

const (
	incompatibleConfigPath = ".config/opencode/opencode.json"
	otherArtifactPath      = ".config/opencode/agents/takt-dev.md"
	brokenConfig           = "not json"
)

func gatePlans(config string) []setup.TargetPlan {
	return []setup.TargetPlan{{
		Target:       "opencode",
		ManagedPaths: []string{incompatibleConfigPath, otherArtifactPath},
		Artifacts: []setup.Artifact{
			{Path: incompatibleConfigPath, Content: []byte(config)},
			{Path: otherArtifactPath, Content: []byte("dev\n")},
		},
	}}
}

// installedWithBrokenConfig installs gatePlans and then breaks the config
// by hand, leaving an incompatible conflict for the next update.
func installedWithBrokenConfig(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := setup.ApplyContext(context.Background(), root, gatePlans("{\"a\": 1}\n"), setup.ProviderRuntime{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(incompatibleConfigPath)), []byte(brokenConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRejectKeptIncompatibleAbortsWithoutWriting(t *testing.T) {
	for name, syncing := range map[string]bool{"install keeps preserved path": false, "sync keeps edited path": true} {
		t.Run(name, func(t *testing.T) {
			root := installedWithBrokenConfig(t)
			var preserve []string
			if !syncing {
				preserve = []string{incompatibleConfigPath}
			}
			before, err := os.ReadFile(filepath.Join(root, setup.OwnershipManifestFilename))
			if err != nil {
				t.Fatal(err)
			}

			err = setup.RejectKeptIncompatible(root, gatePlans("{\"a\": 2}\n"), syncing, preserve...)
			if err == nil || !strings.Contains(err.Error(), incompatibleConfigPath) {
				t.Fatalf("RejectKeptIncompatible() error = %v, want one naming %s", err, incompatibleConfigPath)
			}
			got, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(incompatibleConfigPath)))
			if readErr != nil || string(got) != brokenConfig {
				t.Fatalf("config after refusal = %q, %v, want it untouched", got, readErr)
			}
			after, err := os.ReadFile(filepath.Join(root, setup.OwnershipManifestFilename))
			if err != nil || string(before) != string(after) {
				t.Fatal("refusal changed the ownership manifest")
			}
		})
	}
}

func TestRejectKeptIncompatibleAllowsRestoreAndUnrelatedKeeps(t *testing.T) {
	root := installedWithBrokenConfig(t)
	next := gatePlans("{\"a\": 2}\n")
	if err := setup.RejectKeptIncompatible(root, next, false); err != nil {
		t.Fatalf("restoring Takt's version (nothing preserved) = %v, want nil", err)
	}
	if err := setup.RejectKeptIncompatible(root, next, false, otherArtifactPath); err != nil {
		t.Fatalf("preserving an unrelated path = %v, want nil", err)
	}
}

func TestRejectKeptIncompatibleWithoutConflictsIsNil(t *testing.T) {
	root := t.TempDir()
	plans := gatePlans("{\"a\": 1}\n")
	if _, err := setup.ApplyContext(context.Background(), root, plans, setup.ProviderRuntime{}); err != nil {
		t.Fatal(err)
	}
	if err := setup.RejectKeptIncompatible(root, plans, true, incompatibleConfigPath); err != nil {
		t.Fatalf("RejectKeptIncompatible() with no conflicts = %v, want nil", err)
	}
}

func TestRejectKeptIncompatibleJudgesConfigAsTheMergeReadsIt(t *testing.T) {
	for edited, wantReject := range map[string]bool{
		"{ // comment\n}":       false,
		"{\n// c\n\"a\": 1,\n}": false,
		"[1]":                   true,
	} {
		root := installedWithBrokenConfig(t)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(incompatibleConfigPath)), []byte(edited), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := setup.RejectKeptIncompatible(root, gatePlans("{\"a\": 2}\n"), true); (err != nil) != wantReject {
			t.Errorf("keeping %q: RejectKeptIncompatible() = %v, want rejection %v", edited, err, wantReject)
		}
	}
}
