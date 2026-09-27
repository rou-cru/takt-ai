# Architecture & Development

← [Back to README](../README.md)

---

## Architecture

```
cmd/takt-ai/               CLI entrypoint
internal/
  app/                     Command dispatch + runtime wiring
  model/                   Domain types (agents, components, skills, presets, personas)
  catalog/                 Registry definitions (agents, skills, components)
  system/                  OS/distro detection, dependency checks, platform guards
  cli/                     Install flags, validation, orchestration, dry-run
  planner/                 Dependency graph, resolution, ordering, review payloads
  installcmd/              Profile-aware command resolver (brew/apt/pacman/dnf/winget/go install)
  pipeline/                Staged execution + rollback orchestration
  backup/                  Config snapshot + restore
  assets/                  Embedded skill files + persona templates
  components/              Per-component install/inject logic
    engram/  sdd/  skills/  mcp/  persona/  theme/  permissions/  gga/
    filemerge/             Marker-based file merging (inject without clobbering)
  agents/                  Agent adapters (config strategy per agent)
    opencode/
  opencode/                OpenCode model/config parsing utilities
  state/                   Installation state tracking
  update/                  Self-update + upgrade logic
  verify/                  Post-apply health checks + reporting
  tui/                     Bubbletea TUI (Rose Pine theme)
    styles/  screens/
scripts/                   Installer scripts (bash + PowerShell)
e2e/                       Docker-based E2E tests (Ubuntu + Arch)
testdata/                  Golden test fixtures
```

---

## Testing

```bash
# Unit tests
go test ./...

# Docker E2E (Ubuntu + Arch, requires Docker)
RUN_FULL_E2E=1 RUN_BACKUP_TESTS=1 ./e2e/docker-test.sh

# Dry-run smoke test (macOS/Linux)
takt-ai install --dry-run --agent opencode --preset minimal

# Dry-run smoke test (Windows PowerShell)
takt-ai.exe install --dry-run --agent opencode --preset minimal
```

Test coverage:

- **26 test packages** across the codebase
- **260+ test functions** covering all agent adapters, components, and system detection
- **78 E2E test functions** running in Docker containers (Ubuntu + Arch)
- **17 golden files** for snapshot testing component output
- Full pipeline tested: detection, planning, execution, backup, restore, verification
- The OpenCode agent adapter has unit tests with cross-platform path validation

---

## Relationship to Takt.Dots

| | Takt.Dots | Takt AI Stack |
|--|---------------|-----------------|
| **Purpose** | Dev environment (editors, shells, terminals) | AI development layer (agents, memory, skills) |
| **Installs** | Neovim, Fish/Zsh, Tmux/Zellij, Ghostty | Configures OpenCode |
| **Overlap** | None — complementary | None — different layer |

Install Takt.Dots first for your dev environment, then Takt AI Stack for the AI layer on top.

---

## License

MIT
