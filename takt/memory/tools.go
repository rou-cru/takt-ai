package memory

import "github.com/rou-cru/takt-ai/takt/prompts"

// ServerKey is the MCP server key clients register the service under; it is distinctive so it never
// replaces or collides with a user's server under a generic key such as "memory".
const ServerKey = "takt_memory"

// PrefixedToolName is the name OpenCode gives a tool of the memory MCP server: the server key, an
// underscore, and the tool.
func PrefixedToolName(tool string) string { return ServerKey + "_" + tool }

// Names of the six tools served identically on /crew and /external, the single source of tool names.
const (
	ToolRecord          = "record"
	ToolSearch          = "search"
	ToolRead            = "read"
	ToolSessions        = "sessions"
	ToolContinueSession = "continue_session"
	ToolCloseSession    = "close_session"
)

// Names of the tools served on /admin, which only Takt code calls.
const (
	AdminBind     = "bind"
	AdminDescribe = "describe"
	AdminStatus   = "status"
	AdminInit     = "init"
	AdminBackup   = "backup"
	// AdminContinuations lists the crew sessions a root session continued.
	AdminContinuations = "continuations"
	// AdminShutdown asks the owner answering on the socket to stop; only the lock holder binds the
	// socket, so the request reaches the owner and never another process.
	AdminShutdown = "shutdown"
)

// Tool is one tool's name and its caller-facing description; argument descriptions are the jsonschema tags of its argument type.
type Tool struct {
	Name        string
	Description string
}

// Tools are the six tools of /crew and /external, with the same texts for every caller.
var Tools = loadMemoryTools()

func loadMemoryTools() []Tool {
	reg := prompts.Default()
	rootOnly := reg.MustRender("tool.memory.root_only", nil)
	mustDesc := func(name string, vars map[string]string) string {
		desc, err := reg.RenderToolDescription(name, vars)
		if err != nil {
			panic("render memory tool description " + name + ": " + err.Error())
		}
		return desc
	}
	return []Tool{
		{Name: ToolRecord, Description: mustDesc(ToolRecord, nil)},
		{Name: ToolSearch, Description: mustDesc(ToolSearch, nil)},
		{Name: ToolRead, Description: mustDesc(ToolRead, nil)},
		{Name: ToolSessions, Description: mustDesc(ToolSessions, nil)},
		{Name: ToolContinueSession, Description: mustDesc(ToolContinueSession, map[string]string{"root_only": rootOnly})},
		{Name: ToolCloseSession, Description: mustDesc(ToolCloseSession, map[string]string{"root_only": rootOnly})},
	}
}
