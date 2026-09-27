package setup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// Impact values rank what keeping local content means for ConflictEntry.Impact.
const (
	// ImpactUnrelated means the operation does not need this file; the external change is kept without asking.
	ImpactUnrelated = "unrelated"
	// ImpactUncertain means keeping the user's version works, but Takt cannot guarantee the affected capability.
	ImpactUncertain = "uncertain"
	// ImpactIncompatible means keeping the user's version cannot work; only restoring Takt's version is viable.
	ImpactIncompatible = "incompatible"
)

// engramInjectedPaths are plan artifacts memory setup writes into after deployment.
// A missing file here cannot stay deleted because setup recreates it.
var engramInjectedPaths = map[string]bool{
	opencode.ConfigPath(): true,
}

// classifyConflict rates a conflict by what install/sync would do with the path kept as-is.
// Files memory setup recreates and unmergeable JSON are incompatible; paths the operation
// would not change are unrelated; everything else is uncertain.
func classifyConflict(conflict *ConflictEntry, artifact Artifact, entry OwnershipEntry, managed bool, current []byte) {
	conflict.Affects = capabilityFor(artifact.Path)
	switch {
	case conflict.Reason == "missing" && engramInjectedPaths[artifact.Path]:
		conflict.Impact = ImpactIncompatible
		conflict.Consequence = "Takt memory setup recreates this file, so it cannot stay deleted."
		conflict.Alternative = "Restore Takt version to recreate it."
	case artifact.Path == opencode.ConfigPath() && current != nil && !isJSONObject(current):
		conflict.Impact = ImpactIncompatible
		conflict.Consequence = "Your version is not valid JSON, so Takt memory setup would replace its content instead of merging into it."
		conflict.Alternative = "Restore Takt version, or fix the JSON in " + artifact.Path + " and run this operation again."
	case managed && hashOf(artifact.Content) == entry.SHA256:
		conflict.Impact = ImpactUnrelated
		conflict.Consequence = "This operation does not change Takt's version of this file."
	default:
		conflict.Impact = ImpactUncertain
		kept := "while your version is kept"
		if conflict.Reason == "missing" {
			kept = "while this file stays deleted"
		}
		conflict.Consequence = "Takt cannot guarantee the " + conflict.Affects + " " + kept + "."
	}
}

// isJSONObject reports whether raw survives filemerge's JSON reading as an
// object: MergeJSONObjects yields a bare "{}" for unparseable input. A
// commented-out-only JSONC object ("{ // x }") reads as invalid; harmless, the
// user is only asked to restore or fix it.
func isJSONObject(raw []byte) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true // read as an empty object: nothing to lose
	}
	merged, err := filemerge.MergeJSONObjects(raw, []byte("{}"))
	if err != nil {
		return false
	}
	if strings.TrimSpace(string(merged)) != "{}" {
		return true
	}
	var object map[string]any
	return json.Unmarshal(raw, &object) == nil
}

// hashOf returns the hex SHA-256 of content for manifest and conflict comparison.
func hashOf(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// capabilityFor names the capability a planned path defines, in user terms.
func capabilityFor(artifactPath string) string {
	harness := "OpenCode"
	switch {
	case strings.HasPrefix(artifactPath, engram.MemorySkillsDir+"/"):
		return strings.Split(artifactPath, "/")[2] + " skill"
	}
	base := path.Base(artifactPath)
	switch {
	case path.Base(path.Dir(artifactPath)) == "agents":
		return harness + " " + strings.TrimSuffix(base, path.Ext(base)) + " agent"
	case base == "opencode.json":
		return "OpenCode orchestrator agent and Takt configuration"
	}
	return harness + " " + base
}
