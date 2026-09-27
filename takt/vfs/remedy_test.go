package vfs

import (
	"errors"
	"strings"
	"testing"
)

// Exercise actual refusals, not a parallel table of error strings.
func TestDenialsExplainResponsibleNextAction(t *testing.T) {
	f, _, _ := durable(t)
	key := bind(t, f, "author", "unit", "dev", "owned.go")
	apply(t, f, key, "write", 0, OpCreate, "owned.go", "content")
	cases := []struct {
		name      string
		op        Operation
		want      error
		fragments []string
	}{
		{"unknown binding", Operation{Key: "missing", CallID: "unknown", Action: OpRead}, ErrIdentity, []string{"orchestrator", "binding"}},
		{"stale", Operation{Key: key, CallID: "stale", Action: OpRead, Path: "owned.go"}, ErrStaleRevision, []string{"current revision 1", "fresh call", "re-read"}},
		{"scope", Operation{Key: key, CallID: "scope", ExpectedRevision: 1, Action: OpRead, Path: "other.go"}, ErrScopeDenied, []string{"orchestrator", "scope"}},
		{"protected", Operation{Key: key, CallID: "protected", ExpectedRevision: 1, Action: OpRead, Path: ".git/config"}, ErrInvalidPath, []string{"workspace-relative", "protected"}},
		{"replay", Operation{Key: key, CallID: "write", ExpectedRevision: 1, Action: OpRead, Path: "owned.go"}, ErrDuplicateCall, []string{"prior", "fresh call"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.Apply(tc.op)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v; want %v", err, tc.want)
			}
			for _, fragment := range tc.fragments {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("denial lacks %q: %v", fragment, err)
				}
			}
		})
	}
	_, err := f.Bind(Identity{SessionID: "s", WorkUnitID: "other", AgentID: "other", Specialist: "dev"}, []string{"owned.go"})
	var collision *CollisionError
	if !errors.Is(err, ErrCollision) || !errors.As(err, &collision) {
		t.Fatalf("lost collision identity: %v", err)
	}
	if collision.RequestedPath != "owned.go" || !strings.Contains(err.Error(), "orchestrator") || !strings.Contains(err.Error(), "release") {
		t.Fatalf("collision lacks coordination: %v", err)
	}
	if stagedCount(f, key) != 1 {
		t.Fatal("denial changed staged files")
	}
}
