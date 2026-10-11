package vfs_test

import (
	"errors"
	"github.com/rou-cru/takt-ai/takt/vfs"
	"strings"
	"testing"
)

// Exercise actual refusals, not a parallel table of error strings.
func TestDenialsExplainResponsibleNextAction(t *testing.T) {
	f, _, _ := newStore(t)
	key := bind(t, f, "author", "unit", "dev", "owned.go")
	apply(t, f, applyCase{key, "write", 0, vfs.OpCreate, "owned.go", "content"})
	cases := []struct {
		name      string
		op        vfs.Operation
		want      error
		fragments []string
	}{
		{"unknown binding", vfs.Operation{Key: "missing", CallID: "unknown", Action: vfs.OpRead}, vfs.ErrIdentity, []string{"orchestrator", "binding"}},
		{"stale", vfs.Operation{Key: key, CallID: "stale", Action: vfs.OpRead, Path: "owned.go"}, vfs.ErrStaleRevision, []string{"current revision 1", "fresh call", "re-read"}},
		{"scope", vfs.Operation{Key: key, CallID: "scope", ExpectedRevision: 1, Action: vfs.OpRead, Path: "other.go"}, vfs.ErrScopeDenied, []string{"orchestrator", "scope"}},
		{"protected", vfs.Operation{Key: key, CallID: "protected", ExpectedRevision: 1, Action: vfs.OpRead, Path: ".git/config"}, vfs.ErrInvalidPath, []string{"workspace-relative", "protected"}},
		{"replay", vfs.Operation{Key: key, CallID: "write", ExpectedRevision: 1, Action: vfs.OpRead, Path: "owned.go"}, vfs.ErrDuplicateCall, []string{"prior", "fresh call"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertDenialExplainsNextAction(t, f, tc.op, tc.want, tc.fragments)
		})
	}
	assertBindCollisionExplainsCoordination(t, f)
	if stagedCount(f, key) != 1 {
		t.Fatal("denial changed staged files")
	}
}

// assertDenialExplainsNextAction applies op and checks the resulting error
// both matches want and mentions every fragment, so the message tells the
// caller what to do next rather than just that it failed.
func assertDenialExplainsNextAction(t *testing.T, f *vfs.FS, op vfs.Operation, want error, fragments []string) {
	t.Helper()
	_, err := f.Apply(op)
	if !errors.Is(err, want) {
		t.Fatalf("got %v; want %v", err, want)
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("denial lacks %q: %v", fragment, err)
		}
	}
}

// assertBindCollisionExplainsCoordination proves a colliding Bind against an
// already-owned path preserves the collision identity and tells the caller
// to continue existing work or choose a non-colliding scope.
func assertBindCollisionExplainsCoordination(t *testing.T, f *vfs.FS) {
	t.Helper()
	_, err := f.Bind(vfs.Identity{SessionID: "s", WorkUnitID: "other", AgentID: "other", Specialist: "dev"}, []string{"owned.go"})
	var collision *vfs.CollisionError
	if !errors.Is(err, vfs.ErrCollision) || !errors.As(err, &collision) {
		t.Fatalf("lost collision identity: %v", err)
	}
	if collision.RequestedPath != "owned.go" || !strings.Contains(err.Error(), "claim_list") || !strings.Contains(err.Error(), "author_key") {
		t.Fatalf("collision lacks coordination: %v", err)
	}
}
