package runtime_test

import (
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// TestMain puts a compatible fake engram first on PATH, so install, sync and
// drift correction resolve it instead of the host binary or a real download.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithFakeEngram(m))
}
