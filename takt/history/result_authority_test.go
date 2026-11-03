package history_test

import (
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

func TestEvaluatedResultRequiresRevisionAndEvidence(t *testing.T) {
	for _, status := range []history.ResultStatus{history.ResultVerified, history.ResultFailed} {
		for _, missing := range []string{"revision", "evidence", "both"} {
			t.Run(string(status)+"/"+missing, func(t *testing.T) {
				h := open(t, filepath.Join(t.TempDir(), "state"))
				if err := h.Append(observed(t, "result", history.KindAdmitted, "")); err != nil {
					t.Fatal(err)
				}
				e := resultAt(t, "result", status, 1, "journal/1")
				if missing != "evidence" {
					e.Revision = nil
				}
				if missing != "revision" {
					e.Evidence = " "
				}
				if err := h.Append(e); err == nil {
					t.Fatal("incomplete evaluated result was recorded")
				}
				if len(h.Entries()) != 1 {
					t.Fatal("refusal changed history")
				}
			})
		}
	}
}
