package drift_test

import (
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

func TestMain(m *testing.M) {
	code := testutil.RunWithFakeEngram(m)
	removeFixtures()
	os.Exit(code)
}
