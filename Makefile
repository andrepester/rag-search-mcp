.DEFAULT_GOAL := help

.PHONY: help mod test build run reindex compose-up compose-down compose-logs

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make mod' 'Download and tidy Go modules'
	@printf '  %-20s %s\n' 'make test' 'Run Go tests'
	@printf '  %-20s %s\n' 'make build' 'Build rag binaries'
	@printf '  %-20s %s\n' 'make run' 'Run MCP server locally'
	@printf '  %-20s %s\n' 'make reindex' 'Run local index build'
	@printf '  %-20s %s\n' 'make compose-up' 'Start compose stack'
	@printf '  %-20s %s\n' 'make compose-down' 'Stop compose stack'
	@printf '  %-20s %s\n' 'make compose-logs' 'Tail compose logs'

mod:
	go mod tidy

test:
	go test ./...

build:
	go build ./cmd/rag-mcp
	go build ./cmd/rag-index

run:
	go run ./cmd/rag-mcp

reindex:
	go run ./cmd/rag-index

compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

compose-logs:
	docker compose logs -f
