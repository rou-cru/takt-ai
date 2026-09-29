package gc

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestGuardDenialsExplainCoordination(t *testing.T) {
	c := &Coordinator{Cycle: &Cycle{Phase: "verify"}}
	cases := []struct {
		name      string
		err       error
		fragments []string
	}{
		{"collector bind", guardVFSBind(c, "bind", vfs.Identity{CycleID: "cycle"}), []string{"coordinator", "request"}},
		{"inactive", guardVFSCycle(c, "missing"), []string{"orchestrator", "active cycle"}},
		{"verdict", guardVFSCommand(c, "verify", "", ""), []string{"coordinator", "submit"}},
		{"phase", guardVFSCommand(c, "op", "write", "x"), []string{"orchestrator", "collect"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err == nil {
				t.Fatal("expected denial")
			}
			for _, fragment := range tc.fragments {
				if !strings.Contains(tc.err.Error(), fragment) {
					t.Errorf("denial lacks %q: %v", fragment, tc.err)
				}
			}
		})
	}
}
