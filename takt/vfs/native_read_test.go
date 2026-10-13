package vfs_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestAuthorNativeInspectionsRespectOwnershipAndNativeRoles(t *testing.T) {
	f, root, state := newStore(t)
	for _, path := range []string{"impl.go", "tests.go"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(path+" base"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	implementer, implKey := delegate(t, f, state, testSession, "dev", "impl", "impl.go")
	write(t, f, implementer, "write-impl", "impl.go", "implementation staged")
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests.go")
	if err := f.StartNativeInspection(author, "foreign", "read", "impl.go"); err == nil {
		t.Fatal("foreign physical read admitted")
	}
	if err := f.StartNativeInspection(author, "own", "read", "tests.go"); err != nil {
		t.Fatal(err)
	}
	if err := f.EndNativeInspection(author, "own"); err != nil {
		t.Fatal(err)
	}
	for _, agent := range []vfs.AgentID{"verify", "qa", "mutation", "quality", "explorer", vfs.OrchestratorInstance} {
		c := vfs.Caller{SessionID: testSession, HostSession: "native-" + string(agent), Agent: agent}
		if err := f.StartNativeInspection(c, "native-"+string(agent), "read", "impl.go"); err != nil {
			t.Fatalf("%s native read: %v", agent, err)
		}
		if len(f.NativeInspections()) != 0 {
			t.Fatalf("%s was isolated", agent)
		}
	}
	reviewer, _ := judge(t, f, state, testSession, "verify", "gate", implKey)
	if got := readAs(t, f, reviewer, "read-gate", "impl.go", implKey); string(got.Content) != "implementation staged" {
		t.Fatalf("gate read = %q", got.Content)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := f.StartNativeInspection(author, "alias", "read", filepath.Join(alias, "impl.go")); err == nil {
		t.Fatal("workspace alias bypassed ownership")
	}
	if err := f.DiscardAs(orchestrator(testSession), implKey); err != nil {
		t.Fatal(err)
	}
	if err := f.StartNativeInspection(author, "released", "read", "impl.go"); err != nil {
		t.Fatalf("released read: %v", err)
	}
	if err := f.EndNativeInspection(author, "released"); err != nil {
		t.Fatal(err)
	}
}

func TestInspectionOwnershipIsSerializedThroughCompletion(t *testing.T) {
	f, _, state := newStore(t)
	author, _ := delegate(t, f, state, testSession, "test", "tests", "tests.go")
	other := ident("dev", "implementation", "dev")
	plan, err := f.PrepareShell(author, "before-claim", "cat impl.go", state)
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.AssignScope(other, []string{"impl.go"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ContinueShell(author, "before-claim", "start"); err == nil {
		t.Fatal("ownership changed without start refusal")
	}
	if err := f.ReleaseShell(author, "before-claim"); err != nil {
		t.Fatal(err)
	}
	plan, err = f.PrepareShell(author, "inspection", "cat impl.go", state)
	if err != nil || !slices.Contains(plan.Private, filepath.Join(plan.Cwd, "impl.go")) {
		t.Fatalf("foreign claim absent from denyRead: %+v, %v", plan, err)
	}
	if _, err := f.ContinueShell(author, "inspection", "start"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ContinueShell(author, "inspection", "evaluate"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.AssignScope(ident("dev", "new-unit", "dev"), []string{"new.go"}); err == nil || !strings.Contains(err.Error(), "retry claim_assign") {
		t.Fatalf("conflicting claim not deferred: %v", err)
	}
	if err := f.ReassignScope(other, key, []string{"impl.go", "new.go"}); err == nil {
		t.Fatal("reassignment introduced an exposed claim")
	}
	if err := f.ReleaseClaim(testSession, key, false); err != nil {
		t.Fatal(err)
	}
	// Reclaiming an already denied path cannot introduce a read conflict.
	if _, err := f.AssignScope(other, []string{"impl.go"}); err != nil {
		t.Fatal(err)
	}
	if err := f.ReleaseShell(author, "inspection"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.AssignScope(ident("dev", "new-unit", "dev"), []string{"new.go"}); err != nil {
		t.Fatal(err)
	}
}
