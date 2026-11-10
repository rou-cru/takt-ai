package memory

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout bounds each git query so a hung repository cannot stall a caller.
const gitTimeout = 2 * time.Second

// WorkspaceKey is the single definition of the workspace key of dir: the git origin's
// repository name, else the repository top level's name, else the directory's name, normalized.
func WorkspaceKey(ctx context.Context, dir string) string {
	if out, err := git(ctx, dir, "remote", "get-url", "origin"); err == nil {
		parts := strings.FieldsFunc(strings.TrimSuffix(out, ".git"), func(r rune) bool { return r == '/' || r == ':' })
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			return normalizeKey(parts[len(parts)-1])
		}
	}
	if out, err := git(ctx, dir, "rev-parse", "--show-toplevel"); err == nil && out != "" {
		return normalizeKey(filepath.Base(out))
	}
	base := filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "unknown"
	}
	return normalizeKey(base)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, gitPath, append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func normalizeKey(p string) string {
	n := strings.TrimSpace(strings.ToLower(p))
	if n == "" {
		return "unknown"
	}
	return n
}
