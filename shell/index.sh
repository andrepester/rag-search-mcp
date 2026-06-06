#!/bin/sh
set -eu

. ./shell/lib.sh

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}
fresh_index_raw=${FRESH_INDEX-0}
fresh_index=$(parse_bool_01 "$fresh_index_raw" 0) || {
	printf '%s\n' 'FRESH_INDEX must be one of: 0,1,true,false,yes,no' >&2
	exit 2
}

docker compose --project-directory "$compose_project_dir" -f "$compose_file" config >/dev/null

if ! docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp true >/dev/null 2>&1; then
	printf '%s\n' 'index: rag-mcp container is not running. Start the stack first with make up.' >&2
	exit 1
fi

tmp_dir=${TMPDIR:-/tmp}
log_file=$(mktemp "${tmp_dir%/}/rag-index.XXXXXX")
index_pid=
progress_pid=
supports_progress=0
if [ -t 2 ]; then
	supports_progress=1
fi

cleanup() {
	if [ -n "$progress_pid" ]; then
		kill "$progress_pid" 2>/dev/null || :
		wait "$progress_pid" 2>/dev/null || :
		progress_pid=
	fi
	if [ -n "$index_pid" ]; then
		kill "$index_pid" 2>/dev/null || :
		wait "$index_pid" 2>/dev/null || :
		index_pid=
	fi
	rm -f "$log_file"
}

clear_progress_line() {
	if [ "$supports_progress" -eq 1 ]; then
		printf '\r%79s\r' ' ' >&2
	fi
}

render_progress() {
	elapsed=$(( $(date +%s) - start_epoch ))
	phase=$(( progress_tick % (bar_width + 1) ))
	bar=
	i=0
	while [ "$i" -lt "$bar_width" ]; do
		if [ "$i" -lt "$phase" ]; then
			bar="${bar}#"
		else
			bar="${bar}."
		fi
		i=$(( i + 1 ))
	done
	printf '\rindexing [%s] %ss' "$bar" "$elapsed" >&2
}

start_progress() {
	bar_width=28
	progress_tick=0
	start_epoch=$(date +%s)
	while :; do
		render_progress
		progress_tick=$(( progress_tick + 1 ))
		sleep 1
	done
}

trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

if [ "$fresh_index" -eq 1 ]; then
	printf '%s\n' 'index: FRESH_INDEX=1 requested; resetting the configured Chroma collection before rebuild.' >&2
fi

docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T -e FRESH_INDEX="$fresh_index" rag-mcp /app/rag-index >"$log_file" 2>&1 &
index_pid=$!

if [ "$supports_progress" -eq 1 ]; then
	start_progress &
	progress_pid=$!
else
	printf '%s\n' 'index: running; waiting for completion...' >&2
fi

if wait "$index_pid"; then
	index_status=0
else
	index_status=$?
fi
index_pid=

if [ -n "$progress_pid" ]; then
	kill "$progress_pid" 2>/dev/null || :
	wait "$progress_pid" 2>/dev/null || :
	progress_pid=
fi
clear_progress_line

if [ "$index_status" -eq 0 ]; then
	printf '%s\n' 'index: complete' >&2
	if [ -s "$log_file" ]; then
		cat "$log_file"
	fi
	exit 0
fi

printf 'index: failed with exit code %s\n' "$index_status" >&2
if [ -s "$log_file" ]; then
	cat "$log_file" >&2
fi
exit "$index_status"
