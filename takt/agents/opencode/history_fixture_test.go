package opencode_test

import (
	"os"
	"testing"
)

func writeHistoryFixture(t *testing.T, write func(string, []byte)) {
	t.Helper()
	data, err := os.ReadFile("testdata/history-fixture.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("history-fixture.mjs", data)
}
