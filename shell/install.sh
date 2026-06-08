#!/bin/sh
set -eu

. ./shell/lib.sh

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=$(effective_compose_file)

sh ./shell/install-bootstrap.sh
sh ./shell/config-doctor.sh

COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/up.sh

i=1
while [ "$i" -le 60 ]; do
	if COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T rag-mcp sh -lc 'wget -qO- "${OLLAMA_HOST%/}/api/tags" >/dev/null'; then
		break
	fi
	if [ "$i" -eq 60 ]; then
		printf 'shared ollama host did not become ready in time: %s\n' "$(resolve_ollama_host)" >&2
		exit 1
	fi
	sleep 2
	i=$((i + 1))
done
printf 'install: using shared OLLAMA_HOST=%s; manage model installation outside this stack\n' "$(resolve_ollama_host)" >&2

FRESH_INDEX="${FRESH_INDEX-0}" COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/index.sh
COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/doctor-verify-index.sh
