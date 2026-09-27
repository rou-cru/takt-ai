package models

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeTargetLoadsModelsAndSurfacesFailures(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			assertOpenCodeDiscoveryOutcome(t, fail)
		})
	}
}

// assertOpenCodeDiscoveryOutcome stubs the opencode binary to either report
// its models or fail, then drives the discovery model through init, load and
// selection, checking the loading/error state and final view match fail.
func assertOpenCodeDiscoveryOutcome(t *testing.T, fail bool) {
	t.Helper()
	root := t.TempDir()
	bin := t.TempDir()
	script := `#!/bin/sh
case "$*" in
  'api GET /api/model') printf '%s' '{"location":{},"data":[{"providerID":"opencode","modelID":"big-pickle","limit":{"context":1000,"output":100}},{"providerID":"provider","modelID":"jk-model","limit":{"context":2000,"output":200}}]}' ;;
  *) echo "unexpected OpenCode invocation: $*" >&2; exit 1 ;;
esac
`
	if fail {
		script = "#!/bin/sh\necho unavailable >&2\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	cmd := m.Init()
	if cmd == nil || !m.picker.Loading {
		t.Fatal("discovery was not started")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if m.picker.Loading || (m.picker.LoadErr != nil) != fail {
		t.Fatalf("wrong discovery result: loading=%v error=%v, want failure=%v", m.picker.Loading, m.picker.LoadErr, fail)
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = next.(Model)
	want := "opencode/big-pickle"
	if fail {
		want = "unavailable"
	}
	if !strings.Contains(m.View().Content, want) {
		t.Fatal(m.View().Content)
	}
}
