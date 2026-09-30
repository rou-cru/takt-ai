<div align="center">
  <img src="docs/assets/brand/takt-ai-banner.png" alt="Takt AI" width="100%" />

  [![Quality gate](https://sonarcloud.io/api/project_badges/quality_gate?project=rou-cru_takt-ai)](https://sonarcloud.io/summary/new_code?id=rou-cru_takt-ai)
  [![Go](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go&logoColor=white)](https://go.dev)
  [![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](https://www.gnu.org/licenses/agpl-3.0)
  [![GitHub Release](https://img.shields.io/github/v/release/rou-cru/takt-ai)](https://github.com/rou-cru/takt-ai/releases)
</div>

# Takt AI

Takt AI is a meta-harness for AI coding assistants.

It's not a standalone agent, an LLM, or a simple collection of prompt plugins. It operates a level deeper as an integration layer that sits between your local workspace and your AI tooling. It orchestrates the context, boundaries, and persistent state that raw coding assistants usually lack.

## Supported Agents

Takt AI integrates directly with coding agents:

- **OpenCode**

## Install

### Linux/macOS

```sh
curl -fsSL https://raw.githubusercontent.com/rou-cru/takt-ai/main/install.sh | bash
```

### Homebrew

```sh
brew tap rou-cru/homebrew-takt-ai
brew install --cask rou-cru/takt-ai/takt-ai
```

### Go

> Note: The binary is named "cli"; install.sh --method go renames it.

```sh
go install github.com/rou-cru/takt-ai/takt/cli@latest   
```

### Manual

Linux `.deb`, `.rpm`, `.apk` and Arch packages are attached to every [release](https://github.com/rou-cru/takt-ai/releases), together with SBOMs and signatures.

### Docker & Kubernetes

> Plug & play without changing your current tools. Great for isolated testing, trials, or working on the go.

```sh
docker run --rm -p 4096:4096 -e OPENCODE_PASSWORD=change-me -v "$PWD:/workspace" roucru/takt-ai:latest
```

> Enterprise-grade. Provides Takt AI as a standard environment for organizations and engineering teams.

```sh
helm install demo oci://ghcr.io/rou-cru/charts/takt-ai --namespace takt-workspaces --create-namespace
```

## Documentation

- [Intended usage](docs/intended-usage.md) — the mental model
- [Usage](docs/usage.md) — commands, flags, and supported agents
- [Architecture](docs/architecture.md)
- [Contributing](CONTRIBUTING.md)

## License

[AGPL-3.0](LICENSE)
