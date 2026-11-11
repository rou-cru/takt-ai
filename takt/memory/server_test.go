package memory_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	_ "modernc.org/sqlite"

	"github.com/rou-cru/takt-ai/takt/internal/flock"
	"github.com/rou-cru/takt-ai/takt/memory"
)

// requestCap is the cap the transport states in its refusal; it equals the go-sdk default of 4 MiB.
const requestCap = 4 << 20

// readyWait bounds how long a test waits for the service to answer.
const readyWait = 10 * time.Second

// shortDir returns a memory directory under /tmp: macOS t.TempDir paths can exceed the socket path limit.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

type running struct {
	dir    string
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
	err    error
}

// stop cancels the service and returns what Serve returned; later calls return the same result.
func (r *running) stop(t *testing.T) error {
	t.Helper()
	r.cancel()
	r.once.Do(func() {
		select {
		case r.err = <-r.done:
		case <-time.After(readyWait):
			r.err = errors.New("serve did not return after cancel")
		}
	})
	return r.err
}

func start(t *testing.T, dir string) *running {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	r := &running{dir: dir, cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- memory.Serve(ctx, dir, "test") }()
	t.Cleanup(func() { _ = r.stop(t) })
	deadline := time.Now().Add(readyWait)
	for {
		if _, err := os.Stat(filepath.Join(dir, memory.SocketFile)); err == nil {
			return r
		}
		select {
		case err := <-r.done:
			t.Fatalf("Serve returned early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("socket never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *running) connect(t *testing.T, path string) *mcp.ClientSession {
	t.Helper()
	sock := filepath.Join(r.dir, memory.SocketFile)
	hc := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	s, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: "http://memory" + path, HTTPClient: hc, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatalf("connect %s: %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func call(t *testing.T, s *mcp.ClientSession, name string, args any, meta mcp.Meta) *mcp.CallToolResult {
	t.Helper()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args, Meta: meta})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return res
}

func text(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	return res.Content[0].(*mcp.TextContent).Text
}

func status(t *testing.T, r *running) memory.StatusOutput {
	t.Helper()
	res := call(t, r.connect(t, memory.PathAdmin), memory.AdminStatus, map[string]any{}, nil)
	var out memory.StatusOutput
	raw, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func initStore(t *testing.T, dir string) {
	t.Helper()
	if err := memory.Init(dir); err != nil {
		t.Fatal(err)
	}
}

func crewMeta(session string) mcp.Meta { return mcp.Meta{memory.MetaHostSession: session} }

func recordArgs(title string) map[string]any {
	return map[string]any{"nature": "hypothesis", "scope": "global", "title": title, "content": "body of " + title}
}

func TestNotReadyWithoutStoreThenInit(t *testing.T) {
	dir := shortDir(t)
	r := start(t, dir)
	st := status(t, r)
	if st.State != memory.StateNoStore || st.Reason == "" || st.Version != "test" {
		t.Fatalf("status %+v", st)
	}
	crew := r.connect(t, memory.PathCrew)
	res := call(t, crew, memory.ToolRecord, recordArgs("a"), crewMeta("s1"))
	if !res.IsError || !strings.Contains(text(t, res), "not ready") {
		t.Fatalf("record without store: %+v", res)
	}
	admin := r.connect(t, memory.PathAdmin)
	if res := call(t, admin, memory.AdminInit, map[string]any{}, nil); res.IsError {
		t.Fatalf("init: %s", text(t, res))
	}
	if st := status(t, r); st.State != memory.StateReady {
		t.Fatalf("after init: %+v", st)
	}
	if res := call(t, admin, memory.AdminInit, map[string]any{}, nil); !res.IsError {
		t.Fatal("second init was accepted")
	}
}

func TestNewerStoreReported(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, memory.StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	st := status(t, start(t, dir))
	if st.State != memory.StateNewerStore || !strings.Contains(st.Reason, "newer") {
		t.Fatalf("status %+v", st)
	}
}

func TestCorruptStoreReported(t *testing.T) {
	dir := shortDir(t)
	if err := os.WriteFile(filepath.Join(dir, memory.StoreFile), bytes.Repeat([]byte("not a sqlite database "), 100), 0o600); err != nil {
		t.Fatal(err)
	}
	st := status(t, start(t, dir))
	if st.State != memory.StateCorrupt || st.Reason == "" {
		t.Fatalf("status %+v", st)
	}
}

func TestCrewRecordNeedsBindingThenIsAccepted(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	r := start(t, dir)
	crew := r.connect(t, memory.PathCrew)
	if res := call(t, crew, memory.ToolRecord, recordArgs("a"), nil); !res.IsError {
		t.Fatal("record without _meta was accepted")
	}
	if res := call(t, crew, memory.ToolRecord, recordArgs("a"), crewMeta("s1")); !res.IsError {
		t.Fatalf("record without binding was accepted: %s", text(t, res))
	}
	bind := map[string]any{"host_session": "s1", "agent": "takt", "claimed_root": "s1", "workspace": "ws"}
	if res := call(t, r.connect(t, memory.PathAdmin), memory.AdminBind, bind, nil); res.IsError {
		t.Fatalf("bind: %s", text(t, res))
	}
	args := map[string]any{"nature": "hypothesis", "scope": "workspace", "title": "a", "content": "alpha body"}
	if got := text(t, call(t, crew, memory.ToolRecord, args, crewMeta("s1"))); got != "Recorded #1" {
		t.Fatalf("got %q", got)
	}
	found := call(t, crew, memory.ToolSearch, map[string]any{"query": "alpha", "scope": "workspace"}, crewMeta("s1"))
	if found.IsError || !strings.Contains(text(t, found), "#1") {
		t.Fatalf("search: %s", text(t, found))
	}
}

func TestExternalRecordUsesMetaKeys(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	r := start(t, dir)
	ext := r.connect(t, memory.PathExternal)
	meta := mcp.Meta{memory.MetaExternalSession: "e1", memory.MetaExternalWorkspace: "ws"}
	args := map[string]any{"nature": "hypothesis", "scope": "workspace", "title": "a", "content": "body"}
	if got := text(t, call(t, ext, memory.ToolRecord, args, meta)); got != "Recorded #1" {
		t.Fatalf("got %q", got)
	}
	if res := call(t, ext, memory.ToolRecord, args, nil); !res.IsError {
		t.Fatal("external record without _meta was accepted")
	}
	noWorkspace := mcp.Meta{memory.MetaExternalSession: "e2"}
	res := call(t, ext, memory.ToolRecord, args, noWorkspace)
	if !res.IsError || !strings.Contains(text(t, res), "--workspace") {
		t.Fatalf("workspace tier without workspace: %+v", res)
	}
	if got := text(t, call(t, ext, memory.ToolRecord, recordArgs("g"), noWorkspace)); !strings.HasPrefix(got, "Recorded #") {
		t.Fatalf("global tier: %q", got)
	}
}

func TestToolsMatchTheCallerFacingTexts(t *testing.T) {
	dir := shortDir(t)
	r := start(t, dir)
	for _, path := range []string{memory.PathCrew, memory.PathExternal} {
		listed, err := r.connect(t, path).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(listed.Tools) != len(memory.Tools) {
			t.Fatalf("%s lists %d tools", path, len(listed.Tools))
		}
		for _, tool := range listed.Tools {
			i := slices.IndexFunc(memory.Tools, func(c memory.Tool) bool { return c.Name == tool.Name })
			if i < 0 {
				t.Fatalf("%s lists unknown tool %s", path, tool.Name)
			}
			want := memory.Tools[i]
			if tool.Description != want.Description {
				t.Fatalf("%s tool %s differs from tools.go", path, tool.Name)
			}
			raw, _ := json.Marshal(tool.InputSchema)
			var schema struct {
				Properties map[string]struct {
					Description string
					Enum        []string
				} `json:"properties"`
			}
			if err := json.Unmarshal(raw, &schema); err != nil {
				t.Fatal(err)
			}
			for _, banned := range []string{"session", "host_session", "workspace", "principal", "agent"} {
				if _, ok := schema.Properties[banned]; ok {
					t.Fatalf("%s has identity field %q", tool.Name, banned)
				}
			}
			for name, p := range schema.Properties {
				if p.Description == "" {
					t.Fatalf("%s argument %s has no description", tool.Name, name)
				}
			}
			if tool.Name == memory.ToolRecord && !slices.Equal(schema.Properties["nature"].Enum, memory.Natures) {
				t.Fatalf("record nature enum = %v, want %v", schema.Properties["nature"].Enum, memory.Natures)
			}
		}
	}
}

func TestBackupThroughAdmin(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	r := start(t, dir)
	ext := r.connect(t, memory.PathExternal)
	call(t, ext, memory.ToolRecord, recordArgs("a"), mcp.Meta{memory.MetaExternalSession: "e1"})
	admin := r.connect(t, memory.PathAdmin)
	dest := filepath.Join(dir, "copy.db")
	res := call(t, admin, memory.AdminBackup, map[string]any{"path": dest}, nil)
	if res.IsError {
		t.Fatalf("backup: %s", text(t, res))
	}
	info, err := os.Stat(dest)
	if err != nil || info.Size() == 0 {
		t.Fatalf("backup file: %v", err)
	}
	if st := status(t, r); st.State != memory.StateReady {
		t.Fatalf("status %+v", st)
	}
}

func TestServeNeedsTheDirectory(t *testing.T) {
	if err := memory.Serve(context.Background(), filepath.Join(shortDir(t), "missing"), "test"); err == nil {
		t.Fatal("Serve started without its directory")
	}
}

func TestShutdownRemovesSocketAndReleasesLock(t *testing.T) {
	dir := shortDir(t)
	r := start(t, dir)
	if err := memory.Serve(context.Background(), dir, "test"); err == nil {
		t.Fatal("a second owner started")
	}
	if err := r.stop(t); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, memory.SocketFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket remains: %v", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, memory.LockFile), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := flock.Exclusive(context.Background(), f, time.Second); err != nil {
		t.Fatalf("lock not released: %v", err)
	}
}

func TestSessionToolsReportTheCloseAndThePendingContinuation(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	r := start(t, dir)
	admin, crew := r.connect(t, memory.PathAdmin), r.connect(t, memory.PathCrew)
	for _, host := range []string{"old", "new"} {
		bind := map[string]any{"host_session": host, "agent": "takt", "claimed_root": host, "workspace": "ws"}
		if res := call(t, admin, memory.AdminBind, bind, nil); res.IsError {
			t.Fatalf("bind %s: %s", host, text(t, res))
		}
	}
	args := map[string]any{"nature": "hypothesis", "scope": "workspace", "title": "a", "content": "alpha body"}
	if res := call(t, crew, memory.ToolRecord, args, crewMeta("old")); res.IsError {
		t.Fatalf("record: %s", text(t, res))
	}
	closed := text(t, call(t, crew, memory.ToolCloseSession, map[string]any{"objective": "ship it", "state": "shipped"}, crewMeta("old")))
	for _, want := range []string{"Closed session", "Objective: ship it", "State: shipped"} {
		if !strings.Contains(closed, want) {
			t.Fatalf("close text %q lacks %q", closed, want)
		}
	}
	pending := text(t, call(t, crew, memory.ToolContinueSession, map[string]any{"previous_session_id": "crew:old"}, crewMeta("new")))
	if !strings.Contains(pending, "this session continues") {
		t.Fatalf("pending text %q", pending)
	}
	if got := text(t, call(t, crew, memory.ToolContinueSession, map[string]any{"previous_session_id": ""}, crewMeta("new"))); got != "No continuation is declared." {
		t.Fatalf("withdrawn text %q", got)
	}
}
