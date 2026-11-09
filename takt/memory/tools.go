package memory

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
)

// Tool is one tool's name and its caller-facing description; argument descriptions are the jsonschema tags of its argument type.
type Tool struct {
	Name        string
	Description string
}

// rootOnly states the caller rule shared by the session lifecycle tools.
const rootOnly = "Only the root session of a conversation may call this; from any other session it is refused, " +
	"and the root session makes the call instead."

// Tools are the six tools of /crew and /external, with the same texts for every caller.
var Tools = []Tool{
	{
		Name: ToolRecord,
		Description: "Record one Takt memory entry: something that happened or is true now, such as an idea explored, " +
			"a fact verified, a decision taken, or an artifact changed. Never record plans, next steps, pending work, " +
			"or predictions unless the user directly ordered it. Entries are never changed or deleted; a change in " +
			"understanding is a new entry whose relates_to names the affected entry. A new entry sends content. An entry " +
			"that corrects or supersedes another is stored as the complete current artifact: send the complete content. " +
			"Returns `Recorded #<id>`. A request larger than the transport's request cap is refused, stating the cap.",
	},
	{
		Name: ToolSearch,
		Description: "Search the Takt memory entries that currently apply, most relevant first. Each hit is one line: ID, " +
			"nature, tier, title, author, date, and markers. [disputed] marks an open disagreement and " +
			"[exception] a scoped exception: read the entry before relying on it. An invalid argument is refused; " +
			"correct that argument.",
	},
	{
		Name: ToolRead,
		Description: "Read one Takt memory entry by ID; a known ID needs no search first. Returns its metadata, relations, " +
			"and complete body, its current version when it was corrected or superseded, and the current version of " +
			"each entry that supplements it. When that exceeds the read budget, it returns instead an outline of " +
			"section headings with sizes, the requested or first section, and the supplements by ID and title. An " +
			"unknown ID or section is refused as such and never replaced by another entry: report the ID as not " +
			"found rather than substituting a search result, or choose a heading from the outline.",
	},
	{
		Name: ToolSessions,
		Description: "List the Takt memory sessions that recorded entries, closed or open, to find a previous session to " +
			"resume or continue. Sessions matching the query come first; recency breaks ties. Each shows whether it " +
			"is closed or open, the objective and state of each close, and a bounded chronology of its entries: ID, " +
			"title, nature, author, and whether each still applies, never bodies.",
	},
	{
		Name: ToolContinueSession,
		Description: "Declare that this session continues a previous closed session, which may belong to another " +
			"workspace. The declaration stays pending until this session records its first entry, which makes it " +
			"permanent; until then, calling again replaces it and an empty previous_session_id withdraws it. Returns " +
			"the pending declaration. Refused with no effect when the named session is not closed (resume an open " +
			"session without declaring it), when this session already has an entry (continue without the link), or " +
			"when this connection cannot address the named session (the refusal names the valid targets). " + rootOnly,
	},
	{
		Name: ToolCloseSession,
		Description: "Close this session's Takt memory with its objective and current state, once the requested work is " +
			"reported or the user ends the conversation. The close covers the entries recorded since the previous " +
			"close; their chronology is derived from them when read, so the close never restates them. A later entry " +
			"reopens the session, and the next close covers only what followed. Returns the recorded close with the " +
			"range it covers. Refused, recording nothing, when nothing was recorded since the previous close; no close " +
			"is needed then. " + rootOnly,
	},
}
