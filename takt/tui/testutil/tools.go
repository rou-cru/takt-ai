package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/internal/flock"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/memory/embed"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// fakeNpmScript stands in for `npm install --prefix <dir>`: it leaves the sandbox runtime package
// where InstallSandboxDependency looks for it, without the network.
const fakeNpmScript = `#!/bin/sh
dir="$3/node_modules/@anthropic-ai/sandbox-runtime"
mkdir -p "$dir" && echo '{}' > "$dir/package.json"
`

// RunWithFakeTools runs m with compatible fake opencode, codegraph and npm first on PATH, so tests
// never use host binaries or the network, and lets the binary act as the memory owner. Call it from
// TestMain.
func RunWithFakeTools(m *testing.M) int {
	ServeMemoryIfRequested()
	memoryDir, err := os.MkdirTemp(memoryDirRoot, "tkt")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(memoryDir) }()
	defer StopMemoryOwner(memoryDir)
	if err := os.Setenv(memory.EnvDir, memoryDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	dir, err := os.MkdirTemp("", "takt-fake-tools-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	scripts := map[string][]byte{
		"opencode":  FakeOpenCodeScript(),
		"codegraph": FakeCodegraphScript(codegraph.CodegraphVersion),
		"npm":       []byte(fakeNpmScript),
	}
	for name, script := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), script, executableFileMode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}

const (
	// memoryDirRoot keeps memory directories short enough for a Unix socket path.
	memoryDirRoot = "/tmp"
	// ownerStopWait bounds how long a test waits for its memory owner to release the lock.
	ownerStopWait = 15 * time.Second
	// fakeModelFile is the name of the one small file FakeModel serves.
	fakeModelFile = "tokenizer.json"
)

// ServeMemoryIfRequested makes the test binary act as the memory owner when the service launches it
// as `memory serve`, because Ensure and Start launch os.Executable(). Call it first in TestMain.
func ServeMemoryIfRequested() {
	if len(os.Args) != 3 || os.Args[1] != "memory" || os.Args[2] != "serve" {
		return
	}
	dir, err := memory.ResolveDir()
	if err == nil {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer stop()
		err = memory.Serve(ctx, dir, setup.BuildVersion)
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// MemoryDir creates a short memory directory, points TAKT_MEMORY_DIR at it for the test, and stops
// whatever owner serves it when the test ends.
func MemoryDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(memoryDirRoot, "tkt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(memory.EnvDir, dir)
	t.Cleanup(func() {
		StopMemoryOwner(dir)
		_ = os.RemoveAll(dir)
	})
	return dir
}

// StopMemoryOwner stops the owner process recorded in dir's lock file and waits for it to release the
// lock. A service running inside the test process is stopped by cancelling its context instead.
func StopMemoryOwner(dir string) {
	lockPath := filepath.Join(dir, memory.LockFile)
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid == os.Getpid() || syscall.Kill(pid, syscall.SIGTERM) != nil {
		return
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = lock.Close() }()
	_ = flock.Exclusive(context.Background(), lock, ownerStopWait)
}

// FakeModel serves a one-file model over HTTP and returns the manifest that names it, so install
// acquires a model without the network. The server closes when the test ends.
func FakeModel(t *testing.T) *embed.Manifest {
	t.Helper()
	body := []byte("fake model file")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	digest := sha256.Sum256(body)
	return &embed.Manifest{
		BaseURL: server.URL,
		Files:   []embed.File{{Name: fakeModelFile, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(body))}},
	}
}

// WriteTinyModel writes the smallest model the memory service loads (a two-token vocabulary with
// one-dimensional vectors) into dir, so a service started with HOME set to its owner reports its
// semantic model loaded without the real download.
func WriteTinyModel(t *testing.T, dir string) {
	t.Helper()
	const tokenizer = `{"normalizer":{"type":"BertNormalizer"},"pre_tokenizer":{"type":"BertPreTokenizer"},` +
		`"model":{"type":"WordPiece","vocab":{"[UNK]":0,"a":1},"unk_token":"[UNK]","continuing_subword_prefix":"##","max_input_chars_per_word":100}}`
	const header = `{"embeddings":{"dtype":"F32","shape":[2,1],"data_offsets":[0,8]}}`
	weights := binary.LittleEndian.AppendUint64(nil, uint64(len(header)))
	weights = append(weights, header...)
	weights = append(weights, make([]byte, 8)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"tokenizer.json": []byte(tokenizer), "model.safetensors": weights} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
