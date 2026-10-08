package vfs_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestRefreshRejectsChangedSpecialist(t *testing.T) {
	for _, method := range []string{"Bind", "AssignScope"} {
		t.Run(method, func(t *testing.T) {
			f, err := vfs.Open(t.TempDir(), filepath.Join(t.TempDir(), "state"))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := f.Close(); err != nil {
					t.Errorf("close filesystem: %v", err)
				}
			})
			identity := vfs.Identity{SessionID: "s", WorkUnitID: "u", AgentID: "agent", Specialist: "dev"}
			key, err := f.Bind(identity, []string{"a.go"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Apply(vfs.Operation{Key: key, CallID: "create", Action: vfs.OpCreate, Path: "a.go", Content: []byte("x")}); err != nil {
				t.Fatal(err)
			}
			before, claims := f.InspectDelta(key), f.OwnershipClaims("s")
			refresh := f.Bind
			if method == "AssignScope" {
				refresh = f.AssignScope
			}
			for _, specialist := range []string{"fix", "pm", "", "unknown"} {
				changed := identity
				changed.Specialist = specialist
				got, err := refresh(changed, []string{"a.go"})
				if got != "" || !errors.Is(err, vfs.ErrScopeDenied) {
					t.Fatalf("%q: key=%q err=%v", specialist, got, err)
				}
				if !reflect.DeepEqual(before, f.InspectDelta(key)) || !reflect.DeepEqual(claims, f.OwnershipClaims("s")) {
					t.Fatal("denial changed state")
				}
			}
			got, err := refresh(identity, []string{"a.go"})
			if err != nil || got != key {
				t.Fatalf("refresh key=%q err=%v", got, err)
			}
			if !reflect.DeepEqual(before, f.InspectDelta(key)) || !reflect.DeepEqual(claims, f.OwnershipClaims("s")) {
				t.Fatal("refresh changed state")
			}
		})
	}
}
