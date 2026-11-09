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
package embed_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

const testFileName = "weights.bin"

// served builds a manifest for content and a server that returns body for it.
func served(t *testing.T, content, body string) embed.Manifest {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	digest := sha256.Sum256([]byte(content))
	return embed.Manifest{
		BaseURL: server.URL,
		Files:   []embed.File{{Name: testFileName, SHA256: hex.EncodeToString(digest[:]), Size: int64(len(content))}},
	}
}

func TestAcquireInstallsVerifiedFile(t *testing.T) {
	manifest := served(t, "model bytes", "model bytes")
	dir := filepath.Join(t.TempDir(), "nested")
	if err := manifest.Acquire(context.Background(), http.DefaultClient, dir); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, testFileName))
	if err != nil || string(got) != "model bytes" {
		t.Fatalf("installed file = %q, %v", got, err)
	}
}

func TestAcquireRefusesDigestMismatch(t *testing.T) {
	manifest := served(t, "model bytes", "tampered!!!")
	dir := t.TempDir()
	if err := manifest.Acquire(context.Background(), http.DefaultClient, dir); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	if _, err := os.Stat(filepath.Join(dir, testFileName)); !os.IsNotExist(err) {
		t.Fatalf("refused file was installed: %v", err)
	}
}

func TestAcquireRefusesBodyOverSizeCap(t *testing.T) {
	manifest := served(t, "model bytes", "model bytes and a long tail")
	if err := manifest.Acquire(context.Background(), http.DefaultClient, t.TempDir()); err == nil {
		t.Fatal("expected size cap refusal")
	}
}

func TestAcquireReplacesCorruptFile(t *testing.T) {
	manifest := served(t, "model bytes", "model bytes")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, testFileName), []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Acquire(context.Background(), http.DefaultClient, dir); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, testFileName)); string(got) != "model bytes" {
		t.Fatalf("corrupt file kept: %q", got)
	}
}

// A model file is larger than the configuration files filemerge compares before replacing, so a
// corrupt full-size file must still be replaced.
func TestAcquireReplacesALargeCorruptFile(t *testing.T) {
	const size = 17 << 20
	content := strings.Repeat("m", size)
	manifest := served(t, content, content)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, testFileName), []byte(strings.Repeat("x", size)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Acquire(context.Background(), http.DefaultClient, dir); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, testFileName))
	if err != nil || string(got) != content {
		t.Fatalf("large corrupt file was not replaced: %v", err)
	}
}
