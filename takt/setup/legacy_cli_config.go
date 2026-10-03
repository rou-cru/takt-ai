// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

// legacyCLIConfigPath is cli.json, which earlier releases managed only to
// select a "takt" theme that was never shipped.
var legacyCLIConfigPath = opencode.ConfigDir() + "/cli.json"

// legacyThemeName is the theme earlier releases wrote into cli.json.
const legacyThemeName = "takt"

// ReleaseLegacyCLIConfig hands cli.json back to the user: it drops the
// dangling theme selection Takt wrote and forgets the file in the ownership
// manifest, so no later operation (uninstall included) replaces or deletes it.
// The user's own keys are kept. A root that never managed cli.json is a no-op.
func ReleaseLegacyCLIConfig(rootDir string) error {
	manifest, err := LoadOwnershipManifest(rootDir)
	if err != nil {
		return err
	}
	if _, managed := manifest.Entries[legacyCLIConfigPath]; !managed {
		return nil
	}
	if err := dropLegacyTheme(filepath.Join(rootDir, filepath.FromSlash(legacyCLIConfigPath))); err != nil {
		return err
	}
	delete(manifest.Entries, legacyCLIConfigPath)
	return manifest.Save(rootDir)
}

func dropLegacyTheme(path string) error {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", legacyCLIConfigPath, err)
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		return nil
	}
	theme, ok := config["theme"].(map[string]any)
	if !ok || theme["name"] != legacyThemeName {
		return nil
	}
	delete(theme, "name")
	if len(theme) == 0 {
		delete(config, "theme")
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(content, '\n'), info.Mode().Perm())
}
