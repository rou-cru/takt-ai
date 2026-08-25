.PHONY: test-host test-containerized

# Pure, non-filesystem-touching tests: run directly on the host.
test-host:
	go test ./takt/agents/... ./takt/catalog/...

# Filesystem-mutating / binary-executing tests: run inside a disposable
# Docker container (zero host mounts, caches in named Docker volumes).
test-containerized:
	./scripts/test-containerized.sh
