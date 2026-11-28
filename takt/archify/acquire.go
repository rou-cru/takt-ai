// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package archify acquires, verifies, and locates the pinned Archify skill
// release for Takt deployments.
package archify

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/internal/semver"
	"github.com/rou-cru/takt-ai/takt/model"
)

const (
	// Version is the pinned Archify release Takt installs and expects.
	Version = "3.3.2"
	// ArchiveURL is the immutable upstream release archive for Version.
	ArchiveURL = "https://github.com/tt-a1i/archify/releases/download/v3.3.2/archify.zip"
	// ArchiveSHA256 is the expected SHA-256 digest of ArchiveURL.
	ArchiveSHA256 = "932d915f83fdbf44796cc6b20b085455a763491306e1b8dee92047e11dddc0ea"
	// ArchiveSize is the exact byte length of ArchiveURL.
	ArchiveSize int64 = 5493974

	downloadTimeout  = 2 * time.Minute
	doctorWaitDelay  = 2 * time.Second
	dirMode          = os.FileMode(0o755)
	regularFileMode  = os.FileMode(0o644)
	scriptFileMode   = os.FileMode(0o755)
	updateCheckOff   = "ARCHIFY_UPDATE_CHECK_DISABLED=1"
	packageJSONFile  = "package.json"
	skillDescriptor  = "SKILL.md"
	archiveRootEntry = "archify/"
)

var (
	// binaryRelPath and receiptFile are relative to the Archify skill directory.
	binaryRelPath = strings.TrimPrefix(model.ArchifyBinaryRelPath, model.ArchifySkillRelDir+"/")
	receiptFile   = path.Base(model.ArchifyManagedMarkerKey)
)

// Manifest identifies a verifiable Archify zip archive.
type Manifest struct {
	URL     string
	SHA256  string
	Size    int64
	Version string
}

// Pinned returns the official Archify release manifest Takt ships.
func Pinned() Manifest {
	return Manifest{
		URL:     ArchiveURL,
		SHA256:  ArchiveSHA256,
		Size:    ArchiveSize,
		Version: Version,
	}
}

// InstalledVersion reads the package.json version of an Archify install under root.
func InstalledVersion(root string) (string, error) {
	return packageVersion(model.ArchifySkillDir(root))
}

func packageVersion(skillDir string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(skillDir, packageJSONFile))
	if err != nil {
		return "", fmt.Errorf("read archify %s: %w", packageJSONFile, err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return "", fmt.Errorf("parse archify %s: %w", packageJSONFile, err)
	}
	version := strings.TrimSpace(pkg.Version)
	if version == "" {
		return "", fmt.Errorf("archify %s has no version", packageJSONFile)
	}
	return version, nil
}

// Resolve reports whether root holds a valid Archify installation >= Version.
func Resolve(root string) (string, bool) {
	return Pinned().Resolve(root)
}

// Resolve reports whether root holds a valid Archify installation >= m.Version
// and returns the absolute path of its CLI entrypoint.
func (m Manifest) Resolve(root string) (string, bool) {
	if _, found := installedVersionIn(model.ArchifySkillDir(root), m.Version); !found {
		return "", false
	}
	abs, err := filepath.Abs(model.ArchifyBinaryPath(root))
	if err != nil {
		return "", false
	}
	return abs, true
}

// Intact reports whether the Archify tree under root is exactly the tree Takt
// extracted: its receipt is a regular file whose recorded listing matches every
// file and directory now in the tree. A copy installed without Takt carries no
// receipt.
func Intact(root string) bool {
	skillDir := model.ArchifySkillDir(root)
	receipt := filepath.Join(skillDir, receiptFile)
	if info, err := os.Lstat(receipt); err != nil || !info.Mode().IsRegular() {
		return false
	}
	recorded, err := os.ReadFile(receipt)
	if err != nil {
		return false
	}
	current, err := treeListing(skillDir)
	return err == nil && bytes.Equal(recorded, current)
}

// Acquire returns an existing compatible Archify installation under root, or
// downloads, verifies, and extracts the pinned archive into <root>/.opencode/skills/archify.
// installed is true only when Acquire wrote the directory itself.
func Acquire(ctx context.Context, root string) (binPath string, installed bool, err error) {
	return Pinned().Acquire(ctx, http.DefaultClient, root)
}

// Acquire installs m into root when no copy meeting m.Version is already present.
func (m Manifest) Acquire(ctx context.Context, client *http.Client, root string) (binPath string, installed bool, err error) {
	if binPath, found := m.Resolve(root); found {
		return binPath, false, nil
	}
	destDir := model.ArchifySkillDir(root)
	if _, err := os.Lstat(destDir); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return "", false, fmt.Errorf("archify directory %s already exists and does not satisfy >= %s", destDir, m.Version)
		}
		return "", false, fmt.Errorf("inspect %s: %w", destDir, err)
	}
	body, err := downloadArchive(ctx, client, m)
	if err != nil {
		return "", false, err
	}
	if err := extractArchive(body, root, destDir, m.Version); err != nil {
		return "", false, err
	}
	abs, err := filepath.Abs(model.ArchifyBinaryPath(root))
	if err != nil {
		return "", false, err
	}
	return abs, true, nil
}

// Verify checks that Archify under root meets Version and passes `node bin/archify.mjs doctor`.
func Verify(ctx context.Context, root string) (string, error) {
	binPath, found := Resolve(root)
	if !found {
		return "", fmt.Errorf("archify >= %s not found under %s", Version, model.ArchifySkillDir(root))
	}
	version, err := InstalledVersion(root)
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "node", binPath, "doctor")
	cmd.Env = append(os.Environ(), updateCheckOff)
	// A descendant that outlives a cancelled probe must not hold its output open.
	cmd.WaitDelay = doctorWaitDelay
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return "", fmt.Errorf("archify doctor failed: %w", err)
		}
		return "", fmt.Errorf("archify doctor failed: %w: %s", err, msg)
	}
	return version, nil
}

func downloadArchive(ctx context.Context, client *http.Client, m Manifest) (body []byte, err error) {
	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, m.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("download archify archive: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download archify archive: %w", err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download archify archive %s: HTTP %d", m.URL, resp.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, m.Size+1))
	if err != nil {
		return nil, fmt.Errorf("read archify archive: %w", err)
	}
	if int64(len(body)) != m.Size {
		return nil, fmt.Errorf("archify archive has %d bytes, want %d", len(body), m.Size)
	}
	sum := sha256.Sum256(body)
	if got := hex.EncodeToString(sum[:]); got != m.SHA256 {
		return nil, fmt.Errorf("archify archive checksum mismatch: got %s, want %s", got, m.SHA256)
	}
	return body, nil
}

func extractArchive(body []byte, root, destDir, minVersion string) error {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("open archify zip: %w", err)
	}
	skillsRoot := filepath.Join(root, filepath.FromSlash(model.OpenCodeSkillsDir))
	if err := os.MkdirAll(skillsRoot, dirMode); err != nil {
		return fmt.Errorf("prepare skills dir: %w", err)
	}
	stage, err := os.MkdirTemp(skillsRoot, ".takt-archify-stage-*")
	if err != nil {
		return fmt.Errorf("create archify staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	stageArchify := filepath.Join(stage, model.ArchifySkillID)
	if err := os.MkdirAll(stageArchify, dirMode); err != nil {
		return fmt.Errorf("prepare staged archify dir: %w", err)
	}
	for _, f := range zr.File {
		if err := extractZipEntry(f, stageArchify); err != nil {
			return err
		}
	}
	if _, found := installedVersionIn(stageArchify, minVersion); !found {
		return fmt.Errorf("archify archive lacks valid >= %s payload", minVersion)
	}
	// The receipt lands with the tree in the same rename, so a tree in place
	// always carries the proof that Takt wrote it.
	listing, err := treeListing(stageArchify)
	if err != nil {
		return fmt.Errorf("record archify receipt: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stageArchify, receiptFile), listing, regularFileMode); err != nil {
		return fmt.Errorf("record archify receipt: %w", err)
	}
	if err := os.Rename(stageArchify, destDir); err != nil {
		return fmt.Errorf("install archify skill: %w", err)
	}
	return nil
}

func extractZipEntry(f *zip.File, stageArchify string) error {
	clean := path.Clean(f.Name)
	if strings.Contains(f.Name, "\\") || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("unsafe archify archive path %q", f.Name)
	}
	if f.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("archify archive symlink refused: %q", f.Name)
	}
	if clean == model.ArchifySkillID {
		return nil
	}
	if !strings.HasPrefix(clean, archiveRootEntry) {
		return fmt.Errorf("unexpected archify archive entry %q", f.Name)
	}
	rel := strings.TrimPrefix(clean, archiveRootEntry)
	if rel == "" || rel == "." {
		return nil
	}
	target := filepath.Join(stageArchify, filepath.FromSlash(rel))
	if !strings.HasPrefix(target, stageArchify+string(filepath.Separator)) {
		return fmt.Errorf("unsafe archify archive target %q", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(target, dirMode)
	}
	if err := os.MkdirAll(filepath.Dir(target), dirMode); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	mode := regularFileMode
	if f.Mode()&0o111 != 0 || strings.HasSuffix(rel, ".mjs") {
		mode = scriptFileMode
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// installedVersionIn reports the version of a valid Archify tree at skillDir
// meeting minVersion. The entrypoint, descriptor, and package.json must be
// regular files: a directory, symlink, or named pipe never passes.
func installedVersionIn(skillDir, minVersion string) (string, bool) {
	for _, required := range []string{filepath.Join(skillDir, filepath.FromSlash(binaryRelPath)), filepath.Join(skillDir, skillDescriptor), filepath.Join(skillDir, packageJSONFile)} {
		info, err := os.Lstat(required)
		if err != nil || !info.Mode().IsRegular() {
			return "", false
		}
	}
	version, err := packageVersion(skillDir)
	if err != nil || !semver.AtLeast(version, minVersion) {
		return "", false
	}
	return version, true
}

// treeListing renders, in lexical order, one "sha256 mode path" line per
// regular file under dir and one "- mode path/" line per directory below it,
// leaving out dir itself and the receipt. Recording directories lets an added
// empty directory count as a local change. Any other file type makes the tree
// unlistable.
func treeListing(dir string) ([]byte, error) {
	var listing bytes.Buffer
	err := filepath.WalkDir(dir, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." || rel == receiptFile {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.IsDir() {
			fmt.Fprintf(&listing, "- %04o %s/\n", info.Mode().Perm(), rel)
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("archify tree entry %q is not a regular file", rel)
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		fmt.Fprintf(&listing, "%s %04o %s\n", hex.EncodeToString(sum[:]), info.Mode().Perm(), rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return listing.Bytes(), nil
}
