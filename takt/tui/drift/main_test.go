package drift_test

import (
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.RunWithFakeEngram(m)) }
