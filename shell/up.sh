#!/bin/sh
set -eu

. ./shell/lib.sh

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=$(effective_compose_file)

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

COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" up -d --build --remove-orphans $compose_up_flags
