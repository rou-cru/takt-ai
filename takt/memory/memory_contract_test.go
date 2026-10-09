package memory

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
func failingEngram(t *testing.T, failing ...string) Config {
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
	err := reject("session %q is bad", "x")
	if err.Error() != `session "x" is bad` {
		t.Errorf("Error() = %q", err.Error())
	}
	wantValidation(t, err)
}

func TestRecordRejectsMalformedShapes(t *testing.T) {
	_, cfg := newFake(t)
	cases := map[string]func(*RecordRequest){
		"unknown nature":          func(r *RecordRequest) { r.Nature = "rumour" },
		"unknown scope":           func(r *RecordRequest) { r.Scope = "galaxy" },
		"blank title":             func(r *RecordRequest) { r.Title = "  " },
		"blank content":           func(r *RecordRequest) { r.Content = "" },
		"blank session":           func(r *RecordRequest) { r.Session = " " },
		"blank directory":         func(r *RecordRequest) { r.Directory = "" },
		"dot session":             func(r *RecordRequest) { r.Session = "." },
		"dotdot session":          func(r *RecordRequest) { r.Session = ".." },
		"relation id not > 0":     func(r *RecordRequest) { r.RelatesTo = &RelatesTo{ID: 0, Relation: "corrects"} },
		"code in global memory":   func(r *RecordRequest) { r.Scope, r.Confirmed, r.Content = "personal", true, "see ```x```" },
		"directory in global":     func(r *RecordRequest) { r.Scope, r.Confirmed, r.Evidence = "personal", true, "at "+r.Directory },
		"decision blank proof":    func(r *RecordRequest) { r.Nature, r.Evidence = "decision", "   " },
		"observation blank proof": func(r *RecordRequest) { r.Nature, r.Evidence = "observation", "  " },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base()
			mutate(&req)
			_, err := Record(context.Background(), cfg, req)
			wantValidation(t, err)
		})
	}
}

func TestRecordResumesClosedSession(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	req := base()
	req.Directory = t.TempDir()
	first, err := Record(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	closeReq := CloseRequest{Author: "takt", Session: req.Session, Directory: req.Directory}
	closed, err := Close(ctx, cfg, closeReq)
	if err != nil {
		t.Fatal(err)
	}

	// Retrying an old result is not new work and must not reopen the session.
	duplicate, err := Record(ctx, cfg, req)
	if err != nil || duplicate.ID != first.ID || !duplicate.Deduplicated {
		t.Fatalf("retry = %+v, %v", duplicate, err)
	}
	_, err = Close(ctx, cfg, closeReq)
	wantValidation(t, err)

	// Invalid new work must also leave the previous close intact.
	late := req
	late.Author, late.Title = "spec", "Refined acceptance invariant"
	// An unavailable backend must not reopen the persisted session either.
	offline := cfg
	offline.EngramURL = unusedPort
	if _, err := Record(ctx, offline, late); err == nil {
		t.Fatal("offline record succeeded")
	}
	_, err = Close(ctx, cfg, closeReq)
	wantValidation(t, err)

	late.RelatesTo = &RelatesTo{ID: closed.EndAnchorID, Relation: "supplements"}
	_, err = Record(ctx, cfg, late)
	wantValidation(t, err)
	_, err = Close(ctx, cfg, closeReq)
	wantValidation(t, err)

	late.RelatesTo = &RelatesTo{ID: first.ID, Relation: "supplements"}
	result, err := Record(ctx, cfg, late)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSessionResultIDs(ctx, cfg, req.Session, late.Author, []int64{result.ID}); err != nil {
		t.Fatal(err)
	}
	closeReq.State = "Acceptance invariant refined"
	resumed, err := Close(ctx, cfg, closeReq)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Entries != 2 || resumed.EndAnchorID == closed.EndAnchorID {
		t.Fatalf("resumed close = %+v; previous = %+v", resumed, closed)
	}
	if f.byID(closed.EndAnchorID).ID != closed.EndAnchorID {
		t.Fatal("previous close was lost")
	}
	ids, err := EntryIDsForSession(cfg.Root, req.Session)
	if err != nil || !slices.Equal(ids, []int64{first.ID, result.ID}) {
		t.Fatalf("session results = %v, %v", ids, err)
	}
}

func TestRecordRelatesToRules(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	first, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	l, err := loadSessionIndex(cfg.Root, "ses_1")
	if err != nil {
		t.Fatal(err)
	}

	anchor := base()
	anchor.Title = "Points at an anchor"
	anchor.RelatesTo = &RelatesTo{ID: l.StartAnchor, Relation: "supplements"}
	_, err = Record(ctx, cfg, anchor)
	wantValidation(t, err)
	if !strings.Contains(err.Error(), "session anchor") {
		t.Errorf("error = %v, want the anchor rejection", err)
	}

	global := base()
	global.Scope, global.Confirmed, global.Title = "personal", true, "A preference"
	global.RelatesTo = &RelatesTo{ID: first.ID, Relation: "supplements"}
	_, err = Record(ctx, cfg, global)
	wantValidation(t, err)
	if !strings.Contains(err.Error(), "workspace entry") {
		t.Errorf("error = %v, want the tier rejection", err)
	}

	f.projects[70] = otherProject
	foreign := base()
	foreign.Title = "Crosses projects"
	foreign.RelatesTo = &RelatesTo{ID: 70, Relation: "disputes"}
	_, err = Record(ctx, cfg, foreign)
	wantValidation(t, err)
	if !strings.Contains(err.Error(), "another project") {
		t.Errorf("error = %v, want the project rejection", err)
	}

	f.projects[71] = "  "
	unbound := base()
	unbound.Scope, unbound.Confirmed, unbound.Title = "personal", true, "Global to global"
	unbound.RelatesTo = &RelatesTo{ID: 71, Relation: "supplements"}
	if _, err := Record(ctx, cfg, unbound); err != nil {
		t.Errorf("a global memory relating to a global entry: %v", err)
	}
}

func TestRecordSurfacesEngramFailures(t *testing.T) {
	for _, route := range []string{"POST /sessions", "POST /observations", "POST /conflicts/compare"} {
		t.Run(route, func(t *testing.T) {
			cfg := failingEngram(t, route)
			_, err := Record(context.Background(), cfg, base())
			if err == nil || !strings.Contains(err.Error(), "status 500") {
				t.Fatalf("Record() error = %v, want the engram 500", err)
			}
		})
	}
}

func TestRecordRelatesToLookupFailureIsNotAValidationError(t *testing.T) {
	cfg := failingEngram(t, "GET /observations/5")
	req := base()
	req.RelatesTo = &RelatesTo{ID: 5, Relation: "corrects"}
	_, err := Record(context.Background(), cfg, req)
	var ve *ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("Record() error = %v, want a plain service failure", err)
	}
}

func TestRecordFailsOnUnreadableSessionState(t *testing.T) {
	_, cfg := newFake(t)
	blocker := filepath.Join(t.TempDir(), "root-is-a-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Root = blocker
	if _, err := Record(context.Background(), cfg, base()); err == nil {
		t.Fatal("Record() with an unusable root error = nil, want the lock failure")
	}

	_, cfg = newFake(t)
	writeIndex(t, cfg.Root, "ses_1", "{ not json")
	_, err := Record(context.Background(), cfg, base())
	if err == nil || !strings.Contains(err.Error(), "parse session index") {
		t.Fatalf("Record() error = %v, want the parse failure", err)
	}
}

// writeIndex plants raw session index bytes for session under root.
func writeIndex(t *testing.T, root, session, content string) {
	t.Helper()
	if err := os.MkdirAll(sessionsDir(root), PrivateStateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionIndexPath(root, session), []byte(content), PrivateStateFileMode); err != nil {
		t.Fatal(err)
	}
}

func TestSessionResultIDsAcceptOnlyTheAuthorsOwn(t *testing.T) {
	_, cfg := newFake(t)
	ctx := context.Background()
	mine, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	theirs := base()
	theirs.Author, theirs.Title = "architect", "Someone else's finding"
	other, err := Record(ctx, cfg, theirs)
	if err != nil {
		t.Fatal(err)
	}

	if err := ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", []int64{mine.ID}); err != nil {
		t.Errorf("own result rejected: %v", err)
	}
	wantValidation(t, ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", []int64{other.ID}))
	wantValidation(t, ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", nil))
	wantValidation(t, ValidateSessionResultIDs(ctx, cfg, "ses_1", "dev", []int64{mine.ID, mine.ID}))

	writeIndex(t, cfg.Root, "ses_bad", "{ not json")
	if err := ValidateSessionResultIDs(ctx, cfg, "ses_bad", "dev", []int64{1}); err == nil {
		t.Error("a corrupt session index was accepted")
	}
}

func TestValidateResultIDsRejectsNonPositiveAndUnreachable(t *testing.T) {
	_, cfg := newFake(t)
	wantValidation(t, ValidateResultIDs(context.Background(), cfg, []int64{-3}))

	cfg.EngramURL = unusedPort
	err := ValidateResultIDs(context.Background(), cfg, []int64{1})
	var ve *ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("ValidateResultIDs() error = %v, want a service-availability failure", err)
	}
}

func TestEntryIDsByAuthorFiltersAndDefaultsEmpty(t *testing.T) {
	ids, err := EntryIDsByAuthor(t.TempDir(), "ses_none", "dev")
	if err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("EntryIDsByAuthor(no index) = %v, %v; want an empty non-nil slice", ids, err)
	}

	_, cfg := newFake(t)
	ctx := context.Background()
	dev, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	other := base()
	other.Author, other.Title = "architect", "Architect finding"
	if _, err := Record(ctx, cfg, other); err != nil {
		t.Fatal(err)
	}
	got, err := EntryIDsByAuthor(cfg.Root, "ses_1", "dev")
	if err != nil || !slices.Equal(got, []int64{dev.ID}) {
		t.Fatalf("EntryIDsByAuthor(dev) = %v, %v; want [%d]", got, err, dev.ID)
	}
	if none, _ := EntryIDsByAuthor(cfg.Root, "ses_1", "qa"); len(none) != 0 {
		t.Errorf("EntryIDsByAuthor(qa) = %v, want none", none)
	}

	writeIndex(t, cfg.Root, "ses_bad", "{ not json")
	if _, err := EntryIDsByAuthor(cfg.Root, "ses_bad", "dev"); err == nil {
		t.Error("a corrupt index produced no error")
	}
	if _, err := EntryIDsForSession(cfg.Root, "ses_bad"); err == nil {
		t.Error("a corrupt index produced no error for EntryIDsForSession")
	}
}

func TestContinueRejections(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	dir := t.TempDir()
	request := ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}

	for name, mutate := range map[string]func(*ContinueRequest){
		"itself":              func(r *ContinueRequest) { r.PreviousSession = r.Session },
		"blank session":       func(r *ContinueRequest) { r.Session = "" },
		"invalid previous id": func(r *ContinueRequest) { r.PreviousSession = "a/b" },
		"unknown author":      func(r *ContinueRequest) { r.Author = "stranger" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := request
			mutate(&bad)
			wantValidation(t, Continue(ctx, cfg, bad))
		})
	}

	t.Run("session already recorded memory", func(t *testing.T) {
		writeIndex(t, cfg.Root, "ses_1", `{"session":"ses_1","project":"demo","start_anchor":7,"end_anchor":8,"entries":[]}`)
		writeIndex(t, cfg.Root, "ses_2", `{"session":"ses_2","project":"demo","start_anchor":9,"entries":[]}`)
		defer func() { _ = os.Remove(sessionIndexPath(cfg.Root, "ses_2")) }()
		err := Continue(ctx, cfg, request)
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
		if err := Continue(ctx, cfg, request); err == nil {
			t.Error("Continue() with a corrupt previous index error = nil")
		}
	})
}

func TestContinuityFixSurfacesEngramFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	request := ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}
	prev := `{"session":"ses_1","project":"demo","start_anchor":7,"end_anchor":8,"entries":[]}`

	for _, route := range []string{"POST /sessions", "POST /conflicts/compare"} {
		t.Run(route, func(t *testing.T) {
			cfg := failingEngram(t, route)
			writeIndex(t, cfg.Root, "ses_1", prev)
			if err := Continue(ctx, cfg, request); err != nil {
				t.Fatal(err)
			}
			_, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_2", Directory: dir})
			if err == nil || !strings.Contains(err.Error(), "status 500") {
				t.Fatalf("Close() error = %v, want the engram 500", err)
			}
			l, _ := loadSessionIndex(cfg.Root, "ses_2")
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
			if _, err := Record(ctx, cfg, base()); err != nil {
				t.Fatal(err)
			}
			broken := failingEngram(t, route)
			broken.Root = cfg.Root
			_, err := Close(ctx, broken, CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), "status 500") {
				t.Fatalf("Close() error = %v, want the engram 500", err)
			}
			if l, _ := loadSessionIndex(cfg.Root, "ses_1"); l.EndAnchor != 0 {
				t.Errorf("a failed close recorded end anchor %d", l.EndAnchor)
			}
		})
	}

	t.Run("invalid request", func(t *testing.T) {
		_, cfg := newFake(t)
		_, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "bad/id", Directory: "/x"})
		wantValidation(t, err)
	})
	t.Run("engram unreachable", func(t *testing.T) {
		_, cfg := newFake(t)
		if _, err := Record(ctx, cfg, base()); err != nil {
			t.Fatal(err)
		}
		cfg.EngramURL = unusedPort
		if _, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()}); err == nil {
			t.Fatal("Close() with no engram error = nil")
		}
	})
}

// TestCloseNarrativeMarksEvolution verifies the close record tells a reader
// which entries stopped governing and how entries relate, instead of leaving
// the ordering to imply authority.
func TestCloseNarrativeMarksEvolution(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	first, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	disputed := base()
	disputed.Title = "Contests the split"
	disputed.RelatesTo = &RelatesTo{ID: first.ID, Relation: "disputes"}
	second, err := Record(ctx, cfg, disputed)
	if err != nil {
		t.Fatal(err)
	}
	fixed := base()
	fixed.Title = "Supersedes the second"
	fixed.RelatesTo = &RelatesTo{ID: second.ID, Relation: "corrects"}
	if _, err := Record(ctx, cfg, fixed); err != nil {
		t.Fatal(err)
	}

	res, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_1", Directory: t.TempDir()})
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
	out := renderClose(&sessionIndex{Session: "s"}, CloseRequest{Objective: "goal", State: "done"}, "2026-01-01T00:00:00Z")
	for _, want := range []string{"No entries recorded", "**Continues**: none", "## Objective\ngoal", "## State at close\ndone"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderClose() lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "How the entries relate") {
		t.Errorf("renderClose() listed relations for an empty session:\n%s", out)
	}
}

func TestApplicabilityKeepsSupersededOverDisputed(t *testing.T) {
	entries := []sessionIndexEntry{
		{ID: 2, Relation: "disputes", Target: 1},
		{ID: 3, Relation: "corrects", Target: 1},
		{ID: 4, Relation: "supplements", Target: 9},
		{ID: 5, Relation: "disputes", Target: 1},
	}
	got := applicability(entries)
	if got[1] != applicabilitySuperseded {
		t.Errorf("applicability[1] = %q, want superseded to outrank disputed", got[1])
	}
	if _, listed := got[9]; listed {
		t.Errorf("a supplement changed applicability of #9: %v", got)
	}
	if applicabilityNote(applicabilityCurrent) != "" {
		t.Error("a current entry carries an applicability note")
	}
}

func TestRecordProjectPartitionsTiers(t *testing.T) {
	if got := recordProject(scopePersonal, "demo"); got != "" {
		t.Errorf("recordProject(personal) = %q, want unbound", got)
	}
	if got := recordProject(scopeProject, "demo"); got != "demo" {
		t.Errorf("recordProject(project) = %q, want demo", got)
	}
	if got := targetProject(nil); got != "" {
		t.Errorf("targetProject(nil) = %q, want unbound", got)
	}
}

func TestAuthorRole(t *testing.T) {
	if _, err := authorRole("takt"); err != nil {
		t.Errorf("authorRole(orchestrator) error = %v", err)
	}
	if _, err := authorRole("dev"); err != nil {
		t.Errorf("authorRole(dev) error = %v", err)
	}
	wantValidation(t, func() error { _, err := authorRole("nobody"); return err }())
}

func TestNowUsesInjectedClockElseWallClock(t *testing.T) {
	if got := now(Config{Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600)) }}); got != "2026-01-02T02:04:05Z" {
		t.Errorf("now(injected) = %q, want the UTC render", got)
	}
	if _, err := time.Parse(time.RFC3339, now(Config{})); err != nil {
		t.Errorf("now(default) is not RFC3339: %v", err)
	}
}

func TestResolveProjectPrefersEngramDetection(t *testing.T) {
	_, cfg := newFake(t)
	if got := ResolveProject(context.Background(), cfg, "/work/anything"); got != "demo" {
		t.Errorf("ResolveProject() = %q, want the project Engram reported", got)
	}
}

func TestResolveProjectFallsBackToDirectoryName(t *testing.T) {
	cfg := Config{EngramURL: unusedPort}
	dir := filepath.Join(t.TempDir(), "Fallback__Name")
	if got := ResolveProject(context.Background(), cfg, dir); got != "fallback_name" {
		t.Errorf("ResolveProject() = %q, want the normalized base name", got)
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
	if got := detectProject(context.Background(), dir); got != "my-repo" {
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
	if got := detectProject(context.Background(), sub); got != "local-root" {
		t.Errorf("detectProject(subdir) = %q, want the repository root's name", got)
	}
}

func TestDetectProjectOfFilesystemRootIsUnknown(t *testing.T) {
	if got := detectProject(context.Background(), string(filepath.Separator)); got != "unknown" {
		t.Errorf("detectProject(/) = %q, want unknown", got)
	}
}

func TestNormalizeProject(t *testing.T) {
	for in, want := range map[string]string{
		"  MyApp ":   "myapp",
		"a--b__c":    "a-b_c",
		"":           "unknown",
		"   ":        "unknown",
		"keep-one_x": "keep-one_x",
	} {
		if got := normalizeProject(in); got != want {
			t.Errorf("normalizeProject(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClientCallErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/garbage":
			_, _ = w.Write([]byte("not json"))
		case "/missing":
			http.Error(w, "gone", http.StatusNotFound)
		case "/noid/observations":
			_, _ = w.Write([]byte(`{}`))
		default:
			_, _ = w.Write([]byte(`{"id":1}`))
		}
	}))
	defer srv.Close()
	c := newClient(Config{EngramURL: srv.URL + "/", HTTP: srv.Client()})
	ctx := context.Background()

	var out map[string]any
	if _, err := c.call(ctx, http.MethodGet, "/garbage", nil, &out); err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Errorf("call(garbage) error = %v, want a decode failure", err)
	}
	status, err := c.call(ctx, http.MethodGet, "/missing", nil, nil)
	if status != http.StatusNotFound || err == nil || !strings.Contains(err.Error(), "status 404: gone") {
		t.Errorf("call(missing) = %d, %v; want the status and body reported", status, err)
	}
	if _, err := c.call(ctx, http.MethodPost, "/x", make(chan int), nil); err == nil {
		t.Error("call() with an unmarshalable body error = nil")
	}
	if _, err := c.call(ctx, "BAD METHOD", "/x", nil, nil); err == nil {
		t.Error("call() with an invalid method error = nil")
	}
	if id, err := c.addObservation(ctx, observation{}); err != nil || id != 1 {
		t.Errorf("addObservation() = %d, %v; want id 1", id, err)
	}
	noID := newClient(Config{EngramURL: srv.URL + "/noid", HTTP: srv.Client()})
	if _, err := noID.addObservation(ctx, observation{}); err == nil {
		t.Error("addObservation() with an id-less response error = nil")
	}
}

func TestNewClientResolvesBaseURL(t *testing.T) {
	explicit := newClient(Config{EngramURL: "http://example.test/"})
	if explicit.base != "http://example.test" {
		t.Errorf("base = %q, want the trailing slash trimmed", explicit.base)
	}
	if explicit.http == nil || explicit.http.Timeout != defaultHTTPTimeout {
		t.Errorf("default HTTP client = %+v, want the default timeout", explicit.http)
	}
}

func TestStartDetachedServe(t *testing.T) {
	if err := startDetachedServe(filepath.Join(t.TempDir(), "absent")); err == nil || !strings.Contains(err.Error(), "start engram serve") {
		t.Errorf("startDetachedServe(absent) error = %v, want a start failure", err)
	}
	truePath, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true binary to start")
	}
	if err := startDetachedServe(truePath); err != nil {
		t.Errorf("startDetachedServe(true) error = %v", err)
	}
}

func TestSessionIndexLockFailsWhenDirectoryCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockSession(blocker, "ses_1"); err == nil || !strings.Contains(err.Error(), "create memory session dir") {
		t.Errorf("lockSession() error = %v, want the directory failure", err)
	}
	if err := saveSessionIndex(blocker, &sessionIndex{Session: "ses_1"}); err == nil {
		t.Error("saveSessionIndex() into an unusable root error = nil")
	}
}

func TestLoadSessionIndexReadFailure(t *testing.T) {
	root := t.TempDir()
	// A directory where the index file belongs makes ReadFile fail with a
	// non-NotExist error.
	if err := os.MkdirAll(sessionIndexPath(root, "ses_1"), PrivateStateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSessionIndex(root, "ses_1"); err == nil || !strings.Contains(err.Error(), "read session index") {
		t.Errorf("loadSessionIndex() error = %v, want a read failure", err)
	}
}
