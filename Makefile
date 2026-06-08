.DEFAULT_GOAL := help

.PHONY: help install clean-install up down test index logs doctor

FULL_RESET ?= 0
FRESH_INDEX ?= 0
OUTPUT ?= human
COMPOSE_PROJECT_DIR ?= .
COMPOSE_FILE ?= docker/docker-compose.yml
COMPOSE_UP_FLAGS ?= auto

help:
	@printf '%s\n' 'Available targets:'
	@printf '  %-25s %s\n' 'make install' 'Bootstrap config, start stack, verify Ollama host, index'
	@printf '  %-25s %s\n' 'make clean-install' 'Reinstall stack; use FULL_RESET=1 to wipe index'
	@printf '  %-25s %s\n' 'make up' 'Start runtime stack in detached mode'
	@printf '  %-25s %s\n' 'make down' 'Stop runtime stack (without removing containers)'
	@printf '  %-25s %s\n' 'make test' 'Run Go tests via Dockerfile go-runner stage'
	@printf '  %-25s %s\n' 'make index' 'Build index; use FRESH_INDEX=1 or OUTPUT=logs|json'
	@printf '  %-25s %s\n' 'make logs' 'Tail runtime stack logs'
	@printf '  %-25s %s\n' 'make doctor' 'Validate config and run runtime diagnostics'

install:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' COMPOSE_UP_FLAGS='$(COMPOSE_UP_FLAGS)' sh ./shell/install.sh

test:
	@sh ./shell/go-runner.sh test -count=1 ./...

up:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' COMPOSE_UP_FLAGS='$(COMPOSE_UP_FLAGS)' sh ./shell/up.sh

down:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/down.sh

clean-install:
	@FULL_RESET='$(FULL_RESET)' COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/clean-install.sh

index:
	@FRESH_INDEX='$(FRESH_INDEX)' OUTPUT='$(OUTPUT)' COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/index.sh

logs:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/logs.sh

doctor:
	@COMPOSE_PROJECT_DIR='$(COMPOSE_PROJECT_DIR)' COMPOSE_FILE='$(COMPOSE_FILE)' sh ./shell/doctor.sh
