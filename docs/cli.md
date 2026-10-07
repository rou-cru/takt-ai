# CLI reference

`takt-ai` with no arguments opens the interactive TUI. It needs a terminal on both input and output; otherwise it prints the help and fails. Every other command runs without the TUI.

## Commands

| Command | What it does |
| --- | --- |
| `setup install` | Applies Takt to OpenCode. |
| `setup sync` | Re-applies the managed files to an existing installation. Files you edited are kept; files you deleted are restored. |
| `setup uninstall` | Removes what Takt installed and restores what it replaced. |
| `setup default-request` | Prints the recommended setup request as JSON. |
| `restore` | Puts back the files Takt replaced. Always prints JSON. |
| `doctor` | Checks tools, configuration and capabilities. |
| `version` | Prints `takt-ai` and its version. Also `--version` and `-v`. |
| `help` | Prints the help. Also `--help` and `-h`. |

## Setup flags

| Flag | Meaning |
| --- | --- |
| `--plan-only` | Shows what would change and changes nothing. |
| `--yes` | Applies the change. Required unless `--plan-only` is given; without either, the command refuses to run. |
| `--input FILE` | Setup request as JSON from a file, or `-` for stdin. |
| `--root DIR` | Home directory to act on. Defaults to `$HOME`. |
| `--json` | Prints one JSON value on stdout. Human-readable notices go to stderr. |

When `--input` is omitted on a terminal, the recommended setup is used. When it is omitted and stdin is not a terminal, the request is read from stdin. The request is validated strictly: unknown fields and extra JSON values are rejected.

If a previous run stopped abruptly, setup reports it as a warning and continues; every command re-checks the actual files before acting.

`install` and `sync` stop when an existing file conflicts with what Takt would write and the conflict needs a decision; the error lists the files and asks you to resolve them in the TUI. Conflicts with files unrelated to Takt are left as they are and reported. `uninstall` stops when a file Takt installed was edited since, and asks for a keep or remove decision in the TUI. `--plan-only` shows these situations beforehand.

## Restore flags

`restore` accepts only `--root DIR`, with the same default as setup.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success. |
| 1 | Error, or `doctor` found something unhealthy. |
| 2 | Unknown command. |
| 130 | Cancelled with Ctrl+C. The change stops safely. |

## Install script

`install.sh` installs the `takt-ai` binary. Pass options with `bash -s -- <options>` when piping it from `curl`.

| Option | Meaning |
| --- | --- |
| `--method METHOD` | Forces the install method: `brew`, `go` or `binary`. Without it, Homebrew is used when available, otherwise the binary from GitHub Releases. `go` is never chosen automatically. |
| `--dir DIR` | Install directory for the `binary` method. |
| `--insecure` | Skips the checksum and signature checks. Not recommended. |
| `-h`, `--help` | Prints the help. |

The installer checks the SHA-256 checksums of the download and, when `cosign` is installed, the signature of `checksums.txt` (see [Verifying releases](../SECURITY.md#verifying-releases)). Without `--insecure` it stops when the checksums cannot be verified, or when `cosign` is installed and the signature cannot be. Without `cosign`, only the checksums are verified.

The `go` method builds from source with `go install`, which names the binary `cli`; the installer renames it to `takt-ai`.

## Internal commands

`memory`, `vfs`, `obs`, `gc`, `dispatch`, `dag`, `codegraph` and `setup image` are called by Takt's OpenCode plugins and by image builds. They are not part of the user interface and may change without notice.
