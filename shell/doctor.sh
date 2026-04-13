#!/bin/sh
set -eu

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

docker compose --project-directory "$compose_project_dir" -f "$compose_file" config >/dev/null

if ! docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp true >/dev/null 2>&1; then
	printf '%s\n' 'doctor: rag-mcp container is not running. Start the stack first with make up.' >&2
	exit 1
fi

if ! docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T chroma true >/dev/null 2>&1; then
	printf '%s\n' 'doctor: chroma container is not running. Start the stack first with make up.' >&2
	exit 1
fi

i=1
while [ "$i" -le 60 ]; do
	if docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T ollama ollama list >/dev/null 2>&1; then
		break
	fi
	if [ "$i" -eq 60 ]; then
		printf '%s\n' 'doctor: ollama did not become ready in time.' >&2
		exit 1
	fi
	sleep 2
	i=$((i + 1))
done

docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp /app/rag-index
COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/doctor-verify-index.sh

if ! docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp sh -lc 'wget -qO- http://127.0.0.1:8765/healthz >/dev/null'; then
	printf '%s\n' 'doctor: MCP health endpoint is not ready in rag-mcp container' >&2
	exit 1
fi

printf '%s\n' 'doctor: runtime checks passed'
