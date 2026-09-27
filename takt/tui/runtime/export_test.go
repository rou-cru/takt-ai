package runtime

import "github.com/rou-cru/takt-ai/takt/lifecycle"

// NewTestAdapter builds an Adapter on a fake lifecycle so tests avoid real providers.
func NewTestAdapter(lifecycle lifecycle.Runtime) Adapter {
	return Adapter{lifecycle: lifecycle}
}

// InstalledPreview exposes installedPreview so tests can lock down its
// defaulting behavior against the installed record.
func InstalledPreview(rootDir string) (lifecycle.InstallPreview, error) {
	return installedPreview(rootDir)
}
