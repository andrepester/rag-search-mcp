#!/bin/sh
set -eu

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

docker compose --project-directory "$compose_project_dir" -f "$compose_file" config >/dev/null

if ! docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp true >/dev/null 2>&1; then
	printf '%s\n' 'reindex: rag-mcp container is not running. Start the stack first with make up.' >&2
	exit 1
fi

tmp_dir=${TMPDIR:-/tmp}
log_file=$(mktemp "${tmp_dir%/}/rag-reindex.XXXXXX")
reindex_pid=
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
	if [ -n "$reindex_pid" ]; then
		kill "$reindex_pid" 2>/dev/null || :
		wait "$reindex_pid" 2>/dev/null || :
		reindex_pid=
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
	printf '\rreindexing [%s] %ss' "$bar" "$elapsed" >&2
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

docker compose --project-directory "$compose_project_dir" -f "$compose_file" exec -T rag-mcp /app/rag-index >"$log_file" 2>&1 &
reindex_pid=$!

if [ "$supports_progress" -eq 1 ]; then
	start_progress &
	progress_pid=$!
else
	printf '%s\n' 'reindex: running; waiting for completion...' >&2
fi

if wait "$reindex_pid"; then
	reindex_status=0
else
	reindex_status=$?
fi
reindex_pid=

if [ -n "$progress_pid" ]; then
	kill "$progress_pid" 2>/dev/null || :
	wait "$progress_pid" 2>/dev/null || :
	progress_pid=
fi
clear_progress_line

if [ "$reindex_status" -eq 0 ]; then
	printf '%s\n' 'reindex: complete' >&2
	if [ -s "$log_file" ]; then
		cat "$log_file"
	fi
	exit 0
fi

printf 'reindex: failed with exit code %s\n' "$reindex_status" >&2
if [ -s "$log_file" ]; then
	cat "$log_file" >&2
fi
exit "$reindex_status"
