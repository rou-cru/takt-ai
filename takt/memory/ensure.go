package memory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rou-cru/takt-ai/takt/internal/flock"
)

const (
	// ProbeTimeout bounds one status query so a stuck owner cannot stall a caller; every caller of Probe
	// uses it.
	ProbeTimeout = 2 * time.Second
	// stopPoll is how long one wait for a stopping owner's lock lasts before the wait is retried.
	stopPoll = time.Second
	// stopWait exceeds the owner's shutdown wait, so an owner finishing in-flight requests is not misreported as stuck.
	stopWait = shutdownWait + 5*time.Second
	// launchWait bounds how long a launched owner may take to answer, covering the semantic model load.
	launchWait = 15 * time.Second
	// launchPoll is the interval between status queries while a launched owner starts.
	launchPoll = 50 * time.Millisecond
	// memoryDirMode keeps the memory directory private to the local user.
	memoryDirMode = 0o700
)

// UnavailableError reports that no usable memory service could be reached or started (PR-MSV-2). It
// is never an empty or newly created store.
type UnavailableError struct {
	Reason string
}

func (e *UnavailableError) Error() string {
	return "Takt memory service is unavailable: " + e.Reason
}

func unavailable(format string, args ...any) *UnavailableError {
	return &UnavailableError{Reason: fmt.Sprintf(format, args...)}
}

// Ensure makes the owner of dir available and returns its status (PR-MSV-35): a
// running owner is reused, never replaced. It never creates the directory or the store: a service that
// is up without a store is returned with its state and reason, for the caller to report. Every failure
// is an *UnavailableError.
func Ensure(ctx context.Context, dir string) (StatusOutput, error) {
	if _, err := os.Stat(dir); err != nil {
		return StatusOutput{}, &UnavailableError{Reason: fmt.Sprintf("Takt memory directory is not available: %v; create it with `takt-ai memory start --init`", err)}
	}
	if _, err := SocketPath(dir); err != nil {
		return StatusOutput{}, &UnavailableError{Reason: err.Error()}
	}
	owner, err := probeWithin(ctx, dir)
	switch {
	case err == nil:
		return owner, nil
	case errors.Is(err, ErrUnreachable):
		return launch(ctx, dir)
	}
	return StatusOutput{}, unavailable("%v", err)
}

func probeWithin(ctx context.Context, dir string) (StatusOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	return Probe(ctx, dir)
}

// Health is the outcome of probing the service of a directory, the one mapping from a probe to a state
// that doctor and verification report from.
type Health int

const (
	// HealthUnreachable means nothing answers on the socket.
	HealthUnreachable Health = iota
	// HealthMalformed means something answered that is not a valid memory service.
	HealthMalformed
	// HealthNewerStore means the store is newer than this build.
	HealthNewerStore
	// HealthNotReady means the service reports any other state than ready, no store among them.
	HealthNotReady
	// HealthUncovered means the service is ready but some entries have no semantic vector.
	HealthUncovered
	// HealthReady means the service is ready, with every entry covered.
	HealthReady
)

// Assess probes the owner of dir without starting it (PR-MSV-33) and classifies the answer. The status is
// valid unless the health is HealthUnreachable or HealthMalformed; for HealthMalformed, err says why.
func Assess(ctx context.Context, dir string) (StatusOutput, Health, error) {
	status, err := probeWithin(ctx, dir)
	switch {
	case errors.Is(err, ErrUnreachable):
		return status, HealthUnreachable, err
	case err != nil:
		return status, HealthMalformed, err
	case status.State == StateNewerStore:
		return status, HealthNewerStore, nil
	case status.State != StateReady:
		return status, HealthNotReady, nil
	case status.Covered < status.Total:
		return status, HealthUncovered, nil
	}
	return status, HealthReady, nil
}

// StopOwner sends SIGTERM to the PID in the lock file and waits, bounded, for the lock to be released.
// An owner that is already gone is not a failure.
func StopOwner(ctx context.Context, dir string) (err error) {
	lockPath := filepath.Join(dir, LockFile)
	pid, err := strconv.Atoi(lockOwner(lockPath))
	if err != nil {
		return unavailable("a Takt memory service runs but %s records no PID to stop", lockPath)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return unavailable("stop the Takt memory service (pid %d): %v", pid, err)
	}
	lock, err := os.OpenFile(lockPath, os.O_RDWR, ownerFileMode)
	if err != nil {
		return unavailable("open %s: %v", lockPath, err)
	}
	defer func() { err = errors.Join(err, lock.Close()) }()
	deadline := time.Now().Add(stopWait)
	for {
		err := flock.Exclusive(ctx, lock, stopPoll)
		if err == nil {
			return nil
		}
		if !errors.Is(err, flock.ErrTimeout) || ctx.Err() != nil || time.Now().After(deadline) {
			return unavailable("the Takt memory service (pid %d) did not stop: %v", pid, err)
		}
	}
}

// launch starts `memory serve` detached in its own session and polls status until it answers. A launched
// process that exits is not a failure by itself: it may have lost the lock to a concurrent Ensure, whose
// owner answers; only an unanswered deadline fails.
func launch(ctx context.Context, dir string) (StatusOutput, error) {
	exe, err := os.Executable()
	if err != nil {
		return StatusOutput{}, unavailable("locate executable: %v", err)
	}
	cmd := exec.Command(exe, "memory", "serve")
	cmd.Env = append(os.Environ(), EnvDir+"="+dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// A launched process that fails to start reports its cause on stderr; this is read only once it exits.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return StatusOutput{}, unavailable("launch %s: %v", exe, err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	deadline := time.NewTimer(launchWait)
	defer deadline.Stop()
	tick := time.NewTicker(launchPoll)
	defer tick.Stop()
	exitNote := ""
	for {
		if st, err := probeWithin(ctx, dir); err == nil {
			return st, nil
		}
		select {
		case err := <-exited:
			exitNote = fmt.Sprintf(" (the launched process exited: %v: %s)", err, strings.TrimSpace(stderr.String()))
		case <-deadline.C:
			return StatusOutput{}, unavailable("the Takt memory service did not answer within %s%s", launchWait, exitNote)
		case <-ctx.Done():
			return StatusOutput{}, unavailable("waiting for the Takt memory service: %v", ctx.Err())
		case <-tick.C:
		}
	}
}

// Start is `takt-ai memory start`: it ensures the owner and returns its status once it is
// ready. With create it first creates the directory (mode 0700), and creates the store through /admin
// init when there is none. Without create, a missing directory or store is reported, never created.
func Start(ctx context.Context, dir string, create bool) (_ StatusOutput, err error) {
	if create {
		if err := os.MkdirAll(dir, memoryDirMode); err != nil {
			return StatusOutput{}, err
		}
		if err := os.Chmod(dir, memoryDirMode); err != nil {
			return StatusOutput{}, err
		}
	}
	st, err := Ensure(ctx, dir)
	if err != nil {
		return StatusOutput{}, err
	}
	if st.State == StateNoStore && create {
		admin, dialErr := DialAdmin(ctx, dir)
		if dialErr != nil {
			return StatusOutput{}, dialErr
		}
		defer func() { err = errors.Join(err, admin.Close()) }()
		if st, err = admin.Init(ctx); err != nil {
			return StatusOutput{}, err
		}
	}
	if st.State != StateReady {
		hint := ""
		if st.State == StateNoStore {
			hint = "; create it with `takt-ai memory start --init`"
		}
		return st, fmt.Errorf("memory service is not ready (%s): %s%s", st.State, st.Reason, hint)
	}
	return st, nil
}
