#!/bin/sh
set -eu

: "${GO_IMAGE:=golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447}"
: "${GO_BIN:=/usr/local/go/bin/go}"
compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

GO_IMAGE="$GO_IMAGE" GO_BIN="$GO_BIN" sh ./shell/install-bootstrap.sh

docker compose --project-directory "$compose_project_dir" -f "$compose_file" up -d --build

i=1
while [ "$i" -le 60 ]; do
	if docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T ollama ollama list >/dev/null 2>&1; then
		break
	fi
	if [ "$i" -eq 60 ]; then
		printf '%s\n' 'ollama did not become ready in time' >&2
		exit 1
	fi
	sleep 2
	i=$((i + 1))
done

model=${EMBED_MODEL:-nomic-embed-text}
docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T ollama ollama pull "$model"
docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp /app/rag-index
COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/doctor-verify-index.sh
