# Environment variables

Variables you can set to change how `takt-ai` behaves. Everything else Takt AI reads from the environment is internal and not part of the interface.

| Variable | Effect |
| --- | --- |
| `TAKT_NO_ANIMATION` | Any non-empty value turns off the TUI animation. |
| `TAKT_LOGO` | Forces how the TUI draws the logo: `image` (kitty graphics) or `quadrants` (block characters). Any other value is ignored and the terminal is probed instead. |
| `NO_COLOR` | Any non-empty value, `0` included, keeps the TUI without color whatever the terminal supports, and turns off color in `install.sh`. |
| `TERM` | `dumb` has the same effect as `NO_COLOR`. |
| `ENGRAM_BASE_URL` | Address of the Engram memory server used by `takt-ai` and `doctor`. Defaults to `http://127.0.0.1:7437`. |
| `ENGRAM_DATA_DIR` | Directory holding the Engram database. Defaults to `~/.engram`. Uninstall reads it to offer a copy of your memory. |

Model provider keys, such as `ANTHROPIC_API_KEY`, are read by OpenCode, not by Takt AI.

The workspace image adds its own variables (`OPENCODE_PASSWORD`, `TAKT_WORKSPACE_DIR`, `TAKT_OPENCODE_PORT`); they are listed in [the image README](../deploy/workspace/README.md).
