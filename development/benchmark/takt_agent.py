"""Harbor agent that runs a task with the full Takt crew on OpenCode.

Reuses Harbor's OpenCode agent (run, trajectory, cost parsing) and only swaps
the install and the OpenCode v2 command line.
"""

import json
import shlex
from pathlib import Path
from typing import override

from harbor.agents.installed.base import NonZeroAgentExitCodeError, with_prompt_template
from harbor.agents.installed.opencode import OpenCode
from harbor.environments.base import BaseEnvironment
from harbor.models.agent.context import AgentContext

# Same pins as deploy/images/Dockerfile; OpenCode 2.x is not published on npm.
OPENCODE_VERSION = "2.0.28"
OPENCODE_INSTALL_SHA256 = "6ef97e3d5b8782179efc2b04e6ce2e90ced23675df5792c9ea4f4a0465531865"
# Built by `make bench` from this worktree, so the measured prose is the binary's.
TAKT_BINARY = Path(__file__).resolve().parents[2] / "dist" / "bench" / "takt-ai"
# bubblewrap + socat back the VFS sandbox on Linux; nodejs/npm back codegraph.
APT_PACKAGES = "ca-certificates curl jq nodejs npm bubblewrap socat ripgrep"
# Free-tier mix: the crew is spread over six model families so no single
# free quota carries it. `takt` runs on the -m model (nemotron), which holds as
# few specialists as possible so the orchestrator's bias is not shared with the
# roles that check its work; the judge pair is split across families too.
CAT_JAM, LONGCAT, NEMOTRON, SPACE_BUNNY, MUSE_SPARK, LL_ALPHA = (
    f"opencode/{m}"
    for m in (
        "cat-jam-free",
        "longcat-2.5-preview-free",
        "nemotron-3.5-super-vl-free",
        "space-bunny-free",
        "muse-spark-1.3-contributor-free",
        "ll-alpha-free",
    )
)
MODEL_MIX = {
    CAT_JAM: ["spec", "explorer", "test"],
    LONGCAT: ["dev", "pm", "judge-a"],
    NEMOTRON: ["product-designer"],
    SPACE_BUNNY: ["verify", "simplify", "qa"],
    MUSE_SPARK: ["mutation", "fix", "judge-b"],
    LL_ALPHA: ["analyst", "tpm", "quality", "architect"],
}
OVERRIDES = json.dumps({role: {"Model": m} for m, roles in MODEL_MIX.items() for role in roles})


class TaktOpenCode(OpenCode):
    @override
    async def install(self, environment: BaseEnvironment) -> None:
        # ponytail: apt-only; add an apk branch when a dataset ships musl images.
        await self.exec_as_root(
            environment,
            f"apt-get update -q && DEBIAN_FRONTEND=noninteractive "
            f"apt-get install -y -q --no-install-recommends {APT_PACKAGES}",
        )
        await environment.upload_file(TAKT_BINARY, "/usr/local/bin/takt-ai")
        await self.exec_as_root(environment, "chmod 0755 /usr/local/bin/takt-ai")
        model = shlex.quote(self.model_name or "")
        result = await self.exec_as_agent(
            environment,
            "set -eu; "
            "curl -fsSL --retry 3 --proto '=https' --tlsv1.2 -o /tmp/opencode-install.sh https://opencode.ai/v2/install; "
            f"echo '{OPENCODE_INSTALL_SHA256}  /tmp/opencode-install.sh' | sha256sum -c -; "
            f"bash /tmp/opencode-install.sh --no-modify-path --version {OPENCODE_VERSION}; "
            f"takt-ai setup default-request | jq --arg m {model} --argjson o {shlex.quote(OVERRIDES)} "
            "'.opencode.model=$m | .opencode_model_overrides=$o' "
            "| takt-ai setup image --root \"$HOME\" --yes --json; "
            "mkdir -p \"$HOME/.config/opencode/skills\"; "
            "cp -a \"$HOME/.config/opencode/takt/skills/.\" \"$HOME/.config/opencode/skills/\"; "
            "echo \"$HOME\"",
        )
        home = result.stdout.strip().splitlines()[-1]
        await self.exec_as_root(
            environment, f"ln -sf {shlex.quote(home)}/.opencode/bin/opencode /usr/local/bin/opencode"
        )

    @override
    @with_prompt_template
    async def run(self, instruction: str, environment: BaseEnvironment, context: AgentContext) -> None:
        # Harbor's command is OpenCode v1's (--model before `run`, no --auto) and
        # rewrites opencode.json, which Takt's setup owns; this is the v2 form.
        self._instruction = instruction
        logs = self.environment_logs_dir
        env = {
            **self.model_connection.env,
            "XDG_DATA_HOME": (logs / "opencode" / "xdg-data").as_posix(),
            "XDG_STATE_HOME": (logs / "opencode" / "xdg-state").as_posix(),
        }
        try:
            await self.exec_as_agent(
                environment,
                f"opencode run --standalone --format=json --thinking --auto --agent takt "
                f"-m {shlex.quote(self.model_name or '')} -- {shlex.quote(instruction)} "
                f"2>&1 </dev/null | stdbuf -oL tee {shlex.quote((logs / self._OUTPUT_FILENAME).as_posix())}",
                env=env,
            )
        finally:
            # Per-agent model_usage (cost, tokens) for the whole crew, not just the root session.
            await self.exec_as_agent(
                environment, f"cp .takt-ai/events.db {shlex.quote(logs.as_posix())}/ 2>/dev/null || true"
            )
        if messages := self._error_messages():
            raise NonZeroAgentExitCodeError("OpenCode emitted error event(s): " + "; ".join(messages[:3]))
