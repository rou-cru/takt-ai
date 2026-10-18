.PHONY: test-host test-containerized test-packages test-shell lint docs release-check release-snapshot dev bench
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
	./development/testing/test-packages.sh check

# Offline shell regressions: fake Docker/downloads, isolated install directory.
test-shell:
	./development/testing/test-shell.sh

# Tests that do not mutate real user state or execute external binaries.
test-host: test-packages
	go test -race -coverprofile=coverage.out $$(./development/testing/test-packages.sh host)

# Static analysis beyond `go vet`: default linter set (errcheck, staticcheck,
# unused, gosimple, ineffassign). Mirrors the CI lint step.
lint:
	golangci-lint run ./...

# Filesystem-mutating / binary-executing tests: run inside a disposable
# Docker container (zero host mounts, caches in named Docker volumes).
test-containerized:
	./development/testing/test-containerized.sh

# Regenerate API documentation markdown from Go doc comments into docs/api/
# (gomarkdoc is declared as a Go tool in go.mod). Patterns are explicit:
# gomarkdoc's ./... walk does not skip hidden directories like .dev/
docs:
	go tool gomarkdoc ./takt/... ./development/generate-logo --output docs/api/{{.Dir}}.md

# Validate the GoReleaser config (requires the goreleaser CLI: brew install goreleaser).
release-check:
	goreleaser check

# Local snapshot build without publishing (dist/ output; requires goreleaser).
release-snapshot:
	goreleaser release --snapshot --clean

# Autonomous crew benchmark through Harbor (local Docker). BENCH_MODEL runs the
# orchestrator; the specialists are mixed in development/benchmark/takt_agent.py.
# Defaults cost nothing: a smoke dataset and a free OpenCode Zen model.
BENCH_DATASET ?= hello-world
BENCH_MODEL ?= opencode/nemotron-3.5-super-vl-free
BENCH_ARGS ?=
bench:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o dist/bench/takt-ai ./takt/cli
	PYTHONPATH=. uvx --from harbor==0.24.0 harbor run -d $(BENCH_DATASET) \
	  --agent development.benchmark.takt_agent:TaktOpenCode -m $(BENCH_MODEL) \
	  --ek 'extra_docker_compose=["development/benchmark/sandbox.compose.yaml"]' $(BENCH_ARGS)
