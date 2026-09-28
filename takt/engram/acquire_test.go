// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package engram

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeEngramScript is an executable answering `version` like the real binary.
// It mirrors testutil.FakeEngramScript but stays local to avoid an
// engram→testutil→engram import cycle in this internal test package.
func fakeEngramScript(version string) []byte {
	return []byte("#!/bin/sh\necho 'engram " + version + "'\n")
}

// isolatePath puts only dir on PATH so the host's real engram is never seen.
func isolatePath(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", dir)
}

func writeFakeEngram(t *testing.T, dir, version string) string {
	t.Helper()
	path := filepath.Join(dir, "engram")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, fakeEngramScript(version), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// serveRelease builds a release tarball fixture, pins its checksum and serves it.
func serveRelease(t *testing.T, binary []byte) (tarball []byte, hits *int) {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	archive := tar.NewWriter(gz)
	for _, entry := range []struct {
		name string
		body []byte
		mode int64
	}{{"README.md", []byte("readme"), 0o644}, {"engram", binary, 0o755}, {"LICENSE", []byte("license"), 0o644}} {
		if err := archive.WriteHeader(&tar.Header{Name: entry.name, Mode: entry.mode, Size: int64(len(entry.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := archive.Write(entry.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	tarball = buffer.Bytes()
	platform := runtime.GOOS + "_" + runtime.GOARCH
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		if r.URL.Path != "/v"+EngramVersion+"/engram_"+EngramVersion+"_"+platform+".tar.gz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(tarball)
	}))
	t.Cleanup(server.Close)

	originalURL, originalSums := releaseBaseURL, releaseSHA256
	digest := sha256.Sum256(tarball)
	releaseBaseURL = server.URL
	releaseSHA256 = map[string]string{platform: hex.EncodeToString(digest[:])}
	t.Cleanup(func() { releaseBaseURL, releaseSHA256 = originalURL, originalSums })
	return tarball, &count
}

func TestAcquireDownloadsVerifiesAndInstalls(t *testing.T) {
	isolatePath(t, t.TempDir())
	root := t.TempDir()
	binary := fakeEngramScript(EngramVersion)
	serveRelease(t, binary)

	path, err := Acquire(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if path != ManagedBinaryPath(root) {
		t.Fatalf("path = %q, want %q", path, ManagedBinaryPath(root))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("stat = %v, %v; want mode 0755", info, err)
	}
	if got := readTest(t, path); got != string(binary) {
		t.Fatalf("installed binary = %q", got)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("bin dir holds %v, want only engram", entries)
	}
	if resolved, found := Resolve(root); !found || resolved != path {
		t.Fatalf("Resolve() = %q, %v; want the managed binary", resolved, found)
	}
}

func TestAcquireRejectsChecksumMismatch(t *testing.T) {
	isolatePath(t, t.TempDir())
	root := t.TempDir()
	serveRelease(t, fakeEngramScript(EngramVersion))
	releaseSHA256[runtime.GOOS+"_"+runtime.GOARCH] = strings.Repeat("0", 64)

	if _, err := Acquire(context.Background(), root); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("Acquire() error = %v, want checksum mismatch", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-ai")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mismatch left files behind, stat err = %v", err)
	}
}

func TestAcquireUnsupportedPlatformFails(t *testing.T) {
	isolatePath(t, t.TempDir())
	original := releaseSHA256
	releaseSHA256 = map[string]string{}
	t.Cleanup(func() { releaseSHA256 = original })
	if _, err := Acquire(context.Background(), t.TempDir()); err == nil || !strings.Contains(err.Error(), "no supported release") {
		t.Fatalf("Acquire() error = %v, want unsupported platform", err)
	}
}

func TestResolveReusesCompatibleBinaryOnPath(t *testing.T) {
	bin := t.TempDir()
	want := writeFakeEngram(t, bin, "2.1.3")
	isolatePath(t, bin)
	_, hits := serveRelease(t, fakeEngramScript(EngramVersion))
	root := t.TempDir()

	path, err := Acquire(context.Background(), root)
	if err != nil || path != want {
		t.Fatalf("Acquire() = %q, %v; want reused %q", path, err, want)
	}
	if *hits != 0 {
		t.Fatalf("reused binary still downloaded %d times", *hits)
	}
	if _, err := os.Stat(filepath.Join(root, ".takt-ai")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reusing a user binary wrote into root")
	}
}

func TestOldOrDevBinaryOnPathForcesAcquire(t *testing.T) {
	for _, version := range []string{"2.0.9", "dev"} {
		t.Run(version, func(t *testing.T) {
			bin := t.TempDir()
			writeFakeEngram(t, bin, version)
			isolatePath(t, bin)
			if _, found := Resolve(t.TempDir()); found {
				t.Fatalf("Resolve accepted engram %s", version)
			}
			_, hits := serveRelease(t, fakeEngramScript(EngramVersion))
			root := t.TempDir()
			path, err := Acquire(context.Background(), root)
			if err != nil || path != ManagedBinaryPath(root) || *hits != 1 {
				t.Fatalf("Acquire() = %q, %v, hits %d; want a fresh managed download", path, err, *hits)
			}
		})
	}
}

func TestCompatibleVersion(t *testing.T) {
	for output, want := range map[string]bool{
		"engram 2.1.0":  true,
		"engram v2.1.1": true,
		"engram 2.0.0":  false,
		"engram 1.20.1": false,
		"engram 1.9.99": false,
		"engram 1.20":   false,
		"engram dev":    false,
		"engram":        false,
	} {
		if got := compatibleVersion(output); got != want {
			t.Errorf("compatibleVersion(%q) = %v, want %v", output, got, want)
		}
	}
}
