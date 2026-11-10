package memory_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/internal/flock"
	"github.com/rou-cru/takt-ai/takt/memory"
)

// binaryDir holds the binary built for this test run; TestMain removes it.
var binaryDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("/tmp", "tktbin")
	if err != nil {
		panic(err)
	}
	binaryDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// binary builds the real takt-ai executable, once per run.
var binary = func() func(*testing.T) string {
	build := sync.OnceValues(func() (string, error) {
		root, err := exec.Command("go", "env", "GOMOD").Output()
		if err != nil {
			return "", err
		}
		path := filepath.Join(binaryDir, "takt-ai")
		cmd := exec.Command("go", "build", "-o", path, "./takt/cli")
		cmd.Dir = filepath.Dir(strings.TrimSpace(string(root)))
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", errors.New(string(out))
		}
		return path, nil
	})
	return func(t *testing.T) string {
		t.Helper()
		path, err := build()
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return path
	}
}()

// ownerPID reads the PID the owner's lock file records.
func ownerPID(t *testing.T, dir string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, memory.LockFile))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("lock file records no PID: %q", raw)
	}
	return pid
}

// stopOwnerAtEnd stops whatever owner serves dir when the test ends, and waits for its socket to go.
func stopOwnerAtEnd(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() {
		raw, _ := os.ReadFile(filepath.Join(dir, memory.LockFile))
		if pid, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			f, err := os.OpenFile(filepath.Join(dir, memory.LockFile), os.O_RDWR, 0o600)
			if err == nil {
				_ = flock.Exclusive(context.Background(), f, readyWait)
				_ = f.Close()
			}
		}
	})
}

// takt runs the binary against dir and returns its stdout and stderr and its error.
func takt(t *testing.T, bin, dir, cwd string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), memory.EnvDir+"="+dir)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestEnsureReusesARunningOwner(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	start(t, dir)
	st, err := memory.Ensure(context.Background(), dir)
	if err != nil || st.State != memory.StateReady {
		t.Fatalf("got %+v, %v", st, err)
	}
	if pid := ownerPID(t, dir); pid != os.Getpid() {
		t.Fatalf("owner replaced: pid %d", pid)
	}
}

func TestEnsureReportsANotReadyOwnerWithoutRestartOrStore(t *testing.T) {
	dir := shortDir(t)
	start(t, dir)
	st, err := memory.Ensure(context.Background(), dir)
	if err != nil || st.State != memory.StateNoStore || st.Reason == "" {
		t.Fatalf("got %+v, %v", st, err)
	}
	if pid := ownerPID(t, dir); pid != os.Getpid() {
		t.Fatalf("not-ready owner restarted: pid %d", pid)
	}
	if _, err := os.Stat(filepath.Join(dir, memory.StoreFile)); err == nil {
		t.Fatal("ensure created the store")
	}
}

func TestEnsureNeedsTheDirectory(t *testing.T) {
	dir := filepath.Join(shortDir(t), "absent")
	_, err := memory.Ensure(context.Background(), dir)
	var unavailable *memory.UnavailableError
	if !errors.As(err, &unavailable) || !strings.Contains(err.Error(), "takt-ai memory start --init") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("ensure created the directory")
	}
}

func TestEnsureRefusesASocketPathTooLong(t *testing.T) {
	dir := filepath.Join(shortDir(t), strings.Repeat("a", 120))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := memory.Ensure(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "104") {
		t.Fatalf("got %v", err)
	}
}

func TestStartLaunchesTheOwnerAndReusesIt(t *testing.T) {
	bin := binary(t)
	dir := shortDir(t)
	initStore(t, dir)
	stopOwnerAtEnd(t, dir)
	cwd := shortDir(t)

	if out, err := takt(t, bin, dir, cwd, "memory", "start"); err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	first := ownerPID(t, dir)
	if out, err := takt(t, bin, dir, cwd, "memory", "start"); err != nil || ownerPID(t, dir) != first {
		t.Fatalf("repeated start did not reuse the owner: %v\n%s", err, out)
	}
}

func TestStopOwnerStopsTheRunningOwner(t *testing.T) {
	bin := binary(t)
	dir := shortDir(t)
	initStore(t, dir)
	stopOwnerAtEnd(t, dir)
	if out, err := takt(t, bin, dir, shortDir(t), "memory", "start"); err != nil {
		t.Fatalf("start: %v\n%s", err, out)
	}
	if err := memory.StopOwner(context.Background(), dir); err != nil {
		t.Fatalf("StopOwner() error = %v", err)
	}
	if _, err := memory.Probe(context.Background(), dir); err == nil {
		t.Fatal("the owner still answers after StopOwner")
	}
}

// TestStopOwnerLeavesAProcessAloneWhenNoOwnerHoldsTheLock proves a PID a crashed owner left in the lock file
// is never signaled: it may belong to an unrelated process by now.
func TestStopOwnerLeavesAProcessAloneWhenNoOwnerHoldsTheLock(t *testing.T) {
	dir := shortDir(t)
	bystander := exec.Command("sleep", "60")
	if err := bystander.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- bystander.Wait() }()
	t.Cleanup(func() { _ = bystander.Process.Kill() })
	if err := os.WriteFile(filepath.Join(dir, memory.LockFile), []byte(strconv.Itoa(bystander.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := memory.StopOwner(context.Background(), dir); err != nil {
		t.Fatalf("StopOwner() error = %v", err)
	}
	select {
	case err := <-exited:
		t.Fatalf("the process named in the lock file was signaled and exited: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStartInitCreatesDirectoryAndStoreAndPrintsTheKeyAndSocket(t *testing.T) {
	bin := binary(t)
	dir := filepath.Join(shortDir(t), "mem")
	stopOwnerAtEnd(t, dir)
	cwd := filepath.Join(shortDir(t), "Work_Tree")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := takt(t, bin, dir, cwd, "memory", "start", "--init")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var printed struct{ Workspace, Socket string }
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &printed); err != nil {
		t.Fatalf("printed %q: %v", out, err)
	}
	wantSocket, _ := memory.SocketPath(dir)
	if want := memory.WorkspaceKey(context.Background(), cwd); printed.Workspace != want || want != "work_tree" || printed.Socket != wantSocket {
		t.Fatalf("printed %+v, want workspace %q and socket %q", printed, want, wantSocket)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, memory.StoreFile)); err != nil {
		t.Fatalf("store not created: %v", err)
	}
	if st, err := memory.Probe(context.Background(), dir); err != nil || st.State != memory.StateReady {
		t.Fatalf("got %+v, %v", st, err)
	}
}

func TestStartWithoutInitCreatesNothing(t *testing.T) {
	bin := binary(t)
	cwd := shortDir(t)

	missing := filepath.Join(shortDir(t), "mem")
	out, err := takt(t, bin, missing, cwd, "memory", "start")
	if err == nil || !strings.Contains(out, "takt-ai memory start --init") {
		t.Fatalf("missing directory: %v\n%s", err, out)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Fatal("start created the directory")
	}

	empty := shortDir(t)
	stopOwnerAtEnd(t, empty)
	out, err = takt(t, bin, empty, cwd, "memory", "start")
	if err == nil || !strings.Contains(out, "not ready") {
		t.Fatalf("missing store: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(empty, memory.StoreFile)); err == nil {
		t.Fatal("start created the store")
	}
}

func TestStartReportsAStartupFailureWithItsCause(t *testing.T) {
	bin := binary(t)
	dir := shortDir(t)
	initStore(t, dir)
	// A non-empty directory where the socket belongs makes the owner fail after it holds the lock.
	blocker := filepath.Join(dir, memory.SocketFile)
	if err := os.MkdirAll(filepath.Join(blocker, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := takt(t, bin, dir, shortDir(t), "memory", "start")
	if err == nil || !strings.Contains(out, "unavailable") || !strings.Contains(out, memory.SocketFile) {
		t.Fatalf("got %v\n%s", err, out)
	}
}

func TestServeRefusesASecondOwner(t *testing.T) {
	bin := binary(t)
	dir := shortDir(t)
	initStore(t, dir)
	lock, err := os.OpenFile(filepath.Join(dir, memory.LockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := flock.Exclusive(context.Background(), lock, readyWait); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "memory", "serve")
	cmd.Env = append(os.Environ(), memory.EnvDir+"="+dir)
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "another memory service owns") {
		t.Fatalf("got %v\n%s", err, out)
	}
}

// mcpProcess is `takt-ai memory mcp` with its pipes.
type mcpProcess struct {
	stdin  io.WriteCloser
	stdout *bufio.Reader
}

func startMCP(t *testing.T, bin, dir string) *mcpProcess {
	t.Helper()
	cmd := exec.Command(bin, "memory", "mcp")
	cmd.Env = append(os.Environ(), memory.EnvDir+"="+dir)
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = in.Close(); _ = cmd.Wait() })
	return &mcpProcess{stdin: in, stdout: bufio.NewReader(out)}
}

func (p *mcpProcess) ask(t *testing.T, line string) map[string]any {
	t.Helper()
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		t.Fatal(err)
	}
	raw, err := p.stdout.ReadBytes('\n')
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("not JSON: %q", raw)
	}
	return m
}

func TestAdapterAnswersUnavailableAfterTheOwnerStops(t *testing.T) {
	bin := binary(t)
	dir := shortDir(t)
	initStore(t, dir)
	stopOwnerAtEnd(t, dir)
	p := startMCP(t, bin, dir)
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	if _, ok := p.ask(t, list)["result"]; !ok {
		t.Fatal("tools/list failed with a running owner")
	}
	if err := syscall.Kill(ownerPID(t, dir), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(readyWait)
	for {
		if _, err := memory.Probe(context.Background(), dir); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("owner did not stop")
		}
		time.Sleep(20 * time.Millisecond)
	}
	reply := p.ask(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	rpcErr, _ := reply["error"].(map[string]any)
	if msg, _ := rpcErr["message"].(string); !strings.Contains(msg, "unavailable") {
		t.Fatalf("got %v", reply)
	}
}

func TestAssessClassifiesTheOwnerAndStartCreatesTheStore(t *testing.T) {
	ctx := context.Background()
	dir := shortDir(t)
	if _, health, err := memory.Assess(ctx, dir); health != memory.HealthUnreachable || !errors.Is(err, memory.ErrUnreachable) {
		t.Fatalf("no owner: health %v, %v", health, err)
	}
	start(t, dir)
	if status, health, err := memory.Assess(ctx, dir); err != nil || health != memory.HealthNotReady || status.State != memory.StateNoStore {
		t.Fatalf("owner without a store: %+v, health %v, %v", status, health, err)
	}
	if _, err := memory.Start(ctx, dir, false); err == nil || !strings.Contains(err.Error(), "memory start --init") {
		t.Fatalf("start without init: %v", err)
	}
	status, err := memory.Start(ctx, dir, true)
	if err != nil || status.State != memory.StateReady {
		t.Fatalf("start with init: %+v, %v", status, err)
	}
	if _, health, err := memory.Assess(ctx, dir); err != nil || health != memory.HealthReady {
		t.Fatalf("ready owner: health %v, %v", health, err)
	}
}

func TestResolveDirPrefersTheEnvironmentThenTheHome(t *testing.T) {
	t.Setenv(memory.EnvDir, "/custom/memory")
	if dir, err := memory.ResolveDir(); err != nil || dir != "/custom/memory" {
		t.Fatalf("from the environment: %q, %v", dir, err)
	}
	home := t.TempDir()
	t.Setenv(memory.EnvDir, "")
	t.Setenv("HOME", home)
	if dir, err := memory.ResolveDir(); err != nil || dir != filepath.Join(home, ".takt-ai", "memory") {
		t.Fatalf("from the home: %q, %v", dir, err)
	}
}

func TestPrefixedToolNameJoinsTheServerKeyAndTheTool(t *testing.T) {
	if got := memory.PrefixedToolName(memory.ToolRecord); got != "takt_memory_record" {
		t.Fatalf("got %q", got)
	}
}
