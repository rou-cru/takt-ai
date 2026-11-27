// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package archify_test

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rou-cru/takt-ai/takt/archify"
	"github.com/rou-cru/takt-ai/takt/model"
)

func buildArchifyZip(t *testing.T, entries map[string]string, mode os.FileMode) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		hdr := &zip.FileHeader{Name: name, Method: zip.Deflate}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func validArchive(t *testing.T, version string) []byte {
	t.Helper()
	return buildArchifyZip(t, map[string]string{
		"archify/SKILL.md":        "---\nname: archify\ndescription: diagrams\n---\n",
		"archify/package.json":    `{"name":"archify","version":"` + version + `"}`,
		"archify/bin/archify.mjs": "#!/usr/bin/env node\nif (process.argv[2] === 'doctor') { console.log('ok'); }\n",
	}, 0o644)
}

func manifestFor(t *testing.T, payload []byte, version string) (archify.Manifest, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(payload)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(payload)
	return archify.Manifest{
		URL:     srv.URL,
		SHA256:  hex.EncodeToString(sum[:]),
		Size:    int64(len(payload)),
		Version: version,
	}, &hits
}

func TestPinnedConstants(t *testing.T) {
	p := archify.Pinned()
	if p.Version != archify.Version || p.URL == "" || len(p.SHA256) != 64 || p.Size <= 0 {
		t.Fatalf("unexpected Pinned manifest: %+v", p)
	}
}

func TestAcquireAndIdempotency(t *testing.T) {
	payload := validArchive(t, archify.Version)
	manifest, hits := manifestFor(t, payload, archify.Version)
	root := t.TempDir()

	binPath, installed, err := manifest.Acquire(context.Background(), http.DefaultClient, root)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !installed || hits.Load() != 1 {
		t.Fatalf("installed=%v hits=%d, want true and 1", installed, hits.Load())
	}
	if want := model.ArchifyBinaryPath(root); binPath != want {
		t.Fatalf("binPath = %q, want %q", binPath, want)
	}
	if ver, err := archify.InstalledVersion(root); err != nil || ver != archify.Version {
		t.Fatalf("InstalledVersion() = %q, %v; want %q", ver, err, archify.Version)
	}

	resolved, ok := archify.Resolve(root)
	if !ok || resolved != binPath {
		t.Fatalf("Resolve() = (%q, %v), want (%q, true)", resolved, ok, binPath)
	}

	// Second call must reuse the installed skill without hitting the server.
	binPath2, installed2, err := manifest.Acquire(context.Background(), http.DefaultClient, root)
	if err != nil || installed2 || binPath2 != binPath || hits.Load() != 1 {
		t.Fatalf("second Acquire() = (%q, %v, %v) hits=%d, want reuse with 0 extra requests", binPath2, installed2, err, hits.Load())
	}
}

func TestAcquireRejectsTamperedOrUnsafeArchives(t *testing.T) {
	t.Run("checksum mismatch", func(t *testing.T) {
		payload := validArchive(t, archify.Version)
		m, _ := manifestFor(t, payload, archify.Version)
		m.SHA256 = strings.Repeat("0", 64)
		if _, _, err := m.Acquire(context.Background(), http.DefaultClient, t.TempDir()); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("Acquire() error = %v, want checksum mismatch", err)
		}
	})

	t.Run("zipslip path", func(t *testing.T) {
		payload := buildArchifyZip(t, map[string]string{
			"../escape.txt": "bad",
		}, 0o644)
		m, _ := manifestFor(t, payload, archify.Version)
		if _, _, err := m.Acquire(context.Background(), http.DefaultClient, t.TempDir()); err == nil || !strings.Contains(err.Error(), "unsafe archify archive path") {
			t.Fatalf("Acquire() error = %v, want unsafe path error", err)
		}
	})

	t.Run("symlink entry", func(t *testing.T) {
		payload := buildArchifyZip(t, map[string]string{
			"archify/link": "/etc/passwd",
		}, os.ModeSymlink|0o777)
		m, _ := manifestFor(t, payload, archify.Version)
		if _, _, err := m.Acquire(context.Background(), http.DefaultClient, t.TempDir()); err == nil || !strings.Contains(err.Error(), "symlink refused") {
			t.Fatalf("Acquire() error = %v, want symlink refused", err)
		}
	})

	t.Run("existing older unmanaged copy preserved", func(t *testing.T) {
		root := t.TempDir()
		skillDir := model.ArchifySkillDir(root)
		if err := os.MkdirAll(filepath.Join(skillDir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "package.json"), []byte(`{"version":"3.0.0"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("user"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "bin", "archify.mjs"), []byte("user"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := archify.Resolve(root); ok {
			t.Fatal("Resolve() accepted 3.0.0 below Version floor")
		}
		m, hits := manifestFor(t, validArchive(t, archify.Version), archify.Version)
		if _, _, err := m.Acquire(context.Background(), http.DefaultClient, root); err == nil || hits.Load() != 0 {
			t.Fatalf("Acquire() = %v hits=%d, want refusal before download", err, hits.Load())
		}
	})
}

func TestVerifyRunsNodeDoctor(t *testing.T) {
	root := t.TempDir()
	payload := validArchive(t, archify.Version)
	m, _ := manifestFor(t, payload, archify.Version)
	if _, _, err := m.Acquire(context.Background(), http.DefaultClient, root); err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	nodeScript := "#!/bin/sh\nif [ \"$2\" = \"doctor\" ]; then echo \"Archify ready\"; exit 0; fi\nexit 1\n"
	if err := os.WriteFile(filepath.Join(fakeBin, "node"), []byte(nodeScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	ver, err := archify.Verify(context.Background(), root)
	if err != nil || ver != archify.Version {
		t.Fatalf("Verify() = (%q, %v), want (%q, nil)", ver, err, archify.Version)
	}
}
