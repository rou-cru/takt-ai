package setup_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

func TestIsCurrentVersion(t *testing.T) {
	if !setup.IsCurrentVersion(setup.BuildVersion) {
		t.Errorf("IsCurrentVersion(%q) = false, want true", setup.BuildVersion)
	}
	if setup.IsCurrentVersion("some-other-version") {
		t.Error("IsCurrentVersion(other) = true, want false")
	}
}

func TestUpgradeAvailable(t *testing.T) {
	tests := []struct {
		build, installed string
		want             bool
	}{
		{"0.0.27", "0.0.25", true},
		{"0.0.27", "", true},
		{"0.0.27", "0.0.27", false},
		{"0.0.25", "0.0.27", false},
		{"dev", "0.0.25", false},
	}
	previous := setup.BuildVersion
	t.Cleanup(func() { setup.BuildVersion = previous })
	for _, tt := range tests {
		setup.BuildVersion = tt.build
		if got := setup.UpgradeAvailable(tt.installed); got != tt.want {
			t.Errorf("build %q: UpgradeAvailable(%q) = %v, want %v", tt.build, tt.installed, got, tt.want)
		}
	}
}

func TestIsInstalled(t *testing.T) {
	root := t.TempDir()
	if setup.IsInstalled(root) {
		t.Error("IsInstalled() on a fresh root = true, want false")
	}
	if err := setup.SaveInstalledConfig(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	if !setup.IsInstalled(root) {
		t.Error("IsInstalled() after SaveInstalledConfig = false, want true")
	}
}

func TestForgetInstallationRemovesRecords(t *testing.T) {
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	if err := setup.RecordRiskAcceptances(root, []setup.RiskAcceptance{{Path: "a.txt", SHA256: "x", Impact: setup.ImpactUncertain}}); err != nil {
		t.Fatalf("RecordRiskAcceptances() error = %v", err)
	}

	if err := setup.ForgetInstallation(root); err != nil {
		t.Fatalf("ForgetInstallation() error = %v", err)
	}
	if setup.IsInstalled(root) {
		t.Error("IsInstalled() after ForgetInstallation = true, want false")
	}
}

func TestForgetInstallationOnFreshRootIsNoop(t *testing.T) {
	if err := setup.ForgetInstallation(t.TempDir()); err != nil {
		t.Fatalf("ForgetInstallation() on a fresh root error = %v, want nil", err)
	}
}
