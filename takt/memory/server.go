package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

// Request paths of the service, one per principal.
const (
	PathCrew     = "/crew"
	PathExternal = "/external"
	PathAdmin    = "/admin"
)

// MCP request `_meta` keys that carry the caller's identity (MEM-AUT-7).
const (
	// MetaHostSession is the host session OpenCode sends on every tool call.
	MetaHostSession = "ai.opencode/sessionID"
	// MetaExternalSession is the session key the stdio adapter generates once per process.
	MetaExternalSession = "takt/session"
	// MetaExternalWorkspace is the key of the adapter's --workspace directory.
	MetaExternalWorkspace = "takt/workspace"
)

// Readiness states `status` reports.
const (
	StateReady      = "ready"
	StateNoStore    = "no_store"
	StateCorrupt    = "corrupt"
	StateNewerStore = "newer_store"
)

// StatusOutput is what `status` reports in every state (PR-MSV-32). Covered and Total
// count the entries with a vector of the current model; Covered below Total is the coverage gap.
type StatusOutput struct {
	Version string `json:"version"`
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Covered int64  `json:"covered"`
	Total   int64  `json:"total"`
}

// DescribeInput names the entries `describe` reports on.
type DescribeInput struct {
	IDs []int64 `json:"ids"`
}

// BackupInput is the destination of `backup`: an absolute path that does not exist yet.
type BackupInput struct {
	Path string `json:"path"`
}

// BackupOutput is the written copy.
type BackupOutput struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ReadInput, SessionsInput, ContinueInput, and CloseInput are the arguments of the tools whose
// arguments the store has no struct for.
type ReadInput struct {
	ID      int64  `json:"id" jsonschema:"ID of the entry to read."`
	Section string `json:"section,omitempty" jsonschema:"Heading of the one section to read, from the outline of an earlier read."`
}

type SessionsInput struct {
	Query         string `json:"query,omitempty" jsonschema:"Terms matched against close objectives and states and the titles of each session's entries."`
	AllWorkspaces bool   `json:"all_workspaces,omitempty" jsonschema:"List the sessions of every workspace instead of the active one."`
	Limit         int    `json:"limit,omitempty" jsonschema:"Maximum number of sessions."`
}

type ContinueInput struct {
	PreviousSessionID string `json:"previous_session_id" jsonschema:"ID of the closed session, as the sessions tool lists it or the user gives it; never inferred. Empty withdraws the declaration."`
}

type CloseInput struct {
	Objective string `json:"objective" jsonschema:"What the user asked for in this session, in one or two sentences."`
	State     string `json:"state" jsonschema:"What is true now, in present tense: governing decisions, open disagreements, and what was delivered. Never restate, merge, or upgrade the entries, and never include next steps or future work."`
}

// service holds what the tools read: the store when there is one, the readiness it came from, and
// the model. mu guards store, state, and reason; every tool reads them under it (STR-15).
type service struct {
	dir      string
	version  string
	model    *embed.Model
	embedNow func()

	mu     sync.RWMutex
	store  *Store
	state  string
	reason string
}

// openLocked opens the store and records the readiness the attempt ends in. A newer or corrupt
// store leaves the service not ready with its own state; any other failure is returned.
// The caller holds mu for writing.
func (s *service) openLocked() error {
	st, err := Open(s.dir)
	switch {
	case err == nil:
		s.store, s.state, s.reason = st, StateReady, ""
		return nil
	case errors.Is(err, ErrNoStore):
		s.state = StateNoStore
	case errors.Is(err, ErrNewerStore):
		s.state = StateNewerStore
	case errors.Is(err, ErrCorrupt):
		s.state = StateCorrupt
	default:
		return err
	}
	s.reason = err.Error()
	return nil
}

func (s *service) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.store == nil {
		return nil
	}
	return s.store.Close()
}

// ready returns the open store, or a refusal that names the state and how to leave it.
func (s *service) ready() (*Store, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.store == nil {
		return nil, refuse("the Takt memory service is not ready (%s): %s; record nothing more and report this failure", s.state, s.reason)
	}
	return s.store, nil
}

// identityFrom resolves the caller from the request `_meta`. A missing key is a refusal naming the
// integration that supplies it.
func identityFrom(principal string, meta map[string]any) (Identity, error) {
	text := func(key string) string { v, _ := meta[key].(string); return v }
	if principal == PrincipalCrew {
		host := text(MetaHostSession)
		if host == "" {
			return Identity{}, refuse("this call carried no host session; /crew is reachable only through the Takt crew integration")
		}
		return Identity{Principal: principal, HostSession: host}, nil
	}
	session := text(MetaExternalSession)
	if session == "" {
		return Identity{}, refuse("this call carried no session key; connect through `takt-ai memory mcp`, which supplies it")
	}
	return Identity{Principal: principal, HostSession: session, Workspace: text(MetaExternalWorkspace)}, nil
}

// classify keeps the errors a caller can act on as tool errors, which the SDK returns with isError
// set so the model reads the continuation, and turns every other error into a JSON-RPC internal error.
func classify(err error) error {
	var refusal *RefusalError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &refusal), errors.Is(err, ErrBusy), errors.Is(err, ErrStoreExists):
		return err
	}
	return &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: err.Error()}
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// memoryCall is one /crew or /external tool: it receives the open store and the resolved identity
// and returns the text the caller reads.
type memoryCall[In any] func(ctx context.Context, st *Store, id Identity, in In) (string, error)

func memoryTool[In any](s *service, principal string, call memoryCall[In]) mcp.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
		id, err := identityFrom(principal, req.Params.GetMeta())
		if err != nil {
			return nil, nil, err
		}
		st, err := s.ready()
		if err != nil {
			return nil, nil, err
		}
		text, err := call(ctx, st, id, in)
		if err != nil {
			return nil, nil, classify(err)
		}
		return textResult(text), nil, nil
	}
}

// adminTool is one /admin tool that needs the open store.
func adminTool[In, Out any](s *service, call func(ctx context.Context, st *Store, in In) (Out, error)) mcp.ToolHandlerFor[In, Out] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		var zero Out
		st, err := s.ready()
		if err != nil {
			return nil, zero, err
		}
		out, err := call(ctx, st, in)
		return nil, out, classify(err)
	}
}

func (s *service) status(ctx context.Context) (StatusOutput, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := StatusOutput{Version: s.version, State: s.state, Reason: s.reason}
	if s.store == nil {
		return out, nil
	}
	var err error
	out.Covered, out.Total, err = s.store.Coverage(ctx)
	return out, err
}

// initStore creates the store and opens it. Check and creation share the write lock (STR-16), so two
// inits cannot both create it.
func (s *service) initStore(ctx context.Context) (StatusOutput, error) {
	s.mu.Lock()
	if s.store != nil {
		s.mu.Unlock()
		return StatusOutput{}, ErrStoreExists
	}
	err := Init(s.dir)
	if err == nil {
		err = s.openLocked()
	}
	s.mu.Unlock()
	if err != nil {
		return StatusOutput{}, classify(err)
	}
	return s.status(ctx)
}

func formatRecorded(r RecordResult) string {
	return fmt.Sprintf("Recorded #%d", r.ID)
}

func formatPending(previous string) string {
	if previous == "" {
		return "No continuation is declared."
	}
	return fmt.Sprintf("Pending: this session continues %s; it becomes permanent with this session's first entry.", previous)
}

func formatClose(c RecordedClose) string {
	return fmt.Sprintf("Closed session %s covering entries #%d to #%d.\nObjective: %s\nState: %s",
		c.Session, c.FirstSequence, c.LastSequence, c.Objective, c.State)
}

func toolText(tools []Tool, name string) Tool {
	for _, t := range tools {
		if t.Name == name {
			return t
		}
	}
	panic("memory: no tool text for " + name)
}

// schemaProperty finds the property a dotted Args key names; an array property is entered through its items.
func schemaProperty(root *jsonschema.Schema, path string) (*jsonschema.Schema, error) {
	cur := root
	for _, part := range strings.Split(path, ".") {
		if cur.Items != nil {
			cur = cur.Items
		}
		next, ok := cur.Properties[part]
		if !ok {
			return nil, fmt.Errorf("tool argument %q has no field in its input type", path)
		}
		cur = next
	}
	return cur, nil
}

// inputSchema infers the schema from the argument type, whose jsonschema tags carry the argument
// descriptions, and adds the enums, which the library cannot take from a tag.
func inputSchema[In any](enums map[string][]string) (*jsonschema.Schema, error) {
	s, err := jsonschema.For[In](nil)
	if err != nil {
		return nil, err
	}
	for path, values := range enums {
		p, err := schemaProperty(s, path)
		if err != nil {
			return nil, err
		}
		for _, v := range values {
			p.Enum = append(p.Enum, v)
		}
	}
	return s, nil
}

func addMemoryTool[In any](srv *mcp.Server, s *service, principal string, name string, enums map[string][]string, call memoryCall[In]) error {
	t := toolText(Tools, name)
	schema, err := inputSchema[In](enums)
	if err != nil {
		return fmt.Errorf("tool %s: %w", name, err)
	}
	mcp.AddTool(srv, &mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: schema}, memoryTool(s, principal, call))
	return nil
}

var (
	scopes = []string{ScopeWorkspace, ScopeGlobal}
	modes  = []string{ModeLexical, ModeSemantic}
)

// newMemoryServer builds the MCP server of /crew or /external: the same six tools for either principal.
func (s *service) newMemoryServer(principal string) (*mcp.Server, error) {
	srv := mcp.NewServer(&mcp.Implementation{Name: ServerKey, Version: s.version}, nil)
	for _, add := range []func() error{
		func() error {
			return addMemoryTool(srv, s, principal, ToolRecord,
				map[string][]string{"nature": Natures, "scope": scopes, "relates_to.relation": Relations},
				func(ctx context.Context, st *Store, id Identity, in RecordArgs) (string, error) {
					res, err := st.Record(ctx, id, in)
					if err != nil {
						return "", err
					}
					s.embedNow()
					return formatRecorded(res), nil
				})
		},
		func() error {
			return addMemoryTool(srv, s, principal, ToolSearch,
				map[string][]string{"mode": modes, "scope": scopes},
				func(ctx context.Context, st *Store, id Identity, in SearchArgs) (string, error) {
					res, err := st.Search(ctx, id, in, s.model)
					if err != nil {
						return "", err
					}
					return res.Text(), nil
				})
		},
		func() error {
			return addMemoryTool(srv, s, principal, ToolRead, nil,
				func(ctx context.Context, st *Store, _ Identity, in ReadInput) (string, error) {
					res, err := st.Read(ctx, in.ID, in.Section)
					if err != nil {
						return "", err
					}
					return res.Text(), nil
				})
		},
		func() error {
			return addMemoryTool(srv, s, principal, ToolSessions, nil,
				func(ctx context.Context, st *Store, id Identity, in SessionsInput) (string, error) {
					list, err := st.Sessions(ctx, id, in.Query, in.AllWorkspaces, in.Limit)
					if err != nil {
						return "", err
					}
					return FormatSessions(list), nil
				})
		},
		func() error {
			return addMemoryTool(srv, s, principal, ToolContinueSession, nil,
				func(ctx context.Context, st *Store, id Identity, in ContinueInput) (string, error) {
					previous, err := st.ContinueSession(ctx, id, in.PreviousSessionID)
					if err != nil {
						return "", err
					}
					return formatPending(previous), nil
				})
		},
		func() error {
			return addMemoryTool(srv, s, principal, ToolCloseSession, nil,
				func(ctx context.Context, st *Store, id Identity, in CloseInput) (string, error) {
					c, err := st.CloseSession(ctx, id, in.Objective, in.State)
					if err != nil {
						return "", err
					}
					return formatClose(c), nil
				})
		},
	} {
		if err := add(); err != nil {
			return nil, err
		}
	}
	return srv, nil
}

// newAdminServer builds the MCP server of /admin, with typed input and output for Takt's own callers.
func (s *service) newAdminServer() *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{Name: ServerKey, Version: s.version}, nil)
	text := func(name string) *mcp.Tool { return &mcp.Tool{Name: name} }
	mcp.AddTool(srv, text(AdminBind), adminTool(s, func(ctx context.Context, st *Store, in BindArgs) (struct{}, error) {
		return struct{}{}, st.Bind(ctx, in)
	}))
	mcp.AddTool(srv, text(AdminDescribe), adminTool(s, func(ctx context.Context, st *Store, in DescribeInput) (DescribeResult, error) {
		res, err := st.Describe(ctx, in.IDs)
		if res.Entries == nil {
			res.Entries = []Described{}
		}
		return res, err
	}))
	mcp.AddTool(srv, text(AdminStatus), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, StatusOutput, error) {
		out, err := s.status(ctx)
		return nil, out, classify(err)
	})
	mcp.AddTool(srv, text(AdminInit), func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, StatusOutput, error) {
		out, err := s.initStore(ctx)
		return nil, out, err
	})
	mcp.AddTool(srv, text(AdminBackup), adminTool(s, func(ctx context.Context, st *Store, in BackupInput) (BackupOutput, error) {
		path, size, err := st.Backup(ctx, in.Path)
		return BackupOutput{Path: path, Size: size}, err
	}))
	return srv
}
