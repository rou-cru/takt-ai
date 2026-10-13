package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/rou-cru/takt-ai/takt/memory"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// otherProject is a project name that never matches the fake Engram's "demo".
	otherProject = "elsewhere"
	// unusedPort is a loopback port nothing listens on.
	unusedPort = "http://127.0.0.1:1"
)

// failingEngram serves a healthy Engram whose listed routes answer 500, so a
// test can break exactly one step of the record/close protocol.
func failingEngram(t *testing.T, failing ...string) memory.Config {
	t.Helper()
	_, cfg := newFake(t)
	upstream := cfg.HTTP
	target := cfg.EngramURL
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slices.Contains(failing, r.Method+" "+r.URL.Path) {
			http.Error(w, `{"error":"induced"}`, http.StatusInternalServerError)
			return
		}
		req, err := http.NewRequestWithContext(r.Context(), r.Method, target+r.URL.RequestURI(), r.Body)
		if err != nil {
			t.Errorf("build proxied request: %v", err)
			return
		}
		req.Header = r.Header.Clone()
		resp, err := upstream.Do(req)
		if err != nil {
			t.Errorf("proxy request: %v", err)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(proxy.Close)
	cfg.EngramURL = proxy.URL
	cfg.HTTP = proxy.Client()
	return cfg
}

func TestValidationErrorRendersReasonVerbatim(t *testing.T) {
	err := &memory.ValidationError{Reason: `session "x" is bad`}
	if err.Error() != `session "x" is bad` {
		t.Errorf("Error() = %q", err.Error())
	}
	wantValidation(t, err)
}

func TestRecordRejectsMalformedShapes(t *testing.T) {
	_, cfg := newFake(t)
	cases := map[string]func(*memory.RecordRequest){
		"unknown nature":          func(r *memory.RecordRequest) { r.Nature = "rumour" },
		"unknown scope":           func(r *memory.RecordRequest) { r.Scope = "galaxy" },
		"blank title":             func(r *memory.RecordRequest) { r.Title = "  " },
		"blank content":           func(r *memory.RecordRequest) { r.Content = "" },
		"blank session":           func(r *memory.RecordRequest) { r.Session = " " },
		"blank directory":         func(r *memory.RecordRequest) { r.Directory = "" },
		"dot session":             func(r *memory.RecordRequest) { r.Session = "." },
		"dotdot session":          func(r *memory.RecordRequest) { r.Session = ".." },
		"relation id not > 0":     func(r *memory.RecordRequest) { r.RelatesTo = &memory.RelatesTo{ID: 0, Relation: "corrects"} },
		"code in global memory":   func(r *memory.RecordRequest) { r.Scope, r.Confirmed, r.Content = "personal", true, "see ```x```" },
		"directory in global":     func(r *memory.RecordRequest) { r.Scope, r.Confirmed, r.Evidence = "personal", true, "at "+r.Directory },
		"decision blank proof":    func(r *memory.RecordRequest) { r.Nature, r.Evidence = "decision", "   " },
		"observation blank proof": func(r *memory.RecordRequest) { r.Nature, r.Evidence = "observation", "  " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base()
			mutate(&req)
			_, err := memory.Record(context.Background(), cfg, req)
			wantValidation(t, err)
		})
	}
}

func TestRecordResumesClosedSession(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	req := base()
	req.Directory = t.TempDir()
	first, err := memory.Record(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	closeReq := memory.CloseRequest{Author: "takt", Session: req.Session, Directory: req.Directory}
	closed, err := memory.Close(ctx, cfg, closeReq)
	if err != nil {
		t.Fatal(err)
	}

	// Retrying an old result is not new work and must not reopen the session.
	duplicate, err := memory.Record(ctx, cfg, req)
	if err != nil || duplicate.ID != first.ID || !duplicate.Deduplicated {
		t.Fatalf("retry = %+v, %v", duplicate, err)
	}
	_, err = memory.Close(ctx, cfg, closeReq)
	wantValidation(t, err)

	// Invalid new work must also leave the previous close intact.
	late := req
	late.Author, late.Title = "spec", "Refined acceptance invariant"
	// An unavailable backend must not reopen the persisted session either.
	offline := cfg
	offline.EngramURL = unusedPort
	if _, err := memory.Record(ctx, offline, late); err == nil {
		t.Fatal("offline record succeeded")
	}
	_, err = memory.Close(ctx, cfg, closeReq)
	wantValidation(t, err)

	late.RelatesTo = &memory.RelatesTo{ID: closed.EndAnchorID, Relation: "supplements"}
	_, err = memory.Record(ctx, cfg, late)
	wantValidation(t, err)
	_, err = memory.Close(ctx, cfg, closeReq)
	wantValidation(t, err)

	late.RelatesTo = &memory.RelatesTo{ID: first.ID, Relation: "supplements"}
	result, err := memory.Record(ctx, cfg, late)
	if err != nil {
		t.Fatal(err)
	}
	if err := memory.ValidateSessionResultIDs(ctx, cfg, req.Session, late.Author, []int64{result.ID}); err != nil {
		t.Fatal(err)
	}
	closeReq.State = "Acceptance invariant refined"
	resumed, err := memory.Close(ctx, cfg, closeReq)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Entries != 2 || resumed.EndAnchorID == closed.EndAnchorID {
		t.Fatalf("resumed close = %+v; previous = %+v", resumed, closed)
	}
	if f.byID(closed.EndAnchorID).ID != closed.EndAnchorID {
		t.Fatal("previous close was lost")
	}
	ids, err := memory.EntryIDsForSession(cfg.Root, req.Session)
	if err != nil || !slices.Equal(ids, []int64{first.ID, result.ID}) {
		t.Fatalf("session results = %v, %v", ids, err)
	}
}

func TestRecordRelatesToRules(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	first, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	l, err := readIndex(cfg.Root, "ses_1")
	if err != nil {
		t.Fatal(err)
	}

	anchor := base()
	anchor.Title = "Points at an anchor"
	anchor.RelatesTo = &memory.RelatesTo{ID: l.StartAnchor, Relation: "supplements"}
	_, err = memory.Record(ctx, cfg, anchor)
	wantValidation(t, err)
	if !strings.Contains(err.Error(), "session anchor") {
		t.Errorf("error = %v, want the anchor rejection", err)
	}

	global := base()
	global.Scope, global.Confirmed, global.Title = "personal", true, "A preference"
	global.RelatesTo = &memory.RelatesTo{ID: first.ID, Relation: "supplements"}
	_, err = memory.Record(ctx, cfg, global)
	wantValidation(t, err)
	if !strings.Contains(err.Error(), "workspace entry") {
		t.Errorf("error = %v, want the tier rejection", err)
	}

	f.projects[70] = otherProject
	foreign := base()
	foreign.Title = "Crosses projects"
	foreign.RelatesTo = &memory.RelatesTo{ID: 70, Relation: "disputes"}
	_, err = memory.Record(ctx, cfg, foreign)
	wantValidation(t, err)
	if !strings.Contains(err.Error(), "another project") {
		t.Errorf("error = %v, want the project rejection", err)
	}

	f.projects[71] = "  "
	unbound := base()
	unbound.Scope, unbound.Confirmed, unbound.Title = "personal", true, "Global to global"
	unbound.RelatesTo = &memory.RelatesTo{ID: 71, Relation: "supplements"}
	if _, err := memory.Record(ctx, cfg, unbound); err != nil {
		t.Errorf("a global memory relating to a global entry: %v", err)
	}
}

func TestRecordSurfacesEngramFailures(t *testing.T) {
	for _, route := range []string{"POST /sessions", "POST /observations", "POST /conflicts/compare"} {
		t.Run(route, func(t *testing.T) {
			cfg := failingEngram(t, route)
			_, err := memory.Record(context.Background(), cfg, base())
			if err == nil || !strings.Contains(err.Error(), "status 500") {
				t.Fatalf("memory.Record() error = %v, want the engram 500", err)
			}
		})
	}
}

func TestRecordRelatesToLookupFailureIsNotAValidationError(t *testing.T) {
	cfg := failingEngram(t, "GET /observations/5")
	req := base()
	req.RelatesTo = &memory.RelatesTo{ID: 5, Relation: "corrects"}
	_, err := memory.Record(context.Background(), cfg, req)
	var ve *memory.ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("memory.Record() error = %v, want a plain service failure", err)
	}
}

func TestRecordFailsOnUnreadableSessionState(t *testing.T) {
	_, cfg := newFake(t)
	blocker := filepath.Join(t.TempDir(), "root-is-a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Root = blocker
	if _, err := memory.Record(context.Background(), cfg, base()); err == nil {
		t.Fatal("memory.Record() with an unusable root error = nil, want the lock failure")
	}

	_, cfg = newFake(t)
	writeIndex(t, cfg.Root, "ses_1", "{ not json")
	_, err := memory.Record(context.Background(), cfg, base())
	if err == nil || !strings.Contains(err.Error(), "parse session index") {
		t.Fatalf("memory.Record() error = %v, want the parse failure", err)
	}
}

// writeIndex plants raw session index bytes for session under root.
func writeIndex(t *testing.T, root, session, content string) {
	t.Helper()
	if err := os.MkdirAll(sessionsDir(root), memory.PrivateStateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionIndexPath(root, session), []byte(content), memory.PrivateStateFileMode); err != nil {
		t.Fatal(err)
	} // Every real writer creates the lock before the index.
	if err := os.WriteFile(sessionIndexPath(root, session)+".lock", nil, memory.PrivateStateFileMode); err != nil {
		t.Fatal(err)
	}
}

func TestSessionResultIDsAcceptOnlyTheAuthorsOwn(t *testing.T) {
	_, cfg := newFake(t)
	ctx := context.Background()
	mine, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	theirs := base()
	theirs.Author, theirs.Title = "architect", "Someone else's finding"
	other, err := memory.Record(ctx, cfg, theirs)
	if err != nil {
		t.Fatal(err)
	}

	if err := memory.ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", []int64{mine.ID}); err != nil {
		t.Errorf("own result rejected: %v", err)
	}
	wantValidation(t, memory.ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", []int64{other.ID}))
	wantValidation(t, memory.ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", nil))
	wantValidation(t, memory.ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", []int64{mine.ID, mine.ID}))

	writeIndex(t, cfg.Root, "ses_bad", "{ not json")
	if err := memory.ValidateSessionResultIDs(ctx, cfg, "ses_bad", "dev", []int64{1}); err == nil {
		t.Error("a corrupt session index was accepted")
	}
}

func TestValidateResultIDsRejectsNonPositiveAndUnreachable(t *testing.T) {
	_, cfg := newFake(t)
	wantValidation(t, memory.ValidateResultIDs(context.Background(), cfg, []int64{-3}))

	cfg.EngramURL = unusedPort
	err := memory.ValidateResultIDs(context.Background(), cfg, []int64{1})
	var ve *memory.ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("memory.ValidateResultIDs() error = %v, want a service-availability failure", err)
	}
}

func TestEntryIDsByAuthorFiltersAndDefaultsEmpty(t *testing.T) {
	ids, err := memory.EntryIDsByAuthor(t.TempDir(), "ses_none", "dev")
	if err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("memory.EntryIDsByAuthor(no index) = %v, %v; want an empty non-nil slice", ids, err)
	}

	_, cfg := newFake(t)
	ctx := context.Background()
	dev, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	other := base()
	other.Author, other.Title = "architect", "Architect finding"
	if _, err := memory.Record(ctx, cfg, other); err != nil {
		t.Fatal(err)
	}
	got, err := memory.EntryIDsByAuthor(cfg.Root, "ses_1", "dev")
	if err != nil || !slices.Equal(got, []int64{dev.ID}) {
		t.Fatalf("memory.EntryIDsByAuthor(dev) = %v, %v; want [%d]", got, err, dev.ID)
	}
	if none, _ := memory.EntryIDsByAuthor(cfg.Root, "ses_1", "qa"); len(none) != 0 {
		t.Errorf("memory.EntryIDsByAuthor(qa) = %v, want none", none)
	}

	writeIndex(t, cfg.Root, "ses_bad", "{ not json")
	if _, err := memory.EntryIDsByAuthor(cfg.Root, "ses_bad", "dev"); err == nil {
		t.Error("a corrupt index produced no error")
	}
	if _, err := memory.EntryIDsForSession(cfg.Root, "ses_bad"); err == nil {
		t.Error("a corrupt index produced no error for memory.EntryIDsForSession")
	}
}

func TestContinueRejections(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	dir := t.TempDir()
	request := memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}

	for name, mutate := range map[string]func(*memory.ContinueRequest){
		"itself":              func(r *memory.ContinueRequest) { r.PreviousSession = r.Session },
		"blank session":       func(r *memory.ContinueRequest) { r.Session = "" },
		"invalid previous id": func(r *memory.ContinueRequest) { r.PreviousSession = "a/b" },
		"unknown author":      func(r *memory.ContinueRequest) { r.Author = "stranger" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := request
			mutate(&bad)
			wantValidation(t, memory.Continue(ctx, cfg, bad))
		})
	}

	t.Run("session already recorded memory", func(t *testing.T) {
		writeIndex(t, cfg.Root, "ses_1", `{"session":"ses_1","project":"demo","start_anchor":7,"end_anchor":8,"entries":[]}`)
		writeIndex(t, cfg.Root, "ses_2", `{"session":"ses_2","project":"demo","start_anchor":9,"entries":[]}`)
		defer func() { _ = os.Remove(sessionIndexPath(cfg.Root, "ses_2")) }()
		err := memory.Continue(ctx, cfg, request)
		wantValidation(t, err)
		if !strings.Contains(err.Error(), "before its first entry") {
			t.Errorf("error = %v, want the late declaration", err)
		}
		if len(f.sessions) != 0 {
			t.Errorf("a rejected continue opened sessions: %v", f.sessions)
		}
	})

	t.Run("previous index unreadable", func(t *testing.T) {
		writeIndex(t, cfg.Root, "ses_1", "{ not json")
		if err := memory.Continue(ctx, cfg, request); err == nil {
			t.Error("memory.Continue() with a corrupt previous index error = nil")
		}
	})
}

func TestContinuityFixSurfacesEngramFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	request := memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}
	prev := `{"session":"ses_1","project":"demo","start_anchor":7,"end_anchor":8,"entries":[]}`

	for _, route := range []string{"POST /sessions", "POST /conflicts/compare"} {
		t.Run(route, func(t *testing.T) {
			cfg := failingEngram(t, route)
			writeIndex(t, cfg.Root, "ses_1", prev)
			if err := memory.Continue(ctx, cfg, request); err != nil {
				t.Fatal(err)
			}
			_, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_2", Directory: dir})
			if err == nil || !strings.Contains(err.Error(), "status 500") {
				t.Fatalf("memory.Close() error = %v, want the engram 500", err)
			}
			l, _ := readIndex(cfg.Root, "ses_2")
			if l.Continues != "" {
				t.Fatalf("a failed link was marked as fixed: %+v", l)
			}
		})
	}
}

func TestCloseSurfacesEngramFailures(t *testing.T) {
	ctx := context.Background()
	for _, route := range []string{"POST /observations", "POST /conflicts/compare"} {
		t.Run(route, func(t *testing.T) {
			cfg := failingEngram(t)
			if _, err := memory.Record(ctx, cfg, base()); err != nil {
				t.Fatal(err)
			}
			broken := failingEngram(t, route)
			broken.Root = cfg.Root
			_, err := memory.Close(ctx, broken, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), "status 500") {
				t.Fatalf("memory.Close() error = %v, want the engram 500", err)
			}
			if l, _ := readIndex(cfg.Root, "ses_1"); l.EndAnchor != 0 {
				t.Errorf("a failed close recorded end anchor %d", l.EndAnchor)
			}
		})
	}

	t.Run("invalid request", func(t *testing.T) {
		_, cfg := newFake(t)
		_, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "bad/id", Directory: "/x"})
		wantValidation(t, err)
	})
	t.Run("engram unreachable", func(t *testing.T) {
		_, cfg := newFake(t)
		if _, err := memory.Record(ctx, cfg, base()); err != nil {
			t.Fatal(err)
		}
		cfg.EngramURL = unusedPort
		if _, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()}); err == nil {
			t.Fatal("memory.Close() with no engram error = nil")
		}
	})
}

// TestCloseNarrativeMarksEvolution verifies the close record tells a reader
// which entries stopped governing and how entries relate, instead of leaving
// the ordering to imply authority.
func TestCloseNarrativeMarksEvolution(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	first, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	disputed := base()
	disputed.Title = "Contests the split"
	disputed.RelatesTo = &memory.RelatesTo{ID: first.ID, Relation: "disputes"}
	second, err := memory.Record(ctx, cfg, disputed)
	if err != nil {
		t.Fatal(err)
	}
	fixed := base()
	fixed.Title = "Supersedes the second"
	fixed.RelatesTo = &memory.RelatesTo{ID: second.ID, Relation: "corrects"}
	if _, err := memory.Record(ctx, cfg, fixed); err != nil {
		t.Fatal(err)
	}

	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entries != 3 {
		t.Errorf("Entries = %d, want 3", res.Entries)
	}
	content := f.byID(res.EndAnchorID).Content
	for _, want := range []string{
		"disputed: open, not settled by recency",
		"superseded: history, not a current directive",
		"## How the entries relate",
		"## Objective\nNot stated",
		"## State at close\nNot stated",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("close narrative lacks %q:\n%s", want, content)
		}
	}
}

func TestRenderCloseWithoutEntries(t *testing.T) {
	f, cfg := newFake(t)
	result, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Author: "takt", Session: "s", Directory: t.TempDir(), Objective: "goal", State: "done"})
	if err != nil {
		t.Fatal(err)
	}
	out := f.byID(result.EndAnchorID).Content
	for _, want := range []string{"No entries recorded", "**Continues**: none", "## Objective\ngoal", "## State at close\ndone"} {
		if !strings.Contains(out, want) {
			t.Errorf("close lacks %q: %s", want, out)
		}
	}
	if strings.Contains(out, "How the entries relate") {
		t.Fatal("empty session listed relations")
	}
}

func TestApplicabilityKeepsSupersededOverDisputed(t *testing.T) {
	f, cfg := newFake(t)
	first, err := memory.Record(context.Background(), cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	for _, relation := range []string{"disputes", "corrects", "disputes"} {
		req := base()
		req.Title = "Relation " + relation
		req.RelatesTo = &memory.RelatesTo{ID: first.ID, Relation: relation}
		if _, err := memory.Record(context.Background(), cfg, req); err != nil {
			t.Fatal(err)
		}
	}
	result, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.byID(result.EndAnchorID).Content, "(superseded: history, not a current directive)") {
		t.Fatalf("close lost applicability: %s", f.byID(result.EndAnchorID).Content)
	}
}

func TestRecordProjectPartitionsTiers(t *testing.T) {
	f, cfg := newFake(t)
	for _, scope := range []string{"personal", "project"} {
		req := base()
		req.Scope, req.Confirmed, req.Title = scope, true, "Tier "+scope
		result, err := memory.Record(context.Background(), cfg, req)
		if err != nil {
			t.Fatal(err)
		}
		want := "demo"
		if scope == "personal" {
			want = ""
		}
		if f.byID(result.ID).Project != want {
			t.Fatal("tier project partition lost")
		}
	}
}

func TestAuthorRole(t *testing.T) {
	_, cfg := newFake(t)
	for _, author := range []string{"takt", "dev", "nobody"} {
		req := base()
		req.Author, req.Title = author, "Finding "+author
		_, err := memory.Record(context.Background(), cfg, req)
		if author == "nobody" {
			wantValidation(t, err)
		} else if err != nil {
			t.Fatal(err)
		}
	}
}

func TestNowUsesInjectedClockElseWallClock(t *testing.T) {
	for _, injected := range []bool{true, false} {
		f, cfg := newFake(t)
		if injected {
			cfg.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600)) }
		} else {
			cfg.Now = nil
		}
		if _, err := memory.Record(context.Background(), cfg, base()); err != nil {
			t.Fatal(err)
		}
		content := f.obs[0].Content
		if injected && !strings.Contains(content, "2026-01-02T02:04:05Z") {
			t.Fatalf("UTC clock missing: %s", content)
		}
		if !injected && !strings.Contains(content, time.Now().UTC().Format("2006-01-02")) {
			t.Fatalf("wall clock missing: %s", content)
		}
	}
}

func TestResolveProjectPrefersEngramDetection(t *testing.T) {
	_, cfg := newFake(t)
	if got := memory.ResolveProject(context.Background(), cfg, "/work/anything"); got != "demo" {
		t.Errorf("memory.ResolveProject() = %q, want the project Engram reported", got)
	}
}

func TestResolveProjectFallsBackToDirectoryName(t *testing.T) {
	cfg := memory.Config{EngramURL: unusedPort}
	dir := filepath.Join(t.TempDir(), "Fallback__Name")
	if got := memory.ResolveProject(context.Background(), cfg, dir); got != "fallback_name" {
		t.Errorf("memory.ResolveProject() = %q, want the normalized base name", got)
	}
}

func TestDetectProjectFromGitRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", "git@example.com:Some-Org/My--Repo.git"},
	} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if got := memory.ResolveProject(context.Background(), memory.Config{EngramURL: "http://127.0.0.1:1"}, dir); got != "my-repo" {
		t.Errorf("detectProject(remote) = %q, want the normalized repository name", got)
	}
}

func TestDetectProjectFromRepositoryRootWithoutRemote(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := filepath.Join(t.TempDir(), "Local-Root")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	sub := filepath.Join(dir, "pkg")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := memory.ResolveProject(context.Background(), memory.Config{EngramURL: unusedPort}, sub); got != "local-root" {
		t.Errorf("detectProject(subdir) = %q, want the repository root's name", got)
	}
}

func TestDetectProjectOfFilesystemRootIsUnknown(t *testing.T) {
	if got := memory.ResolveProject(context.Background(), memory.Config{EngramURL: unusedPort}, string(filepath.Separator)); got != "unknown" {
		t.Errorf("detectProject(/) = %q, want unknown", got)
	}
}

func TestNormalizeProject(t *testing.T) {
	for in, want := range map[string]string{"  MyApp ": "myapp", "a--b__c": "a-b_c", "": "unknown", "   ": "unknown", "keep-one_x": "keep-one_x"} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"project":` + strconv.Quote(in) + `,"cwd":` + strconv.Quote(r.URL.Query().Get("cwd")) + `}`))
		}))
		got := memory.ResolveProject(context.Background(), memory.Config{EngramURL: srv.URL, HTTP: srv.Client()}, string(filepath.Separator))
		srv.Close()
		if got != want {
			t.Errorf("ResolveProject(%q)=%q,want %q", in, got, want)
		}
	}
}

func TestClientCallErrors(t *testing.T) {
	for _, body := range []string{"not json", "{}", `{"id":1}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				_, _ = w.Write([]byte(`{"status":"ok"}`))
				return
			}
			if r.URL.Path == "/project/current" {
				_, _ = w.Write([]byte(`{"project":"demo"}`))
				return
			}
			_, _ = w.Write([]byte(body))
		}))
		cfg := memory.Config{Root: t.TempDir(), EngramURL: srv.URL + "/", HTTP: srv.Client()}
		_, err := memory.Record(context.Background(), cfg, base())
		srv.Close()
		if (body == `{"id":1}`) != (err == nil) {
			t.Fatalf("record service response %s: %v", body, err)
		}
	}
}

func TestStartDetachedServeFailureIsReported(t *testing.T) {
	cfg := memory.Config{Root: t.TempDir(), EngramURL: unusedPort, EngramBinary: filepath.Join(t.TempDir(), "absent")}
	if _, err := memory.Record(context.Background(), cfg, base()); err == nil || !strings.Contains(err.Error(), "start engram serve") {
		t.Fatalf("missing binary: %v", err)
	}
}

func TestSessionIndexLockFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Record(context.Background(), memory.Config{Root: blocker}, base()); err == nil || !strings.Contains(err.Error(), "create memory session dir") {
		t.Fatalf("unusable root: %v", err)
	}
}

func TestLoadSessionIndexReadFailure(t *testing.T) {
	_, cfg := newFake(t)
	if err := os.MkdirAll(sessionIndexPath(cfg.Root, "ses_1"), memory.PrivateStateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.EntryIDsForSession(cfg.Root, "ses_1"); err == nil || !strings.Contains(err.Error(), "read session index") {
		t.Fatalf("unreadable index: %v", err)
	}
}

// These fixtures inspect the persisted JSON contract, never private APIs.
func sessionsDir(root string) string { return filepath.Join(root, ".takt-ai", "memory", "sessions") }
func sessionIndexPath(root, session string) string {
	return filepath.Join(sessionsDir(root), session+".json")
}

type indexFixture struct {
	StartAnchor      int64             `json:"start_anchor"`
	EndAnchor        int64             `json:"end_anchor"`
	Continues        string            `json:"continues"`
	PendingContinues string            `json:"pending_continues"`
	Entries          []json.RawMessage `json:"entries"`
}

func readIndex(root, session string) (indexFixture, error) {
	raw, err := os.ReadFile(sessionIndexPath(root, session))
	if err != nil {
		return indexFixture{}, err
	}
	var result indexFixture
	err = json.Unmarshal(raw, &result)
	return result, err
}
