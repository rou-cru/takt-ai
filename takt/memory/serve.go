package memory

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rou-cru/takt-ai/takt/internal/flock"
	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

// EnvDir relocates the memory directory, as deployments do.
const EnvDir = "TAKT_MEMORY_DIR"

// Files of the memory directory beside the store.
const (
	SocketFile = "memory.sock"
	LockFile   = "serve.lock"
)

// maxSocketPath is the shortest Unix socket path limit among supported platforms (darwin: 104 bytes);
// a path must be shorter than it.
const maxSocketPath = 104

// ownerFileMode keeps the lock, log, and socket private to the local user (PR-MSV-43).
const ownerFileMode = 0o600

// lockWait covers a previous owner that is still shutting down; an owner that stays is refused after it.
const lockWait = 2 * time.Second

// shutdownWait bounds how long in-flight requests may finish; it exceeds the store's busy wait.
const shutdownWait = 10 * time.Second

// ResolveDir is the single definition of the memory directory: TAKT_MEMORY_DIR when set, else ~/.takt-ai/memory.
func ResolveDir() (string, error) {
	if dir := os.Getenv(EnvDir); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".takt-ai", "memory"), nil
}

// SocketPath is the owner's socket inside dir. A path the platform cannot bind is refused, naming the limit.
func SocketPath(dir string) (string, error) {
	path := filepath.Join(dir, SocketFile)
	if len(path) >= maxSocketPath {
		return "", fmt.Errorf("socket path %s is %d bytes; it must be shorter than %d: use a shorter memory directory (%s)", path, len(path), maxSocketPath, EnvDir)
	}
	return path, nil
}

// lockOwner is the PID the lock file records, for the message of a refused second owner.
func lockOwner(path string) string {
	raw, _ := os.ReadFile(path)
	if pid := strings.TrimSpace(string(raw)); pid != "" {
		return pid
	}
	return "unknown"
}

// Serve runs the memory service on the existing directory dir until ctx ends. It
// returns the reason when it cannot start. It never creates the directory or the store: without a store the service is not ready and answers only
// `status` and `init`. version is the release version `status` reports.
func Serve(ctx context.Context, dir, version string) (err error) {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("memory directory is not available: %w; create it with `takt-ai memory start --init`", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("memory directory %s is not a directory", dir)
	}
	socketPath, err := SocketPath(dir)
	if err != nil {
		return err
	}
	lockPath := filepath.Join(dir, LockFile)
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, ownerFileMode)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	if err := flock.Exclusive(ctx, lock, lockWait); err != nil {
		return fmt.Errorf("another memory service owns %s (pid %s): %w", dir, lockOwner(lockPath), err)
	}
	if err := lock.Truncate(0); err != nil {
		return err
	}
	if _, err := lock.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lock.Truncate(0)) }()

	workerCtx, stopWorker := context.WithCancel(context.Background())
	workerDone := make(chan struct{})
	trigger := make(chan struct{}, 1)
	svc := &service{dir: dir, version: version, embedNow: func() {
		select {
		case trigger <- struct{}{}:
		default:
		}
	}}
	defer func() { err = errors.Join(err, svc.close()) }()

	if home, herr := os.UserHomeDir(); herr == nil {
		// A model that cannot load leaves svc.model nil, whose entries then stay without a semantic vector, which `status` reports as a coverage gap.
		svc.model, _ = embed.Load(embed.ModelDir(home))
	}
	go svc.embedWorker(workerCtx, trigger, workerDone)
	defer func() { stopWorker(); <-workerDone }()
	svc.mu.Lock()
	err = svc.openLocked()
	svc.mu.Unlock()
	if err != nil {
		return err
	}
	svc.embedNow()

	mux := http.NewServeMux()
	crew, err := svc.newMemoryServer(PrincipalCrew)
	if err != nil {
		return err
	}
	external, err := svc.newMemoryServer(PrincipalExternal)
	if err != nil {
		return err
	}
	for path, server := range map[string]*mcp.Server{PathCrew: crew, PathExternal: external, PathAdmin: svc.newAdminServer()} {
		mux.Handle(path, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
			Stateless:    true,
			JSONResponse: true,
		}))
	}

	// Holding the lock makes any existing socket file a leftover of a dead owner.
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(socketPath, ownerFileMode); err != nil {
		return errors.Join(err, ln.Close())
	}
	httpServer := &http.Server{Handler: mux}
	defer func() {
		sctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
		defer cancel()
		err = errors.Join(err, httpServer.Shutdown(sctx))
	}()
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(ln) }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-served:
		return err
	}
}

// embedWorker embeds committed entries after each trigger, one run at a time; triggers that arrive during
// a run coalesce into the next one. It never runs inside a request.
func (s *service) embedWorker(ctx context.Context, trigger <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-trigger:
		}
		// A failed run leaves entries without a vector, which `status` reports as uncovered.
		if st, err := s.ready(); err == nil {
			_ = st.EmbedPending(ctx, s.model)
		}
	}
}
