package drift_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/drift"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
	testWidth  = 200
	testHeight = 40
	// manyMissing overflows the checklist so the position footer appears.
	manyMissing = 40
	// shortHeight is a window too small to hold manyMissing rows.
	shortHeight = 24
	// olderVersion and newerVersion bracket a release build for version drift.
	olderVersion = "0.0.25"
	newerVersion = "0.0.27"
)

var (
	keyLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyRight = tea.KeyPressMsg{Code: tea.KeyRight}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyPgDn  = tea.KeyPressMsg{Code: tea.KeyPgDown}
	keyPgUp  = tea.KeyPressMsg{Code: tea.KeyPgUp}
)

// fixtureDir holds the installed roots shared by the tests: drift.Model only
// reads them, so each is installed once per package run.
var (
	fixtureDir string
	fixtures   = map[string]driftFixture{}
)

// driftFixture is an installed root and the managed paths it drifted, one per
// drift reason.
type driftFixture struct {
	root                         string
	missing, edited, preexisting string
}

func removeFixtures() {
	if fixtureDir != "" {
		_ = os.RemoveAll(fixtureDir)
	}
}

// fixture installs a root once under name and lets drifted alter it.
func fixture(t *testing.T, name string, drifted func(t *testing.T, root string, paths []string) driftFixture) driftFixture {
	t.Helper()
	if cached, ok := fixtures[name]; ok {
		return cached
	}
	if fixtureDir == "" {
		dir, err := os.MkdirTemp("", "takt-drift-fixtures-")
		if err != nil {
			t.Fatalf("MkdirTemp() error = %v", err)
		}
		fixtureDir = dir
	}
	root := filepath.Join(fixtureDir, name)
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v", err)
	}
	var paths []string
	for path := range manifest.Entries {
		if strings.HasSuffix(path, ".md") {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	made := drifted(t, root, paths)
	made.root = root
	fixtures[name] = made
	return made
}

// cleanRoot is an installation nothing changed.
func cleanRoot(t *testing.T) driftFixture {
	return fixture(t, "clean", func(*testing.T, string, []string) driftFixture { return driftFixture{} })
}

// driftedRoot has one missing, one edited and one pre-existing managed file.
func driftedRoot(t *testing.T) driftFixture {
	return fixture(t, "drifted", func(t *testing.T, root string, paths []string) driftFixture {
		if len(paths) < 3 {
			t.Fatalf("installed manifest has %d markdown entries, want at least 3", len(paths))
		}
		made := driftFixture{missing: paths[0], edited: paths[1], preexisting: paths[2]}
		if err := os.Remove(filepath.Join(root, made.missing)); err != nil {
			t.Fatalf("Remove() error = %v", err)
		}
		for _, path := range []string{made.edited, made.preexisting} {
			appendTo(t, filepath.Join(root, path))
		}
		// A file the manifest no longer owns reads as pre-existing.
		manifest, err := setup.LoadOwnershipManifest(root)
		if err != nil {
			t.Fatalf("LoadOwnershipManifest() error = %v", err)
		}
		delete(manifest.Entries, made.preexisting)
		if err := manifest.Save(root); err != nil {
			t.Fatalf("Save() error = %v", err)
		}
		return made
	})
}

// manyMissingRoot lost more managed files than a short window shows.
func manyMissingRoot(t *testing.T) driftFixture {
	return fixture(t, "many", func(t *testing.T, root string, paths []string) driftFixture {
		if len(paths) < manyMissing {
			t.Fatalf("installed manifest has %d markdown entries, want at least %d", len(paths), manyMissing)
		}
		for _, path := range paths[:manyMissing] {
			if err := os.Remove(filepath.Join(root, path)); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
		}
		return driftFixture{}
	})
}

func appendTo(t *testing.T, path string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, append(content, []byte("\n# edited by test\n")...), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

// withFile replaces root's name with content while fn runs, then puts the
// original back, so a shared fixture stays as installed.
func withFile(t *testing.T, root, name string, content []byte, fn func()) {
	t.Helper()
	path := filepath.Join(root, name)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	defer func() {
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatalf("restore %q: %v", path, err)
		}
	}()
	fn()
}

// scanned is the report after a finished scan of root, read as if version
// had installed it.
func scanned(t *testing.T, root, version string) drift.Model {
	t.Helper()
	path := filepath.Join(root, setup.InstalledConfigFilename)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("Unmarshal(installed record) error = %v", err)
	}
	record["takt_version"] = version
	if version == "" {
		delete(record, "takt_version")
	}
	rewritten, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal(installed record) error = %v", err)
	}
	var m drift.Model
	withFile(t, root, setup.InstalledConfigFilename, rewritten, func() { m = scanNow(t, root) })
	return m
}

// scanNow runs the scan of root as the event loop would, at the test size.
func scanNow(t *testing.T, root string) drift.Model {
	t.Helper()
	m := drift.New(root)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned no scan for an installed root")
	}
	m, _ = send(t, m, cmd())
	return m
}

// withBuild runs the test as a release build of version.
func withBuild(t *testing.T, version string) {
	t.Helper()
	previous := setup.BuildVersion
	setup.BuildVersion = version
	t.Cleanup(func() { setup.BuildVersion = previous })
}

func send(t *testing.T, m drift.Model, msg tea.Msg) (drift.Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(drift.Model), cmd
}

func text(m drift.Model) string { return ansi.Strip(m.View().Content) }

func requireContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !testutil.Shows(got, want) {
			t.Errorf("view missing %q:\n%s", want, got)
		}
	}
}

func requireAbsent(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, want := range unwanted {
		if testutil.Shows(got, want) {
			t.Errorf("view shows %q:\n%s", want, got)
		}
	}
}

// requireMsg presses Enter on m and requires cmd to produce a message of type T.
func requireMsg[T any](t *testing.T, m drift.Model) {
	t.Helper()
	_, cmd := send(t, m, keyEnter)
	if cmd == nil {
		t.Fatal("Enter produced no command")
	}
	if msg, ok := cmd().(T); !ok {
		t.Fatalf("Enter produced %T, want %T", msg, *new(T))
	}
}

// reviewScreen walks report, select (choosing every file) and review.
func reviewScreen(t *testing.T) (drift.Model, driftFixture) {
	t.Helper()
	f := driftedRoot(t)
	m, _ := send(t, scanned(t, f.root, setup.BuildVersion), keyEnter) // Select files
	for range 3 {
		m, _ = send(t, m, keySpace)
		m, _ = send(t, m, keyDown)
	}
	m, _ = send(t, m, keyEnter)
	if m.State() != drift.StateReview {
		t.Fatalf("state = %v, want review", m.State())
	}
	return m, f
}

// restoreRequest starts the reviewed correction and returns its request.
func restoreRequest(t *testing.T) (drift.Model, runtime.ActionRequest) {
	t.Helper()
	m, _ := reviewScreen(t)
	m, _ = send(t, m, keyLeft) // focus starts on the safe action
	m, cmd := send(t, m, keyEnter)
	if cmd == nil {
		t.Fatal("Restore produced no command")
	}
	request, ok := cmd().(runtime.ActionRequest)
	if !ok {
		t.Fatal("Restore did not emit an action request")
	}
	return m, request
}

func TestReportStatesBeforeAndAfterScan(t *testing.T) {
	clean := cleanRoot(t)
	requireContains(t, text(drift.New(clean.root)), ui.TextDriftChecking)

	withFile(t, clean.root, setup.OwnershipManifestFilename, []byte("{"), func() {
		requireContains(t, text(scanNow(t, clean.root)), ui.TextDriftCannotCheck)
	})
	requireContains(t, text(scanned(t, clean.root, setup.BuildVersion)), ui.TextDriftClean, ui.TextDriftNoVerify)

	f := driftedRoot(t)
	found := text(scanned(t, f.root, setup.BuildVersion))
	requireContains(t, found, fmt.Sprintf(ui.TextDriftManagedFmt, 3)+ui.TextDriftDifferOutro, f.missing, f.edited, f.preexisting, ui.TextActionSelectFiles)
}

func TestCleanReportOffersOnlyTheMenu(t *testing.T) {
	m := scanned(t, cleanRoot(t).root, setup.BuildVersion)
	requireAbsent(t, text(m), ui.TextActionSelectFiles, ui.TextActionUpgrade)
	requireMsg[ui.BackMsg](t, m)
}

func TestVersionDriftAloneRecommendsUpgrade(t *testing.T) {
	withBuild(t, newerVersion)
	m := scanned(t, cleanRoot(t).root, olderVersion)
	view := text(m)
	requireContains(t, view, fmt.Sprintf(ui.TextDriftVersionFmt, olderVersion, newerVersion), ui.TextDriftFilesMatch, ui.TextDriftUpgradeNext, ui.TextActionUpgrade)
	requireAbsent(t, view, ui.TextDriftClean, ui.TextActionSelectFiles)
	// Upgrade is the focused, recommended action.
	requireMsg[drift.OpenUpgradeMsg](t, m)
}

func TestVersionDriftWithEditsReportsBoth(t *testing.T) {
	withBuild(t, newerVersion)
	f := driftedRoot(t)
	m := scanned(t, f.root, olderVersion)
	view := text(m)
	requireContains(t, view, fmt.Sprintf(ui.TextDriftVersionFmt, olderVersion, newerVersion), f.missing, f.edited, f.preexisting, ui.TextDriftUpgradeAsks)
	requireAbsent(t, view, ui.TextActionSelectFiles)
	requireMsg[drift.OpenUpgradeMsg](t, m)
}

func TestUnrecordedVersionIsVersionDrift(t *testing.T) {
	withBuild(t, newerVersion)
	view := text(scanned(t, cleanRoot(t).root, ""))
	requireContains(t, view, fmt.Sprintf(ui.TextDriftVersionFmt, ui.TextVersionUnrecorded, newerVersion), ui.TextActionUpgrade)
}

func TestNewerInstallationCannotBeUpgradedOrCorrected(t *testing.T) {
	withBuild(t, olderVersion)
	m := scanned(t, driftedRoot(t).root, newerVersion)
	view := text(m)
	requireContains(t, view, fmt.Sprintf(ui.TextDriftVersionFmt, newerVersion, olderVersion), ui.TextDriftNoUpgrade)
	requireAbsent(t, view, ui.TextActionUpgrade, ui.TextActionSelectFiles)
	requireMsg[ui.BackMsg](t, m)
}

func TestSelectionRequiresAtLeastOneFile(t *testing.T) {
	m, _ := send(t, scanned(t, driftedRoot(t).root, setup.BuildVersion), keyEnter)
	if m.State() != drift.StateSelect {
		t.Fatalf("state = %v, want select", m.State())
	}
	requireContains(t, text(m), ui.TextDriftSelectIntro, ui.TextDriftSelectStar)

	m, _ = send(t, m, keyEnter)
	if m.State() != drift.StateSelect {
		t.Errorf("state = %v, want Enter with nothing chosen to stay on select", m.State())
	}

	m, _ = send(t, m, keySpace)
	if !m.Dirty() {
		t.Fatal("Dirty() = false after choosing a file")
	}
	m, _ = send(t, m, keySpace)
	if m.Dirty() {
		t.Error("Dirty() = true, want a second Space to untoggle")
	}

	back, _ := send(t, m, keyEsc)
	if back.State() != drift.StateReport {
		t.Errorf("state = %v, want Esc on select to return to the report", back.State())
	}
}

func TestSelectShowsPositionWhenListOverflows(t *testing.T) {
	m := scanned(t, manyMissingRoot(t).root, setup.BuildVersion)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: testWidth, Height: shortHeight})
	m, _ = send(t, m, keyEnter)
	// The shell shows which rows are visible once the checklist overflows.
	if !regexp.MustCompile(`\d+–\d+ / \d+`).MatchString(text(m)) {
		t.Fatalf("overflowing checklist shows no position:\n%s", text(m))
	}
}

func TestPageKeysScrollTheBody(t *testing.T) {
	m := scanned(t, manyMissingRoot(t).root, setup.BuildVersion)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: testWidth, Height: shortHeight})
	top := text(m)
	m, _ = send(t, m, keyPgDn)
	if text(m) == top {
		t.Error("PgDn did not scroll the body")
	}
	m, _ = send(t, m, keyPgUp)
	if text(m) != top {
		t.Error("PgUp did not return to the top")
	}
}

func TestReviewNamesReferenceAndConsequencePerReason(t *testing.T) {
	m, f := reviewScreen(t)
	requireContains(t, text(m),
		fmt.Sprintf(ui.TextDriftRefFmt, setup.BuildVersion),
		ui.TextDriftRestoreHead, ui.TextDriftPreserveHead, ui.TextDriftPreserveAll,
		ui.TextDriftRecreated, ui.TextDriftReplacedEdit, ui.TextDriftReplacedFile,
		f.missing, f.edited, f.preexisting,
		ui.TextActionRestoreSelected, ui.TextActionReselect)

	back, _ := send(t, m, keyEsc)
	if back.State() != drift.StateSelect || !back.Dirty() {
		t.Errorf("state = %v dirty = %v, want Esc to return to select keeping the choice", back.State(), back.Dirty())
	}
}

func TestRestoreStartsCorrectionForSelectedPaths(t *testing.T) {
	m, request := restoreRequest(t)
	f := driftedRoot(t)
	if request.Action != runtime.ActionCorrectDrift {
		t.Fatalf("action = %q, want correct-drift", request.Action)
	}
	for _, path := range []string{f.missing, f.edited, f.preexisting} {
		if !slices.Contains(request.SelectedDriftPaths, path) {
			t.Errorf("request paths = %v, want %q", request.SelectedDriftPaths, path)
		}
	}
	if !m.Run().Busy() {
		t.Fatal("the correction is not running")
	}
	requireContains(t, text(m), ui.TextDriftBusy)

	m, _ = send(t, m, keyEnter)
	if !m.Run().Busy() {
		t.Error("a key while busy ended the run")
	}
	m, _ = send(t, m, runtime.CancelRequest{ID: request.ID})
	if !m.Run().CancelRequested {
		t.Error("CancelRequest was not recorded")
	}
	m, _ = send(t, m, spinner.TickMsg{})
	if m.Run().ProgressView().Frame != 1 {
		t.Errorf("progress frame = %d, want 1 after a tick", m.Run().ProgressView().Frame)
	}

	stale := request
	stale.ID++
	if m, _ = send(t, m, runtime.ActionResultMsg{Request: stale}); m.State() != drift.StateReview {
		t.Errorf("state = %v, want a stale result to be ignored", m.State())
	}
	if m, _ = send(t, m, struct{}{}); !m.Run().Busy() {
		t.Error("an unknown message ended the run")
	}
}

// result delivers correction and err to a running correction.
func result(t *testing.T, correction runtime.ActionResult, err error) drift.Model {
	t.Helper()
	m, request := restoreRequest(t)
	m, _ = send(t, m, runtime.ActionResultMsg{Request: request, Result: correction, Err: err})
	return m
}

func TestResultBodies(t *testing.T) {
	// A backup note needs a restored path the scan found edited.
	edited := driftedRoot(t).edited
	tests := []struct {
		name       string
		correction runtime.ActionResult
		err        error
		want       []string
	}{
		{
			name:       "restored files with a backup note",
			correction: runtime.ActionResult{Changed: []string{edited}, CancelRequested: true},
			want:       []string{ui.TextDriftRestoredOk, ui.TextDriftRestoredHead, edited, ui.TextDriftTakeEffect, ui.TextDriftBackupIntro + setup.BackupDir, ui.TextLateCancel},
		},
		{
			name:       "nothing needed restoring",
			correction: runtime.ActionResult{},
			want:       []string{ui.TextDriftAlreadyMatch, ui.TextDriftNoFuncVerify},
		},
		{
			name:       "only unplanned files",
			correction: runtime.ActionResult{Unresolved: []string{"agents/gone.md"}},
			want:       []string{ui.TextDriftNoPlanned, ui.TextDriftUnplanned, "agents/gone.md", ui.TextDriftLeftAsIs},
		},
		{
			name:       "restored and unplanned files",
			correction: runtime.ActionResult{Changed: []string{"agents/missing.md"}, Unresolved: []string{"agents/gone.md"}},
			want:       []string{"agents/missing.md", "agents/gone.md", ui.TextDriftLeftAsIs},
		},
		{
			name:       "cancelled after some restores",
			correction: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"agents/missing.md"}, NotApplied: []string{"agents/edited.md"}},
			want:       []string{"agents/missing.md", ui.TextDriftCheckAgain, ui.TextActionKeepCurrent},
		},
		{
			name:       "failure after some restores",
			correction: runtime.ActionResult{Changed: []string{"agents/missing.md"}},
			err:        errors.New("engram gone"),
			want:       []string{"engram gone", ui.TextDriftRestoredHead, "agents/missing.md", ui.TextDriftSeeCurrent},
		},
		{
			name: "failure before any restore",
			err:  errors.New("locked"),
			want: []string{ui.TextDriftFailedIntro + "locked", ui.TextDriftSeeCurrent},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := result(t, tt.correction, tt.err)
			if m.State() != drift.StateResult || m.Run().Busy() {
				t.Fatalf("state = %v busy = %v, want the idle result step", m.State(), m.Run().Busy())
			}
			if m.Title() != ui.TextDriftResultTitle || m.Dirty() {
				t.Errorf("title = %q dirty = %v, want the result title and a clean flow", m.Title(), m.Dirty())
			}
			requireContains(t, text(m), tt.want...)
		})
	}
}

func TestResultActionsRescanAndLeave(t *testing.T) {
	restored := runtime.ActionResult{Changed: []string{"agents/missing.md"}}
	t.Run("rescan clears the scan and starts another", func(t *testing.T) {
		m, cmd := send(t, result(t, restored, nil), keyEnter)
		if m.State() != drift.StateReport || m.Dirty() || cmd == nil {
			t.Fatalf("state = %v dirty = %v cmd = %v, want a fresh pending scan", m.State(), m.Dirty(), cmd != nil)
		}
		requireContains(t, text(m), ui.TextDriftChecking)
	})
	t.Run("menu leaves the flow", func(t *testing.T) {
		m, _ := send(t, result(t, restored, nil), keyRight)
		requireMsg[ui.BackMsg](t, m)
	})
	t.Run("keep current state leaves after a partial correction", func(t *testing.T) {
		m, _ := send(t, result(t, runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"agents/missing.md"}}, nil), keyRight)
		requireMsg[ui.BackMsg](t, m)
	})
}

func TestTitlesFollowTheStep(t *testing.T) {
	report := scanned(t, driftedRoot(t).root, setup.BuildVersion)
	selecting, _ := send(t, report, keyEnter)
	review, _ := reviewScreen(t)
	steps := []struct {
		m    drift.Model
		want string
	}{
		{drift.New(t.TempDir()), ui.TextDriftEmptyTitle},
		{report, ui.TextDriftReportTitle},
		{selecting, ui.TextDriftSelectTitle},
		{review, ui.TextDriftReviewTitle},
		{result(t, runtime.ActionResult{}, nil), ui.TextDriftResultTitle},
	}
	for _, step := range steps {
		if got := step.m.Title(); got != step.want {
			t.Errorf("Title() = %q, want %q", got, step.want)
		}
	}
}
