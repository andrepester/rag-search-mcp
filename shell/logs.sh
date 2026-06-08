#!/bin/sh
set -eu

. ./shell/lib.sh

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=$(effective_compose_file)

COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" logs -f
