// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stubRunner records every call and replays a canned answer. Tests key the
// answer by "METHOD /path" so multi-call flows (like the handshake) can mix
// responses.
type stubRunner struct {
	calls  [][]string
	routes map[string]stubResponse
}

type stubResponse struct {
	stdout string
	stderr string
	err    error
}

func (s *stubRunner) run(_ context.Context, args []string) ([]byte, []byte, error) {
	s.calls = append(s.calls, args)
	key := fmt.Sprintf("%s %s", args[len(args)-2], args[len(args)-1])
	response, ok := s.routes[key]
	if !ok {
		return nil, nil, fmt.Errorf("stub has no route for %q", key)
	}
	return []byte(response.stdout), []byte(response.stderr), response.err
}

func stubClient(routes map[string]stubResponse) (*Client, *stubRunner) {
	stub := &stubRunner{routes: routes}
	return New(WithRunner(stub.run)), stub
}

func TestDoSendsMethodAndPathThroughTheAPISubcommand(t *testing.T) {
	client, stub := stubClient(map[string]stubResponse{
		"GET /api/info": {stdout: `{"version":"2.0.16"}`},
	})

	if _, err := client.Info(context.Background()); err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	// The raw "METHOD path" form needs no OpenAPI lookup and no extra flags
	// by default: the CLI resolves the background service on its own.
	want := []string{"api", "GET", "/api/info"}
	if !reflect.DeepEqual(stub.calls[0], want) {
		t.Fatalf("args = %v, want %v", stub.calls[0], want)
	}
}

func TestDoAddsDataFlagBeforeTheRequestPair(t *testing.T) {
	client, stub := stubClient(map[string]stubResponse{
		"POST /api/experimental/generate": {stdout: "{}"},
	})

	_, _ = client.do(context.Background(), "POST", "/api/experimental/generate", []byte(`{"q":"x"}`))

	want := []string{"api", "--data", `{"q":"x"}`, "POST", "/api/experimental/generate"}
	if !reflect.DeepEqual(stub.calls[0], want) {
		t.Fatalf("args = %v, want %v", stub.calls[0], want)
	}
}

func TestMissingBinaryMapsToErrBinaryMissing(t *testing.T) {
	for name, cause := range map[string]error{
		"exec lookup": &exec.Error{Name: "opencode", Err: exec.ErrNotFound},
		"generic fs":  fs.ErrNotExist,
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := stubClient(map[string]stubResponse{})
			client.run = func(context.Context, []string) ([]byte, []byte, error) {
				return nil, nil, cause
			}

			if _, err := client.Info(context.Background()); !errors.Is(err, ErrBinaryMissing) {
				t.Fatalf("Info() error = %v, want ErrBinaryMissing", err)
			}
		})
	}
}

func TestFailedCallMapsToErrUnavailableAndCarriesStderr(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{})
	client.run = func(_ context.Context, args []string) ([]byte, []byte, error) {
		return nil, []byte("Could not reach server at http://127.0.0.1:59999\n"), errors.New("exit status 1")
	}

	_, err := client.Info(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Info() error = %v, want ErrUnavailable", err)
	}
	if !strings.Contains(err.Error(), "Could not reach server at http://127.0.0.1:59999") {
		t.Fatalf("error = %v, want it to carry the CLI stderr", err)
	}
}

func TestFailedCallWithoutStderrFallsBackToTheCause(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{})
	client.run = func(context.Context, []string) ([]byte, []byte, error) {
		return nil, nil, errors.New("signal: killed")
	}

	_, err := client.Info(context.Background())
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "signal: killed") {
		t.Fatalf("Info() error = %v, want ErrUnavailable naming the cause", err)
	}
}

func TestEveryCallRunsUnderTheDefaultTimeout(t *testing.T) {
	// The timeout is the only thing standing between Takt and a hung CLI, so
	// it must be enforced for every call.
	client := New(WithRunner(func(ctx context.Context, _ []string) ([]byte, []byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("runner received a context without deadline")
		}
		if remaining := time.Until(deadline); remaining > DefaultTimeout {
			t.Errorf("default deadline is %v away, want %v", remaining, DefaultTimeout)
		}
		return []byte(`{"version":"2.0.16"}`), nil, nil
	}))
	if _, err := client.Info(context.Background()); err != nil {
		t.Fatalf("Info() error = %v", err)
	}
}
