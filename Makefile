.DEFAULT_GOAL := help

.PHONY: help install install-bootstrap install-wait-ollama install-model doctor doctor-index doctor-verify-index fmt-check vet mod test test-cover build bootstrap-smoke govulncheck sbom-go licenses-export run down clean-install reindex compose-logs compose-validate

GO_IMAGE ?= golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447
GO_BIN ?= /usr/local/go/bin/go
GOFMT_BIN ?= /usr/local/go/bin/gofmt
GO_RUN = docker run --rm -u "$$(id -u):$$(id -g)" -e HOME=/tmp -e RAG_HTTP_PORT -e HOST_DOCS_DIR -e HOST_CODE_DIR -e HOST_INDEX_DIR -e HOST_MODELS_DIR -v "$(PWD):/workspace" -w /workspace $(GO_IMAGE)
COVERAGE_MIN ?= 60
FULL_RESET ?= 0
COMPOSE_PROJECT_DIR ?= .
COMPOSE_FILE ?= docker/docker-compose.yml
COMPOSE = docker compose --project-directory $(COMPOSE_PROJECT_DIR) -f $(COMPOSE_FILE)

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make install' 'Create local config, start stack, pull model, and reindex'
	@printf '  %-20s %s\n' 'make clean-install' 'Reinstall stack; use FULL_RESET=1 to wipe index/models'
	@printf '  %-20s %s\n' 'make doctor' 'Run tests/build/compose checks and verify indexed data'
	@printf '  %-20s %s\n' 'make down' 'Stop runtime stack (controlled shutdown)'
	@printf '  %-20s %s\n' 'make fmt-check' 'Verify gofmt output in a container'
	@printf '  %-20s %s\n' 'make vet' 'Run go vet in a container'
	@printf '  %-20s %s\n' 'make mod' 'Download and tidy Go modules'
	@printf '  %-20s %s\n' 'make test' 'Run Go tests in a Go container'
	@printf '  %-20s %s\n' 'make test-cover' 'Run Go tests with coverage gate in container'
	@printf '  %-20s %s\n' 'make build' 'Run containerized Go compile check (no binaries)'
	@printf '  %-20s %s\n' 'make run' 'Run MCP server via Docker Compose'
	@printf '  %-20s %s\n' 'make reindex' 'Run index build in the service container'
	@printf '  %-20s %s\n' 'make compose-logs' 'Tail compose logs'
	@printf '  %-20s %s\n' 'make compose-validate' 'Validate Docker Compose config'

install: install-bootstrap run install-wait-ollama install-model reindex doctor-verify-index

install-bootstrap:
	@GO_IMAGE='$(GO_IMAGE)' GO_BIN='$(GO_BIN)' ./shell/install-bootstrap.sh

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

fmt-check:
	$(GO_RUN) sh -lc 'set -eu; out="$$("$(GOFMT_BIN)" -l .)"; if [ -n "$$out" ]; then printf "%s\n" "Go files are not formatted:" >&2; printf "%s\n" "$$out" >&2; exit 1; fi'

vet:
	$(GO_RUN) $(GO_BIN) vet ./...

doctor-index: run reindex doctor-verify-index

doctor-verify-index:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' ./shell/doctor-verify-index.sh

mod:
	$(GO_RUN) $(GO_BIN) mod tidy

test:
	$(GO_RUN) $(GO_BIN) test -count=1 ./...

test-cover:
	$(GO_RUN) sh -lc "set -eu; $(GO_BIN) test -count=1 -covermode=atomic -coverprofile=coverage.out ./...; $(GO_BIN) tool cover -func=coverage.out | tee coverage.txt; awk -v min=\"$(COVERAGE_MIN)\" '/^total:/ { gsub(/%/, \"\", \$$3); if ((\$$3 + 0) < (min + 0)) { printf(\"coverage %.1f%% is below minimum %.1f%%\\n\", \$$3, min); exit 1 }; found=1 } END { if (!found) { print \"coverage total not found\"; exit 1 } }' coverage.txt"

build:
	$(GO_RUN) $(GO_BIN) build ./...

bootstrap-smoke:
	@./shell/bootstrap-smoke.sh

govulncheck:
	$(GO_RUN) $(GO_BIN) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

sbom-go:
	$(GO_RUN) sh -lc 'set -eu; PATH="/usr/local/go/bin:$$PATH"; toolbin=/tmp/bin; mkdir -p "$$toolbin"; GOBIN="$$toolbin" $(GO_BIN) install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0; "$$toolbin"/cyclonedx-gomod mod -json -licenses -output sbom-go.cdx.json'

licenses-export:
	$(GO_RUN) sh -lc 'set -eu; PATH="/usr/local/go/bin:$$PATH"; toolbin=/tmp/bin; mkdir -p "$$toolbin"; GOBIN="$$toolbin" $(GO_BIN) install github.com/google/go-licenses@v1.6.0; "$$toolbin"/go-licenses report ./... > licenses.csv'

run:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) down --remove-orphans

clean-install:
	@FULL_RESET='$(FULL_RESET)' ./shell/clean-install.sh

reindex:
	$(COMPOSE) run --rm --entrypoint /app/rag-index rag-mcp

compose-logs:
	$(COMPOSE) logs -f

compose-validate:
	$(COMPOSE) config
