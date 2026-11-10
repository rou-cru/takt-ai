package memory

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// sessionKeyBytes is the entropy of the external session key an adapter process generates.
const sessionKeyBytes = 16

// AdapterOptions are the flags of `takt-ai memory mcp`.
type AdapterOptions struct {
	// Crew relays to /crew with every message unchanged; otherwise to /external with identity added.
	Crew bool
	// Workspace is the external client's workspace directory; empty means no workspace.
	Workspace string
}

// RunAdapter is the stdio MCP adapter: it ensures the owner, then relays each
// JSON-RPC line of in to the owner as one POST and writes one line to out per JSON response. A
// notification produces no output. It returns when in ends.
func RunAdapter(ctx context.Context, dir string, opts AdapterOptions, in io.Reader, out io.Writer) error {
	if _, err := Ensure(ctx, dir); err != nil {
		return err
	}
	a := &adapter{http: unixHTTPClient(dir), url: endpoint(PathCrew)}
	if !opts.Crew {
		raw := make([]byte, sessionKeyBytes)
		if _, err := rand.Read(raw); err != nil {
			return err
		}
		a.url = endpoint(PathExternal)
		a.identity = map[string]any{MetaExternalSession: hex.EncodeToString(raw)}
		if opts.Workspace != "" {
			a.identity[MetaExternalWorkspace] = WorkspaceKey(ctx, opts.Workspace)
		}
	}
	reader := bufio.NewReader(in)
	for {
		line, err := reader.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 {
			reply, rerr := a.relay(ctx, line)
			if rerr != nil {
				return rerr
			}
			if reply != nil {
				if _, werr := out.Write(append(reply, '\n')); werr != nil {
					return werr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type adapter struct {
	url  string
	http *http.Client
	// identity is the `_meta` the adapter sets on every external request; nil on /crew.
	identity map[string]any
}

// relay handles one message and returns the line to write, or nil for none. Failures a client can read
// become JSON-RPC errors; only a failure to produce output ends the adapter.
func (a *adapter) relay(ctx context.Context, line []byte) (_ []byte, err error) {
	var msg map[string]json.RawMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return rpcError(nil, jsonrpc.CodeParseError, "each line must be one JSON-RPC message object: "+err.Error()), nil
	}
	if msg == nil { // the line `null` decodes without error to a nil map
		return rpcError(nil, jsonrpc.CodeParseError, "each line must be one JSON-RPC message object: got null"), nil
	}
	id := msg["id"]
	body := line
	if a.identity != nil {
		var err error
		if body, err = withIdentity(msg, a.identity); err != nil {
			return rpcError(id, jsonrpc.CodeInvalidParams, err.Error()), nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	// The stateless Streamable HTTP handler refuses a POST without a JSON Content-Type and an Accept
	// naming both application/json and text/event-stream (go-sdk v1.8.0 mcp/streamable.go:388-397); it
	// needs no protocol version header.
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := a.http.Do(req)
	if err != nil {
		return reply(id, jsonrpc.CodeInternalError, "the Takt memory service is unavailable: "+err.Error()), nil
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return reply(id, jsonrpc.CodeInternalError, "the Takt memory service is unavailable: "+err.Error()), nil
	}
	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil, nil
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		// The transport states its refusal in the body, such as the request cap of a 413; the
		// client sees only what is relayed here.
		return reply(id, jsonrpc.CodeInternalError, fmt.Sprintf("the Takt memory service refused the request (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(payload)))), nil
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, payload); err != nil {
		return reply(id, jsonrpc.CodeInternalError, "the Takt memory service answered with a malformed response: "+err.Error()), nil
	}
	return compact.Bytes(), nil
}

// reply answers a call; a notification has no id and gets no output.
func reply(id json.RawMessage, code int64, message string) []byte {
	if id == nil {
		return nil
	}
	return rpcError(id, code, message)
}

func rpcError(id json.RawMessage, code int64, message string) []byte {
	if id == nil {
		id = json.RawMessage("null")
	}
	out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": message}})
	return out
}

// withIdentity sets the identity keys in the message's params._meta, overwriting what the client sent,
// and removes the workspace key the client sent when the adapter has none. Other values keep
// their raw JSON, so large integers survive.
func withIdentity(msg map[string]json.RawMessage, identity map[string]any) ([]byte, error) {
	params := map[string]json.RawMessage{}
	if raw := msg["params"]; raw != nil {
		if err := json.Unmarshal(raw, &params); err != nil || params == nil {
			return nil, errors.New("params must be a JSON object")
		}
	}
	meta := map[string]json.RawMessage{}
	if raw := params["_meta"]; raw != nil {
		if err := json.Unmarshal(raw, &meta); err != nil || meta == nil {
			return nil, errors.New("params._meta must be a JSON object")
		}
	}
	delete(meta, MetaExternalWorkspace)
	for k, v := range identity {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		meta[k] = raw
	}
	var err error
	if params["_meta"], err = json.Marshal(meta); err != nil {
		return nil, err
	}
	if msg["params"], err = json.Marshal(params); err != nil {
		return nil, err
	}
	return json.Marshal(msg)
}
