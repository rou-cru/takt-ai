// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package gc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
)

// reachDepth bounds how far outward the code graph is followed per symbol.
const reachDepth = 3

// Runner runs one codegraph subcommand against the workspace and returns stdout.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// ExecRunner runs the codegraph binary with the workspace as its project.
func ExecRunner(binary, workspace string) Runner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		// These commands take the project as a positional path and reject -p.
		switch args[0] {
		case "init", "index", "sync", "status":
			args = append(args, workspace)
		default:
			args = append(args, "-p", workspace)
		}
		out, err := exec.CommandContext(ctx, binary, args...).Output()
		if err != nil {
			return nil, fmt.Errorf("codegraph %s: %w", args[0], err)
		}
		return out, nil
	}
}

// Codegraph is the Reach backed by the codegraph CLI, queried through the JSON
// of `query` and `impact`; only the symbols-only file listing has no JSON form
// and is parsed line by line.
type Codegraph struct{ run Runner }

// NewCodegraph builds the adapter over run.
func NewCodegraph(run Runner) *Codegraph { return &Codegraph{run: run} }

// symbolLine matches "- `Name` (kind)" in the symbols-only listing.
var symbolLine = regexp.MustCompile("(?m)^- `([^`]+)` \\(")

// Dependents returns the files reachable outward from path's symbols, sorted
// and deduplicated; unresolvable files or symbols yield nothing.
func (c *Codegraph) Dependents(ctx context.Context, path string) ([]string, error) {
	listing, err := c.run(ctx, "node", "-f", path, "--symbols-only")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, m := range symbolLine.FindAllSubmatch(listing, -1) {
		name := string(m[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		qualified, err := c.symbolsIn(ctx, name, path)
		if err != nil {
			return nil, err
		}
		for _, q := range qualified {
			out, err := c.run(ctx, "impact", "-j", "-d", strconv.Itoa(reachDepth), q)
			if err != nil {
				return nil, err
			}
			// An unknown symbol prints a notice and exits 0; only JSON carries results.
			if !bytes.HasPrefix(bytes.TrimSpace(out), []byte("{")) {
				continue
			}
			var res struct {
				Affected []struct {
					FilePath string `json:"filePath"`
				} `json:"affected"`
			}
			if err := json.Unmarshal(out, &res); err != nil {
				return nil, fmt.Errorf("codegraph impact: %w", err)
			}
			for _, a := range res.Affected {
				files = append(files, a.FilePath)
			}
		}
	}
	slices.Sort(files)
	return slices.Compact(files), nil
}

// symbol is one indexed node as `query -j` reports it.
type symbol struct {
	QualifiedName string `json:"qualifiedName"`
	FilePath      string `json:"filePath"`
}

// symbolsIn lists the qualified names of the indexed symbols called name that live in path.
func (c *Codegraph) symbolsIn(ctx context.Context, name, path string) ([]string, error) {
	out, err := c.run(ctx, "query", "-j", "-l", "100", name)
	if err != nil {
		return nil, err
	}
	var hits []struct {
		Node symbol `json:"node"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &hits); err != nil {
		return nil, fmt.Errorf("codegraph query: %w", err)
	}
	var names []string
	for _, h := range hits {
		if h.Node.FilePath == path {
			names = append(names, h.Node.QualifiedName)
		}
	}
	return names, nil
}
