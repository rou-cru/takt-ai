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
// Package embed turns text into the fixed-size semantic vectors the memory
// service stores, in process and without network access at runtime.
package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

const (
	// modelRepo is the Hugging Face repository of the Model2Vec model.
	modelRepo = "minishlab/potion-base-8M"
	// modelRevision pins the repository commit; files are never fetched from a moving ref.
	modelRevision = "bf8b056651a2c21b8d2565580b8569da283cab23"
	// ModelIdentity is the name plus revision that tags every vector (MEM-RET-7).
	ModelIdentity = modelRepo + "@" + modelRevision
	// modelBaseURL is where pinned files are resolved from.
	modelBaseURL = "https://huggingface.co/" + modelRepo + "/resolve/" + modelRevision
	// downloadTimeout bounds one file download.
	downloadTimeout = 5 * time.Minute
	// modelDirectoryMode keeps the model directory traversable by its owner.
	modelDirectoryMode os.FileMode = 0o755
	// modelFileMode is the mode of installed model files.
	modelFileMode os.FileMode = 0o644
	// Files inference reads from the model directory.
	tokenizerFile = "tokenizer.json"
	weightsFile   = "model.safetensors"
)

// File is one manifest entry: a file name with its expected digest and size.
type File struct {
	Name   string
	SHA256 string
	Size   int64
}

// Manifest lists the files of a model and the URL prefix they resolve from.
type Manifest struct {
	BaseURL string
	Files   []File
}

// Pinned returns the manifest of the model Takt ships.
func Pinned() Manifest {
	return Manifest{
		BaseURL: modelBaseURL,
		Files: []File{
			{Name: tokenizerFile, SHA256: "e67e803f624fb4d67dea1c730d06e1067e1b14d830e2c2202569e3ef0f70bb50", Size: 683666},
			{Name: weightsFile, SHA256: "f65d0f325faadc1e121c319e2faa41170d3fa07d8c89abd48ca5358d9a223de2", Size: 30236760},
		},
	}
}

// ModelDir is the single definition of where the model lives under home.
func ModelDir(home string) string {
	return filepath.Join(home, ".takt-ai", "models", filepath.FromSlash(modelRepo), modelRevision)
}

// Acquire installs every manifest file into dir. A file already there that matches its manifest size
// and SHA-256 is kept, so an install with the model present needs no network. Each download is capped
// at its manifest size, verified against its SHA-256, and written atomically.
func (m Manifest) Acquire(ctx context.Context, client *http.Client, dir string) error {
	for _, file := range m.Files {
		destination := filepath.Join(dir, file.Name)
		if present, err := os.ReadFile(destination); err == nil && file.verify(present) == nil {
			continue
		}
		body, err := download(ctx, client, m.BaseURL+"/"+file.Name, file.Size)
		if err != nil {
			return err
		}
		if err := file.verify(body); err != nil {
			return err
		}
		if err := writeAtomic(destination, body); err != nil {
			return fmt.Errorf("install model file %s: %w", file.Name, err)
		}
	}
	return nil
}

// verify checks content against the manifest size and digest.
func (f File) verify(content []byte) error {
	if int64(len(content)) != f.Size {
		return fmt.Errorf("model file %s has %d bytes, want %d", f.Name, len(content), f.Size)
	}
	digest := sha256.Sum256(content)
	if got := hex.EncodeToString(digest[:]); got != f.SHA256 {
		return fmt.Errorf("model file %s checksum mismatch: got %s, want %s", f.Name, got, f.SHA256)
	}
	return nil
}

// download fetches one file within downloadTimeout, refusing bodies larger than limit.
func download(ctx context.Context, client *http.Client, url string, limit int64) (body []byte, err error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("download model file: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("download model file: %w", err)
	}
	defer func() { err = errors.Join(err, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download model file %s: HTTP %d", url, response.StatusCode)
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download model file: %w", err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("download model file %s: body exceeds %d bytes", url, limit)
	}
	return body, nil
}

// writeAtomic stages content beside destination and renames it into place. Model files exceed the
// size filemerge.WriteFileAtomic compares before replacing, so it reuses only the staging and sync steps.
func writeAtomic(destination string, content []byte) error {
	dir := filepath.Dir(destination)
	if err := os.MkdirAll(dir, modelDirectoryMode); err != nil {
		return err
	}
	staged, err := filemerge.StageTempFile(dir, ".takt-ai-*.tmp", content, modelFileMode)
	if err != nil {
		return err
	}
	if err := os.Rename(staged, destination); err != nil {
		return errors.Join(err, os.Remove(staged))
	}
	return filemerge.SyncDir(dir)
}
