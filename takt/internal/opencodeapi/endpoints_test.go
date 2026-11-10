// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi_test

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
)

// stubRunner records every call and replays a canned answer. Tests key the
// answer by "METHOD /path" so multi-call flows (like the handshake) can mix
// responses.
type stubRunner struct {
	calls  [][]string
	routes map[string]stubResponse
}

type stubResponse struct {
	stdout string
	stderr string
	err    error
}

func (s *stubRunner) run(_ context.Context, args []string) ([]byte, []byte, error) {
	s.calls = append(s.calls, args)
	key := fmt.Sprintf("%s %s", args[len(args)-2], args[len(args)-1])
	response, ok := s.routes[key]
	if !ok {
		return nil, nil, fmt.Errorf("stub has no route for %q", key)
	}
	return []byte(response.stdout), []byte(response.stderr), response.err
}

func stubClient(routes map[string]stubResponse) (*opencodeapi.Client, *stubRunner) {
	stub := &stubRunner{routes: routes}
	return opencodeapi.New(opencodeapi.WithRunner(stub.run)), stub
}

// The fixtures below are real payloads captured from OpenCode v2.0.16
// ("opencode api GET /api/..."), trimmed to the fields that drive assertions
// and padded with unknown fields to prove lenient parsing.

const infoBody = `{"version":"2.0.16","pid":4242,"urls":["http://127.0.0.1:59998"],"paths":{"tmp":"/tmp/opencode"}}`

const modelsBody = `{"location":{"directory":"/home/dev"},"data":[
  {
    "id":"deepseek-v4.1-flash",
    "modelID":"deepseek-v4.1-flash",
    "providerID":"opencode",
    "family":"deepseek-flash",
    "name":"DeepSeek V4.1 Flash",
    "compatibility":{"reasoningField":"reasoning_content"},
    "package":"@opencode/ai/providers/openai-compatible",
    "settings":{"baseURL":"https://opencode.ai/inference/openai/v1","provider":"opencode"},
    "headers":{"x-opencode-org-id":"wrk_01KPCZP4FR9AR0PV5VX195A83Y"},
    "capabilities":{"tools":true,"input":["text","image"],"output":["text"]},
    "variants":[{"id":"low","settings":{"reasoningEffort":"low"}},{"id":"high","settings":{"reasoningEffort":"high"}}],
    "time":{"released":1788998400000},
    "cost":[{"input":0.3,"output":1.2,"cache":{"read":0.006,"write":0}}],
    "status":"active",
    "enabled":true,
    "limit":{"context":1000000,"output":384000},
    "unknownFieldTheAdapterMustIgnore":{"nested":true}
  },
  {
    "id":"glm-5.3-flash",
    "modelID":"glm-5.3-flash",
    "providerID":"opencode-go",
    "name":"GLM 5.3 Flash",
    "capabilities":{"tools":true,"input":["text"],"output":["text"]},
    "time":{"released":1780000000000},
    "cost":[{"tier":{"type":"context","size":200000},"input":0.6,"output":2.2,"cache":{"read":0.11,"write":0.2}}],
    "status":"active",
    "enabled":true,
    "limit":{"context":204800,"output":32768}
  }
]}`

const mcpBody = `{"location":{"directory":"/home/dev"},"data":[
  {"name":"aws-cost-management","status":{"status":"disabled"}},
  {"name":"codegraph","status":{"status":"connected"}},
  {"name":"context7","status":{"status":"connected"},"integrationID":"mcp_0123456789abcdef"},
  {"name":"ci-gateway","status":{"status":"failed","error":"connect ECONNREFUSED 127.0.0.1:8080"}},
  {"name":"obsidian","status":{"status":"needs_auth","error":"authorization required"}},
  {"name":"warming","status":{"status":"pending"}}
]}`

const agentsBody = `{"location":{"directory":"/home/dev"},"data":[
  {
    "id":"build","name":"Build",
    "request":{"settings":{},"headers":{},"body":{}},
    "description":"The default agent. Executes tools based on configured permissions.",
    "mode":"primary","hidden":false,
    "permissions":[{"action":"*","resource":"*","effect":"allow"}]
  },
  {
    "id":"takt-orchestrator","name":"Takt Orchestrator",
    "request":{"settings":{},"headers":{},"body":{}},
    "description":"SDD orchestrator","mode":"primary","hidden":false,
    "model":{"id":"glm-5.3-flash","providerID":"opencode-go","variant":"max"},
    "permissions":[{"action":"*","resource":"*","effect":"allow"}]
  }
]}`

const skillsBody = `{"location":{"directory":"/home/dev"},"data":[
  {
    "id":"opencode","name":"OpenCode",
    "description":"Use this skill for any question about OpenCode itself.",
    "path":"/home/dev/.config/opencode/skills/opencode/SKILL.md",
    "content":"---\nname: OpenCode\n---\n..."
  },
  {
    "id":"sdd-verify","name":"SDD Verify",
    "path":"/home/dev/.config/opencode/skills/sdd-verify/SKILL.md",
    "content":"---\nname: SDD Verify\n---\n..."
  }
]}`

const pluginsBody = `{"location":{"directory":"/home/dev"},"data":[
  {"id":"opencode.tool.input.repair","source":{"type":"builtin"},"features":{"server":true},"state":{"status":"active"}},
  {"id":"takt-skill-registry","source":{"type":"local","path":"/home/dev/.config/opencode/plugins/takt-skill-registry.ts"},"features":{"server":true},"state":{"status":"failed","error":"module not found","ref":"plugin/index.ts:42"}}
]}`

func routesFor(body string) map[string]stubResponse {
	return map[string]stubResponse{"GET /api/info": {stdout: body}}
}

func TestListEndpointContracts(t *testing.T) {
	testListEndpoint(t, "/api/model", (*opencodeapi.Client).Models,
		`{"id":"m","modelID":"m","providerID":"p","limit":{"context":1,"output":1}}`,
		`{"id":"m","modelID":"m","providerID":"p","limit":{"context":0,"output":1}}`, "non-positive limits")
	testListEndpoint(t, "/api/mcp", (*opencodeapi.Client).MCPStatus,
		`{"name":"m","status":{"status":"connected"}}`,
		`{"name":"m","status":{"status":"half-open"}}`, "unknown state")
	testListEndpoint(t, "/api/agent", (*opencodeapi.Client).Agents,
		`{"id":"a","mode":"primary"}`, `{"name":"NoID"}`, "agent entry has no id")
	testListEndpoint(t, "/api/skill", (*opencodeapi.Client).Skills,
		`{"id":"s"}`, `{"name":"NoID"}`, "skill entry has no id")
	testListEndpoint(t, "/api/plugin", (*opencodeapi.Client).Plugins,
		`{"state":{"status":"active"}}`, `{"state":{"status":"loading"}}`, "unknown status")
}

func testListEndpoint[T any](t *testing.T, path string, call func(*opencodeapi.Client, context.Context) ([]T, error), valid, invalid, detail string) {
	t.Helper()
	t.Run(path, func(t *testing.T) {
		for _, tc := range []struct {
			name     string
			response stubResponse
			wantErr  error
		}{
			{"empty list", stubResponse{stdout: `{"data":[]}`}, nil},
			{"unavailable", stubResponse{err: errors.New("offline")}, opencodeapi.ErrUnavailable},
			{"missing binary", stubResponse{err: exec.ErrNotFound}, opencodeapi.ErrBinaryMissing},
			{"empty response", stubResponse{}, opencodeapi.ErrInvalidResponse},
			{"invalid JSON", stubResponse{stdout: "not JSON"}, opencodeapi.ErrInvalidResponse},
			{"wrong data shape", stubResponse{stdout: `{"data":{}}`}, opencodeapi.ErrInvalidResponse},
			{"invalid second entry", stubResponse{stdout: `{"data":[` + valid + `,` + invalid + `]}`}, opencodeapi.ErrInvalidResponse},
		} {
			t.Run(tc.name, func(t *testing.T) {
				client, stub := stubClient(map[string]stubResponse{"GET " + path: tc.response})
				got, err := call(client, context.Background())
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("error = %v, want %v", err, tc.wantErr)
				}
				if tc.wantErr != nil && got != nil {
					t.Fatalf("failed list returned partial results: %+v", got)
				}
				if tc.wantErr == nil && (got == nil || len(got) != 0) {
					t.Fatalf("empty list = %+v, want non-nil empty slice", got)
				}
				if tc.name == "invalid second entry" && (!strings.Contains(err.Error(), path+" entry 1:") || !strings.Contains(err.Error(), detail)) {
					t.Fatalf("error = %v, want endpoint, index and %q", err, detail)
				}
				if want := [][]string{{"api", "GET", path}}; !reflect.DeepEqual(stub.calls, want) {
					t.Fatalf("calls = %v, want %v", stub.calls, want)
				}
			})
		}
	})
}

func TestInfoReturnsTheExactServerVersion(t *testing.T) {
	client, _ := stubClient(routesFor(infoBody))

	info, err := client.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.Version != "2.0.16" {
		t.Fatalf("version = %q, want %q", info.Version, "2.0.16")
	}
}

func TestModelsReturnsStructuredModelsInReportedOrder(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{"GET /api/model": {stdout: modelsBody}})

	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}

	first := models[0]
	if first.Ref != (opencodeapi.ModelRef{ProviderID: "opencode", ModelID: "deepseek-v4.1-flash"}) {
		t.Fatalf("first ref = %+v", first.Ref)
	}
	if first.Ref.String() != "opencode/deepseek-v4.1-flash" {
		t.Fatalf("first String() = %q", first.Ref.String())
	}
	second := models[1]
	if second.Ref.String() != "opencode-go/glm-5.3-flash" {
		t.Fatalf("second String() = %q", second.Ref.String())
	}
}

// A variant shares modelID with its base; only id tells them apart and is
// what OpenCode config references.
func TestModelsRefsVariantsByID(t *testing.T) {
	body := `{"data":[
	  {"id":"gpt-6-luna","modelID":"gpt-6-luna","providerID":"openai","name":"GPT-6 Luna","limit":{"context":1,"output":1}},
	  {"id":"gpt-6-luna-fast","modelID":"gpt-6-luna","providerID":"openai","name":"GPT-6 Luna Fast","limit":{"context":1,"output":1}}]}`
	client, _ := stubClient(map[string]stubResponse{"GET /api/model": {stdout: body}})
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 2 || models[0].Ref.String() != "openai/gpt-6-luna" || models[1].Ref.String() != "openai/gpt-6-luna-fast" {
		t.Fatalf("refs = %+v, want base and variant distinguished by id", models)
	}
	if models[1].Name != "GPT-6 Luna Fast" {
		t.Fatalf("name = %q", models[1].Name)
	}
}

func TestModelsReportsAnEmptyListWithoutError(t *testing.T) {
	// "No models" is data the consumer must act on, not an adapter failure:
	// The models route needs to distinguish an empty picker from a broken installation.
	client, _ := stubClient(map[string]stubResponse{"GET /api/model": {stdout: `{"location":{"directory":"/x"},"data":[]}`}})

	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("Models() error = %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("models = %v, want empty", models)
	}
}

func TestModelsRejectsAnEntryWithoutIdentity(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/model": {stdout: `{"location":{"directory":"/x"},"data":[{"id":"orphan","modelID":"orphan","providerID":"","name":"Orphan"}]}`},
	})

	if _, err := client.Models(context.Background()); !errors.Is(err, opencodeapi.ErrInvalidResponse) {
		t.Fatalf("Models() error = %v, want ErrInvalidResponse", err)
	}
}

func TestDefaultModelReturnsTheServerDefault(t *testing.T) {
	// The default endpoint answers with a single model, not an array.
	raw := `{"location":{"directory":"/home/dev"},"data":{"id":"deepseek-v4.1-flash","modelID":"deepseek-v4.1-flash","providerID":"opencode","name":"DeepSeek V4.1 Flash","capabilities":{"tools":true,"input":["text"],"output":["text"]},"time":{"released":1788998400000},"cost":[],"status":"active","enabled":true,"limit":{"context":1000000,"output":384000}}}`
	client, _ := stubClient(map[string]stubResponse{"GET /api/model/default": {stdout: raw}})

	model, err := client.DefaultModel(context.Background())
	if err != nil {
		t.Fatalf("DefaultModel() error = %v", err)
	}
	if model == nil || model.Ref.String() != "opencode/deepseek-v4.1-flash" {
		t.Fatalf("default model = %+v", model)
	}
}

func TestDefaultModelReturnsNilWhenTheServerHasNoDefault(t *testing.T) {
	// data:null is the PR-SET-7 "unresolved default" signal: it must reach
	// the caller as (nil, nil), not as an error that hides the distinction.
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/model/default": {stdout: `{"location":{"directory":"/x"},"data":null}`},
	})

	model, err := client.DefaultModel(context.Background())
	if err != nil {
		t.Fatalf("DefaultModel() error = %v", err)
	}
	if model != nil {
		t.Fatalf("model = %+v, want nil", model)
	}
}

func TestMCPStatusMapsEveryReportedState(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{"GET /api/mcp": {stdout: mcpBody}})

	servers, err := client.MCPStatus(context.Background())
	if err != nil {
		t.Fatalf("MCPStatus() error = %v", err)
	}

	want := []opencodeapi.MCPServer{
		{Name: "aws-cost-management", State: opencodeapi.MCPDisabled},
		{Name: "codegraph", State: opencodeapi.MCPConnected},
		{Name: "context7", State: opencodeapi.MCPConnected},
		{Name: "ci-gateway", State: opencodeapi.MCPFailed, Error: "connect ECONNREFUSED 127.0.0.1:8080"},
		{Name: "obsidian", State: opencodeapi.MCPNeedsAuth, Error: "authorization required"},
		{Name: "warming", State: opencodeapi.MCPPending},
	}
	if !reflect.DeepEqual(servers, want) {
		t.Fatalf("servers =\n%+v\nwant\n%+v", servers, want)
	}
}

func TestMCPStatusRejectsAnUnknownState(t *testing.T) {
	// Verification must never guess what an unknown state means: a server
	// half-connecting under a new status name must fail loudly, not pass.
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/mcp": {stdout: `{"location":{"directory":"/x"},"data":[{"name":"codegraph","status":{"status":"half-open"}}]}`},
	})

	if _, err := client.MCPStatus(context.Background()); !errors.Is(err, opencodeapi.ErrInvalidResponse) {
		t.Fatalf("MCPStatus() error = %v, want ErrInvalidResponse", err)
	}
}

func TestAgentsReturnsRegisteredAgents(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{"GET /api/agent": {stdout: agentsBody}})

	agents, err := client.Agents(context.Background())
	if err != nil {
		t.Fatalf("Agents() error = %v", err)
	}
	if len(agents) != 2 {
		t.Fatalf("len(agents) = %d, want 2", len(agents))
	}
	if agents[0].ID != "build" || agents[0].Mode != "primary" || agents[0].Hidden {
		t.Fatalf("first agent = %+v", agents[0])
	}
	// The second entry pins a model ("opencode-go/glm-5.3-flash#max"); Agent
	// no longer carries that ref (nothing reads it), but toAgent still
	// validates the pin, so a malformed one would fail parsing here.
	if agents[1].ID != "takt-orchestrator" {
		t.Fatalf("second agent id = %q", agents[1].ID)
	}
}

func TestAgentsRejectsAnEntryWithoutID(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/agent": {stdout: `{"location":{"directory":"/x"},"data":[{"name":"NoID"}]}`},
	})

	if _, err := client.Agents(context.Background()); !errors.Is(err, opencodeapi.ErrInvalidResponse) {
		t.Fatalf("Agents() error = %v, want ErrInvalidResponse", err)
	}
}

func TestSkillsReturnsDiscoveredSkills(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{"GET /api/skill": {stdout: skillsBody}})

	skills, err := client.Skills(context.Background())
	if err != nil {
		t.Fatalf("Skills() error = %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("len(skills) = %d, want 2", len(skills))
	}
	if skills[0].ID != "opencode" {
		t.Fatalf("first skill = %+v", skills[0])
	}
	if skills[1].ID != "sdd-verify" {
		t.Fatalf("second skill = %+v", skills[1])
	}
}

func TestPluginsReportsLoadState(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{"GET /api/plugin": {stdout: pluginsBody}})

	plugins, err := client.Plugins(context.Background())
	if err != nil {
		t.Fatalf("Plugins() error = %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("len(plugins) = %d, want 2", len(plugins))
	}
	if plugins[0] != (opencodeapi.Plugin{ID: "opencode.tool.input.repair", Type: "builtin", Status: "active"}) {
		t.Fatalf("first plugin = %+v", plugins[0])
	}
	if plugins[1].Status != "failed" || plugins[1].Error != "module not found" {
		t.Fatalf("second plugin = %+v", plugins[1])
	}
}

func TestPluginsRejectsAnUnknownStatus(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/plugin": {stdout: `{"location":{"directory":"/x"},"data":[{"id":"p","source":{"type":"builtin"},"state":{"status":"loading"}}]}`},
	})

	if _, err := client.Plugins(context.Background()); !errors.Is(err, opencodeapi.ErrInvalidResponse) {
		t.Fatalf("Plugins() error = %v, want ErrInvalidResponse", err)
	}
}

func TestReloadRestartsTheServiceThenPostsTheEndpoint(t *testing.T) {
	client, stub := stubClient(map[string]stubResponse{
		"service restart":           {stdout: "http://127.0.0.1:4096\n"},
		"POST /api/location/reload": {stdout: ""}, // 204 answers with no body
	})

	if err := client.Reload(context.Background()); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	// The restart comes first: a server that loaded plugins before their
	// dependencies existed keeps failing to resolve them until replaced.
	want := [][]string{{"service", "restart"}, {"api", "POST", "/api/location/reload"}}
	if !reflect.DeepEqual(stub.calls, want) {
		t.Fatalf("calls = %v, want %v (no --data: reload takes no body)", stub.calls, want)
	}
}

func TestReloadSurfacesAFailedRestartAsAnError(t *testing.T) {
	client, stub := stubClient(map[string]stubResponse{
		"service restart": {stderr: "cannot stop service", err: errors.New("exit status 1")},
	})

	if err := client.Reload(context.Background()); !errors.Is(err, opencodeapi.ErrUnavailable) {
		t.Fatalf("Reload() error = %v, want ErrUnavailable", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("calls = %v, want no location reload after a failed restart", stub.calls)
	}
}

func TestReloadSurfacesAFailedReloadAsAnError(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"service restart":           {stdout: "http://127.0.0.1:4096\n"},
		"POST /api/location/reload": {stderr: "HTTP 503 Service Unavailable", err: errors.New("exit status 1")},
	})

	// A reload that did not happen is a functional failure: it must be
	// an error the plan can report as "not verified", never a silent success.
	if err := client.Reload(context.Background()); !errors.Is(err, opencodeapi.ErrUnavailable) {
		t.Fatalf("Reload() error = %v, want ErrUnavailable", err)
	}
}

func TestHandshakePassesOnAV2ServerWithWorkingRoutes(t *testing.T) {
	client, stub := stubClient(map[string]stubResponse{
		"GET /api/info":  {stdout: `{"version":"` + opencodeapi.MinimumVersion + `"}`},
		"GET /api/model": {stdout: modelsBody},
	})

	handshake, err := client.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if handshake.Version != opencodeapi.MinimumVersion || handshake.Major != 2 {
		t.Fatalf("handshake = %+v", handshake)
	}
	// The probe order is the failure ladder: identity first, mechanism last.
	if len(stub.calls) != 2 || stub.calls[0][2] != "/api/info" || stub.calls[1][2] != "/api/model" {
		t.Fatalf("calls = %v, want /api/info then /api/model", stub.calls)
	}
}

func TestHandshakeGatesOnTheMinimumVersion(t *testing.T) {
	for _, tc := range []struct {
		version  string
		accepted bool
	}{
		{"1.99.99", false},
		{"2.0.3", false},
		{opencodeapi.MinimumVersion, true},
	} {
		t.Run(tc.version, func(t *testing.T) {
			client, _ := stubClient(map[string]stubResponse{
				"GET /api/info":  {stdout: `{"version":"` + tc.version + `"}`},
				"GET /api/model": {stdout: `{"location":{"directory":"/x"},"data":[]}`},
			})
			_, err := client.Handshake(context.Background())
			if tc.accepted {
				if err != nil {
					t.Fatalf("Handshake(%s) error = %v, want it accepted", tc.version, err)
				}
				return
			}
			if !errors.Is(err, opencodeapi.ErrVersion) || !strings.Contains(err.Error(), opencodeapi.MinimumVersion) {
				t.Fatalf("Handshake(%s) error = %v, want ErrVersion naming %s", tc.version, err, opencodeapi.MinimumVersion)
			}
		})
	}
}

func TestHandshakeAcceptsANewerMajor(t *testing.T) {
	// The requirement is >= 2, not == 2: OpenCode v3 must not be rejected.
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/info":  {stdout: `{"version":"3.1.0"}`},
		"GET /api/model": {stdout: `{"location":{"directory":"/x"},"data":[]}`},
	})

	handshake, err := client.Handshake(context.Background())
	if err != nil {
		t.Fatalf("Handshake() error = %v", err)
	}
	if handshake.Major != 3 {
		t.Fatalf("major = %d, want 3", handshake.Major)
	}
}

func TestHandshakeRejectsAV1Server(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/info": {stdout: `{"version":"1.6.2"}`},
	})

	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrVersion) {
		t.Fatalf("Handshake() error = %v, want ErrVersion", err)
	}
}

func TestHandshakeRejectsAMissingBinary(t *testing.T) {
	client := opencodeapi.New(opencodeapi.WithRunner(func(context.Context, []string) ([]byte, []byte, error) {
		return nil, nil, &exec.Error{Name: "opencode", Err: exec.ErrNotFound}
	}))

	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrBinaryMissing) {
		t.Fatalf("Handshake() error = %v, want ErrBinaryMissing", err)
	}
}

func TestHandshakeRejectsAnUnreachableAPI(t *testing.T) {
	client := opencodeapi.New(opencodeapi.WithRunner(func(context.Context, []string) ([]byte, []byte, error) {
		return nil, []byte("Could not reach server at http://127.0.0.1:59999\n"), errors.New("exit status 1")
	}))

	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrUnavailable) {
		t.Fatalf("Handshake() error = %v, want ErrUnavailable", err)
	}
}

func TestHandshakeRejectsAGarbageBodyAsDrift(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/info": {stdout: "<html>not the API</html>"},
	})

	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrInvalidResponse) {
		t.Fatalf("Handshake() error = %v, want ErrInvalidResponse", err)
	}
}

func TestHandshakeRejectsAVersionWithoutDigits(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/info": {stdout: `{"version":"opencode"}`},
	})

	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrInvalidResponse) {
		t.Fatalf("Handshake() error = %v, want ErrInvalidResponse", err)
	}
}

func TestHandshakeRejectsAV2BinaryWithoutModelRoutes(t *testing.T) {
	// Installed is not functional: a v2 server whose experimental model
	// routes fail must not pass the handshake, or Takt would mutate on a
	// broken foundation.
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/info":  {stdout: `{"version":"` + opencodeapi.MinimumVersion + `"}`},
		"GET /api/model": {stderr: "HTTP 503 Service Unavailable", err: errors.New("exit status 1")},
	})

	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrUnavailable) {
		t.Fatalf("Handshake() error = %v, want ErrUnavailable", err)
	}
}

func TestHandshakeCleansUpMalformedModelEntries(t *testing.T) {
	client, _ := stubClient(map[string]stubResponse{
		"GET /api/info":  {stdout: `{"version":"` + opencodeapi.MinimumVersion + `"}`},
		"GET /api/model": {stdout: `{"location":{"directory":"/x"},"data":[{"modelID":"orphan"}]}`},
	})

	// A broken model entry means the experimental routes drifted: probe
	// failure, not version failure.
	_, err := client.Handshake(context.Background())
	if !errors.Is(err, opencodeapi.ErrUnavailable) {
		t.Fatalf("Handshake() error = %v, want ErrUnavailable", err)
	}
}
