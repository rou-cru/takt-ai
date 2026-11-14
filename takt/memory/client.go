package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Outcomes of probing the service that the doctor reports apart (PR-MSV-33).
var (
	// ErrUnreachable means nothing accepted a connection on the socket.
	ErrUnreachable = errors.New("the Takt memory service is unreachable")
	// ErrMalformed means something answered that is not a valid memory service.
	ErrMalformed = errors.New("the Takt memory service answered with a malformed response")
)

// clientName identifies Takt's own admin callers to the service.
const clientName = "takt-ai"

// unixHTTPClient reaches the service over the Unix socket of dir; the URL host is not used.
func unixHTTPClient(dir string) *http.Client {
	sock := filepath.Join(dir, SocketFile)
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
}

// endpoint is the URL of a service path; the host is a placeholder because the transport dials the socket.
func endpoint(path string) string { return "http://memory" + path }

// Admin is a connection to /admin for Takt's own callers (PR-MSV-41).
type Admin struct{ session *mcp.ClientSession }

// DialAdmin connects to the owner of dir. It never starts the owner; see Ensure.
func DialAdmin(ctx context.Context, dir string) (*Admin, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: clientName, Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:             endpoint(PathAdmin),
		HTTPClient:           unixHTTPClient(dir),
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return &Admin{session: session}, nil
}

// Close ends the connection.
func (a *Admin) Close() error { return a.session.Close() }

func adminCall[Out any](ctx context.Context, a *Admin, tool string, in any) (Out, error) {
	var out Out
	res, err := a.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: in})
	if err != nil {
		return out, err
	}
	if res.IsError {
		if len(res.Content) > 0 {
			if t, ok := res.Content[0].(*mcp.TextContent); ok {
				return out, errors.New(t.Text)
			}
		}
		return out, fmt.Errorf("%s failed", tool)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("%w: %s result: %v", ErrMalformed, tool, err)
	}
	return out, nil
}

// Status reports the service's state.
func (a *Admin) Status(ctx context.Context) (StatusOutput, error) {
	return adminCall[StatusOutput](ctx, a, AdminStatus, struct{}{})
}

// Bind records a crew binding.
func (a *Admin) Bind(ctx context.Context, in BindArgs) error {
	_, err := adminCall[struct{}](ctx, a, AdminBind, in)
	return err
}

// Describe reports on the named entries.
func (a *Admin) Describe(ctx context.Context, in DescribeInput) (DescribeResult, error) {
	return adminCall[DescribeResult](ctx, a, AdminDescribe, in)
}

// Init creates the store when there is none.
func (a *Admin) Init(ctx context.Context) (StatusOutput, error) {
	return adminCall[StatusOutput](ctx, a, AdminInit, struct{}{})
}

// Backup writes a consistent copy of the store to a path that does not exist yet.
func (a *Admin) Backup(ctx context.Context, in BackupInput) (BackupOutput, error) {
	return adminCall[BackupOutput](ctx, a, AdminBackup, in)
}

// Shutdown asks the owner to stop. It returns once the owner accepted; the caller waits for the lock.
func (a *Admin) Shutdown(ctx context.Context) error {
	_, err := adminCall[struct{}](ctx, a, AdminShutdown, struct{}{})
	return err
}

// Probe reads the service's status and never starts it, for doctor and verification (PR-MSV-33). The
// error wraps ErrUnreachable when nothing listens and ErrMalformed when the answer is not a memory service.
func Probe(ctx context.Context, dir string) (_ StatusOutput, err error) {
	admin, err := DialAdmin(ctx, dir)
	if err != nil {
		return StatusOutput{}, err
	}
	defer func() { err = errors.Join(err, admin.Close()) }()
	out, err := admin.Status(ctx)
	if err != nil && !errors.Is(err, ErrMalformed) {
		return StatusOutput{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return out, err
}
