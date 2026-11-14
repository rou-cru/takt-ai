package runtime_test

import (
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// TestMain puts compatible fake tools first on PATH, so install, sync and
// drift correction resolve them instead of host binaries or a real download.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithFakeTools(m))
}
