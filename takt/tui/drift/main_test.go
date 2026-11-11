package drift_test

import (
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// TestMain runs the package with fake tools and removes the shared installed roots afterwards.
func TestMain(m *testing.M) {
	code := testutil.RunWithFakeTools(m)
	removeFixtures()
	os.Exit(code)
}
