package memory_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestWorkspaceKey(t *testing.T) {
	root := shortDir(t)
	remote := filepath.Join(root, "checkout")
	topLevel := filepath.Join(root, "Top-Level")
	plain := filepath.Join(root, "Plain_Dir")
	for _, d := range []string{remote, filepath.Join(topLevel, "sub"), plain} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, remote, "init", "-q")
	runGit(t, remote, "remote", "add", "origin", "git@github.com:Org/My_Repo.git")
	runGit(t, topLevel, "init", "-q")

	cases := []struct{ name, dir, want string }{
		{"git remote", remote, "my_repo"},
		{"repository top level", filepath.Join(topLevel, "sub"), "top-level"},
		{"plain directory", plain, "plain_dir"},
	}
	for _, c := range cases {
		if got := memory.WorkspaceKey(context.Background(), c.dir); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
