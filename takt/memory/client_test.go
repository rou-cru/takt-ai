package memory

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEnsureServeAvailabilityBudget(t *testing.T) {
	for _, callerShorter := range []bool{false, true} {
		t.Run(map[bool]string{false: "availability", true: "caller"}[callerShorter], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
			defer server.Close()
			httpClient := server.Client()
			httpClient.Timeout = time.Minute
			c := newClient(Config{HTTP: httpClient, EngramURL: server.URL, EngramBinary: "unused"})
			c.availabilityTimeout = 80 * time.Millisecond
			starts := 0
			c.startServe = func(string) error { starts++; return nil }
			ctx := context.Background()
			if callerShorter {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			start := time.Now()
			err := c.ensureServe(ctx)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error: %v", err)
			}
			if time.Since(start) > time.Second {
				t.Fatal("first health request escaped budget")
			}
			if starts != 0 {
				t.Fatalf("started after deadline: %d", starts)
			}
			if !callerShorter && !strings.Contains(err.Error(), "within 80ms") {
				t.Fatal(err)
			}
			if c.http != httpClient || httpClient.Timeout != time.Minute {
				t.Fatal("mutated caller HTTP client")
			}
		})
	}
}

func TestEnsureServeSharesDeadlineAndStartsOnce(t *testing.T) {
	for _, healthy := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "healthy"}[healthy], func(t *testing.T) {
			var deadline time.Time
			calls, starts := 0, 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				d, ok := r.Context().Deadline()
				if !ok {
					t.Fatal("request has no deadline")
				}
				if calls == 1 {
					deadline = d
				} else if !deadline.Equal(d) {
					t.Fatal("deadline reset between probes")
				}
				if calls == 1 {
					return nil, errors.New("offline")
				}
				if !healthy {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
			})}
			c := newClient(Config{HTTP: client, EngramBinary: "fake"})
			c.availabilityTimeout, c.pollInterval = 40*time.Millisecond, time.Millisecond
			c.startServe = func(string) error { starts++; return nil }
			err := c.ensureServe(context.Background())
			if healthy && err != nil {
				t.Fatal(err)
			}
			if !healthy && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error: %v", err)
			}
			if starts != 1 || calls != 2 {
				t.Fatalf("starts=%d calls=%d", starts, calls)
			}
		})
	}
}

func TestEnsureServeCancellationAndStartErrors(t *testing.T) {
	for _, scenario := range []string{"cancelled before health", "cancelled during health", "cancelled while waiting", "healthy", "no binary", "bad binary"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls, starts := 0, 0
			c := newClient(Config{EngramBinary: filepath.Join(t.TempDir(), "missing"), HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if scenario == "cancelled during health" {
					cancel()
				}
				if scenario == "healthy" {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
				}
				return nil, errors.New("offline")
			})}})
			c.availabilityTimeout, c.pollInterval = time.Second, time.Millisecond
			c.startServe = func(binary string) error {
				starts++
				if scenario == "cancelled while waiting" {
					cancel()
					return nil
				}
				return startDetachedServe(binary)
			}
			if scenario == "no binary" {
				c.binary = ""
			}
			if scenario == "cancelled before health" {
				cancel()
			}
			err := c.ensureServe(ctx)
			switch scenario {
			case "healthy":
				if err != nil || starts != 0 {
					t.Fatalf("%v, starts=%d", err, starts)
				}
			case "bad binary":
				if err == nil || !strings.Contains(err.Error(), "start engram serve") || starts != 1 {
					t.Fatalf("%v, starts=%d", err, starts)
				}
			case "no binary":
				if err == nil || !strings.Contains(err.Error(), "no engram binary") || starts != 0 {
					t.Fatalf("%v, starts=%d", err, starts)
				}
			default:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				wantStarts := 0
				if scenario == "cancelled while waiting" {
					wantStarts = 1
				}
				if starts != wantStarts {
					t.Fatalf("starts=%d", starts)
				}
			}
			if scenario == "cancelled before health" && calls != 0 {
				t.Fatal("health called after cancellation")
			}
		})
	}
}

func TestEnsureServeDeadlineDoesNotKillDetachedProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "survived")
	binary := filepath.Join(dir, "engram")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nsleep 0.1\nprintf survived > '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := newClient(Config{EngramBinary: binary, HTTP: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}})
	c.availabilityTimeout, c.pollInterval = 20*time.Millisecond, time.Millisecond
	if err := c.ensureServe(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
	// The detached command is intentionally not tied to the caller's deadline;
	// allow instrumented binaries enough time to start under the race detector.
	deadline := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("detached process did not survive wait timeout")
		case <-ticker.C:
			if data, err := os.ReadFile(marker); err == nil && string(data) == "survived" {
				return
			}
		}
	}
}

func TestEnsureServeBlockedHealthBodyIsNotHealthy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	c := newClient(Config{EngramURL: server.URL})
	c.availabilityTimeout = 20 * time.Millisecond
	if err := c.ensureServe(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}
