// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package catalog_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rou-cru/takt-ai/takt/catalog"
)

const (
	validSkillDescriptor = "---\nname: s\ndescription: d\n---\nbody\n"
	validAgentYAML       = "id: x\nlabel: x\ngroup: building\ndisplay_names: {x: X}\ninstances: [x]\ndescription: d\nrole: execution\nvfs_capabilities: {x: [bind]}\ncontext: {operations: OPERATIONS.md}\n"
	scriptFileMode       = fs.FileMode(0o755)
	regularFileMode      = fs.FileMode(0o644)
)

// validTree is the smallest catalog LoadFS accepts: one skill, one agent.
func validTree() fstest.MapFS {
	return fstest.MapFS{
		"README.md":              {Data: []byte("r")},
		catalog.BaselinePath:     {Data: []byte("base")},
		"skills/s/SKILL.md":      {Data: []byte(validSkillDescriptor)},
		"agents/x/agent.yaml":    {Data: []byte(validAgentYAML)},
		"agents/x/OPERATIONS.md": {Data: []byte("ops")},
	}
}

// TestLoadFSAcceptsMinimalTree checks that one skill and one agent form a valid catalog.
func TestLoadFSAcceptsMinimalTree(t *testing.T) {
	c, err := catalog.LoadFS(validTree())
	if err != nil {
		t.Fatalf("LoadFS() error = %v", err)
	}
	if len(c.Skills) != 1 || c.Skills[0].ID != "s" || len(c.Agents) != 1 || c.Agents[0].ID != "x" {
		t.Fatalf("catalog = %+v, want skill s and agent x", c)
	}
	var paths []string
	for _, f := range c.Files {
		paths = append(paths, f.Path)
		if f.Mode != regularFileMode {
			t.Errorf("file %q mode = %v, want %v", f.Path, f.Mode, regularFileMode)
		}
	}
	if !slices.IsSorted(paths) || len(paths) != 5 {
		t.Fatalf("file order = %v, want the five files in lexical order", paths)
	}
	if got := c.Skills[0].Files; len(got) != 1 || got[0].Path != catalog.SkillFileName {
		t.Fatalf("skill files = %+v, want SKILL.md with the skill prefix trimmed", got)
	}
}

// TestLoadFSMarksDeclaredExecutables checks that only the scripts a skill declares are marked
// executable.
func TestLoadFSMarksDeclaredExecutables(t *testing.T) {
	fsys := validTree()
	fsys["skills/s/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: s\ndescription: d\nmetadata:\n  executables: [scripts/run.sh]\n---\n")}
	fsys["skills/s/scripts/run.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\n")}
	fsys["skills/s/scripts/other.sh"] = &fstest.MapFile{Data: []byte("#!/bin/sh\n")}
	c, err := catalog.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS() error = %v", err)
	}
	modes := map[string]fs.FileMode{}
	for _, f := range c.Skills[0].Files {
		modes[f.Path] = f.Mode
	}
	if modes["scripts/run.sh"] != scriptFileMode {
		t.Errorf("declared script mode = %v, want %v", modes["scripts/run.sh"], scriptFileMode)
	}
	if modes["scripts/other.sh"] != regularFileMode {
		t.Errorf("undeclared script mode = %v, want %v", modes["scripts/other.sh"], regularFileMode)
	}
}

func TestLoadFSRejectsInvalidTrees(t *testing.T) {
	skillWith := func(descriptor string) func(fstest.MapFS) {
		return func(m fstest.MapFS) { m["skills/s/SKILL.md"] = &fstest.MapFile{Data: []byte(descriptor)} }
	}
	agentWith := func(yaml string) func(fstest.MapFS) {
		return func(m fstest.MapFS) { m["agents/x/agent.yaml"] = &fstest.MapFile{Data: []byte(yaml)} }
	}
	for name, tc := range map[string]struct {
		mutate func(fstest.MapFS)
		want   string
	}{
		"missing readme":   {func(m fstest.MapFS) { delete(m, "README.md") }, "missing or empty catalog file"},
		"blank baseline":   {func(m fstest.MapFS) { m[catalog.BaselinePath] = &fstest.MapFile{Data: []byte(" \n")} }, "missing or empty catalog file"},
		"non-regular file": {func(m fstest.MapFS) { m["link"] = &fstest.MapFile{Mode: fs.ModeSymlink} }, "not regular"},
		"no skills dir":    {func(m fstest.MapFS) { delete(m, "skills/s/SKILL.md") }, "skills"},
		"stray skill file": {func(m fstest.MapFS) { m["skills/notes.md"] = &fstest.MapFile{Data: []byte("n")} }, "invalid skill package"},
		"bad skill id": {func(m fstest.MapFS) {
			m["skills/Bad_ID/SKILL.md"] = &fstest.MapFile{Data: []byte(validSkillDescriptor)}
		}, "invalid skill package"},
		"skill missing descriptor": {func(m fstest.MapFS) { m["skills/t/other.md"] = &fstest.MapFile{Data: []byte("o")} }, "missing or empty catalog file"},
		"no frontmatter":           {skillWith("just a body\n"), "missing skill frontmatter"},
		"broken frontmatter":       {skillWith("---\nname: [unclosed\n---\n"), "yaml"},
		"blank skill name":         {skillWith("---\nname: ' '\ndescription: d\n---\n"), "invalid skill identity"},
		"blank skill desc":         {skillWith("---\nname: s\n---\n"), "invalid skill identity"},
		"executable outside scripts": {
			skillWith("---\nname: s\ndescription: d\nmetadata:\n  executables: [bin/run.sh]\n---\n"), "invalid executable"},
		"executable escapes": {
			skillWith("---\nname: s\ndescription: d\nmetadata:\n  executables: [../evil]\n---\n"), "invalid executable"},
		"executable missing": {
			skillWith("---\nname: s\ndescription: d\nmetadata:\n  executables: [scripts/gone.sh]\n---\n"), "missing or empty catalog file"},
		"executable bad chars": {
			skillWith("---\nname: s\ndescription: d\nmetadata:\n  executables: ['scripts/a:b']\n---\n"), "invalid catalog path"},
		"stray agent file":         {func(m fstest.MapFS) { m["agents/notes.md"] = &fstest.MapFile{Data: []byte("n")} }, "unexpected agent file"},
		"agent no yaml":            {func(m fstest.MapFS) { delete(m, "agents/x/agent.yaml") }, "missing or empty catalog file"},
		"agent unknown key":        {agentWith(validAgentYAML + "surprise: 1\n"), "agents/x/agent.yaml"},
		"agent id mismatch":        {agentWith(strings.Replace(validAgentYAML, "id: x", "id: y", 1)), "invalid or duplicate agent definition"},
		"agent no instance":        {agentWith("id: x\nlabel: x\ngroup: building\ndisplay_names: {x: X}\ninstances: []\ndescription: d\nrole: execution\ncontext: {operations: OPERATIONS.md}\n"), "invalid or duplicate agent definition"},
		"agent no label":           {agentWith(strings.Replace(validAgentYAML, "label: x\n", "", 1)), "invalid or duplicate agent definition"},
		"agent wide label":         {agentWith(strings.Replace(validAgentYAML, "label: x", "label: much-too-wide", 1)), "invalid or duplicate agent definition"},
		"agent unknown group":      {agentWith(strings.Replace(validAgentYAML, "group: building", "group: misc", 1)), "unknown group"},
		"agent no group":           {agentWith(strings.Replace(validAgentYAML, "group: building\n", "", 1)), "unknown group"},
		"agent no display name":    {agentWith(strings.Replace(validAgentYAML, "display_names: {x: X}\n", "display_names: {}\n", 1)), "missing display name"},
		"agent stray display name": {agentWith(strings.Replace(validAgentYAML, "{x: X}", "{x: X, z: Z}", 1)), "undeclared instance"},
		"agent no role":            {agentWith(strings.Replace(validAgentYAML, "role: execution\n", "", 1)), "missing role class"},
		"agent bad role":           {agentWith(strings.Replace(validAgentYAML, "execution", "wizard", 1)), "unknown role class"},
		"bad instance id":          {agentWith(strings.Replace(validAgentYAML, "instances: [x]", "instances: [X_1]", 1)), "invalid or duplicate agent instance"},
		"duplicate instance in agent": {
			agentWith("id: x\nlabel: x\ngroup: building\ndisplay_names: {x: X}\ninstances: [x, x]\ndescription: d\nrole: execution\nvfs_capabilities: {x: []}\ncontext: {operations: OPERATIONS.md}\n"),
			"invalid or duplicate agent instance"},
		"instance claimed by two agents": {func(m fstest.MapFS) {
			m["agents/a/agent.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(validAgentYAML, "id: x", "id: a", 1))}
			m["agents/a/OPERATIONS.md"] = &fstest.MapFile{Data: []byte("ops")}
		}, "invalid or duplicate agent instance"},
		"duplicate capability": {agentWith(strings.Replace(validAgentYAML, "[bind]", "[bind, bind]", 1)), "duplicate VFS capability"},
		"no operations":        {agentWith(strings.Replace(validAgentYAML, "context: {operations: OPERATIONS.md}", "context: {persona: P.md}", 1)), "requires operations"},
		"persona escapes":      {agentWith(strings.Replace(validAgentYAML, "{operations", "{persona: ../x.md, operations", 1)), "escapes its package"},
		"persona missing file": {agentWith(strings.Replace(validAgentYAML, "{operations", "{persona: P.md, operations", 1)), "missing or empty catalog file"},
		"unknown skill":        {agentWith(validAgentYAML + "skills: [ghost]\n"), "references missing skill"},
	} {
		t.Run(name, func(t *testing.T) {
			fsys := validTree()
			tc.mutate(fsys)
			if _, err := catalog.LoadFS(fsys); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadFS() error = %v, want %q", err, tc.want)
			}
		})
	}
}

// TestLoadFSWithoutAgentsDirFails checks that a catalog without agents is refused.
func TestLoadFSWithoutAgentsDirFails(t *testing.T) {
	fsys := validTree()
	delete(fsys, "agents/x/agent.yaml")
	delete(fsys, "agents/x/OPERATIONS.md")
	if _, err := catalog.LoadFS(fsys); err == nil {
		t.Fatal("catalog.LoadFS() succeeded without an agents directory")
	}
}

// TestLoadFSAcceptsAgentReferencingSkill checks that an agent may declare a skill the catalog
// ships.
func TestLoadFSAcceptsAgentReferencingSkill(t *testing.T) {
	fsys := validTree()
	fsys["agents/x/agent.yaml"] = &fstest.MapFile{Data: []byte(validAgentYAML + "skills: [s]\n")}
	c, err := catalog.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS() error = %v", err)
	}
	if !slices.Equal(c.Agents[0].Skills, []string{"s"}) {
		t.Fatalf("agent skills = %v, want [s]", c.Agents[0].Skills)
	}
}

func TestContextPathsAndComposeText(t *testing.T) {
	fsys := validTree()
	fsys["agents/x/agent.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(validAgentYAML, "{operations", "{persona: PERSONA.md, soul: SOUL.md, operations", 1))}
	fsys["agents/x/PERSONA.md"] = &fstest.MapFile{Data: []byte("\npersona\n")}
	fsys["agents/x/SOUL.md"] = &fstest.MapFile{Data: []byte("soul\n")}
	c, err := catalog.LoadFS(fsys)
	if err != nil {
		t.Fatalf("LoadFS() error = %v", err)
	}
	a := c.Agents[0]
	wantPaths := []string{catalog.BaselinePath, "agents/x/PERSONA.md", "agents/x/SOUL.md", "agents/x/OPERATIONS.md"}
	if got := a.ContextPaths(); !slices.Equal(got, wantPaths) {
		t.Fatalf("ContextPaths() = %v, want %v", got, wantPaths)
	}
	text, err := a.ComposeText(fsys)
	if err != nil {
		t.Fatalf("ComposeText() error = %v", err)
	}
	if want := "base\n\npersona\n\nsoul\n\nops"; text != want {
		t.Fatalf("ComposeText() = %q, want %q", text, want)
	}
	delete(fsys, "agents/x/SOUL.md")
	if _, err := a.ComposeText(fsys); err == nil || !strings.Contains(err.Error(), `read context file "agents/x/SOUL.md" for agent "x"`) {
		t.Fatalf("ComposeText() error = %v, want the missing SOUL.md named", err)
	}
}
