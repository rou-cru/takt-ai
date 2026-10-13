package main_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// vfsIPCVersion is the request version the plugin and `takt-ai vfs` agree on.
const vfsIPCVersion = 5

// vfs runs one `takt-ai vfs` command with its request on stdin.
func (s *session) vfs(command string, req map[string]any) result {
	s.t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		s.t.Fatalf("marshal request: %v", err)
	}
	cmd := exec.Command(s.bin, "vfs", command, "--workspace", s.root, "--state", s.state)
	cmd.Env = []string{"HOME=" + s.home, "TMPDIR=" + s.home, "PATH=" + os.Getenv("PATH")}
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewReader(body), &stdout, &stderr
	err = cmd.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		s.t.Fatalf("run takt-ai vfs %s: %v", command, err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: cmd.ProcessState.ExitCode()}
}

func TestNativeInspectionAndQARoundCommands(t *testing.T) {
	bin := buildTaktAI(t)

	t.Run("native inspection commands answer the plugin", func(t *testing.T) {
		s := newSession(t, bin)
		// verify holds no VFS write grant, so its native reads reserve nothing.
		req := map[string]any{"ipc_version": vfsIPCVersion, "session_id": rootSession, "caller_session": "verify-host", "caller_agent": "verify", "call_id": "read-1", "tool": "read", "path": "src"}
		for _, command := range []string{"native-inspection-start", "native-inspection-end"} {
			if res := s.vfs(command, req); res.code != 0 || !strings.Contains(res.stdout, `"ok":true`) {
				t.Fatalf("%s: exit %d %s %s", command, res.code, res.stdout, res.stderr)
			}
		}
		res := s.vfs("native-inspections", map[string]any{"ipc_version": vfsIPCVersion})
		var listed struct {
			OK          bool             `json:"ok"`
			Inspections []map[string]any `json:"inspections"`
		}
		res.decode(t, &listed)
		if !listed.OK || listed.Inspections == nil || len(listed.Inspections) != 0 {
			t.Fatalf("native-inspections = %s; want ok with an empty list", res.stdout)
		}
	})

	t.Run("a plan commitment declares its QA round", func(t *testing.T) {
		s := newSession(t, bin)
		r := commit("v1", "author", "sibling")
		r.QARound = &history.QARound{Objective: "acceptance", Units: []string{"author", "sibling"}}
		s.must(r)
		if version, round := s.projection().Budgets(rootSession).QARoundFor("sibling"); version != "v1" || round.Objective != "acceptance" {
			t.Fatalf("QARoundFor(sibling) = %q %+v; want the v1 acceptance round", version, round)
		}
	})
}
