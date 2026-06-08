#!/bin/sh
set -eu

. ./shell/lib.sh

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=$(effective_compose_file)

sh ./shell/config-doctor.sh

COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" config >/dev/null

if ! COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T rag-mcp true >/dev/null 2>&1; then
	printf '%s\n' 'doctor: rag-mcp container is not running. Start the stack first with make up.' >&2
	exit 1
fi

if ! COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T chroma true >/dev/null 2>&1; then
	printf '%s\n' 'doctor: chroma container is not running. Start the stack first with make up.' >&2
	exit 1
fi

if ! COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T rag-mcp sh -lc 'wget -qO- "${OLLAMA_HOST%/}/api/tags" >/dev/null'; then
	printf 'doctor: shared ollama host is not reachable from rag-mcp: %s\n' "$(resolve_ollama_host)" >&2
	exit 1
fi

COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/index.sh
COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/doctor-verify-index.sh

if ! COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T rag-mcp sh -lc 'wget -qO- http://127.0.0.1:8765/healthz >/dev/null'; then
	printf '%s\n' 'doctor: MCP health endpoint is not ready in rag-mcp container' >&2
	exit 1
fi

printf '%s\n' 'doctor: runtime checks passed'
