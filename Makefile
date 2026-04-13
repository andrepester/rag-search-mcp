.DEFAULT_GOAL := help

.PHONY: help install clean-install up down test reindex logs doctor

GO_IMAGE ?= golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447
GO_BIN ?= /usr/local/go/bin/go
GO_RUN = docker run --rm -u "$$(id -u):$$(id -g)" -e HOME=/tmp -e RAG_HTTP_PORT -e HOST_DOCS_DIR -e HOST_CODE_DIR -e HOST_INDEX_DIR -e HOST_MODELS_DIR -v "$(PWD):/workspace" -w /workspace $(GO_IMAGE)
FULL_RESET ?= 0
COMPOSE_PROJECT_DIR ?= .
COMPOSE_FILE ?= docker/docker-compose.yml
COMPOSE = docker compose --project-directory $(COMPOSE_PROJECT_DIR) -f $(COMPOSE_FILE)

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-20s %s\n' 'make install' 'Bootstrap config, start stack, pull model, reindex, verify'
	@printf '  %-20s %s\n' 'make clean-install' 'Reinstall stack; use FULL_RESET=1 to wipe index/models'
	@printf '  %-20s %s\n' 'make up' 'Start runtime stack in detached mode'
	@printf '  %-20s %s\n' 'make down' 'Stop runtime stack (without removing containers)'
	@printf '  %-20s %s\n' 'make test' 'Run Go tests in a Go container'
	@printf '  %-20s %s\n' 'make reindex' 'Rebuild index in the running rag-mcp container'
	@printf '  %-20s %s\n' 'make logs' 'Tail runtime stack logs'
	@printf '  %-20s %s\n' 'make doctor' 'Run runtime diagnostics on the running stack'

install:
	@GO_IMAGE='$(GO_IMAGE)' GO_BIN='$(GO_BIN)' COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/install.sh

test:
	$(GO_RUN) $(GO_BIN) test -count=1 ./...

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) stop

clean-install:
	@GO_IMAGE='$(GO_IMAGE)' GO_BIN='$(GO_BIN)' FULL_RESET='$(FULL_RESET)' COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/clean-install.sh

reindex:
	$(COMPOSE) exec -T rag-mcp /app/rag-index

logs:
	$(COMPOSE) logs -f

doctor:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/doctor.sh
