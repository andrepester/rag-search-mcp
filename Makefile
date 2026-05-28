.DEFAULT_GOAL := help

.PHONY: help install clean-install up down test govulncheck toolchain-security security reindex logs doctor

FULL_RESET ?= 0
COMPOSE_PROJECT_DIR ?= .
COMPOSE_FILE ?= docker/docker-compose.yml
COMPOSE = docker compose --project-directory $(COMPOSE_PROJECT_DIR) -f $(COMPOSE_FILE)

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-25s %s\n' 'make install' 'Bootstrap config, start stack, pull model, reindex, verify'
	@printf '  %-25s %s\n' 'make clean-install' 'Reinstall stack; use FULL_RESET=1 to wipe index/models'
	@printf '  %-25s %s\n' 'make up' 'Start runtime stack in detached mode'
	@printf '  %-25s %s\n' 'make down' 'Stop runtime stack (without removing containers)'
	@printf '  %-25s %s\n' 'make test' 'Run Go tests via Dockerfile go-runner stage'
	@printf '  %-25s %s\n' 'make govulncheck' 'Run govulncheck via Dockerfile go-runner stage'
	@printf '  %-25s %s\n' 'make toolchain-security' 'Check tools module dependency guardrails via go-runner'
	@printf '  %-25s %s\n' 'make security' 'Run runtime and tools dependency security checks'
	@printf '  %-25s %s\n' 'make reindex' 'Rebuild index in the running rag-mcp container'
	@printf '  %-25s %s\n' 'make logs' 'Tail runtime stack logs'
	@printf '  %-25s %s\n' 'make doctor' 'Run runtime diagnostics on the running stack'

install:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/install.sh

test:
	@sh ./shell/go-runner.sh test -count=1 ./...

govulncheck:
	@sh ./shell/ci-govulncheck.sh

toolchain-security:
	@sh ./shell/ci-toolchain-security.sh

security: govulncheck toolchain-security

up:
	$(COMPOSE) up -d --build

down:
	$(COMPOSE) stop

clean-install:
	@FULL_RESET='$(FULL_RESET)' COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/clean-install.sh

reindex:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/reindex.sh

logs:
	$(COMPOSE) logs -f

doctor:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/doctor.sh
