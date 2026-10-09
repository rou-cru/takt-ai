package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// TestMain puts a compatible fake engram first on PATH so install, sync and
// the memory command never touch the host binary or the network.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithFakeEngram(m))
}

// fakeEngramServer answers every Engram call the memory core makes with a fresh id.
func fakeEngramServer(t *testing.T) {
	t.Helper()
	var next atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/current" {
			if err := json.NewEncoder(w).Encode(map[string]string{"project": "demo", "cwd": r.URL.Query().Get("cwd")}); err != nil {
				t.Errorf("encode project response: %v", err)
			}
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]int64{"id": next.Add(1)}); err != nil {
			t.Errorf("encode observation response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("ENGRAM_BASE_URL", srv.URL)
	t.Setenv("HOME", t.TempDir())
}

func runMemoryCLI(t *testing.T, command, input string) (map[string]any, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run([]string{"memory", command}, strings.NewReader(input), &stdout, &stderr)
	code := 0
	var exit exitCode
	if errors.As(err, &exit) {
		code = int(exit)
	} else if err != nil {
		t.Fatalf("run() error = %v, want exitCode", err)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("stdout = %q, want one JSON line", stdout.String())
	}
	var line map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	return line, code
}

func TestMemoryCommandExitCodes(t *testing.T) {
	fakeEngramServer(t)
	record := `{"author":"dev","session":"ses_1","directory":"/work/demo","nature":"proposal","scope":"project","title":"Split the parser","content":"The parser mixes IO and parsing."}`
	cases := []struct {
		name, command, input string
		code                 int
		want                 string
	}{
		{"record ok", "record", record, 0, `"ok":true`},
		{"close ok", "close", `{"author":"takt","session":"ses_1","directory":"/work/demo","objective":"Split parser","state":"Parser split"}`, 0, `"end_anchor_id"`},
		{"record after close resumes", "record", strings.Replace(record, "Split the parser", "Refined the parser", 1), 0, `"ok":true`},
		{"close resumed session", "close", `{"author":"takt","session":"ses_1","directory":"/work/demo","objective":"Refine parser","state":"Parser refined"}`, 0, `"end_anchor_id"`},
		{"quoted wording accepted", "record", strings.NewReplacer("ses_1", "ses_words", "Split the parser", "Quoted: will", "The parser mixes IO and parsing.", "se verificó todo").Replace(record), 0, `"ok":true`},
		{"invalid json", "record", `{"author":`, 2, "invalid request"},
		{"unknown field", "record", strings.Replace(record, `"author"`, `"confirmed_by":"x","author"`, 1), 2, "unknown field"},
		{"multiple values", "continue", `{} {}`, 2, "multiple JSON values"},
		{"unknown verb", "delete", `{}`, 1, "unknown memory command"},
	}
	for _, tc := range cases {
		line, code := runMemoryCLI(t, tc.command, tc.input)
		raw, _ := json.Marshal(line)
		if code != tc.code || !strings.Contains(string(raw), tc.want) {
			t.Errorf("%s: code=%d line=%s, want code %d containing %q", tc.name, code, raw, tc.code, tc.want)
		}
	}
}

func TestMemoryCommandEngramUnreachable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ENGRAM_BASE_URL", "http://127.0.0.1:1")
	t.Setenv("PATH", t.TempDir()) // no engram binary to start
	line, code := runMemoryCLI(t, "record", `{"author":"dev","session":"ses_1","directory":"/w","nature":"proposal","scope":"project","title":"T","content":"C"}`)
	if code != 1 || line["ok"] != false {
		t.Fatalf("code=%d line=%v, want internal failure", code, line)
	}
}

func TestMemoryCapabilitiesReadOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	for _, tt := range []struct {
		author string
		count  int
	}{{"spec", 1}, {"takt", 3}, {"architect", 3}} {
		t.Run(tt.author, func(t *testing.T) {
			input, err := json.Marshal(map[string]string{"author": tt.author, "session": "root", "directory": dir})
			if err != nil {
				t.Fatal(err)
			}
			line, code := runMemoryCLI(t, "capabilities", string(input))
			if code != 0 || line["ok"] != true {
				t.Fatalf("%v: %v", code, line)
			}
			tools, ok := line["result"].([]any)
			if !ok || len(tools) != tt.count || tools[0] != "memory_record" {
				t.Fatal(line)
			}
		})
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read-only query mutated workspace: %v %v", entries, err)
	}
	_, code := runMemoryCLI(t, "capabilities", `{"author":"spec","unexpected":true}`)
	if code != invalidRequestExitStatus {
		t.Fatalf("unknown identity fields accepted: %d", code)
	}
}
