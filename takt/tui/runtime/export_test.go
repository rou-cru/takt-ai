package runtime

import "github.com/rou-cru/takt-ai/takt/lifecycle"

// InstalledPreview exposes installedPreview so tests can lock down its
// defaulting behavior against the installed record.
func InstalledPreview(rootDir string) (lifecycle.InstallPreview, error) {
	return installedPreview(rootDir)
}
