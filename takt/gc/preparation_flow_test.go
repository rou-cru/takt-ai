package gc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	// fixtureToolVersion is the analyzer version the fixture's version script reports.
	fixtureToolVersion = "1.2.3"
	// fixtureScriptMode makes fixture scripts runnable as project-local executables.
	fixtureScriptMode os.FileMode = 0o755
	// shortTimeout bounds fixture commands that are expected to be cut short.
	shortTimeout = 200 * time.Millisecond
	// slowSleepSeconds outlasts shortTimeout by a wide margin.
	slowSleepSeconds = "5"
)

// putFile writes content under root, creating parent directories.
func putFile(t *testing.T, root, name, content string, mode os.FileMode) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

// fixtureConfig is a valid v1 project configuration whose analyzer is a
// project-local script, so no global tool or download is involved.
func fixtureConfig() ProjectConfig {
	return ProjectConfig{
		Version: 1,
		Locks:   []string{"lock.txt"},
		Checks:  [][]string{{"./check.sh"}},
		Analyzers: []Analyzer{{
			Language: "python", Mandate: MandateComplexity, Tool: "fixture", Version: fixtureToolVersion,
			Command: []string{"./analyze.sh"}, VersionCommand: []string{"./version.sh"},
		}},
	}
}

// writeWorkspace lays out a workspace that Prepare accepts.
func writeWorkspace(t *testing.T, cfg ProjectConfig) string {
	t.Helper()
	root := t.TempDir()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	putFile(t, root, ".takt/gc.json", string(raw), 0o644)
	putFile(t, root, "lock.txt", "pinned", 0o644)
	putFile(t, root, "version.sh", "#!/bin/sh\necho fixture "+fixtureToolVersion+"\n", fixtureScriptMode)
	putFile(t, root, "analyze.sh", "#!/bin/sh\nexit 0\n", fixtureScriptMode)
	putFile(t, root, "check.sh", "#!/bin/sh\necho checked\n", fixtureScriptMode)
	return root
}

func TestProjectConfigRejections(t *testing.T) {
	cases := map[string]func(*testing.T, string){
		"missing file": func(t *testing.T, root string) { _ = os.Remove(filepath.Join(root, ".takt/gc.json")) },
		"unknown field": func(t *testing.T, root string) {
			putFile(t, root, ".takt/gc.json", `{"version":1,"surprise":true}`, 0o644)
		},
		"not json":      func(t *testing.T, root string) { putFile(t, root, ".takt/gc.json", `nope`, 0o644) },
		"wrong version": func(t *testing.T, root string) { putFile(t, root, ".takt/gc.json", `{"version":2}`, 0o644) },
		"no analyzers": func(t *testing.T, root string) {
			putFile(t, root, ".takt/gc.json", `{"version":1,"locks":["l"],"checks":[["c"]],"analyzers":[]}`, 0o644)
		},
		"bad timeout": func(t *testing.T, root string) {
			putFile(t, root, ".takt/gc.json", `{"version":1,"locks":["l"],"checks":[["c"]],"analyzers":[{}],"check_timeout":"soon"}`, 0o644)
		},
		"non-positive timeout": func(t *testing.T, root string) {
			putFile(t, root, ".takt/gc.json", `{"version":1,"locks":["l"],"checks":[["c"]],"analyzers":[{}],"analyzer_timeout":"-1s"}`, 0o644)
		},
	}
	for name, sabotage := range cases {
		t.Run(name, func(t *testing.T) {
			root := writeWorkspace(t, fixtureConfig())
			sabotage(t, root)
			if _, err := projectConfig(root); err == nil {
				t.Fatal("projectConfig() error = nil, want a rejection")
			}
		})
	}
	root := writeWorkspace(t, fixtureConfig())
	cfg, err := projectConfig(root)
	if err != nil || cfg.Analyzers[0].Tool != "fixture" {
		t.Fatalf("projectConfig(valid) = %+v, %v", cfg, err)
	}
}

func TestDigestFile(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "lock.txt", "pinned", 0o644)
	got, err := digestFile(root, "lock.txt")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("pinned"))
	if want := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("digest = %q, want the hex SHA-256 %q", got, want)
	}
	again, _ := digestFile(root, "lock.txt")
	if again != got {
		t.Errorf("digest not stable: %q vs %q", got, again)
	}
	putFile(t, root, "lock.txt", "changed", 0o644)
	if changed, _ := digestFile(root, "lock.txt"); changed == got {
		t.Error("digest unchanged after the content changed")
	}
	for _, escape := range []string{"../outside", "/etc/passwd"} {
		if _, err := digestFile(root, escape); err == nil {
			t.Errorf("digestFile(%q) error = nil, want a non-local path rejection", escape)
		}
	}
	if _, err := digestFile(root, "absent.txt"); err == nil {
		t.Error("digestFile(absent) error = nil")
	}
	if _, err := digestFile(filepath.Join(root, "no-such-dir"), "x"); err == nil {
		t.Error("digestFile(missing workspace) error = nil")
	}
}

func TestRunPreparedWithTimeout(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "ok.sh", "#!/bin/sh\necho out; echo err >&2\n", fixtureScriptMode)
	putFile(t, root, "fail.sh", "#!/bin/sh\necho boom; exit 3\n", fixtureScriptMode)
	putFile(t, root, "slow.sh", "#!/bin/sh\nsleep "+slowSleepSeconds+"\n", fixtureScriptMode)
	ctx := context.Background()

	if ev := runPreparedWithTimeout(ctx, root, nil, time.Minute); ev.Completed || ev.Exit != -1 || ev.Output != "missing command" {
		t.Errorf("empty argv evidence = %+v", ev)
	}

	ev := runPreparedWithTimeout(ctx, root, []string{"./ok.sh"}, time.Minute)
	if !ev.Completed || ev.Exit != 0 || strings.TrimSpace(ev.Stdout) != "out" || !strings.Contains(ev.Output, "err") || ev.At.IsZero() {
		t.Errorf("success evidence = %+v, want stdout kept apart from the interleaved output", ev)
	}

	ev = runPreparedWithTimeout(ctx, root, []string{"./fail.sh"}, time.Minute)
	if !ev.Completed || ev.Exit != 3 || !strings.Contains(ev.Output, "boom") {
		t.Errorf("failing evidence = %+v, want a completed run with exit 3", ev)
	}

	ev = runPreparedWithTimeout(ctx, root, []string{"./slow.sh"}, shortTimeout)
	if ev.Completed || !ev.TimedOut || ev.Exit != -1 {
		t.Errorf("timeout evidence = %+v, want TimedOut and not Completed", ev)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if ev = runPreparedWithTimeout(cancelled, root, []string{"./ok.sh"}, time.Minute); ev.Completed || ev.TimedOut {
		t.Errorf("cancelled evidence = %+v, want an incomplete, non-timeout run", ev)
	}

	if ev = runPreparedWithTimeout(ctx, root, []string{"./absent.sh"}, time.Minute); ev.Completed || ev.Exit != -1 {
		t.Errorf("unstartable evidence = %+v, want an incomplete run", ev)
	}
}

func TestPrepareRecordsFrozenEvidenceAndKeepsBaselinePerSession(t *testing.T) {
	root := writeWorkspace(t, fixtureConfig())
	putFile(t, root, "notes.json", "{}", 0o644) // not a source or analysis config: never snapshotted
	putFile(t, root, "package.json", "before", 0o644)
	state := t.TempDir()

	p, err := Prepare(context.Background(), root, state, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.Digests["lock.txt"]; !ok {
		t.Errorf("Digests = %v, want the lock digested", p.Digests)
	}
	if _, ok := p.Digests[".takt/gc.json"]; !ok {
		t.Errorf("Digests = %v, want the reviewed config digested", p.Digests)
	}
	if got := p.Sources["package.json"].Content; got != "before" {
		t.Errorf("Sources[package.json] = %q, want the snapshotted content", got)
	}
	if _, ok := p.Sources["notes.json"]; ok {
		t.Error("an unrelated file was snapshotted")
	}

	// The baseline of a session survives idempotent re-preparation.
	putFile(t, root, "package.json", "after", 0o644)
	again, err := Prepare(context.Background(), root, state, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Sources["package.json"].Content; got != "before" {
		t.Errorf("re-prepare rebaselined to %q, want the original session baseline", got)
	}
	// A new session takes a fresh snapshot.
	fresh, err := Prepare(context.Background(), root, state, "s2")
	if err != nil {
		t.Fatal(err)
	}
	if got := fresh.Sources["package.json"].Content; got != "after" {
		t.Errorf("new session baseline = %q, want the current content", got)
	}

	loaded, err := LoadPreparation(root, state)
	if err != nil || loaded.SessionID != "s2" {
		t.Fatalf("LoadPreparation() = session %q, %v; want s2", loaded.SessionID, err)
	}
}

func TestPrepareRejections(t *testing.T) {
	cases := map[string]func(*testing.T, string, string){
		"no reviewed config": func(t *testing.T, root, _ string) { _ = os.Remove(filepath.Join(root, ".takt/gc.json")) },
		"lock file missing":  func(t *testing.T, root, _ string) { _ = os.Remove(filepath.Join(root, "lock.txt")) },
		"version mismatch": func(t *testing.T, root, _ string) {
			putFile(t, root, "version.sh", "#!/bin/sh\necho fixture 9.9.9\n", fixtureScriptMode)
		},
		"version script fails": func(t *testing.T, root, _ string) {
			putFile(t, root, "version.sh", "#!/bin/sh\nexit 1\n", fixtureScriptMode)
		},
		"corrupt earlier preparation": func(t *testing.T, _, state string) {
			putFile(t, state, preparationFileName, "{ nope", 0o600)
		},
		"unreadable earlier preparation": func(t *testing.T, _, state string) {
			if err := os.MkdirAll(filepath.Join(state, preparationFileName), 0o755); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, sabotage := range cases {
		t.Run(name, func(t *testing.T) {
			root, state := writeWorkspace(t, fixtureConfig()), t.TempDir()
			sabotage(t, root, state)
			if _, err := Prepare(context.Background(), root, state, "s"); err == nil {
				t.Fatal("Prepare() error = nil, want a rejection")
			}
		})
	}
	t.Run("state directory missing", func(t *testing.T) {
		root := writeWorkspace(t, fixtureConfig())
		if _, err := Prepare(context.Background(), root, filepath.Join(t.TempDir(), "absent"), "s"); err == nil {
			t.Fatal("Prepare() into a missing state directory error = nil")
		}
	})
	t.Run("invalid analyzer", func(t *testing.T) {
		cfg := fixtureConfig()
		cfg.Analyzers[0].Command = []string{"npx", "lint"}
		if _, err := Prepare(context.Background(), writeWorkspace(t, cfg), t.TempDir(), "s"); err == nil {
			t.Fatal("Prepare() with a global-tool analyzer error = nil")
		}
	})
}

func TestValidateAnalyzer(t *testing.T) {
	valid := func(mutate func(*Analyzer)) Analyzer {
		a := Analyzer{Language: "python", Mandate: MandateComplexity, Tool: "x", Version: "1", Command: []string{"./run"}, VersionCommand: []string{"./ver"}}
		mutate(&a)
		return a
	}
	rta := func(command ...string) func(*Analyzer) {
		return func(a *Analyzer) {
			a.Language, a.Mandate, a.Tool, a.Command = "go", MandateDeadCode, "deadcode", command
		}
	}
	cases := []struct {
		name   string
		mutate func(*Analyzer)
		ok     bool
	}{
		{"local scripts", func(*Analyzer) {}, true},
		{"missing version", func(a *Analyzer) { a.Version = "" }, false},
		{"missing command", func(a *Analyzer) { a.Command = nil }, false},
		{"missing version command", func(a *Analyzer) { a.VersionCommand = nil }, false},
		{"global command", func(a *Analyzer) { a.Command = []string{"eslint", "."} }, false},
		{"global version command", func(a *Analyzer) { a.VersionCommand = []string{"eslint", "-v"} }, false},
		{"escaping local path", func(a *Analyzer) { a.Command = []string{"./../evil"} }, false},
		{"go tool", func(a *Analyzer) { a.Command = []string{"go", "tool", "staticcheck"} }, true},
		{"production RTA via go tool", rta("go", "tool", "deadcode", "-json", "./..."), true},
		{"production RTA via local binary", rta("./deadcode", "-json", "./..."), true},
		{"RTA with tests as roots", rta("./deadcode", "-test", "-json", "./..."), false},
		{"RTA with a filter", rta("./deadcode", "-json", "./cmd/..."), false},
		{"RTA through go without tool", rta("go", "run", "deadcode"), false},
		{"RTA go tool of another tool", rta("go", "tool", "vet", "-json", "./..."), false},
		{"RTA under another tool name", func(a *Analyzer) {
			rta("./deadcode", "-json", "./...")(a)
			a.Tool = "other"
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAnalyzer(valid(tc.mutate))
			if (err == nil) != tc.ok {
				t.Fatalf("validateAnalyzer() error = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestLoadPreparationFailsClosed(t *testing.T) {
	root := writeWorkspace(t, fixtureConfig())
	state := t.TempDir()
	if _, err := LoadPreparation(root, state); err == nil || !strings.Contains(err.Error(), "not prepared") {
		t.Fatalf("LoadPreparation(unprepared) error = %v", err)
	}
	if _, err := Prepare(context.Background(), root, state, "s"); err != nil {
		t.Fatal(err)
	}

	t.Run("intact", func(t *testing.T) {
		if _, err := LoadPreparation(root, state); err != nil {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("corrupt state", func(t *testing.T) {
		broken := t.TempDir()
		putFile(t, broken, preparationFileName, "{ nope", 0o600)
		if _, err := LoadPreparation(root, broken); err == nil {
			t.Fatal("error = nil")
		}
	})
	t.Run("no digests", func(t *testing.T) {
		empty := t.TempDir()
		putFile(t, empty, preparationFileName, `{"digests":{}}`, 0o600)
		if _, err := LoadPreparation(root, empty); err == nil || !strings.Contains(err.Error(), "empty preparation evidence") {
			t.Fatalf("error = %v, want the empty-evidence rejection", err)
		}
	})
	t.Run("invalid timeout in state", func(t *testing.T) {
		bad := t.TempDir()
		putFile(t, bad, preparationFileName, `{"digests":{"lock.txt":"x"},"config":{"check_timeout":"never"}}`, 0o600)
		if _, err := LoadPreparation(root, bad); err == nil {
			t.Fatal("error = nil")
		}
	})
	t.Run("dependency changed", func(t *testing.T) {
		putFile(t, root, "lock.txt", "drifted", 0o644)
		if _, err := LoadPreparation(root, state); err == nil || !strings.Contains(err.Error(), "changed: lock.txt") {
			t.Fatalf("error = %v, want the drifted dependency named", err)
		}
	})
}

func TestRunChecksAndChecksPass(t *testing.T) {
	root := writeWorkspace(t, fixtureConfig())
	putFile(t, root, "red.sh", "#!/bin/sh\nexit 1\n", fixtureScriptMode)
	ctx := context.Background()

	green := Preparation{Config: fixtureConfig()}
	if ev := RunChecks(ctx, root, green); len(ev) != 1 || !ChecksPass(ev) {
		t.Errorf("green checks = %+v, want one passing record", ev)
	}

	red := Preparation{Config: fixtureConfig()}
	red.Config.Checks = [][]string{{"./check.sh"}, {"./red.sh"}}
	ev := RunChecks(ctx, root, red)
	if len(ev) != 2 || ChecksPass(ev) || ev[1].Exit != 1 {
		t.Errorf("mixed checks = %+v, want two records and a failing verdict", ev)
	}

	badTimeout := Preparation{Config: fixtureConfig()}
	badTimeout.Config.CheckTimeout = "whenever"
	if ev := RunChecks(ctx, root, badTimeout); len(ev) != 1 || ev[0].Exit != -1 || ChecksPass(ev) {
		t.Errorf("invalid-timeout checks = %+v, want a single failed record", ev)
	}

	timed := Preparation{Config: fixtureConfig()}
	timed.Config.Checks = [][]string{{"./slow.sh"}}
	timed.Config.CheckTimeout = shortTimeout.String()
	putFile(t, root, "slow.sh", "#!/bin/sh\nsleep "+slowSleepSeconds+"\n", fixtureScriptMode)
	if ev := RunChecks(ctx, root, timed); len(ev) != 1 || !ev[0].TimedOut {
		t.Errorf("slow check = %+v, want the configured timeout applied", ev)
	}
}

func TestChecksPassFailsClosed(t *testing.T) {
	now := time.Now()
	pass := CheckEvidence{Command: []string{"c"}, Completed: true, At: now}
	cases := map[string][]CheckEvidence{
		"no evidence":       nil,
		"incomplete":        {{Command: []string{"c"}, At: now}},
		"non-zero exit":     {{Command: []string{"c"}, Completed: true, Exit: 2, At: now}},
		"no command":        {{Completed: true, At: now}},
		"no timestamp":      {{Command: []string{"c"}, Completed: true}},
		"one bad among ok":  {pass, {Command: []string{"d"}, Completed: true, Exit: 1, At: now}},
		"failing after ok":  {pass, pass, {Command: []string{"d"}, At: now}},
		"failing at first":  {{Command: []string{"d"}, At: now}, pass},
		"zero value record": {{}},
	}
	for name, evidence := range cases {
		if ChecksPass(evidence) {
			t.Errorf("ChecksPass(%s) = true, want false", name)
		}
	}
	if !ChecksPass([]CheckEvidence{pass, pass}) {
		t.Error("ChecksPass(all green) = false, want true")
	}
}

func TestDurationOrDefaultAndAnalyzerTimeout(t *testing.T) {
	if got := durationOrDefault("", time.Hour); got != time.Hour {
		t.Errorf("durationOrDefault(empty) = %v, want the fallback", got)
	}
	if got := durationOrDefault("90s", time.Hour); got != 90*time.Second {
		t.Errorf("durationOrDefault(90s) = %v", got)
	}
	if got := analyzerTimeout(ProjectConfig{}); got != DefaultAnalyzerTimeout {
		t.Errorf("analyzerTimeout(default) = %v", got)
	}
	if got := analyzerTimeout(ProjectConfig{AnalyzerTimeout: "1m"}); got != time.Minute {
		t.Errorf("analyzerTimeout(1m) = %v", got)
	}
}
