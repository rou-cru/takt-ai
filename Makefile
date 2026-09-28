.PHONY: test-host test-containerized test-packages test-shell lint docs release-check release-snapshot dev
.DEFAULT_GOAL := test-host

# Build (if needed) and drop into the disposable dev container.
# Exit 130 (128+SIGINT) means the interactive session ended cleanly
# (^C or `exit` after one); any other code is a real failure.
dev:
	@set +e; \
	docker compose run --rm dev; \
	code=$$?; \
	if [ "$$code" -eq 130 ]; then exit 0; fi; \
	exit "$$code"

# Fail on unclassified, stale, or overlapping package entries before host tests.
test-packages:
	./scripts/test-packages.sh check

# Offline shell regressions: fake Docker/downloads, isolated install directory.
test-shell:
	./scripts/test-shell.sh

# Tests that do not mutate real user state or execute external binaries.
test-host: test-packages
	go test -race -coverprofile=coverage.out $$(./scripts/test-packages.sh host)

# Static analysis beyond `go vet`: default linter set (errcheck, staticcheck,
# unused, gosimple, ineffassign). Mirrors the CI lint step.
lint:
	golangci-lint run ./...

# Filesystem-mutating / binary-executing tests: run inside a disposable
# Docker container (zero host mounts, caches in named Docker volumes).
test-containerized:
	./scripts/test-containerized.sh

# Regenerate API documentation markdown from Go doc comments into docs/api/
# (gomarkdoc is declared as a Go tool in go.mod). Patterns are explicit:
# gomarkdoc's ./... walk does not skip hidden directories like .dev/
docs:
	go tool gomarkdoc ./cmd/... ./takt/... --output docs/api/{{.Dir}}.md

# Validate the GoReleaser config (requires the goreleaser CLI: brew install goreleaser).
release-check:
	goreleaser check

# Local snapshot build without publishing (dist/ output; requires goreleaser).
release-snapshot:
	goreleaser release --snapshot --clean
