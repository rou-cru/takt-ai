package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

// firstObservationID seeds the fake store's observation counter.
const firstObservationID = 100

// minimalEngram answers the Engram calls a record or close makes and counts observation writes.
func minimalEngram(t *testing.T) (memory.Config, *atomic.Int64) {
	t.Helper()
	var next atomic.Int64
	next.Store(firstObservationID)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"status":"ok"}`)) })
	mux.HandleFunc("GET /project/current", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"project": "demo"})
	})
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"status":"created"}`))
	})
	mux.HandleFunc("POST /observations", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]int64{"id": next.Add(1)})
	})
	mux.HandleFunc("POST /conflicts/compare", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"sync_id":"rel-x"}`)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return memory.Config{Root: t.TempDir(), EngramURL: srv.URL, HTTP: srv.Client()}, &next
}

func TestFallbackCloseIsIdempotentAndNeedsNoDirectory(t *testing.T) {
	ctx := context.Background()
	cfg, writes := minimalEngram(t)
	if _, err := memory.Record(ctx, cfg, memory.RecordRequest{
		Author: "dev", Session: "ses_1", Directory: "/work/demo",
		Nature: "proposal", Scope: "project", Title: "Split the parser", Content: "The parser mixes IO and parsing.",
	}); err != nil {
		t.Fatal(err)
	}
	// Same Root, no directory and no prior process state: a restarted plugin's request.
	closeReq := memory.CloseRequest{Author: "takt", Session: "ses_1", Fallback: true}
	res, err := memory.Close(ctx, cfg, closeReq)
	if err != nil || res.EndAnchorID == 0 || res.Entries != 1 {
		t.Fatalf("fallback close = %+v, %v", res, err)
	}
	before := writes.Load()
	if res, err := memory.Close(ctx, cfg, closeReq); err != nil || res != (memory.CloseResult{}) || writes.Load() != before {
		t.Fatalf("second fallback close = %+v, %v, writes %d -> %d", res, err, before, writes.Load())
	}
	// A closed session can now be continued from.
	if err := memory.Continue(ctx, cfg, memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: t.TempDir(), PreviousSession: "ses_1"}); err != nil {
		t.Fatalf("continue from a fallback-closed session: %v", err)
	}
}

func TestFallbackCloseOfUnknownSessionIsNoOp(t *testing.T) {
	cfg, writes := minimalEngram(t)
	res, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Author: "takt", Session: "ses_none", Fallback: true})
	if err != nil || res != (memory.CloseResult{}) || writes.Load() != firstObservationID {
		t.Fatalf("res=%+v err=%v writes=%d", res, err, writes.Load())
	}
}

func TestFallbackCloseOfUnknownSessionLeavesNoFile(t *testing.T) {
	cfg, _ := minimalEngram(t)
	if _, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Author: "takt", Session: "ses_none", Fallback: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.Root, ".takt-ai")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("fallback close created state: %v", err)
	}
	// With the sessions directory present, its content stays identical.
	dir := filepath.Join(cfg.Root, ".takt-ai", "memory", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Author: "takt", Session: "ses_none", Fallback: true}); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("sessions dir = %v, %v", entries, err)
	}
}

// concurrentRounds is the number of independent sessions raced record against fallback close.
const concurrentRounds = 40

func TestConcurrentRecordAndFallbackCloseKeepEntry(t *testing.T) {
	ctx := context.Background()
	cfg, _ := minimalEngram(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2*concurrentRounds)
	for i := 0; i < concurrentRounds; i++ {
		session := fmt.Sprintf("ses_race_%d", i)
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := memory.Record(ctx, cfg, memory.RecordRequest{
				Author: "dev", Session: session, Directory: "/work/demo",
				Nature: "proposal", Scope: "project", Title: "Split the parser", Content: "The parser mixes IO and parsing.",
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: session, Fallback: true})
			errs <- err
		}()
		close(start)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < concurrentRounds; i++ {
		ids, err := memory.EntryIDsForSession(cfg.Root, fmt.Sprintf("ses_race_%d", i))
		if err != nil || len(ids) != 1 {
			t.Fatalf("session %d entries = %v, %v", i, ids, err)
		}
	}
}

// stateAbsent fails when the Root holds any session state.
func stateAbsent(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".takt-ai")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("state was created: %v", err)
	}
}

// An explicit close of a never-recorded session still records it, with start and end anchors (MEM-OPS-4/5).
func TestExplicitCloseOfUnknownSessionRecordsClosedSession(t *testing.T) {
	ctx := context.Background()
	cfg, writes := minimalEngram(t)
	dir := t.TempDir()
	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_none", Directory: dir})
	if err != nil || res.EndAnchorID == 0 || res.Entries != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := writes.Load() - firstObservationID; got != 2 {
		t.Fatalf("observation writes = %d, want start and end anchors", got)
	}
	if err := memory.Continue(ctx, cfg, memory.ContinueRequest{Author: "takt", Session: "ses_next", Directory: dir, PreviousSession: "ses_none"}); err != nil {
		t.Fatalf("continue from the closed session: %v", err)
	}
}

func TestContinueFromUnknownSessionCreatesNothing(t *testing.T) {
	cfg, _ := minimalEngram(t)
	err := memory.Continue(context.Background(), cfg, memory.ContinueRequest{Author: "takt", Session: "ses_new", Directory: t.TempDir(), PreviousSession: "ses_none"})
	if err == nil {
		t.Fatal("continue from an unknown session was accepted")
	}
	stateAbsent(t, cfg.Root)
}

func TestExplicitCloseAndContinueWorkOnRecordedSessions(t *testing.T) {
	ctx := context.Background()
	cfg, _ := minimalEngram(t)
	dir := t.TempDir()
	if _, err := memory.Record(ctx, cfg, memory.RecordRequest{
		Author: "dev", Session: "ses_1", Directory: dir,
		Nature: "proposal", Scope: "project", Title: "Split the parser", Content: "The parser mixes IO and parsing.",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: dir})
	if err != nil || res.EndAnchorID == 0 || res.Entries != 1 {
		t.Fatalf("close = %+v, %v", res, err)
	}
	if err := memory.Continue(ctx, cfg, memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentRecordAndExplicitCloseAreRaceFree(t *testing.T) {
	ctx := context.Background()
	cfg, _ := minimalEngram(t)
	dir := t.TempDir()
	var wg sync.WaitGroup
	errs := make(chan error, 2*concurrentRounds)
	for i := 0; i < concurrentRounds; i++ {
		session := fmt.Sprintf("ses_xrace_%d", i)
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, err := memory.Record(ctx, cfg, memory.RecordRequest{
				Author: "dev", Session: session, Directory: dir,
				Nature: "proposal", Scope: "project", Title: "Split the parser", Content: "The parser mixes IO and parsing.",
			})
			errs <- err
		}()
		go func() {
			defer wg.Done()
			<-start
			_, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: session, Directory: dir})
			errs <- err
		}()
		close(start)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}
