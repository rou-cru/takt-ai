package gc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// preparationFileMode keeps the persisted preparation state private.
const preparationFileMode os.FileMode = 0o600

// preparationFileName is the persisted preparation state's file name inside state.
const preparationFileName = "gc-preparation.json"

// ProjectConfig is reviewed project configuration, never supplied by the collector.
// Executables must already be installed by the project's ordinary preparation.
type ProjectConfig struct {
	Version   int        `json:"version"`
	Locks     []string   `json:"locks"`
	Checks    [][]string `json:"checks"`
	Analyzers []Analyzer `json:"analyzers"`
	// Durations use Go syntax (e.g. "20m"). Empty fields in v1 config/state
	// retain defaults: checks 15m, analyzers 10m, versions/AST 30s.
	CheckTimeout    string `json:"check_timeout,omitempty"`
	AnalyzerTimeout string `json:"analyzer_timeout,omitempty"`
}

// Analyzer is one project-declared analysis tool with the exact version and
// commands that produce and report its version.
type Analyzer struct {
	Language       string       `json:"language"`
	Mandate        MandateClass `json:"mandate"`
	Tool           string       `json:"tool"`
	Version        string       `json:"version"`
	Command        []string     `json:"command"`
	VersionCommand []string     `json:"version_command"`
}

// Preparation is the frozen environment evidence: source snapshots, reviewed
// config and dependency digests captured before the cycle runs.
type Preparation struct {
	Sources   map[string]SourceSnapshot `json:"sources"`
	Config    ProjectConfig             `json:"config"`
	Digests   map[string]string         `json:"digests"`
	SessionID string                    `json:"session_id"`
	Prepared  time.Time                 `json:"prepared"`
}

// CheckEvidence is the recorded outcome of one acceptance check command.
type CheckEvidence struct {
	// Command is the exact argv that ran.
	Command []string `json:"command"`
	// Output is stdout and stderr interleaved, kept as evidence; Stdout alone is what tools' machine-readable formats are parsed from.
	Output string `json:"output"`
	// Stdout is the machine-readable stream parsed for findings.
	Stdout string `json:"stdout,omitempty"`
	// Exit is the process exit code, -1 when it did not complete.
	Exit int `json:"exit"`
	// Completed reports whether the command ran to a normal exit.
	Completed bool `json:"completed"`
	// TimedOut marks evidence cut short by the configured timeout.
	TimedOut bool `json:"timed_out,omitempty"`
	// At is the UTC moment the command started.
	At time.Time `json:"at"`
}

func projectConfig(workspace string) (ProjectConfig, error) {
	var cfg ProjectConfig
	b, e := os.ReadFile(filepath.Join(workspace, ".takt/gc.json"))
	if e != nil {
		return cfg, fmt.Errorf("gc preparation: reviewed .takt/gc.json required: %w", e)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&cfg); e != nil {
		return cfg, fmt.Errorf("gc preparation: parse .takt/gc.json: %w", e)
	}
	if cfg.Version != 1 || len(cfg.Locks) == 0 || len(cfg.Checks) == 0 || len(cfg.Analyzers) == 0 {
		return cfg, errors.New("gc preparation: version 1, project locks, acceptance checks and analyzers required")
	}
	return cfg, validateTimeouts(cfg)
}
func digestFile(workspace, p string) (digest string, err error) {
	if !filepath.IsLocal(p) {
		return "", errors.New("gc: project path must be relative")
	}
	root, e := os.OpenRoot(workspace)
	if e != nil {
		return "", e
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	b, err := root.ReadFile(p)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// lockedBuffer interleaves stdout and stderr, which exec copies from separate goroutines.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func runPreparedWithTimeout(ctx context.Context, workspace string, argv []string, timeout time.Duration) CheckEvidence {
	ev := CheckEvidence{Command: append([]string{}, argv...), Exit: -1, At: time.Now().UTC()}
	if len(argv) == 0 {
		ev.Output = "missing command"
		return ev
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// A timeout must stop the tool's children too (go, linters and check runners).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "npm_config_offline=true", "UV_OFFLINE=1", "PIP_NO_INDEX=1")
	var stdout bytes.Buffer
	both := &lockedBuffer{}
	cmd.Stdout = io.MultiWriter(&stdout, both)
	cmd.Stderr = both
	e := cmd.Run()
	ev.Output, ev.Stdout = both.String(), stdout.String()
	if ctx.Err() != nil {
		ev.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		ev.Output += "\n" + ctx.Err().Error()
		return ev
	}
	if e == nil {
		ev.Exit = 0
		ev.Completed = true
		return ev
	}
	var exit *exec.ExitError
	if errors.As(e, &exit) && ctx.Err() == nil {
		ev.Exit = exit.ExitCode()
		ev.Completed = true
	} else {
		ev.Output += "\n" + e.Error()
	}
	return ev
}

// Prepare validates the project config, digests its locks, verifies every
// analyzer's exact version and snapshots the sources under state, so
// acceptance always compares like with like.
func Prepare(ctx context.Context, workspace, state, session string) (Preparation, error) {
	p := Preparation{SessionID: session, Prepared: time.Now().UTC(), Digests: map[string]string{}}
	cfg, e := projectConfig(workspace)
	if e != nil {
		return p, e
	}
	p.Config = cfg
	p.Digests, e = preparationDigests(workspace, cfg)
	if e != nil {
		return p, e
	}
	for _, a := range cfg.Analyzers {
		if e = validateAnalyzer(a); e != nil {
			return p, e
		}
		if e = verifyAnalyzer(ctx, workspace, a); e != nil {
			return p, e
		}
	}
	p.Sources, e = preparationSources(ctx, workspace, state, session, cfg)
	if e != nil {
		return p, e
	}
	b, e := json.Marshal(p)
	if e != nil {
		return p, e
	}
	e = os.WriteFile(filepath.Join(state, preparationFileName), b, preparationFileMode)
	return p, e
}

func preparationDigests(workspace string, cfg ProjectConfig) (map[string]string, error) {
	out := map[string]string{}
	for _, path := range append(append([]string{}, cfg.Locks...), ".takt/gc.json") {
		h, err := digestFile(workspace, path)
		if err != nil {
			return nil, err
		}
		out[path] = h
	}
	return out, nil
}

func preparationSources(ctx context.Context, workspace, state, session string, cfg ProjectConfig) (map[string]SourceSnapshot, error) {
	// Preserve the original session baseline across idempotent preparation.
	var old Preparation
	if bytes, err := os.ReadFile(filepath.Join(state, preparationFileName)); err == nil {
		if err = json.Unmarshal(bytes, &old); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if old.SessionID == session && old.Sources != nil {
		return old.Sources, nil
	}
	return snapshotProject(ctx, workspace)
}

func validateAnalyzer(a Analyzer) error {
	if a.Version == "" || len(a.Command) == 0 || len(a.VersionCommand) == 0 {
		return errors.New("gc preparation: analyzer commands and exact version required")
	}
	if err := validateAnalyzerCommand(a.Command); err != nil {
		return err
	}
	if err := validateAnalyzerCommand(a.VersionCommand); err != nil {
		return err
	}
	if a.Language == "go" && a.Mandate == MandateDeadCode {
		return validateProductionRTA(a)
	}
	return nil
}

func validateProductionRTA(a Analyzer) error {
	args := a.Command[1:]
	if a.Command[0] == "go" {
		if len(args) < 2 || args[0] != "tool" || args[1] != "deadcode" {
			return errors.New("gc: Go death requires go tool deadcode")
		}
		args = args[2:]
	}
	// Prevent tests from becoming roots or filters from silently hiding findings.
	if a.Tool != "deadcode" || len(args) != 2 || args[0] != "-json" || args[1] != "./..." {
		return errors.New("gc: production deadcode command must use exactly -json ./... (no -test or filters)")
	}
	return nil
}

func verifyAnalyzer(ctx context.Context, workspace string, a Analyzer) error {
	ev := runPreparedWithTimeout(ctx, workspace, a.VersionCommand, DefaultVersionTimeout)
	if !ev.Completed || ev.Exit != 0 || !strings.Contains(ev.Output, a.Version) {
		return fmt.Errorf("gc preparation: %s absent or incompatible (want %s): %s", a.Tool, a.Version, ev.Output)
	}
	return nil
}
func validateAnalyzerCommand(argv []string) error {
	if len(argv) == 0 {
		return errors.New("gc: missing analyzer")
	}
	// `go tool` runs a module-pinned tool; `go list`/`go version` read the pinned version. None downloads under GOPROXY=off.
	if argv[0] == "go" && len(argv) > 2 && (argv[1] == "tool" || argv[1] == "list" || argv[1] == "version") {
		return nil
	}
	if strings.HasPrefix(argv[0], "./") && filepath.IsLocal(argv[0]) {
		return nil
	}
	return errors.New("gc: analyzers must use project-local executables or go tool; global tools and package downloads are forbidden")
}

// LoadPreparation reloads persisted preparation after verifying its digests:
// any dependency or config change since Prepare fails the cycle instead of
// running acceptance against a moved baseline.
func LoadPreparation(workspace, state string) (Preparation, error) {
	var p Preparation
	b, e := os.ReadFile(filepath.Join(state, preparationFileName))
	if e != nil {
		return p, fmt.Errorf("gc: project not prepared: %w", e)
	}
	if e = json.Unmarshal(b, &p); e != nil {
		return p, e
	}
	for path, want := range p.Digests {
		got, e := digestFile(workspace, path)
		if e != nil || got != want {
			return p, fmt.Errorf("gc: prepared dependency/configuration changed: %s; ordinary preparation required", path)
		}
	}
	if len(p.Digests) == 0 {
		return p, errors.New("gc: empty preparation evidence")
	}
	return p, validateTimeouts(p.Config)
}

// RunChecks executes the project's acceptance checks against the workspace
// and returns one evidence record per check.
func RunChecks(ctx context.Context, workspace string, p Preparation) []CheckEvidence {
	if err := validateTimeouts(p.Config); err != nil {
		return []CheckEvidence{{Exit: -1, Output: err.Error(), At: time.Now().UTC()}}
	}
	var out []CheckEvidence
	for _, argv := range p.Config.Checks {
		out = append(out, runPreparedWithTimeout(ctx, workspace, argv, durationOrDefault(p.Config.CheckTimeout, DefaultCheckTimeout)))
	}
	return out
}

// ChecksPass reports whether every check ran to completion and exited zero.
// Empty evidence fails closed.
func ChecksPass(checks []CheckEvidence) bool {
	if len(checks) == 0 {
		return false
	}
	for _, c := range checks {
		if !c.Completed || c.Exit != 0 || len(c.Command) == 0 || c.At.IsZero() {
			return false
		}
	}
	return true
}
