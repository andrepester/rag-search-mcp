.DEFAULT_GOAL := help

.PHONY: help install install-bootstrap install-wait-ollama install-model doctor doctor-index doctor-verify-index fmt-check vet mod test test-cover build bootstrap-smoke govulncheck sbom-go licenses-export run reindex compose-up compose-down compose-logs compose-validate

GO_IMAGE ?= golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447
GO_BIN ?= /usr/local/go/bin/go
GOFMT_BIN ?= /usr/local/go/bin/gofmt
GO_RUN = docker run --rm -u "$$(id -u):$$(id -g)" -e HOME=/tmp -e RAG_HTTP_PORT -e HOST_DOCS_DIR -e HOST_CODE_DIR -e HOST_INDEX_DIR -e HOST_MODELS_DIR -v "$(PWD):/workspace" -w /workspace $(GO_IMAGE)
COVERAGE_MIN ?= 60
COMPOSE = docker compose --project-directory . -f docker/docker-compose.yml

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make install' 'Create local config, start stack, pull model, and reindex'
	@printf '  %-20s %s\n' 'make doctor' 'Run tests/build/compose checks and verify indexed data'
	@printf '  %-20s %s\n' 'make fmt-check' 'Verify gofmt output in a container'
	@printf '  %-20s %s\n' 'make vet' 'Run go vet in a container'
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
	@set -eu; \
		host_repo="$$(pwd -P)"; \
		host_parent="$$(dirname "$$host_repo")"; \
		repo_name="$$(basename "$$host_repo")"; \
		resolve_host_override() { \
			key="$$1"; \
			eval "value=\$${$$key-}"; \
			value_non_ws="$$(printf '%s' "$$value" | tr -d '[:space:]')"; \
			if [ -n "$$value_non_ws" ]; then \
				printf '%s' "$$value"; \
				return 0; \
			fi; \
			if [ ! -f .env ]; then \
				return 0; \
			fi; \
			while IFS= read -r line || [ -n "$$line" ]; do \
				trimmed="$${line#"$${line%%[![:space:]]*}"}"; \
				case "$$trimmed" in ''|\#*) continue ;; esac; \
				case "$$trimmed" in *=*) ;; *) continue ;; esac; \
				entry_key="$${trimmed%%=*}"; \
				entry_key="$${entry_key%"$${entry_key##*[![:space:]]}"}"; \
				if [ "$$entry_key" != "$$key" ]; then \
					continue; \
				fi; \
				value="$${trimmed#*=}"; \
				value="$${value#"$${value%%[![:space:]]*}"}"; \
				value="$${value%"$${value##*[![:space:]]}"}"; \
				value="$${value#\"}"; value="$${value%\"}"; \
				value="$${value#\'}"; value="$${value%\'}"; \
				value_non_ws="$$(printf '%s' "$$value" | tr -d '[:space:]')"; \
				if [ -n "$$value_non_ws" ]; then \
					printf '%s' "$$value"; \
				fi; \
				return 0; \
			done < .env; \
		}; \
		set -- docker run --rm -u "$$(id -u):$$(id -g)" -e HOME=/tmp -e RAG_HTTP_PORT -e HOST_DOCS_DIR -e HOST_CODE_DIR -e HOST_INDEX_DIR -e HOST_MODELS_DIR -v "$$host_parent:/workspace-parent" -w "/workspace-parent/$$repo_name"; \
		for key in HOST_DOCS_DIR HOST_CODE_DIR HOST_INDEX_DIR HOST_MODELS_DIR; do \
			resolved="$$(resolve_host_override "$$key")"; \
			if [ -n "$$resolved" ]; then \
				if [ "$${resolved#/}" = "$$resolved" ]; then \
					resolved_abs="$$(cd "$$host_repo" && mkdir -p "$$resolved" && cd "$$resolved" && pwd -P)"; \
				else \
					resolved_abs="$$(mkdir -p "$$resolved" && cd "$$resolved" && pwd -P)"; \
				fi; \
				set -- "$$@" -e "$$key=$$resolved_abs" -v "$$resolved_abs:$$resolved_abs"; \
			fi; \
		done; \
		"$$@" $(GO_IMAGE) $(GO_BIN) run ./cmd/rag-install --repo-root "/workspace-parent/$$repo_name"

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
	$(COMPOSE) exec -T rag-mcp sh -lc 'set -eu; tenant="$${RAG_CHROMA_TENANT:-default_tenant}"; database="$${RAG_CHROMA_DATABASE:-default_database}"; collection="$${RAG_COLLECTION_NAME:-rag}"; base="http://chroma:8000/api/v2/tenants/$$tenant/databases/$$database"; col_payload="$$(printf "{\"name\":\"%s\",\"get_or_create\":true,\"metadata\":{\"hnsw:space\":\"cosine\"}}" "$$collection")"; col="$$(printf "%s" "$$col_payload" | wget -qO- --header "Content-Type: application/json" --post-file=- "$$base/collections")"; cid="$$(printf "%s" "$$col" | sed -n "s/.*\"id\":\"\([^\"]*\)\".*/\1/p")"; test -n "$$cid"; get="$$(printf "%s" "{\"limit\":1,\"offset\":0,\"include\":[\"metadatas\"]}" | wget -qO- --header "Content-Type: application/json" --post-file=- "$$base/collections/$$cid/get")"; printf "%s" "$$get" | grep -Eq "\"ids\":\[[^]]*\"[^\"]+\"" && echo "doctor: indexed data present in Chroma"'

mod:
	$(GO_RUN) $(GO_BIN) mod tidy

test:
	$(GO_RUN) $(GO_BIN) test -count=1 ./...

test-cover:
	$(GO_RUN) sh -lc "set -eu; $(GO_BIN) test -count=1 -covermode=atomic -coverprofile=coverage.out ./...; $(GO_BIN) tool cover -func=coverage.out | tee coverage.txt; awk -v min=\"$(COVERAGE_MIN)\" '/^total:/ { gsub(/%/, \"\", \$$3); if ((\$$3 + 0) < (min + 0)) { printf(\"coverage %.1f%% is below minimum %.1f%%\\n\", \$$3, min); exit 1 }; found=1 } END { if (!found) { print \"coverage total not found\"; exit 1 } }' coverage.txt"

build:
	$(GO_RUN) $(GO_BIN) build ./...

bootstrap-smoke:
	@set -eu; \
	backup_dir="$$(mktemp -d .bootstrap-smoke-backup.XXXXXX)"; \
	alongside_root=""; \
	absolute_root=""; \
	had_env=0; \
	had_config=0; \
	had_config_invalid=0; \
	had_smoke_override=0; \
	restored=0; \
	if [ -f .env ]; then cp .env "$$backup_dir/.env"; had_env=1; fi; \
	if [ -f opencode.json ]; then cp opencode.json "$$backup_dir/opencode.json"; had_config=1; fi; \
	if [ -f opencode.json.invalid ]; then cp opencode.json.invalid "$$backup_dir/opencode.json.invalid"; had_config_invalid=1; fi; \
	if [ -e .smoke-override ]; then cp -R .smoke-override "$$backup_dir/.smoke-override"; had_smoke_override=1; fi; \
	restore() { \
		if [ "$$restored" -eq 1 ]; then return; fi; \
		restored=1; \
		rm -rf .smoke-override; \
		if [ "$$had_smoke_override" -eq 1 ] && [ -e "$$backup_dir/.smoke-override" ]; then cp -R "$$backup_dir/.smoke-override" .smoke-override; fi; \
		if [ -n "$$alongside_root" ] && [ -d "$$alongside_root" ]; then rm -rf "$$alongside_root"; fi; \
		if [ -n "$$absolute_root" ] && [ -d "$$absolute_root" ]; then rm -rf "$$absolute_root"; fi; \
		if [ "$$had_env" -eq 1 ] && [ -f "$$backup_dir/.env" ]; then cp "$$backup_dir/.env" .env; else rm -f .env; fi; \
		if [ "$$had_config" -eq 1 ] && [ -f "$$backup_dir/opencode.json" ]; then cp "$$backup_dir/opencode.json" opencode.json; else rm -f opencode.json; fi; \
		if [ "$$had_config_invalid" -eq 1 ] && [ -f "$$backup_dir/opencode.json.invalid" ]; then cp "$$backup_dir/opencode.json.invalid" opencode.json.invalid; else rm -f opencode.json.invalid; fi; \
		rm -rf "$$backup_dir"; \
	}; \
	trap 'status=$$?; restore; exit $$status' 0; \
	trap 'exit 129' 1; \
	trap 'exit 130' 2; \
	trap 'exit 131' 3; \
	trap 'exit 143' 15; \
	rm -f .env opencode.json opencode.json.invalid; \
	rm -rf .smoke-override; \
	HOST_DOCS_DIR= HOST_CODE_DIR= HOST_INDEX_DIR= HOST_MODELS_DIR= $(MAKE) install-bootstrap; \
	test -f .env; \
	test -f opencode.json; \
	HOST_DOCS_DIR=./.smoke-override/docs HOST_CODE_DIR=./.smoke-override/code HOST_INDEX_DIR=./.smoke-override/index HOST_MODELS_DIR=./.smoke-override/models $(MAKE) install-bootstrap; \
	test -d ./.smoke-override/docs; \
	test -d ./.smoke-override/code; \
	test -d ./.smoke-override/index; \
	test -d ./.smoke-override/models; \
	host_parent="$$(dirname "$$(pwd -P)")"; \
	absolute_root="$$(mktemp -d "$$host_parent/.bootstrap-smoke-absolute.XXXXXX")"; \
	HOST_DOCS_DIR="$$absolute_root/docs" HOST_CODE_DIR="$$absolute_root/code" HOST_INDEX_DIR="$$absolute_root/index" HOST_MODELS_DIR="$$absolute_root/models" $(MAKE) install-bootstrap; \
	test -d "$$absolute_root/docs"; \
	test -d "$$absolute_root/code"; \
	test -d "$$absolute_root/index"; \
	test -d "$$absolute_root/models"; \
	alongside_root="$$(mktemp -d "$$host_parent/.bootstrap-smoke-alongside.XXXXXX")"; \
	alongside_name="$$(basename "$$alongside_root")"; \
	HOST_DOCS_DIR="../$$alongside_name/docs" HOST_CODE_DIR="../$$alongside_name/code" HOST_INDEX_DIR="../$$alongside_name/index" HOST_MODELS_DIR="../$$alongside_name/models" $(MAKE) install-bootstrap; \
	test -d "$$alongside_root/docs"; \
	test -d "$$alongside_root/code"; \
	test -d "$$alongside_root/index"; \
	test -d "$$alongside_root/models"

govulncheck:
	$(GO_RUN) $(GO_BIN) run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...

sbom-go:
	$(GO_RUN) sh -lc 'set -eu; PATH="/usr/local/go/bin:$$PATH"; toolbin=/tmp/bin; mkdir -p "$$toolbin"; GOBIN="$$toolbin" $(GO_BIN) install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0; "$$toolbin"/cyclonedx-gomod mod -json -licenses -output sbom-go.cdx.json'

licenses-export:
	$(GO_RUN) sh -lc 'set -eu; PATH="/usr/local/go/bin:$$PATH"; toolbin=/tmp/bin; mkdir -p "$$toolbin"; GOBIN="$$toolbin" $(GO_BIN) install github.com/google/go-licenses@v1.6.0; "$$toolbin"/go-licenses report ./... > licenses.csv'

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
