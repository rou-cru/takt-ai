package opencode_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

func TestSandboxProjectionMatchesCanonicalAdapter(t *testing.T) {
	canonical, err := os.ReadFile("../../runtime/sandbox/adapter.mjs")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, opencode.TaktSandboxAdapterArtifact().Content) {
		t.Fatal("sandbox installation artifact differs from canonical adapter; run go generate ./takt/agents/opencode")
	}
}
