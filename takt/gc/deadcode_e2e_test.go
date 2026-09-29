package gc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
)

// This fixture executes the module-pinned x/tools RTA, not a caller-count model.
// Building the pinned tool is ordinary test preparation, before analysis. Both
// build and analysis have downloads disabled; missing cached dependencies fail.
func TestDeadChainAndMutualRecursionRemainDead(t *testing.T) {
	if testing.Short() {
		t.Skip("real production RTA fixture")
	}
	deadcode := os.Getenv("TAKT_E2E_DEADCODE")
	if deadcode == "" {
		t.Skip("real production RTA fixture requires the container-prepared deadcode binary")
	}
	root := t.TempDir()
	tool, err := os.ReadFile(deadcode)
	if err != nil {
		t.Fatalf("read prepared deadcode: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "deadcode"), tool, 0o755); err != nil {
		t.Fatalf("copy prepared deadcode: %v", err)
	}
	writeFixture(t, root, "go.mod", "module fixture\n\ngo 1.25\n")
	writeFixture(t, root, "main.go", `package main
type worker interface { Work() }
type productive struct{}
func (productive) Work() { helper() }
func invoke(w worker) { w.Work() }
func callback() { helper() }
func dispatch(f func()) { f() }
func helper() {}
func main() { invoke(productive{}); dispatch(callback) }
func init() { helper() }
func head() { tail() }
func tail() {}
func left() { right() }
func right() { left() }
func OnlyTest() {}
type idle struct{}
func (idle) Unused() {}
`)
	writeFixture(t, root, "main_test.go", "package main\nimport \"testing\"\nfunc TestOnly(t *testing.T) { OnlyTest() }\n")
	goVersion, err := exec.Command("go", "version").Output()
	if err != nil {
		t.Fatalf("resolve Go toolchain version: %v", err)
	}
	versionFields := strings.Fields(string(goVersion))
	if len(versionFields) < 3 {
		t.Fatalf("unexpected go version output: %q", goVersion)
	}
	writeFixture(t, root, ".takt/gc.json", `{"version":1,"locks":["go.mod"],"checks":[["go","test","./..."]],"analyzers":[{"language":"go","mandate":"dead-code","tool":"deadcode","version":"`+versionFields[2]+`","command":["./deadcode","-json","./..."],"version_command":["go","version","./deadcode"]}]}`)
	p, err := gc.Prepare(context.Background(), root, t.TempDir(), "s")
	if err != nil {
		t.Fatal(err)
	}
	// New declarations remain pending. Existing dead symbols in the same edited
	// file remain dead, rather than receiving blanket file-level protection.
	f, err := os.OpenFile(filepath.Join(root, "main.go"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.WriteString("func fresh() {}\n")
	closeErr := f.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	plan := deadCodePlan("main.go", "main_test.go")
	plan.Delta = []gc.Change{{Path: "main.go"}}
	r, err := gc.AnalyzePrepared(context.Background(), root, plan, p)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"head": gc.StatusDead, "tail": gc.StatusDead, "left": gc.StatusDead, "right": gc.StatusDead, "OnlyTest": gc.StatusDead, "idle::Unused": gc.StatusDead, "fresh": gc.StatusPending}
	if len(r.Findings) != len(want) {
		t.Fatalf("RTA findings: %+v", r.Findings)
	}
	for _, finding := range r.Findings {
		if want[finding.Symbol] != finding.Status {
			t.Errorf("unexpected reachability/status: %+v", finding)
		}
		t.Logf("%s %s", finding.ID, finding.Status)
	}
	if _, err := os.Stat(filepath.Join(root, ".codegraph")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("RTA initialized CodeGraph")
	}
	// No main roots is an explicit analysis error, not an empty success.
	writeFixture(t, root, "main.go", "package library\nfunc Unused() {}\n")
	if err := os.Remove(filepath.Join(root, "main_test.go")); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.AnalyzePrepared(context.Background(), root, plan, p); err == nil {
		t.Fatal("library without production roots became empty success")
	} else {
		t.Logf("missing roots correctly rejected: %v", err)
	}
}
