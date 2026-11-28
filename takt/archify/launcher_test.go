package archify_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// fakeUpstream records each invocation and answers check like Archify does:
// a JSON report whose follow-up task commands name the CLI by absolute path.
const fakeUpstream = `import { appendFileSync, existsSync, mkdirSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const args = process.argv.slice(2);
appendFileSync(process.env.FAKE_LOG, JSON.stringify({ args, update: process.env.ARCHIFY_UPDATE_CHECK_DISABLED ?? null }) + '\n');
const value = (flag) => args[args.indexOf(flag) + 1];
if (args[0] === 'code-analysis' && args[1] === 'check') {
  mkdirSync(value('--out'), { recursive: true });
  const task = 'node ' + fileURLToPath(import.meta.url) + ' code-analysis task ' + value('--out') + ' --root ' + args[2] + ' --format json --finding F1';
  console.log(JSON.stringify({ out: value('--out'), investigation: { leads: [{ id: 'F1', task }] } }));
} else if (args[0] === 'code-analysis' && args[1] === 'task') {
  process.exit(existsSync(args[2]) ? 0 : 3);
}
`

type invocation struct {
	Args   []string `json:"args"`
	Update *string  `json:"update"`
}

type launcherHome struct {
	t       *testing.T
	home    string
	tmp     string
	log     string
	command string // the launcher as the prompt names it
}

func newLauncherHome(t *testing.T) launcherHome {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	home := t.TempDir()
	launcher, err := fs.ReadFile(catalog.AssetFS(), "skills/takt-archify/scripts/archify.mjs")
	if err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		model.ArchifyLauncherRelPath: string(launcher),
		model.ArchifyBinaryRelPath:   fakeUpstream,
	} {
		target := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return launcherHome{t: t, home: home, tmp: t.TempDir(), log: filepath.Join(home, "calls.jsonl"), command: "node ~/" + model.ArchifyLauncherRelPath}
}

// run executes a command line as an agent's shell would, with ~ as home.
func (h launcherHome) run(command string) (stdout, stderr string, code int) {
	h.t.Helper()
	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Dir = h.home
	cmd.Env = append(os.Environ(), "HOME="+h.home, "TMPDIR="+h.tmp, "FAKE_LOG="+h.log)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return out.String(), errOut.String(), exit.ExitCode()
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return out.String(), errOut.String(), 0
}

func (h launcherHome) calls() []invocation {
	h.t.Helper()
	raw, err := os.ReadFile(h.log)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		h.t.Fatal(err)
	}
	var calls []invocation
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var c invocation
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			h.t.Fatal(err)
		}
		calls = append(calls, c)
	}
	return calls
}

func flagValue(args []string, flag string) string {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func TestLauncherOwnsArchifyAnalysisOutput(t *testing.T) {
	t.Run("check writes to a fresh private run and prints runnable follow-ups", func(t *testing.T) {
		h := newLauncherHome(t)
		stdout, stderr, code := h.run(h.command + " code-analysis check .")
		if code != 0 {
			t.Fatalf("check exit = %d, stderr %q", code, stderr)
		}
		calls := h.calls()
		if len(calls) != 1 {
			t.Fatalf("upstream calls = %d, want 1", len(calls))
		}
		args := calls[0].Args
		out := flagValue(args, "--out")
		if filepath.Dir(out) != h.tmp || !strings.HasPrefix(filepath.Base(out), "takt-archify-") {
			t.Fatalf("--out = %q, want a takt-archify- run directly under TMPDIR %s", out, h.tmp)
		}
		if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("run directory %s = %v, %v; want private (0700)", out, info, err)
		}
		if flagValue(args, "--fail-on") != "never" || !strings.Contains(strings.Join(args, " "), "--json") {
			t.Fatalf("check args = %q, want --json and --fail-on never", args)
		}
		if calls[0].Update == nil || *calls[0].Update != "1" {
			t.Fatalf("ARCHIFY_UPDATE_CHECK_DISABLED = %v, want 1", calls[0].Update)
		}
		var report struct {
			Investigation struct {
				Leads []struct{ Task string } `json:"leads"`
			} `json:"investigation"`
		}
		if err := json.Unmarshal([]byte(stdout), &report); err != nil || len(report.Investigation.Leads) != 1 {
			t.Fatalf("check stdout = %q (%v), want the upstream report", stdout, err)
		}
		task := report.Investigation.Leads[0].Task
		if !strings.HasPrefix(task, h.command+" code-analysis task "+out+" ") {
			t.Fatalf("lead task = %q, want it rewritten to %q", task, h.command)
		}
		if _, stderr, code := h.run(task); code != 0 {
			t.Fatalf("printed task exit = %d, stderr %q", code, stderr)
		}
	})
	t.Run("a task whose run is gone analyzes again", func(t *testing.T) {
		h := newLauncherHome(t)
		gone := filepath.Join(h.tmp, "takt-archify-gone")
		_, stderr, code := h.run(h.command + " code-analysis task " + gone + " --root . --format json --finding F1")
		if code != 0 {
			t.Fatalf("task exit = %d, stderr %q", code, stderr)
		}
		calls := h.calls()
		if len(calls) != 2 || calls[0].Args[1] != "check" || calls[1].Args[1] != "task" {
			t.Fatalf("upstream calls = %+v, want check then task", calls)
		}
		if rerun := calls[1].Args[2]; rerun != flagValue(calls[0].Args, "--out") || rerun == gone {
			t.Fatalf("task read %q, want the fresh run %q", rerun, flagValue(calls[0].Args, "--out"))
		}
	})
	t.Run("drift writes into a new directory the launcher chose", func(t *testing.T) {
		h := newLauncherHome(t)
		if _, stderr, code := h.run(h.command + " code-analysis drift . --base a --head b --ir ir.json"); code != 0 {
			t.Fatalf("drift exit = %d, stderr %q", code, stderr)
		}
		out := flagValue(h.calls()[0].Args, "--out")
		if !strings.HasPrefix(out, filepath.Join(h.tmp, "takt-archify-")) {
			t.Fatalf("drift --out = %q, want a run under %s", out, h.tmp)
		}
		if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("drift --out %s exists before drift writes it: %v", out, err)
		}
	})
	for _, refused := range []string{"check . --out ./report", "check . --out=./report", "task x --finding F1 --out t.json", "drift . --out d",
		"check . --update-baseline", "check . --baseline b.json --accept F1 --reason ok", "check . --baseline b.json --revoke F1"} {
		t.Run("refuses "+refused, func(t *testing.T) {
			h := newLauncherHome(t)
			_, stderr, code := h.run(h.command + " code-analysis " + refused)
			if code != 2 || !strings.HasPrefix(stderr, "Takt: ") {
				t.Fatalf("exit = %d, stderr %q; want 2 with Takt's reason", code, stderr)
			}
			if calls := h.calls(); len(calls) != 0 {
				t.Fatalf("upstream ran %+v for a refused command", calls)
			}
		})
	}
	t.Run("a refused baseline change names the command to run with approval", func(t *testing.T) {
		h := newLauncherHome(t)
		_, stderr, _ := h.run(h.command + " code-analysis check . --update-baseline")
		upstream := filepath.Join(h.home, filepath.FromSlash(model.ArchifyBinaryRelPath))
		if !strings.Contains(stderr, "node "+upstream+" code-analysis check . --update-baseline") {
			t.Fatalf("stderr = %q, want the upstream command", stderr)
		}
	})
	t.Run("other commands pass through with update checks off", func(t *testing.T) {
		h := newLauncherHome(t)
		if _, stderr, code := h.run(h.command + " guide review a pull request"); code != 0 {
			t.Fatalf("guide exit = %d, stderr %q", code, stderr)
		}
		calls := h.calls()
		if len(calls) != 1 || strings.Join(calls[0].Args, " ") != "guide review a pull request" || calls[0].Update == nil {
			t.Fatalf("upstream calls = %+v, want guide unchanged with updates off", calls)
		}
	})
}
