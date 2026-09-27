package verify

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
)

func TestNativeOrchestratorCheck(t *testing.T) {
	for _, tc := range []struct {
		name        string
		output      string
		runErr      error
		want        State
		explanation string
	}{
		{name: "primary", output: `{"data":[{"id":"takt","mode":"primary","hidden":false}]}`, want: Verified},
		{name: "all", output: `{"data":[{"id":"build","mode":"primary"},{"id":"takt","mode":"all"}]}`, want: Verified},
		{name: "subagent only", output: `{"data":[{"id":"takt","mode":"subagent"}]}`, want: NotVerified},
		{name: "hidden primary", output: `{"data":[{"id":"takt","mode":"primary","hidden":true}]}`, want: NotVerified},
		{name: "hidden all", output: `{"data":[{"id":"takt","mode":"all","hidden":true}]}`, want: NotVerified},
		{name: "other agents only", output: `{"data":[{"id":"build","mode":"primary"}]}`, want: NotVerified},
		{name: "empty inventory", output: `{"data":[]}`, want: NotVerified},
		{name: "V1 text is not inventory", output: "takt (primary)\n", want: NotVerifiable, explanation: opencodeapi.ErrInvalidResponse.Error()},
		{name: "invalid entry after orchestrator", output: `{"data":[{"id":"takt","mode":"primary"},{"name":"NoID"}]}`, want: NotVerifiable, explanation: "/api/agent entry 1: agent entry has no id"},
		{name: "invalid model reference", output: `{"data":[{"id":"takt","mode":"primary","model":{"id":"orphan"}}]}`, want: NotVerifiable, explanation: "model ref is missing providerID or id"},
		{name: "unavailable", runErr: errors.New("fake server unavailable"), want: NotVerifiable, explanation: opencodeapi.ErrUnavailable.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := opencodeapi.New(opencodeapi.WithRunner(func(_ context.Context, args []string) ([]byte, []byte, error) {
				calls++
				if want := []string{"api", "GET", "/api/agent"}; !reflect.DeepEqual(args, want) {
					t.Fatalf("args = %v, want %v", args, want)
				}
				return []byte(tc.output), nil, tc.runErr
			}))
			got := nativeOrchestratorCheck(context.Background(), client)
			if calls != 1 {
				t.Fatalf("inventory calls = %d, want 1", calls)
			}
			if got.ID != "orchestrator:opencode:takt" || got.State != tc.want {
				t.Fatalf("nativeOrchestratorCheck() = %+v, want state %q", got, tc.want)
			}
			if got.Explanation == "" || !strings.Contains(got.Explanation, tc.explanation) {
				t.Fatalf("explanation = %q, want nonempty and containing %q", got.Explanation, tc.explanation)
			}
		})
	}
}
