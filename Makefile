.DEFAULT_GOAL := help

.PHONY: help install clean-install up down test reindex logs doctor

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
	@printf '  %-20s %s\n' 'make test' 'Run Go tests via Dockerfile go-runner stage'
	@printf '  %-20s %s\n' 'make reindex' 'Rebuild index in the running rag-mcp container'
	@printf '  %-20s %s\n' 'make logs' 'Tail runtime stack logs'
	@printf '  %-20s %s\n' 'make doctor' 'Run runtime diagnostics on the running stack'

install:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/install.sh

test:
	@sh ./shell/go-runner.sh test -count=1 ./...

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) stop

clean-install:
	@FULL_RESET='$(FULL_RESET)' COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/clean-install.sh

reindex:
	$(COMPOSE) exec -T rag-mcp /app/rag-index

logs:
	$(COMPOSE) logs -f

doctor:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/doctor.sh
