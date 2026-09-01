package runtime

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	skillsutil "github.com/rou-cru/takt-ai/takt/skills/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/common"
)

func TestConfirmationAcceptAndCancel(t *testing.T) {
	confirmation := common.Confirmation{Prompt: "Continue?"}
	if got := confirmation.Update(common.AcceptConfirmation{}); got != common.DecisionAccepted {
		t.Fatalf("accept decision = %v", got)
	}
	if got := confirmation.Update(common.CancelConfirmation{}); got != common.DecisionCanceled {
		t.Fatalf("cancel decision = %v", got)
	}
}

func TestAdapterLifecycleActions(t *testing.T) {
	root := t.TempDir()
	adapter := Adapter{}
	targets := []model.AgentID{model.AgentOpenCode}

	install, err := adapter.Execute(ActionRequest{Action: ActionInstall, RootDir: root, Targets: targets})
	if err != nil || len(install.Changed) == 0 {
		t.Fatalf("install = %+v, %v", install, err)
	}
	sync, err := adapter.Execute(ActionRequest{Action: ActionSync, RootDir: root, Targets: targets})
	if err != nil || len(sync.Unchanged) == 0 {
		t.Fatalf("sync = %+v, %v", sync, err)
	}
	uninstall, err := adapter.Execute(ActionRequest{Action: ActionUninstall, RootDir: root, Targets: targets})
	if err != nil || len(uninstall.Removed) == 0 {
		t.Fatalf("uninstall = %+v, %v", uninstall, err)
	}
}

func TestAdapterInstallDeploysSkills(t *testing.T) {
	root := t.TempDir()
	result, err := (Adapter{}).Execute(ActionRequest{Action: ActionInstall, RootDir: root, Targets: []model.AgentID{model.AgentOpenCode}})
	if err != nil {
		t.Fatalf("install error = %v", err)
	}
	skillPath, _ := skillsutil.FirstSkill(t)
	if !slices.Contains(result.Changed, skillPath) {
		t.Fatalf("install changed = %v, want %q", result.Changed, skillPath)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); err != nil {
		t.Fatalf("deployed skill file: %v", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	entry, ok := manifest.Entries[skillPath]
	if !ok || !slices.Contains(entry.Targets, setup.OwnershipTarget("skills")) {
		t.Fatalf("manifest entry for %q = %+v, want skills ownership", skillPath, entry)
	}
}

func TestAdapterSyncPreservesModifiedAndRedeploysDeletedSkills(t *testing.T) {
	skillPath, embedded := skillsutil.FirstSkill(t)
	adapter := Adapter{}
	targets := []model.AgentID{model.AgentOpenCode}

	t.Run("locally modified skill is preserved", func(t *testing.T) {
		root := t.TempDir()
		if _, err := adapter.Execute(ActionRequest{Action: ActionInstall, RootDir: root, Targets: targets}); err != nil {
			t.Fatalf("install error = %v", err)
		}
		local := append(append([]byte(nil), embedded...), []byte("\nlocal edit")...)
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(skillPath)), local, 0o644); err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Execute(ActionRequest{Action: ActionSync, RootDir: root, Targets: targets})
		if err != nil {
			t.Fatalf("sync error = %v", err)
		}
		if slices.Contains(result.Changed, skillPath) {
			t.Fatalf("sync changed = %v, want locally modified skill preserved", result.Changed)
		}
		current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillPath)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, local) {
			t.Fatal("sync did not preserve the locally modified skill content")
		}
	})

	t.Run("locally deleted skill is redeployed", func(t *testing.T) {
		root := t.TempDir()
		if _, err := adapter.Execute(ActionRequest{Action: ActionInstall, RootDir: root, Targets: targets}); err != nil {
			t.Fatalf("install error = %v", err)
		}
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(skillPath))); err != nil {
			t.Fatal(err)
		}
		result, err := adapter.Execute(ActionRequest{Action: ActionSync, RootDir: root, Targets: targets})
		if err != nil {
			t.Fatalf("sync error = %v", err)
		}
		if !slices.Contains(result.Changed, skillPath) {
			t.Fatalf("sync changed = %v, want redeployed %q", result.Changed, skillPath)
		}
		current, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(skillPath)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(current, embedded) {
			t.Fatal("redeployed skill content does not match the embedded skill")
		}
	})
}

func TestAdapterUninstallRemovesSkills(t *testing.T) {
	root := t.TempDir()
	adapter := Adapter{}
	targets := []model.AgentID{model.AgentOpenCode}
	if _, err := adapter.Execute(ActionRequest{Action: ActionInstall, RootDir: root, Targets: targets}); err != nil {
		t.Fatalf("install error = %v", err)
	}
	skillPath, _ := skillsutil.FirstSkill(t)
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); err != nil {
		t.Fatalf("precondition: installed skill file: %v", err)
	}
	if _, err := adapter.Execute(ActionRequest{Action: ActionUninstall, RootDir: root, Targets: targets}); err != nil {
		t.Fatalf("uninstall error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(skillPath))); !os.IsNotExist(err) {
		t.Fatalf("skill file after uninstall stat error = %v, want removed", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err == nil {
		if _, ok := manifest.Entries[skillPath]; ok {
			t.Fatalf("manifest still records %q after uninstall", skillPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("load manifest: %v", err)
	}
}

func TestActionCommandReturnsErrorMessage(t *testing.T) {
	message := Adapter{}.Command(ActionRequest{Action: ActionInstall, RootDir: t.TempDir()})()
	result, ok := message.(ActionResultMsg)
	if !ok || result.Err == nil {
		t.Fatalf("command message = %#v, want ActionResultMsg with error", message)
	}
}

func TestModelDefersFilesystemWorkUntilCommand(t *testing.T) {
	root := t.TempDir()
	runtime := NewModel()
	updated, command := runtime.Update(ActionRequest{Action: ActionInstall, RootDir: root, Targets: []model.AgentID{model.AgentOpenCode}})
	if command == nil {
		t.Fatal("Update() returned no action command")
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Update() performed filesystem work: %v", err)
	}
	state := updated.(Model)
	if !state.Busy {
		t.Fatal("model is not busy while command is pending")
	}
	message := command()
	updated, _ = state.Update(message)
	state = updated.(Model)
	if state.Busy || state.Presentation.Err != nil {
		t.Fatalf("completed state = %+v", state)
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-manifest.json")); err != nil {
		t.Fatalf("command did not perform action: %v", err)
	}
}

func TestAdapterExecuteCarriesComponents(t *testing.T) {
	root := t.TempDir()
	if _, err := (Adapter{}).Execute(ActionRequest{
		Action:     ActionInstall,
		RootDir:    root,
		Targets:    []model.AgentID{model.AgentOpenCode},
		Components: []string{"theme"},
	}); err != nil {
		t.Fatalf("install with components error = %v", err)
	}
	config, err := os.ReadFile(filepath.Join(root, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatalf("read opencode.json: %v", err)
	}
	if !bytes.Contains(config, []byte(`"theme": "takt-kanagawa"`)) {
		t.Fatalf("opencode.json missing component merge:\n%s", config)
	}

	plain, err := (Adapter{}).Execute(ActionRequest{Action: ActionSync, RootDir: root, Targets: []model.AgentID{model.AgentOpenCode}})
	if err != nil {
		t.Fatalf("sync without components error = %v", err)
	}
	if len(plain.Unchanged) == 0 {
		t.Fatalf("sync without components changed %v, want byte-identical redeploy", plain.Changed)
	}
}
