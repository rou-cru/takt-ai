package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/model"
	"gopkg.in/yaml.v2"
)

//go:embed all:assets
var assetsFS embed.FS

// AssetFS exposes the single declarative source tree, without its embedding prefix.
func AssetFS() fs.FS { f, _ := fs.Sub(assetsFS, "assets"); return f }

// ContextFiles names the agent context markdown files one agent definition references.
type ContextFiles struct {
	Persona    string `yaml:"persona"`
	Soul       string `yaml:"soul"`
	Operations string `yaml:"operations"`
}

// AgentDefinition retains references until the target adapter composes its
// prompt. VFS grants are always declared separately by exact instance ID.
type AgentDefinition struct {
	// ID is the agent's stable identifier, the key adapters compose prompts from.
	ID string `yaml:"id"`
	// Instances lists the deployable instances sharing this definition.
	Instances []string `yaml:"instances"`
	// Label is the short name every instance shows where space is scarce,
	// such as the DAG sidebar's completed row.
	Label string `yaml:"label"`
	// VFSGrants declares an explicit VFS capability list for every instance ID.
	// Grants never inherit from the definition or role.
	VFSGrants map[string][]model.VFSCapability `yaml:"vfs_capabilities"`
	// Description says when to pick this agent.
	Description string `yaml:"description"`
	// Role is the default role class instances inherit.
	Role model.RoleClass `yaml:"role"`
	// Context names the agent context markdown files composing its prompt.
	Context ContextFiles `yaml:"context"`
	// Skills lists the skill package IDs the agent may use.
	Skills []string `yaml:"skills"`
}

// VFSCapabilities returns the grants declared for this exact instance ID. The
// bool is false when the declaration is missing; no definition or role fallback
// is performed. The returned slice is independent of the catalog's map.
func (a AgentDefinition) VFSCapabilities(instanceID string) ([]model.VFSCapability, bool) {
	capabilities, ok := a.VFSGrants[instanceID]
	if !ok {
		return nil, false
	}
	return slices.Clone(capabilities), true
}

// PackageFile preserves binary bytes and an explicit deployment mode.
type PackageFile struct {
	// Path is the slash-separated path inside the catalog source tree.
	Path string
	// Content holds the file's binary bytes, preserved as authored.
	Content []byte
	// Mode is the deployment mode: 0755 only for scripts a skill declares executable.
	Mode fs.FileMode
}

// SkillPackage is one validated skill: its package ID and SKILL.md descriptor bytes.
type SkillPackage struct {
	// ID is the package identifier, also its directory name under skills/.
	ID string
	// Descriptor holds the SKILL.md frontmatter and body bytes.
	Descriptor []byte
	// Files lists every file deployed for the skill, executables marked.
	Files []PackageFile
}

// Catalog is the validated declarative content Takt deploys: agent definitions,
// skill packages, and free files shared across agents.
type Catalog struct {
	// Agents lists validated agent definitions.
	Agents []AgentDefinition
	// Skills lists validated skill packages with their files.
	Skills []SkillPackage
	// Files lists shared files deployed across agents.
	Files []PackageFile
}

var packageID = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const (
	// packageFileMode is the checked-in mode assigned to catalog files.
	packageFileMode fs.FileMode = 0o644
	// frontMatterSections is the YAML front-matter delimiter plus body split count.
	frontMatterSections = 3
)

// SkillFileName is the descriptor filename every skill package carries.
const SkillFileName = "SKILL.md"

// BaselinePath is the shared prompt prefix every agent composes on top of.
const BaselinePath = "shared/BASELINE.md"

// LoadPackages loads and validates the embedded declarative catalog.
func LoadPackages() (Catalog, error) { return LoadFS(AssetFS()) }

// LoadFS validates the entire package before any deployment can be planned;
// skill executables are declared in frontmatter, not taken from file modes.
func LoadFS(fsys fs.FS) (Catalog, error) {
	files, err := readFiles(fsys)
	if err != nil {
		return Catalog{}, err
	}
	for _, p := range []string{"README.md", BaselinePath} {
		if err := files.require(p); err != nil {
			return Catalog{}, err
		}
	}
	var c Catalog
	if c.Skills, err = loadSkills(fsys, files); err != nil {
		return Catalog{}, err
	}
	if c.Agents, err = loadAgents(fsys, files, c.Skills); err != nil {
		return Catalog{}, err
	}
	for _, p := range files.order {
		c.Files = append(c.Files, files.byPath[p])
	}
	for i := range c.Skills {
		c.Skills[i].Files = files.under("skills/" + c.Skills[i].ID + "/")
	}
	return c, nil
}

// pkgFiles is every regular file of the source tree. order is fs.WalkDir's
// lexical order, independent of map iteration or the source filesystem.
type pkgFiles struct {
	byPath map[string]PackageFile
	order  []string
}

func readFiles(fsys fs.FS) (pkgFiles, error) {
	files := pkgFiles{byPath: map[string]PackageFile{}}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("catalog file %q is not regular", p)
		}
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		files.byPath[p] = PackageFile{Path: p, Content: b, Mode: packageFileMode}
		files.order = append(files.order, p)
		return nil
	})
	return files, err
}

// require fails unless p is a valid catalog path holding non-blank content.
func (files pkgFiles) require(p string) error {
	if !fs.ValidPath(p) || strings.ContainsAny(p, `\:{}`) {
		return fmt.Errorf("invalid catalog path %q", p)
	}
	f, ok := files.byPath[p]
	if !ok || len(strings.TrimSpace(string(f.Content))) == 0 {
		return fmt.Errorf("missing or empty catalog file %q", p)
	}
	return nil
}

// under returns the files below base, with base trimmed from their paths.
func (files pkgFiles) under(base string) []PackageFile {
	var out []PackageFile
	for _, p := range files.order {
		if strings.HasPrefix(p, base) {
			f := files.byPath[p]
			f.Path = strings.TrimPrefix(p, base)
			out = append(out, f)
		}
	}
	return out
}

// markExecutables sets the executable mode on the scripts a skill declares.
func (files pkgFiles) markExecutables(base string, executables []string) error {
	for _, executable := range executables {
		if !fs.ValidPath(executable) || !strings.HasPrefix(executable, "scripts/") {
			return fmt.Errorf("invalid executable %q", executable)
		}
		if err := files.require(base + executable); err != nil {
			return err
		}
		f := files.byPath[base+executable]
		f.Mode = 0755
		files.byPath[f.Path] = f
	}
	return nil
}

type skillMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Metadata    struct {
		Executables []string `yaml:"executables"`
	} `yaml:"metadata"`
}

// parseSkillMeta reads the YAML frontmatter of the descriptor at p.
func parseSkillMeta(p string, descriptor []byte) (skillMeta, error) {
	parts := strings.SplitN(string(descriptor), "---", frontMatterSections)
	if len(parts) != 3 || strings.TrimSpace(parts[0]) != "" {
		return skillMeta{}, fmt.Errorf("missing skill frontmatter %q", p)
	}
	var meta skillMeta
	err := yaml.Unmarshal([]byte(parts[1]), &meta)
	return meta, err
}

func loadSkills(fsys fs.FS, files pkgFiles) ([]SkillPackage, error) {
	dirs, err := fs.ReadDir(fsys, "skills")
	if err != nil {
		return nil, err
	}
	var skills []SkillPackage
	for _, dir := range dirs {
		id := dir.Name()
		if !dir.IsDir() || !packageID.MatchString(id) {
			return nil, fmt.Errorf("invalid skill package %q", id)
		}
		base := "skills/" + id + "/"
		p := base + SkillFileName
		if err := files.require(p); err != nil {
			return nil, err
		}
		descriptor := files.byPath[p].Content
		meta, err := parseSkillMeta(p, descriptor)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
			return nil, fmt.Errorf("invalid skill identity %q", meta.Name)
		}
		if err := files.markExecutables(base, meta.Metadata.Executables); err != nil {
			return nil, err
		}
		skills = append(skills, SkillPackage{ID: id, Descriptor: descriptor})
	}
	return skills, nil
}

func loadAgents(fsys fs.FS, files pkgFiles, skills []SkillPackage) ([]AgentDefinition, error) {
	dirs, err := fs.ReadDir(fsys, "agents")
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, s := range skills {
		known[s.ID] = true
	}
	instances := map[string]bool{}
	var agents []AgentDefinition
	for _, dir := range dirs {
		if !dir.IsDir() {
			return nil, fmt.Errorf("unexpected agent file %q", dir.Name())
		}
		a, err := loadAgent(files, dir.Name(), known, instances)
		if err != nil {
			return nil, err
		}
		agents = append(agents, a)
	}
	return agents, nil
}

// loadAgent validates the agent in directory id. instances collects the
// instance names claimed so far, which must be unique across agents.
func loadAgent(files pkgFiles, id string, skills, instances map[string]bool) (AgentDefinition, error) {
	var a AgentDefinition
	p := "agents/" + id + "/agent.yaml"
	if err := files.require(p); err != nil {
		return a, err
	}
	if err := yaml.UnmarshalStrict(files.byPath[p].Content, &a); err != nil {
		return a, fmt.Errorf("%s: %w", p, err)
	}
	if !validAgentDefinition(a, id) {
		return a, fmt.Errorf("invalid or duplicate agent definition %q", a.ID)
	}
	if err := model.ValidateRoleClass(a.ID, a.Role); err != nil {
		return a, err
	}
	if err := claimInstances(a, instances); err != nil {
		return a, err
	}
	if err := validateVFSCapabilities(a); err != nil {
		return a, err
	}
	if err := files.requireContext(a); err != nil {
		return a, err
	}
	if err := validateSkills(a, skills); err != nil {
		return a, err
	}
	return a, nil
}

// maxAgentLabelWidth bounds an agent label so several still share one row of
// the OpenCode sidebar, whose usable width is 37 columns.
const maxAgentLabelWidth = 8

func validAgentDefinition(a AgentDefinition, id string) bool {
	return a.ID == id && packageID.MatchString(a.ID) && strings.TrimSpace(a.Description) != "" && len(a.Instances) > 0 &&
		packageID.MatchString(a.Label) && len(a.Label) <= maxAgentLabelWidth
}

func claimInstances(a AgentDefinition, instances map[string]bool) error {
	for _, instance := range a.Instances {
		if !packageID.MatchString(instance) || instances[instance] {
			return fmt.Errorf("invalid or duplicate agent instance %q", instance)
		}
		instances[instance] = true
	}
	return nil
}

func validateVFSCapabilities(a AgentDefinition) error {
	for instance, capabilities := range a.VFSGrants {
		if !slices.Contains(a.Instances, instance) {
			return fmt.Errorf("agent %q VFS capabilities declare undeclared instance %q", a.ID, instance)
		}
		if capabilities == nil {
			return fmt.Errorf("agent instance %q: VFS capabilities must be an explicit list (use [] for no VFS access)", instance)
		}
		if err := validateVFSCapabilityList(instance, capabilities); err != nil {
			return err
		}
	}
	for _, instance := range a.Instances {
		if _, ok := a.VFSCapabilities(instance); !ok {
			return fmt.Errorf("agent instance %q: missing explicit VFS capability", instance)
		}
	}
	return nil
}

// validateVFSCapabilityList rejects an unknown or duplicate capability
// within one instance's granted list.
func validateVFSCapabilityList(instance string, capabilities []model.VFSCapability) error {
	seen := make(map[model.VFSCapability]bool, len(capabilities))
	for _, capability := range capabilities {
		if !capability.Valid() {
			return fmt.Errorf("agent instance %q: unknown VFS capability %q", instance, capability)
		}
		if seen[capability] {
			return fmt.Errorf("agent instance %q: duplicate VFS capability %q", instance, capability)
		}
		seen[capability] = true
	}
	return nil
}

func validateSkills(a AgentDefinition, skills map[string]bool) error {
	for _, skill := range a.Skills {
		if !skills[skill] {
			return fmt.Errorf("agent %q references missing skill %q", a.ID, skill)
		}
	}
	return nil
}

// requireContext checks the agent's declared context files exist inside its package.
func (files pkgFiles) requireContext(a AgentDefinition) error {
	if a.Context.Operations == "" {
		return fmt.Errorf("agent %q requires operations", a.ID)
	}
	for _, name := range []string{a.Context.Persona, a.Context.Soul, a.Context.Operations} {
		if name == "" {
			continue
		}
		if !fs.ValidPath(name) {
			return fmt.Errorf("agent %q escapes its package: %q", a.ID, name)
		}
		if err := files.require("agents/" + a.ID + "/" + name); err != nil {
			return err
		}
	}
	return nil
}

// ContextPaths is the declared composition order, not an authority hierarchy.
func (a AgentDefinition) ContextPaths() []string {
	paths := []string{BaselinePath}
	for _, name := range []string{a.Context.Persona, a.Context.Soul, a.Context.Operations} {
		if name != "" {
			paths = append(paths, path.Join("agents", a.ID, name))
		}
	}
	return paths
}

// ComposeText joins this agent's context files (see ContextPaths) into one flat prompt body.
func (a AgentDefinition) ComposeText(fsys fs.FS) (string, error) {
	var parts []string
	for _, p := range a.ContextPaths() {
		b, err := fs.ReadFile(fsys, p)
		if err != nil {
			return "", fmt.Errorf("read context file %q for agent %q: %w", p, a.ID, err)
		}
		parts = append(parts, strings.TrimSpace(string(b)))
	}
	return strings.Join(parts, "\n\n"), nil
}
