package setup_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestRecordRiskAcceptancesMarksConflictAccepted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("already here"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	plans := simplePlan("hello")

	conflicts, err := setup.DetectConflicts(root, plans)
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("DetectConflicts() = %+v, %v, want exactly one conflict", conflicts, err)
	}
	conflict := conflicts[0]
	if conflict.Accepted {
		t.Fatal("conflict.Accepted = true before recording it, want false")
	}

	err = setup.RecordRiskAcceptances(root, []setup.RiskAcceptance{
		{Path: conflict.Path, SHA256: conflict.SHA256, Impact: conflict.Impact},
	})
	if err != nil {
		t.Fatalf("RecordRiskAcceptances() error = %v", err)
	}

	conflicts, err = setup.DetectConflicts(root, plans)
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("DetectConflicts() after acceptance = %+v, %v, want exactly one conflict", conflicts, err)
	}
	if !conflicts[0].Accepted {
		t.Error("conflict.Accepted = false after RecordRiskAcceptances, want true")
	}
}

func TestRecordRiskAcceptancesEmptyIsNoop(t *testing.T) {
	if err := setup.RecordRiskAcceptances(t.TempDir(), nil); err != nil {
		t.Errorf("RecordRiskAcceptances(nil) error = %v, want nil", err)
	}
}
