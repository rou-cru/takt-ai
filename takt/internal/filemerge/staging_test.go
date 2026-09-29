package filemerge

import (
	"io/fs"
	"os"
	"testing"
)

func TestStageTempFileSuccessComplete(t *testing.T) {
	for _, mode := range []fs.FileMode{0o600, 0o755} {
		name, err := StageTempFile(t.TempDir(), "stage-*", []byte("complete"), mode)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(name)
		if err != nil || string(data) != "complete" {
			t.Fatalf("content: %q, %v", data, err)
		}
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("mode: %v", info.Mode())
		}
	}
}
