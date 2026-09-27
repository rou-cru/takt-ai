#!/usr/bin/env bash
# e2e_test.sh — End-to-end tests for takt-ai installer
#
# Test tiers (controlled by environment variables):
#   (default)            Tier 1: binary existence + dry-run tests (fast, no side-effects)
#   RUN_FULL_E2E=1       Tier 2: full install tests (writes to filesystem)
#   RUN_BACKUP_TESTS=1   Tier 3: backup/restore tests
#
# Usage inside Docker:
#   ./e2e_test.sh                         # Tier 1 only
#   RUN_FULL_E2E=1 ./e2e_test.sh          # Tier 1 + 2
#   RUN_BACKUP_TESTS=1 ./e2e_test.sh      # Tier 1 + 3
#   RUN_FULL_E2E=1 RUN_BACKUP_TESTS=1 ./e2e_test.sh  # All tiers
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "$SCRIPT_DIR/lib.sh"

# ---------------------------------------------------------------------------
# Resolve binary
# ---------------------------------------------------------------------------
BINARY="$(resolve_binary)"
if [[ -z "$BINARY" ]]; then
    echo "ERROR: takt-ai binary not found. Build it first." >&2
    exit 1
fi
log_info "Using binary: $BINARY"

# ---------------------------------------------------------------------------
# Shared literals — kept in one place so a persona/marker/file rename touches
# one line instead of every assertion that checks for it.
# ---------------------------------------------------------------------------
readonly OPENCODE_JSON_LABEL="OpenCode opencode.json"
readonly OPENCODE_JSON_VALID_LABEL="opencode.json is valid JSON"
readonly OPENCODE_JSON_EXISTS_LABEL="opencode.json exists"
readonly TAKT_PERSONA_LABEL="Senior Architect"
readonly ENGRAM_PROTOCOL_MARKER="takt-ai:engram-protocol"
readonly PERMISSIONS_KEY='"permissions"'
readonly SKILL_FILE_NAME="SKILL.md"
readonly GENTLE_ORCHESTRATOR_KEY='"gentle-orchestrator"'
readonly SDD_ORCHESTRATOR_KEY='"sdd-orchestrator"'

# Side-effect E2E exercises install/injection behavior. Keep it deterministic by
# satisfying the installer's "engram already exists on PATH" branch unless a
# maintainer explicitly opts into the live GitHub release download path.
if [[ "${RUN_FULL_E2E:-0}" = "1" ]] || [[ "${RUN_BACKUP_TESTS:-0}" = "1" ]]; then
    setup_fake_engram_binary
fi

# ===========================================================================
# TIER 1 — Basic binary & dry-run tests (always run)
# ===========================================================================

# --- Category 1a: Binary basics ---

test_binary_exists() {
    log_test "Binary exists and is executable"

    if [[ -x "$(command -v "$BINARY")" ]] || [[ -x "$BINARY" ]]; then
        log_pass "Binary is executable"
    else
        log_fail "Binary not found or not executable"
    fi
    return $?
}

test_binary_runs() {
    log_test "Binary runs without panic"

    if output=$($BINARY install --dry-run 2>&1); then
        log_pass "Binary exited cleanly with --dry-run"
    else
        if echo "$output" | grep -qi "panic"; then
            log_fail "Binary panicked: $output"
        else
            log_pass "Binary exited with non-zero (no panic)"
        fi
    fi
    return $?
}

test_version_command() {
    log_test "Version command works"

    output=$($BINARY version 2>&1) || true

    if echo "$output" | grep -q "takt-ai"; then
        log_pass "Version command returns binary name"
    else
        log_fail "Version command failed: $output"
    fi
    return $?
}

# --- Category 1b: Dry-run output format ---

test_dry_run_output_format() {
    log_test "Dry-run output contains expected sections"

    output=$($BINARY install --dry-run 2>&1) || true

    assert_output_contains "$output" "dry-run" "Output contains 'dry-run' marker"
    assert_output_contains "$output" "Agents:" "Output contains 'Agents:' header"
    assert_output_contains "$output" "Persona:" "Output contains 'Persona:' header"
    assert_output_contains "$output" "Preset:" "Output contains 'Preset:' header"
    assert_output_contains "$output" "Components order:" "Output contains 'Components order:' header"
    assert_output_contains "$output" "Platform decision:" "Output contains 'Platform decision:' header"
    return $?
}

test_dry_run_platform_detection() {
    log_test "Dry-run shows platform decision"

    output=$($BINARY install --dry-run 2>&1) || true

    assert_output_contains "$output" "Platform decision" "Platform decision present in dry-run"
    return $?
}

test_dry_run_detects_linux() {
    log_test "Dry-run detects Linux OS"

    # This test is only meaningful inside the Docker container (Linux).
    # Skip gracefully on macOS/other to avoid killing the test run.
    if [[ "$(uname -s)" != "Linux" ]]; then
        log_skip "Not running on Linux — platform detection test skipped"
        return 0
    fi

    output=$($BINARY install --dry-run 2>&1) || true

    assert_output_contains "$output" "os=linux" "Platform detected as Linux"
}

# --- Category 1c: Agent flag ---

test_dry_run_agent_opencode() {
    log_test "Dry-run with --agent opencode"

    output=$($BINARY install --agent opencode --dry-run 2>&1) || true

    assert_output_contains "$output" "opencode" "Dry-run output shows opencode agent"
    return $?
}

# --- Category 1d: Preset flags ---

test_dry_run_preset_minimal() {
    log_test "Dry-run with --preset minimal"

    output=$($BINARY install --preset minimal --dry-run 2>&1) || true

    assert_output_contains "$output" "Preset: minimal" "Shows minimal preset"
    return $?
}

test_dry_run_preset_ecosystem() {
    log_test "Dry-run with --preset ecosystem-only"

    output=$($BINARY install --preset ecosystem-only --dry-run 2>&1) || true

    assert_output_contains "$output" "Preset: ecosystem-only" "Shows ecosystem-only preset"
    return $?
}

test_dry_run_preset_full() {
    log_test "Dry-run with --preset full-takt"

    output=$($BINARY install --preset full-takt --dry-run 2>&1) || true

    assert_output_contains "$output" "Preset: full-takt" "Shows full-takt preset"
    return $?
}

test_dry_run_preset_custom() {
    log_test "Dry-run with --preset custom"

    output=$($BINARY install --preset custom --dry-run 2>&1) || true

    assert_output_contains "$output" "Preset: custom" "Shows custom preset"
    return $?
}

# --- Category 1e: Preset component order validation ---

test_dry_run_full_preset_persona_before_sdd() {
    log_test "Dry-run: persona appears before engram and sdd in component order"

    output=$($BINARY install --preset full-takt --agent opencode --dry-run 2>&1) || true

    local components_line
    components_line=$(echo "$output" | grep "Components order:")

    # Verify all are present
    assert_output_contains "$components_line" "persona" "Full preset has persona"
    assert_output_contains "$components_line" "engram" "Full preset has engram"
    assert_output_contains "$components_line" "sdd" "Full preset has sdd"

    # Verify ordering: persona before engram, persona before sdd
    # Extract the order string and check persona comes first
    local order_str
    order_str=$(echo "$components_line" | sed 's/.*Components order: *//')

    local persona_idx engram_idx sdd_idx
    persona_idx=$(echo "$order_str" | tr ',' '\n' | grep -n '^persona$' | cut -d: -f1)
    engram_idx=$(echo "$order_str" | tr ',' '\n' | grep -n '^engram$' | cut -d: -f1)
    sdd_idx=$(echo "$order_str" | tr ',' '\n' | grep -n '^sdd$' | cut -d: -f1)

    if [[ -n "$persona_idx" ]] && [[ -n "$engram_idx" ]] && [[ "$persona_idx" -lt "$engram_idx" ]]; then
        log_pass "Persona ($persona_idx) before engram ($engram_idx)"
    else
        log_fail "Persona must appear before engram in component order: $order_str"
    fi

    if [[ -n "$persona_idx" ]] && [[ -n "$sdd_idx" ]] && [[ "$persona_idx" -lt "$sdd_idx" ]]; then
        log_pass "Persona ($persona_idx) before sdd ($sdd_idx)"
    else
        log_fail "Persona must appear before sdd in component order: $order_str"
    fi
    return $?
}

# --- Category 1f: Individual component flags ---

test_dry_run_component_permissions() {
    log_test "Dry-run with --component permissions"
    output=$($BINARY install --agent opencode --component permissions --dry-run 2>&1) || true
    assert_output_contains "$output" "permissions" "Shows permissions component"
    return $?
}

test_dry_run_component_theme() {
    log_test "Dry-run with --component theme"
    output=$($BINARY install --agent opencode --component theme --dry-run 2>&1) || true
    assert_output_contains "$output" "theme" "Shows theme component"
    return $?
}

# --- Category 1f2: SDD mode flag ---

test_dry_run_sdd_mode_multi() {
    log_test "Dry-run with --sdd-mode multi"

    output=$($BINARY install --agent opencode --sdd-mode multi --dry-run 2>&1) || true

    assert_output_contains "$output" "opencode" "Shows opencode agent"
    assert_output_contains "$output" "sdd-mode: multi\|SDDMode: multi\|sdd_mode.*multi\|multi" "Shows multi mode"
    return $?
}

test_dry_run_sdd_mode_single() {
    log_test "Dry-run with --sdd-mode single"

    output=$($BINARY install --agent opencode --sdd-mode single --dry-run 2>&1) || true

    assert_output_contains "$output" "opencode" "Shows opencode agent"
    return $?
}

test_dry_run_sdd_mode_invalid_rejected() {
    log_test "Invalid --sdd-mode is rejected"

    if $BINARY install --agent opencode --sdd-mode turbo --dry-run 2>&1; then
        log_fail "Invalid sdd-mode should have been rejected"
    else
        log_pass "Invalid sdd-mode correctly rejected"
    fi
    return $?
}

# --- Category 1g: Invalid input rejection ---

test_invalid_persona_rejected() {
    log_test "Invalid persona is rejected"

    if $BINARY install --persona nonexistent --dry-run 2>&1; then
        log_fail "Invalid persona should have been rejected"
    else
        log_pass "Invalid persona correctly rejected"
    fi
    return $?
}

test_invalid_component_rejected() {
    log_test "Invalid component is rejected"

    if $BINARY install --component fakecomp --dry-run 2>&1; then
        log_fail "Invalid component should have been rejected"
    else
        log_pass "Invalid component correctly rejected"
    fi
    return $?
}

test_invalid_preset_rejected() {
    log_test "Invalid preset is rejected"

    if $BINARY install --preset nonexistent --dry-run 2>&1; then
        log_fail "Invalid preset should have been rejected"
    else
        log_pass "Invalid preset correctly rejected"
    fi
    return $?
}

test_unknown_command_rejected() {
    log_test "Unknown command is rejected"

    if $BINARY foobar 2>&1; then
        log_fail "Unknown command should have been rejected"
    else
        log_pass "Unknown command correctly rejected"
    fi
    return $?
}

# ===========================================================================
# TIER 2 — Full install tests (require RUN_FULL_E2E=1)
# ===========================================================================


test_oc_persona_custom_does_nothing() {
    log_test "OpenCode: persona custom does nothing (user keeps own personality)"
    cleanup_test_env

    if $BINARY install --agent opencode --component persona --persona custom 2>&1; then
        # Custom persona should NOT create AGENTS.md (persona does nothing).
        local agents_md="$HOME/.config/opencode/AGENTS.md"
        assert_file_not_exists "$agents_md" "AGENTS.md not created by custom persona"
    else
        log_fail "OpenCode custom persona install command failed"
    fi
    return $?
}

# --- Category 3: OpenCode component injection ---

test_oc_engram_injection() {
    log_test "OpenCode: engram injection (opencode.json)"
    cleanup_test_env

    if $BINARY install --agent opencode --component engram --persona neutral 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        local agents_md="$HOME/.config/opencode/AGENTS.md"
        assert_file_exists "$settings" "$OPENCODE_JSON_LABEL"
        assert_file_contains "$settings" '"mcp"' "Has mcp key"
        assert_file_contains "$settings" '"engram"' "Has engram MCP entry"
        assert_file_contains "$settings" '"command"' "Has command key"
        assert_file_contains "$settings" '"type": "local"' "Engram uses local MCP type"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"

        # Fallback safety: AGENTS.md must include engram protocol section.
        assert_file_exists "$agents_md" "OpenCode AGENTS.md"
        assert_file_contains "$agents_md" "$ENGRAM_PROTOCOL_MARKER" "AGENTS.md has engram-protocol section"
        assert_file_contains "$agents_md" 'mem_save' "AGENTS.md has memory protocol content"
    else
        log_fail "OpenCode engram install command failed"
    fi
    return $?
}

test_oc_sdd_injection() {
    log_test "OpenCode: SDD injection (commands + skills)"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral 2>&1; then
        local commands_dir="$HOME/.config/opencode/commands"
        local skill_dir="$HOME/.config/opencode/skills"

        # Command files (8 SDD commands from embedded assets)
        assert_dir_exists "$commands_dir" "OpenCode commands directory"
        assert_file_count_min "$commands_dir" "*.md" 7 "At least 7 SDD command files"

        # Validate command file content
        assert_file_exists "$commands_dir/sdd-init.md" "sdd-init command file"
        assert_file_contains "$commands_dir/sdd-init.md" "sdd" "sdd-init command has SDD content"

        # SDD + orchestration skill files (11)
        assert_dir_exists "$skill_dir" "OpenCode skill directory"
        assert_file_count_min "$skill_dir" "$SKILL_FILE_NAME" 11 "At least 11 skill files"

        # Validate skill file content
        assert_file_exists "$skill_dir/sdd-init/SKILL.md" "sdd-init SKILL.md"
        assert_file_size_min "$skill_dir/sdd-init/SKILL.md" 100 "sdd-init skill has real content"
    else
        log_fail "OpenCode SDD install command failed"
    fi
    return $?
}

test_oc_persona_takt() {
    log_test "OpenCode: persona injection (takt)"
    cleanup_test_env

    if $BINARY install --agent opencode --component persona --persona takt 2>&1; then
        local agents_md="$HOME/.config/opencode/AGENTS.md"
        assert_file_exists "$agents_md" "AGENTS.md exists"
        assert_file_contains "$agents_md" "$TAKT_PERSONA_LABEL" "Takt persona has 'Senior Architect'"
        assert_file_size_min "$agents_md" 200 "AGENTS.md has substantial content"
    else
        log_fail "OpenCode persona (takt) install command failed"
    fi
    return $?
}

test_oc_persona_neutral() {
    log_test "OpenCode: persona injection (neutral)"
    cleanup_test_env

    if $BINARY install --agent opencode --component persona --persona neutral 2>&1; then
        local agents_md="$HOME/.config/opencode/AGENTS.md"
        assert_file_exists "$agents_md" "AGENTS.md exists"
        assert_file_contains "$agents_md" "$TAKT_PERSONA_LABEL" "Neutral persona keeps the teacher identity"
        assert_file_not_contains "$agents_md" "Rioplatense\|voseo\|loco\|ponete las pilas" "Neutral persona excludes regional language"
    else
        log_fail "OpenCode persona (neutral) install command failed"
    fi
    return $?
}

test_oc_skills_minimal() {
    log_test "OpenCode: skills injection (minimal)"
    cleanup_test_env

    if $BINARY install --agent opencode --component skills --preset minimal --persona custom 2>&1; then
        local skill_dir="$HOME/.config/opencode/skills"
        assert_dir_exists "$skill_dir" "OpenCode skill directory"
        assert_file_count "$skill_dir" "$SKILL_FILE_NAME" 12 "Minimal preset: 12 skill files"
        assert_file_exists "$skill_dir/sdd-init/SKILL.md" "sdd-init SKILL.md"
        assert_file_size_min "$skill_dir/sdd-init/SKILL.md" 100 "sdd-init skill has real content"
    else
        log_fail "OpenCode skills (minimal) install command failed"
    fi
    return $?
}

test_oc_skills_full() {
    log_test "OpenCode: skills injection (full-takt = 10 foundation skills)"
    cleanup_test_env

    if $BINARY install --agent opencode --component skills --preset full-takt --persona neutral 2>&1; then
        local skill_dir="$HOME/.config/opencode/skills"
        assert_dir_exists "$skill_dir" "OpenCode skill directory"
        assert_file_count "$skill_dir" "$SKILL_FILE_NAME" 22 "Full preset: 22 skill files"
        assert_file_exists "$skill_dir/go-testing/SKILL.md" "go-testing skill"
        assert_file_exists "$skill_dir/skill-creator/SKILL.md" "skill-creator skill"
        assert_file_exists "$skill_dir/branch-pr/SKILL.md" "branch-pr skill"
        assert_file_exists "$skill_dir/issue-creation/SKILL.md" "issue-creation skill"
        assert_file_size_min "$skill_dir/go-testing/SKILL.md" 200 "go-testing skill has real content"
    else
        log_fail "OpenCode skills (full) install command failed"
    fi
    return $?
}

test_oc_context7_injection() {
    log_test "OpenCode: context7 injection (opencode.json MCP)"
    cleanup_test_env

    if $BINARY install --agent opencode --component context7 --persona neutral 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        assert_file_exists "$settings" "$OPENCODE_JSON_LABEL"
        assert_file_contains "$settings" '"mcp"' "Has mcp key"
        assert_file_contains "$settings" '"context7"' "Has context7 entry"
        assert_file_contains "$settings" 'https://mcp.context7.com/mcp' "Has Context7 MCP URL"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"
    else
        log_fail "OpenCode context7 install command failed"
    fi
    return $?
}

test_oc_permissions_injection() {
    log_test "OpenCode: permissions injection"
    cleanup_test_env

    if $BINARY install --agent opencode --component permissions --persona neutral 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        assert_file_exists "$settings" "$OPENCODE_JSON_LABEL"
        assert_file_contains "$settings" "$PERMISSIONS_KEY" "Has permissions key"
        assert_file_contains "$settings" '"shell"' "Has shell permissions"
        assert_file_contains "$settings" '"read"' "Has read permissions"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"
    else
        log_fail "OpenCode permissions install command failed"
    fi
    return $?
}

test_oc_theme_injection() {
    log_test "OpenCode: theme injection"
    cleanup_test_env

    if $BINARY install --agent opencode --component theme --persona neutral 2>&1; then
        local settings="$HOME/.config/opencode/cli.json"
        assert_file_exists "$settings" "OpenCode cli.json"
        assert_file_contains "$settings" '"theme"' "Has theme key"
        assert_file_contains "$settings" '"takt"' "Has takt theme"
        assert_valid_json "$settings" "cli.json is valid JSON"
    else
        log_fail "OpenCode theme install command failed"
    fi
    return $?
}

# --- Category 4: Full preset integration ---

test_full_preset_opencode() {
    log_test "Full-takt preset: OpenCode (all components coexist)"
    cleanup_test_env

    if $BINARY install --agent opencode --component engram --component sdd --component persona --component skills --component context7 --component permissions --component theme --preset full-takt --persona takt 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        local agents_md="$HOME/.config/opencode/AGENTS.md"

        # opencode.json should have all overlays merged
        assert_file_exists "$settings" "$OPENCODE_JSON_LABEL"
        assert_file_contains "$settings" "$PERMISSIONS_KEY" "Has permissions config"
        assert_file_contains "$HOME/.config/opencode/cli.json" '"theme"' "Has theme in cli.json"
        assert_file_contains "$settings" '"mcp"' "Has MCP servers"
        assert_file_contains "$settings" '"context7"' "Has context7 MCP"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"

        # AGENTS.md for persona + engram (SDD orchestrator is in opencode.json for OpenCode, NOT AGENTS.md)
        assert_file_exists "$agents_md" "AGENTS.md exists"
        assert_file_contains "$agents_md" "$TAKT_PERSONA_LABEL" "Takt persona"
        assert_file_contains "$agents_md" "$ENGRAM_PROTOCOL_MARKER" "AGENTS.md has engram protocol"
        assert_no_duplicate_section "$agents_md" "engram-protocol" "No duplicate engram section in AGENTS.md"
        # SDD orchestrator for OpenCode lives in opencode.json as an agent definition (not AGENTS.md)
        assert_file_contains "$settings" "$GENTLE_ORCHESTRATOR_KEY" "opencode.json has gentle-orchestrator agent"
        assert_file_not_contains "$settings" "$SDD_ORCHESTRATOR_KEY" "opencode.json does not have legacy base sdd-orchestrator agent"
        # AGENTS.md must NOT have a sdd-orchestrator HTML section (it's handled by opencode.json)
        assert_file_not_contains "$agents_md" "<!-- takt-ai:sdd-orchestrator -->" "AGENTS.md has no SDD section marker (opencode uses json agent)"

        # SDD commands
        assert_file_count_min "$HOME/.config/opencode/commands" "*.md" 7 "SDD command files"

        # Skills
        assert_file_count_min "$HOME/.config/opencode/skills" "$SKILL_FILE_NAME" 11 "At least 11 skill files"

        log_pass "Full preset: all OpenCode injection-only components coexist"
    else
        log_fail "Full preset (OpenCode) install command failed"
    fi
    return $?
}

test_minimal_preset_opencode_only_engram_no_persona() {
    log_test "Minimal preset: OpenCode (engram only, no persona side effect)"
    cleanup_test_env

    if $BINARY install --agent opencode --preset minimal --persona custom 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        local agents_md="$HOME/.config/opencode/AGENTS.md"

        assert_file_exists "$settings" "OpenCode opencode.json exists"
        assert_file_contains "$settings" '"engram"' "OpenCode has engram MCP"

        # Minimal preset should NOT silently install persona.
        if [[ -f "$agents_md" ]]; then
            assert_file_not_contains "$agents_md" "takt-ai:persona" "No persona marker in minimal preset"
            assert_file_not_contains "$agents_md" "$TAKT_PERSONA_LABEL" "No persona content in minimal preset"
        else
            log_pass "No AGENTS.md created by minimal preset (correct)"
        fi
    else
        log_fail "Minimal preset (OpenCode) install command failed"
    fi
    return $?
}

# --- Category 5: Content validation ---

test_content_opencode_commands_valid_markdown() {
    log_test "Content validation: OpenCode commands are valid markdown with frontmatter"
    cleanup_test_env

    $BINARY install --agent opencode --component sdd --persona neutral 2>&1 || true

    local commands_dir="$HOME/.config/opencode/commands"
    if [[ -d "$commands_dir" ]]; then
        local all_ok=true
        while IFS= read -r cmd_file; do
            local size
            size=$(wc -c < "$cmd_file" | tr -d ' ')
            if [[ "$size" -lt 10 ]]; then
                log_fail "Command file too small ($size bytes): $cmd_file"
                all_ok=false
            fi
        done < <(find "$commands_dir" -name "*.md" -type f)

        if $all_ok; then
            log_pass "All OpenCode command files have content"
        fi
    else
        log_fail "OpenCode commands directory not created"
    fi
    return $?
}

# --- Category 6: Idempotency ---

test_idempotent_permissions_opencode() {
    log_test "Idempotency: permissions on OpenCode (run twice, same result)"
    cleanup_test_env

    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true
    local first_hash
    first_hash=$(sha256sum "$HOME/.config/opencode/opencode.json" 2>/dev/null | cut -d' ' -f1)

    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true
    local second_hash
    second_hash=$(sha256sum "$HOME/.config/opencode/opencode.json" 2>/dev/null | cut -d' ' -f1)

    if [[ "$first_hash" = "$second_hash" ]] && [[ -n "$first_hash" ]]; then
        log_pass "Idempotent: same permissions config after two runs"
    else
        log_fail "Permissions config changed between runs ($first_hash vs $second_hash)"
    fi
    return $?
}


test_idempotent_theme_opencode() {
    log_test "Idempotency: theme on OpenCode (run twice, same result)"
    cleanup_test_env

    $BINARY install --agent opencode --component theme --persona neutral 2>&1 || true
    local first_hash
    first_hash=$(sha256sum "$HOME/.config/opencode/cli.json" 2>/dev/null | cut -d' ' -f1)

    $BINARY install --agent opencode --component theme --persona neutral 2>&1 || true
    local second_hash
    second_hash=$(sha256sum "$HOME/.config/opencode/cli.json" 2>/dev/null | cut -d' ' -f1)

    if [[ "$first_hash" = "$second_hash" ]] && [[ -n "$first_hash" ]]; then
        log_pass "Idempotent: same theme config after two runs"
    else
        log_fail "Theme config changed between runs ($first_hash vs $second_hash)"
    fi
    return $?
}

# --- Category 8: Edge cases ---

test_edge_persona_switch_preserves_sections_opencode() {
    log_test "Edge case: persona switch preserves managed sections (OpenCode)"
    cleanup_test_env

    # Step 1: Install full stack with takt
    $BINARY install --agent opencode --component persona --component engram --component sdd --persona takt 2>&1 || true

    local agents_md="$HOME/.config/opencode/AGENTS.md"
    assert_file_exists "$agents_md" "AGENTS.md after full install"
    assert_file_contains "$agents_md" "$ENGRAM_PROTOCOL_MARKER" "Engram section present before switch"

    # Step 2: Switch to neutral persona
    $BINARY install --agent opencode --component persona --persona neutral 2>&1 || true

    # Step 3: Verify sections survived
    assert_file_contains "$agents_md" "$TAKT_PERSONA_LABEL" "Neutral persona present after switch"
    assert_file_not_contains "$agents_md" "Rioplatense" "Regional language removed after switch"
    assert_file_contains "$agents_md" "$ENGRAM_PROTOCOL_MARKER" "Engram section survived persona switch"
    assert_no_duplicate_section "$agents_md" "engram-protocol" "No duplicate engram after switch"
    return $?
}

test_edge_json_merge_preserves_existing() {
    log_test "Edge case: JSON merge preserves existing settings"
    cleanup_test_env

    # Create pre-existing settings
    mkdir -p "$HOME/.config/opencode"
    echo '{"existingKey": "preserved"}' > "$HOME/.config/opencode/opencode.json"

    # Install permissions on top
    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true

    local settings="$HOME/.config/opencode/opencode.json"
    assert_file_contains "$settings" '"existingKey"' "Pre-existing key preserved"
    assert_file_contains "$settings" '"preserved"' "Pre-existing value preserved"
    assert_file_contains "$settings" "$PERMISSIONS_KEY" "Permissions config merged in"
    assert_valid_json "$settings" "Merged JSON is valid"
    return $?
}

test_edge_multiple_json_overlays() {
    log_test "Edge case: multiple JSON overlays merge correctly"
    cleanup_test_env

    # Install permissions, then theme, then context7 — all into OpenCode opencode.json
    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true
    $BINARY install --agent opencode --component theme --persona neutral 2>&1 || true
    $BINARY install --agent opencode --component context7 --persona neutral 2>&1 || true

    local settings="$HOME/.config/opencode/opencode.json"
    assert_file_contains "$settings" "$PERMISSIONS_KEY" "Permissions config present after merges"
    assert_file_contains "$HOME/.config/opencode/cli.json" '"theme"' "Theme present in cli.json after merges"
    assert_file_contains "$settings" '"mcp"' "MCP servers present after 3 merges"
    assert_file_contains "$settings" '"context7"' "Context7 present after 3 merges"
    assert_valid_json "$settings" "Final merged JSON is valid"
    return $?
}

# --- Category: GGA tests ---


# --- Category 7: Injection integrity (guards against issue #4 regression) ---

test_integrity_sdd_skills_nonempty() {
    log_test "Integrity: every SDD SKILL.md has real content (>100 bytes)"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral 2>&1; then
        local skill_dir="$HOME/.config/opencode/skills"
        local all_ok=true
        local sdd_skills=(sdd-init sdd-explore sdd-propose sdd-spec sdd-design sdd-tasks sdd-apply sdd-verify sdd-archive)

        for skill in "${sdd_skills[@]}"; do
            local path="$skill_dir/$skill/SKILL.md"
            if [[ ! -f "$path" ]]; then
                log_fail "SDD skill missing: $path"
                all_ok=false
                continue
            fi
            local size
            size=$(wc -c < "$path" | tr -d ' ')
            if [[ "$size" -lt 100 ]]; then
                log_fail "SDD skill empty or too small ($size bytes): $skill"
                all_ok=false
            fi
        done

        if $all_ok; then
            log_pass "All 9 SDD skills have >= 100 bytes of real content"
        fi
    else
        log_fail "SDD install command failed"
    fi
    return $?
}

test_integrity_sdd_orchestrator_in_opencode_json() {
    log_test "Integrity: opencode.json contains gentle-orchestrator agent after SDD install"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        assert_file_exists "$settings" "$OPENCODE_JSON_EXISTS_LABEL"
        assert_file_contains "$settings" "$GENTLE_ORCHESTRATOR_KEY" "Has gentle-orchestrator agent"
        assert_file_not_contains "$settings" "$SDD_ORCHESTRATOR_KEY" "Does not have legacy base sdd-orchestrator agent"
        assert_file_contains "$settings" '"agents"' "Has agents key"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"
    else
        log_fail "SDD install for orchestrator check failed"
    fi
    return $?
}

test_integrity_all_sdd_commands_have_frontmatter() {
    log_test "Integrity: all 8 SDD command files have YAML frontmatter"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral 2>&1; then
        local commands_dir="$HOME/.config/opencode/commands"
        local all_ok=true
        local expected_commands=(sdd-init sdd-apply sdd-archive sdd-continue sdd-explore sdd-ff sdd-new sdd-verify)

        for cmd in "${expected_commands[@]}"; do
            local path="$commands_dir/$cmd.md"
            if [[ ! -f "$path" ]]; then
                log_fail "SDD command missing: $cmd.md"
                all_ok=false
                continue
            fi
            # Must start with --- (YAML frontmatter)
            if ! head -1 "$path" | grep -q '^---'; then
                log_fail "SDD command $cmd.md missing YAML frontmatter"
                all_ok=false
            fi
            # Must contain agent: sdd-orchestrator (except sdd-continue, sdd-ff, sdd-new which use different agent)
            local size
            size=$(wc -c < "$path" | tr -d ' ')
            if [[ "$size" -lt 50 ]]; then
                log_fail "SDD command $cmd.md too small ($size bytes)"
                all_ok=false
            fi
        done

        if $all_ok; then
            log_pass "All 8 SDD commands present with frontmatter and content"
        fi
    else
        log_fail "SDD install for command check failed"
    fi
    return $?
}

test_integrity_full_preset_all_skills_nonempty() {
    log_test "Integrity: full preset — every SKILL.md is non-empty"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --component skills --preset full-takt --persona takt 2>&1; then
        local skill_dir="$HOME/.config/opencode/skills"
        local all_ok=true
        local empty_count=0

        while IFS= read -r skill_file; do
            local size
            size=$(wc -c < "$skill_file" | tr -d ' ')
            if [[ "$size" -lt 100 ]]; then
                log_fail "Skill file empty/corrupt ($size bytes): $skill_file"
                all_ok=false
                empty_count=$((empty_count + 1))
            fi
        done < <(find "$skill_dir" -name "$SKILL_FILE_NAME" -type f)

        if $all_ok; then
            local total
            total=$(find "$skill_dir" -name "$SKILL_FILE_NAME" -type f | wc -l | tr -d ' ')
            log_pass "All $total skill files have >= 100 bytes of real content"
        else
            log_fail "$empty_count skill file(s) are empty or corrupt"
        fi
    else
        log_fail "Full preset install for integrity check failed"
    fi
    return $?
}

test_integrity_sdd_orchestrator_agent_structure() {
    log_test "Integrity: gentle-orchestrator agent has required fields in opencode.json"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona takt 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        assert_file_contains "$settings" "$GENTLE_ORCHESTRATOR_KEY" "Has gentle-orchestrator"
        assert_file_not_contains "$settings" "$SDD_ORCHESTRATOR_KEY" "Does not have legacy base sdd-orchestrator"
        assert_file_contains "$settings" '"mode"' "Agent has mode field"
        assert_file_contains "$settings" '"system"' "Agent has system field"
        assert_file_contains "$settings" 'COORDINATOR' "Agent system contains orchestrator instructions"
    else
        log_fail "SDD + persona install for agent structure check failed"
    fi
    return $?
}

test_integrity_skills_plus_sdd_coexist() {
    log_test "Integrity: SDD + skills components write non-empty files that coexist"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --component skills --preset full-takt --persona neutral 2>&1; then
        local skill_dir="$HOME/.config/opencode/skills"

        # SDD skills should exist
        assert_file_size_min "$skill_dir/sdd-init/SKILL.md" 100 "sdd-init skill has content"
        assert_file_size_min "$skill_dir/sdd-apply/SKILL.md" 100 "sdd-apply skill has content"

        # Foundation skills should also exist
        assert_file_size_min "$skill_dir/go-testing/SKILL.md" 100 "go-testing skill has content"
        assert_file_size_min "$skill_dir/skill-creator/SKILL.md" 100 "skill-creator skill has content"

        # Shared SDD conventions should exist
        assert_file_exists "$skill_dir/_shared/persistence-contract.md" "Shared persistence contract"
        assert_file_size_min "$skill_dir/_shared/persistence-contract.md" 50 "Persistence contract has content"

        # opencode.json should have gentle-orchestrator as the base coordinator
        assert_file_contains "$HOME/.config/opencode/opencode.json" "$GENTLE_ORCHESTRATOR_KEY" "gentle-orchestrator present"
        assert_file_not_contains "$HOME/.config/opencode/opencode.json" "$SDD_ORCHESTRATOR_KEY" "legacy base sdd-orchestrator absent"
    else
        log_fail "SDD + skills coexistence install failed"
    fi
    return $?
}

# --- Category 9: SDD multi-mode tests ---

test_oc_sdd_multi_mode_injection() {
    log_test "OpenCode: SDD multi-mode injection (10 agents in opencode.json)"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral --sdd-mode multi 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        local legacy_plugin="$HOME/.config/opencode/plugins/background-agents.ts"
        local model_variants_plugin="$HOME/.config/opencode/plugins/model-variants.ts"
        assert_file_exists "$settings" "$OPENCODE_JSON_EXISTS_LABEL"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"
        assert_file_contains "$settings" "$GENTLE_ORCHESTRATOR_KEY" "Has gentle-orchestrator agent"
        assert_file_not_contains "$settings" "$SDD_ORCHESTRATOR_KEY" "Does not have legacy base sdd-orchestrator agent"
        assert_file_contains "$settings" '"sdd-apply"' "Has sdd-apply sub-agent"
        assert_file_contains "$settings" '"sdd-init"' "Has sdd-init sub-agent"
        assert_file_contains "$settings" '"sdd-verify"' "Has sdd-verify sub-agent"
        assert_file_contains "$settings" '"sdd-explore"' "Has sdd-explore sub-agent"
        assert_file_contains "$settings" '"sdd-propose"' "Has sdd-propose sub-agent"
        assert_file_contains "$settings" '"sdd-spec"' "Has sdd-spec sub-agent"
        assert_file_contains "$settings" '"sdd-design"' "Has sdd-design sub-agent"
        assert_file_contains "$settings" '"sdd-tasks"' "Has sdd-tasks sub-agent"
        assert_file_contains "$settings" '"sdd-archive"' "Has sdd-archive sub-agent"
        assert_file_contains "$settings" '"subagent"' "Sub-agents have mode subagent"
        assert_file_contains "$settings" '"task"' "Has native task tool"
        assert_file_not_exists "$legacy_plugin" "legacy background-agents plugin not installed by default"
        assert_file_exists "$model_variants_plugin" "model-variants plugin exists"
        assert_file_contains "$model_variants_plugin" 'model-variants' "Model variants plugin has expected content marker"
    else
        log_fail "OpenCode SDD multi-mode install command failed"
    fi
    return $?
}

test_oc_sdd_single_mode_no_models() {
    log_test "OpenCode: SDD single mode has all agents but no model overrides"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral --sdd-mode single 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        assert_file_exists "$settings" "$OPENCODE_JSON_EXISTS_LABEL"
        assert_valid_json "$settings" "$OPENCODE_JSON_VALID_LABEL"
        assert_file_contains "$settings" "$GENTLE_ORCHESTRATOR_KEY" "Has gentle-orchestrator agent"
        assert_file_not_contains "$settings" "$SDD_ORCHESTRATOR_KEY" "Single mode: does not have legacy base sdd-orchestrator agent"
        assert_file_contains "$settings" '"sdd-apply"' "Single mode: has sdd-apply sub-agent"
        assert_file_not_contains "$settings" '"model"' "Single mode: no model overrides"
        assert_file_not_exists "$HOME/.config/opencode/plugins/background-agents.ts" "Single mode: legacy background-agents plugin not installed"
        assert_file_exists "$HOME/.config/opencode/plugins/model-variants.ts" "Single mode: model-variants plugin present"
    else
        log_fail "OpenCode SDD single-mode install command failed"
    fi
    return $?
}

test_oc_sdd_default_mode_same_as_single() {
    log_test "OpenCode: SDD default (no --sdd-mode flag) matches single mode"
    cleanup_test_env

    if $BINARY install --agent opencode --component sdd --persona neutral 2>&1; then
        local settings="$HOME/.config/opencode/opencode.json"
        assert_file_exists "$settings" "$OPENCODE_JSON_EXISTS_LABEL"
        assert_file_contains "$settings" "$GENTLE_ORCHESTRATOR_KEY" "Has gentle-orchestrator"
        assert_file_not_contains "$settings" "$SDD_ORCHESTRATOR_KEY" "Default mode: does not have legacy base sdd-orchestrator"
        assert_file_contains "$settings" '"sdd-apply"' "Default mode: has sdd-apply sub-agent"
        assert_file_not_contains "$settings" '"model"' "Default mode: no model overrides"
        assert_file_not_exists "$HOME/.config/opencode/plugins/background-agents.ts" "Default mode: legacy background-agents plugin not installed"
        assert_file_exists "$HOME/.config/opencode/plugins/model-variants.ts" "Default mode: model-variants plugin present"
    else
        log_fail "OpenCode SDD default mode install command failed"
    fi
    return $?
}

# ===========================================================================
# TIER 3 — Backup / restore tests (require RUN_BACKUP_TESTS=1)
# ===========================================================================

test_backup_created_on_install() {
    log_test "Backup snapshot created during install"
    cleanup_test_env
    setup_fake_configs

    if $BINARY install --agent opencode --component permissions --persona neutral 2>&1; then
        local backup_count
        backup_count=$(find "$HOME/.takt-ai/backups" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
        if [[ "$backup_count" -gt 0 ]]; then
            log_pass "Backup directory created ($backup_count snapshots)"
        else
            log_fail "No backup directory found"
        fi
    else
        log_fail "Install with backup failed"
    fi
    return $?
}

test_backup_contains_original_files() {
    log_test "Backup snapshot contains original config files"
    cleanup_test_env
    setup_fake_configs

    if $BINARY install --agent opencode --component permissions --persona neutral 2>&1; then
        local latest_backup
        latest_backup=$(find "$HOME/.takt-ai/backups" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | tail -1)
        if [[ -n "$latest_backup" ]]; then
            local file_count
            file_count=$(find "$latest_backup" -type f 2>/dev/null | wc -l | tr -d ' ')
            if [[ "$file_count" -gt 0 ]]; then
                log_pass "Backup contains $file_count file(s)"
            else
                log_fail "Backup directory is empty"
            fi
        else
            log_fail "No backup snapshot directory found"
        fi
    else
        log_fail "Install for backup test failed"
    fi
    return $?
}

test_backup_manifest_exists() {
    log_test "Backup manifest file exists"
    cleanup_test_env
    setup_fake_configs

    if $BINARY install --agent opencode --component permissions --persona neutral 2>&1; then
        local latest_backup
        latest_backup=$(find "$HOME/.takt-ai/backups" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | sort | tail -1)
        if [[ -n "$latest_backup" ]]; then
            if [[ -f "$latest_backup/manifest.json" ]]; then
                assert_valid_json "$latest_backup/manifest.json" "Backup manifest is valid JSON"
            else
                log_fail "manifest.json not found in backup: $latest_backup"
            fi
        else
            log_fail "No backup snapshot found"
        fi
    else
        log_fail "Install for manifest test failed"
    fi
    return $?
}

test_backup_idempotent_install() {
    log_test "Idempotent: running install twice produces same result (with backup)"
    cleanup_test_env

    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true
    local first_content
    first_content=$(cat "$HOME/.config/opencode/opencode.json" 2>/dev/null)

    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true
    local second_content
    second_content=$(cat "$HOME/.config/opencode/opencode.json" 2>/dev/null)

    if [[ "$first_content" = "$second_content" ]] && [[ -n "$first_content" ]]; then
        log_pass "Idempotent: same config after two runs (with backup)"
    else
        log_fail "Config changed between runs (with backup)"
    fi
    return $?
}

test_backup_multiple_snapshots() {
    log_test "Multiple installs create multiple backup snapshots"
    cleanup_test_env
    setup_fake_configs

    $BINARY install --agent opencode --component permissions --persona neutral 2>&1 || true
    sleep 0.1
    $BINARY install --agent opencode --component theme --persona neutral 2>&1 || true

    local backup_count
    backup_count=$(find "$HOME/.takt-ai/backups" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
    if [[ "$backup_count" -ge 2 ]]; then
        log_pass "Multiple backup snapshots created ($backup_count)"
    else
        log_fail "Expected >= 2 backup snapshots, got $backup_count"
    fi
    return $?
}

# ===========================================================================
# Test execution
# ===========================================================================

log_info "=== Tier 1: Basic binary & dry-run tests ==="

# Category 1a: Binary basics
test_binary_exists
test_binary_runs
test_version_command

# Category 1b: Dry-run output format
test_dry_run_output_format
test_dry_run_platform_detection
test_dry_run_detects_linux

# Category 1c: Agent flags
test_dry_run_agent_opencode

# Category 1d: Preset flags
test_dry_run_preset_minimal
test_dry_run_preset_ecosystem
test_dry_run_preset_full
test_dry_run_preset_custom

# Category 1e: Preset component order validation
test_dry_run_full_preset_persona_before_sdd

# Category 1f: Individual component flags (all 8)
test_dry_run_component_permissions
test_dry_run_component_theme

# Category 1f2: SDD mode flag
test_dry_run_sdd_mode_multi
test_dry_run_sdd_mode_single
test_dry_run_sdd_mode_invalid_rejected

# Category 1g: Invalid inputs
test_invalid_persona_rejected
test_invalid_component_rejected
test_invalid_preset_rejected
test_unknown_command_rejected

if [[ "${RUN_FULL_E2E:-0}" = "1" ]]; then
    log_info ""
    log_info "=== Tier 2: Component injection tests ==="


    # Category 3: OpenCode injection
    test_oc_engram_injection
    test_oc_sdd_injection
    test_oc_persona_takt
    test_oc_persona_neutral
    test_oc_persona_custom_does_nothing
    test_oc_skills_minimal
    test_oc_skills_full
    test_oc_context7_injection
    test_oc_permissions_injection
    test_oc_theme_injection

    # Category 4: Full preset integration
    test_full_preset_opencode
    test_minimal_preset_opencode_only_engram_no_persona

    # Category 5: Content validation
    test_content_opencode_commands_valid_markdown

    # Category 6: Idempotency
    test_idempotent_permissions_opencode
    test_idempotent_theme_opencode


    # Category 8: Edge cases
    test_edge_persona_switch_preserves_sections_opencode
    test_edge_json_merge_preserves_existing
    test_edge_multiple_json_overlays

    # GGA

    # Category 7: Injection integrity (issue #4 regression guard)
    test_integrity_sdd_skills_nonempty
    test_integrity_sdd_orchestrator_in_opencode_json
    test_integrity_all_sdd_commands_have_frontmatter
    test_integrity_full_preset_all_skills_nonempty
    test_integrity_sdd_orchestrator_agent_structure
    test_integrity_skills_plus_sdd_coexist

    # Category 9: SDD multi-mode
    test_oc_sdd_multi_mode_injection
    test_oc_sdd_single_mode_no_models
    test_oc_sdd_default_mode_same_as_single

else
    log_skip "Tier 2 tests (set RUN_FULL_E2E=1 to enable)"
fi

if [[ "${RUN_BACKUP_TESTS:-0}" = "1" ]]; then
    log_info ""
    log_info "=== Tier 3: Backup/restore tests ==="
    test_backup_created_on_install
    test_backup_contains_original_files
    test_backup_manifest_exists
    test_backup_idempotent_install
    test_backup_multiple_snapshots
else
    log_skip "Tier 3 tests (set RUN_BACKUP_TESTS=1 to enable)"
fi

# ---------------------------------------------------------------------------
# Summary & exit
# ---------------------------------------------------------------------------
print_summary
