.DEFAULT_GOAL := help

.PHONY: help install install-bootstrap install-wait-ollama install-model doctor doctor-index doctor-verify-index mod test test-cover build run reindex compose-up compose-down compose-logs compose-validate

GO_IMAGE ?= golang:1.25-alpine
GO_BIN ?= /usr/local/go/bin/go
GO_RUN = docker run --rm -u "$$(id -u):$$(id -g)" -e HOME=/tmp -v "$(PWD):/workspace" -w /workspace $(GO_IMAGE)
COVERAGE_MIN ?= 60
COMPOSE = docker compose --project-directory . -f docker/docker-compose.yml

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make install' 'Create local config, start stack, pull model, and reindex'
	@printf '  %-20s %s\n' 'make doctor' 'Run tests/build/compose checks and verify indexed data'
	@printf '  %-20s %s\n' 'make mod' 'Download and tidy Go modules'
	@printf '  %-20s %s\n' 'make test' 'Run Go tests in a Go container'
	@printf '  %-20s %s\n' 'make test-cover' 'Run Go tests with coverage gate in container'
	@printf '  %-20s %s\n' 'make build' 'Run containerized Go compile check (no binaries)'
	@printf '  %-20s %s\n' 'make run' 'Run MCP server via Docker Compose'
	@printf '  %-20s %s\n' 'make reindex' 'Run index build in the service container'
	@printf '  %-20s %s\n' 'make compose-up' 'Start compose stack'
	@printf '  %-20s %s\n' 'make compose-down' 'Stop compose stack'
	@printf '  %-20s %s\n' 'make compose-logs' 'Tail compose logs'
	@printf '  %-20s %s\n' 'make compose-validate' 'Validate Docker Compose config'

install: install-bootstrap run install-wait-ollama install-model reindex doctor-verify-index

install-bootstrap:
	$(GO_RUN) $(GO_BIN) run ./cmd/rag-install --repo-root /workspace

install-wait-ollama:
	@for i in $$(seq 1 60); do \
		if $(COMPOSE) exec -T ollama ollama list >/dev/null 2>&1; then \
			exit 0; \
		fi; \
		sleep 2; \
	done; \
	printf '%s\n' 'ollama did not become ready in time' >&2; \
	exit 1

install-model:
	@model="$${EMBED_MODEL:-nomic-embed-text}"; \
	$(COMPOSE) exec -T ollama ollama pull "$$model"

doctor: test build compose-validate doctor-index

doctor-index: run reindex doctor-verify-index

doctor-verify-index:
	$(COMPOSE) exec -T rag-mcp sh -lc 'set -eu; tenant="$${RAG_CHROMA_TENANT:-default_tenant}"; database="$${RAG_CHROMA_DATABASE:-default_database}"; collection="$${RAG_COLLECTION_NAME:-rag}"; base="http://chroma:8000/api/v2/tenants/$$tenant/databases/$$database"; col_payload="$$(printf "{\"name\":\"%s\",\"get_or_create\":true,\"metadata\":{\"hnsw:space\":\"cosine\"}}" "$$collection")"; col="$$(printf "%s" "$$col_payload" | wget -qO- --header "Content-Type: application/json" --post-file=- "$$base/collections")"; cid="$$(printf "%s" "$$col" | sed -n "s/.*\"id\":\"\([^\"]*\)\".*/\1/p")"; test -n "$$cid"; get="$$(printf "%s" "{\"limit\":1,\"offset\":0,\"include\":[\"metadatas\"]}" | wget -qO- --header "Content-Type: application/json" --post-file=- "$$base/collections/$$cid/get")"; printf "%s" "$$get" | grep -Eq "\"ids\":\[[^]]*\"[^\"]+\"" && echo "doctor: indexed data present in Chroma"'

mod:
	$(GO_RUN) $(GO_BIN) mod tidy

test:
	$(GO_RUN) $(GO_BIN) test -count=1 ./...

test-cover:
	$(GO_RUN) sh -lc "set -eu; $(GO_BIN) test -count=1 -covermode=atomic -coverprofile=coverage.out ./...; $(GO_BIN) tool cover -func=coverage.out | tee coverage.txt; awk -v min=\"$(COVERAGE_MIN)\" '/^total:/ { gsub(/%/, \"\", \$$3); if ((\$$3 + 0) < (min + 0)) { printf(\"coverage %.1f%% is below minimum %.1f%%\\n\", \$$3, min); exit 1 }; found=1 } END { if (!found) { print \"coverage total not found\"; exit 1 } }' coverage.txt"

build:
	$(GO_RUN) $(GO_BIN) build ./...

run:
	$(COMPOSE) up -d --build

reindex:
	$(COMPOSE) run --rm --entrypoint /app/rag-index rag-mcp

compose-up:
	$(COMPOSE) up -d --build

compose-down:
	$(COMPOSE) down

compose-logs:
	$(COMPOSE) logs -f

compose-validate:
	$(COMPOSE) config
