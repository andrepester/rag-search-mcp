#!/bin/sh
set -eu

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

if [ "${COMPOSE_UP_FLAGS+x}" = x ] && [ "$COMPOSE_UP_FLAGS" != "auto" ]; then
	compose_up_flags=$COMPOSE_UP_FLAGS
else
	compose_up_flags=''
	compose_up_help=$(docker compose up --help 2>/dev/null || true)
	case "$compose_up_help" in
		*--quiet-build*) compose_up_flags="$compose_up_flags --quiet-build" ;;
	esac
	case "$compose_up_help" in
		*--quiet-pull*) compose_up_flags="$compose_up_flags --quiet-pull" ;;
	esac
fi

docker compose --project-directory "$compose_project_dir" -f "$compose_file" up -d --build $compose_up_flags
