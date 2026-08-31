.PHONY: test-host test-containerized docs release-check release-snapshot

# Pure, non-filesystem-touching tests: run directly on the host.
test-host:
	go test -coverprofile=coverage.out ./takt/agents/... ./takt/catalog/... ./takt/model/... ./takt/internal/artifacts/...

# Filesystem-mutating / binary-executing tests: run inside a disposable
# Docker container (zero host mounts, caches in named Docker volumes).
test-containerized:
	./scripts/test-containerized.sh

# Regenerate API documentation markdown from Go doc comments into docs/api/
# (gomarkdoc is declared as a Go tool in go.mod).
docs:
	go tool gomarkdoc ./... --output docs/api/{{.Dir}}.md

# Validate the GoReleaser config (requires the goreleaser CLI: brew install goreleaser).
release-check:
	goreleaser check

# Local snapshot build without publishing (dist/ output; requires goreleaser).
release-snapshot:
	goreleaser release --snapshot --clean
