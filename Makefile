.DEFAULT_GOAL := help

.PHONY: help doctor mod test build run reindex compose-up compose-down compose-logs compose-validate

GO_IMAGE ?= golang:1.25-alpine
GO_BIN ?= /usr/local/go/bin/go
GO_RUN = docker run --rm -u "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(PWD):/workspace" -w /workspace $(GO_IMAGE)

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make doctor' 'Run local quality checks in containers'
	@printf '  %-20s %s\n' 'make mod' 'Download and tidy Go modules'
	@printf '  %-20s %s\n' 'make test' 'Run Go tests in a Go container'
	@printf '  %-20s %s\n' 'make build' 'Build rag binaries in a Go container'
	@printf '  %-20s %s\n' 'make run' 'Run MCP server via Docker Compose'
	@printf '  %-20s %s\n' 'make reindex' 'Run index build in the service container'
	@printf '  %-20s %s\n' 'make compose-up' 'Start compose stack'
	@printf '  %-20s %s\n' 'make compose-down' 'Stop compose stack'
	@printf '  %-20s %s\n' 'make compose-logs' 'Tail compose logs'
	@printf '  %-20s %s\n' 'make compose-validate' 'Validate Docker Compose config'

doctor: test build compose-validate

mod:
	$(GO_RUN) $(GO_BIN) mod tidy

test:
	$(GO_RUN) $(GO_BIN) test -count=1 ./...

build:
	$(GO_RUN) sh -lc '$(GO_BIN) build ./cmd/rag-mcp && $(GO_BIN) build ./cmd/rag-index'

run:
	docker compose up -d --build

reindex:
	docker compose run --rm --entrypoint /app/rag-index rag-mcp

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f

compose-validate:
	docker compose config
