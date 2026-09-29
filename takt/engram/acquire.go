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
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

const (
	// engramDirectoryMode keeps the installed binary directory traversable by its owner.
	engramDirectoryMode os.FileMode = 0o755
	// semverFieldCount is the minimum number of fields in engram version output.
	semverFieldCount = 2
)

// EngramVersion is the minimum Engram release Takt configures and the one it installs.
const EngramVersion = "2.1.0"

const (
	downloadTimeout = 5 * time.Minute
	maxTarballBytes = 256 << 20
)

var (
	// releaseBaseURL is the release host; tests point it at httptest.
	releaseBaseURL = "https://github.com/Gentleman-Programming/engram/releases/download"
	// releaseSHA256 pins each platform tarball; checksums are never fetched at install time.
	releaseSHA256 = map[string]string{
		"darwin_amd64": "2c8f56f36c6779b1c0f5f56bf9339ed121192218272e4fc144a8ad7bed2da22c",
		"darwin_arm64": "b9167999ba6deca652e367bd7d44766afa33430ab93b01f7cc0be42e6364d806",
		"linux_amd64":  "3579cf5d92ae9349c6941ff08cde04012b12c9c695d307e71afaee4fb5e4a9c1",
		"linux_arm64":  "12b84e62a9763290d086efc0332dfbd9ebe2f3ff7461c44ed091c9f946d2f0cc",
	}
)

// managedBinaryKey is the manifest slash-path for the managed copy.
const managedBinaryKey = ".takt-ai/bin/engram"

// ManagedBinaryPath returns where Takt installs its own engram copy.
func ManagedBinaryPath(root string) string {
	return filepath.Join(root, filepath.FromSlash(managedBinaryKey))
}

// Resolve searches for an engram binary whose version is compatible.
func Resolve(root string) (string, bool) {
	candidates := []string{}
	if onPath, err := lookPath("engram"); err == nil {
		candidates = append(candidates, onPath)
	}
	candidates = append(candidates, ManagedBinaryPath(root))
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		if info, err := os.Stat(absolute); err != nil || info.IsDir() {
			continue
		}
		if version, err := VerifyVersion(absolute); err == nil && compatibleVersion(version) {
			return absolute, true
		}
	}
	return "", false
}

// Acquire returns Resolve's path when found, or downloads and installs the pinned release.
func Acquire(ctx context.Context, root string) (string, error) {
	if path, found := Resolve(root); found {
		return path, nil
	}
	platform := runtime.GOOS + "_" + runtime.GOARCH
	want, supported := releaseSHA256[platform]
	if !supported {
		return "", fmt.Errorf("engram %s has no supported release for %s", EngramVersion, platform)
	}
	destination, err := filepath.Abs(ManagedBinaryPath(root))
	if err != nil {
		return "", fmt.Errorf("resolve engram install path: %w", err)
	}
	url := fmt.Sprintf("%s/v%s/engram_%s_%s.tar.gz", releaseBaseURL, EngramVersion, EngramVersion, platform)
	tarball, err := download(ctx, url)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(tarball)
	if got := hex.EncodeToString(digest[:]); got != want {
		return "", fmt.Errorf("engram release checksum mismatch for %s: got %s, want %s", platform, got, want)
	}
	binary, err := extractEngram(tarball)
	if err != nil {
		return "", err
	}
	if err := writeAtomic(destination, binary); err != nil {
		return "", fmt.Errorf("install engram binary: %w", err)
	}
	return destination, nil
}

// download fetches one release asset within downloadTimeout.
func download(ctx context.Context, url string) (body []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download engram release: %w", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download engram release: %w", err)
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download engram release %s: HTTP %d", url, response.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, maxTarballBytes))
	if err != nil {
		return nil, fmt.Errorf("download engram release: %w", err)
	}
	return body, nil
}

// extractEngram returns the regular `engram` entry at the tarball root and ignores everything else.
func extractEngram(tarball []byte) (binary []byte, err error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, fmt.Errorf("open engram release: %w", err)
	}
	defer func() { err = errors.Join(err, gz.Close()) }()
	archive := tar.NewReader(gz)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("engram release has no engram binary")
		}
		if err != nil {
			return nil, fmt.Errorf("read engram release: %w", err)
		}
		if strings.TrimPrefix(header.Name, "./") != "engram" || header.Typeflag != tar.TypeReg {
			continue
		}
		return io.ReadAll(archive)
	}
}

// writeAtomic stages content beside destination and renames it into place.
func writeAtomic(destination string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(destination), engramDirectoryMode); err != nil {
		return err
	}
	_, err := filemerge.WriteFileAtomic(destination, content, filemerge.ExecutableDirectoryMode)
	return err
}

// compatibleVersion parses `engram X.Y.Z` output and checks compatibility.
func compatibleVersion(output string) bool {
	fields := strings.Fields(output)
	if len(fields) < semverFieldCount {
		return false
	}
	return shared.VersionAtLeast(fields[1], EngramVersion)
}
