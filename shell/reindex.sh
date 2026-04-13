#!/bin/sh
set -eu

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

docker compose --project-directory "$compose_project_dir" -f "$compose_file" config >/dev/null

if ! docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp true >/dev/null 2>&1; then
	printf '%s\n' 'reindex: rag-mcp container is not running. Start the stack first with make up.' >&2
	exit 1
fi

docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp /app/rag-index
