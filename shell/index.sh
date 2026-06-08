#!/bin/sh
set -eu

. ./shell/lib.sh

compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=$(effective_compose_file)
fresh_index_raw=${FRESH_INDEX-0}
fresh_index=$(parse_bool_01 "$fresh_index_raw" 0) || {
	printf '%s\n' 'FRESH_INDEX must be one of: 0,1,true,false,yes,no' >&2
	exit 2
}
output=${OUTPUT:-human}
case "$output" in
	human|json|logs)
		;;
	*)
		printf '%s\n' 'OUTPUT must be one of: human,json,logs' >&2
		exit 2
		;;
esac

COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" config >/dev/null

if ! COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T rag-mcp true >/dev/null 2>&1; then
	printf '%s\n' 'index: rag-mcp container is not running. Start the stack first with make up.' >&2
	exit 1
fi

tmp_dir=${TMPDIR:-/tmp}
log_file=$(mktemp "${tmp_dir%/}/rag-index.XXXXXX")
index_pid=
progress_pid=
index_run_token="index-$$-$(date +%s)"
supports_progress=0
if [ -t 2 ]; then
	supports_progress=1
fi
progress_interval=1
if [ "$supports_progress" -ne 1 ]; then
	progress_interval=5
fi

cleanup() {
	if [ -n "$progress_pid" ]; then
		kill "$progress_pid" 2>/dev/null || :
		wait "$progress_pid" 2>/dev/null || :
		progress_pid=
	fi
	if [ -n "$index_pid" ]; then
		kill "$index_pid" 2>/dev/null || :
		terminate_container_index
		wait "$index_pid" 2>/dev/null || :
		index_pid=
	fi
	rm -f "$log_file"
}

terminate_container_index() {
	if [ -z "$index_run_token" ]; then
		return
	fi
	COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T -e RAG_INDEX_RUN_TOKEN="$index_run_token" rag-mcp /bin/sh -c '
		pid_file="/data/index-state/rag-index-${RAG_INDEX_RUN_TOKEN}.pid"
		pid=$(cat "$pid_file" 2>/dev/null || :)
		case "$pid" in
			""|*[!0-9]*)
				rm -f "$pid_file"
				exit 0
				;;
		esac
		kill "$pid" 2>/dev/null || :
		i=0
		while [ "$i" -lt 2 ] && kill -0 "$pid" 2>/dev/null; do
			sleep 1
			i=$(( i + 1 ))
		done
		if kill -0 "$pid" 2>/dev/null; then
			kill -KILL "$pid" 2>/dev/null || :
		fi
		rm -f "$pid_file"
	' >/dev/null 2>&1 || :
}

clear_progress_line() {
	if [ "$supports_progress" -eq 1 ]; then
		printf '\r%120s\r' ' ' >&2
	fi
}

progress_json_progress_number() {
	key=$1
	awk -v key="\"$key\"" '
		index($0, "\"progress\"") {
			in_progress = 1
			next
		}
		in_progress && index($0, "}") {
			exit
		}
		in_progress && index($0, key) {
			value = $0
			sub(/^[^:]*:[[:space:]]*/, "", value)
			sub(/,.*/, "", value)
			gsub(/[^0-9-]/, "", value)
			print value
			exit
		}
	'
}

progress_json_string() {
	key=$1
	awk -v key="\"$key\"" '
		index($0, key) {
			value = $0
			sub(/^[^:]*:[[:space:]]*"/, "", value)
			sub(/".*/, "", value)
			print value
			exit
		}
	'
}

build_progress_bar() {
	filled=$1
	bar=
	i=0
	while [ "$i" -lt "$bar_width" ]; do
		if [ "$i" -lt "$filled" ]; then
			bar="${bar}#"
		else
			bar="${bar}."
		fi
		i=$(( i + 1 ))
	done
	printf '%s' "$bar"
}

render_progress() {
	elapsed=$(( $(date +%s) - start_epoch ))
	status_json=$(COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T rag-mcp /app/rag-index --status 2>/dev/null || :)
	status_value=$(printf '%s\n' "$status_json" | progress_json_string status)
	progress_line=

	if [ "$status_value" = "running" ]; then
		total_documents=$(printf '%s\n' "$status_json" | progress_json_progress_number total_documents)
		processed_documents=$(printf '%s\n' "$status_json" | progress_json_progress_number processed_documents)
		case "$total_documents" in
			''|*[!0-9]*)
				total_documents=
				;;
		esac
		case "$processed_documents" in
			''|*[!0-9]*)
				processed_documents=
				;;
		esac
		if [ -n "$total_documents" ] && [ -n "$processed_documents" ]; then
			if [ "$processed_documents" -gt "$total_documents" ]; then
				processed_documents=$total_documents
			fi
			if [ "$total_documents" -gt 0 ]; then
				percent=$(( processed_documents * 100 / total_documents ))
				filled=$(( processed_documents * bar_width / total_documents ))
			else
				percent=0
				filled=0
			fi
			bar=$(build_progress_bar "$filled")
			progress_line=$(printf 'indexing [%s] %3d%% %s/%s docs %ss' "$bar" "$percent" "$processed_documents" "$total_documents" "$elapsed")
		fi
	fi

	if [ -z "$progress_line" ]; then
		bar=$(build_progress_bar 0)
		progress_line=$(printf 'indexing [%s] waiting for document count %ss' "$bar" "$elapsed")
	fi

	if [ "$supports_progress" -eq 1 ]; then
		clear_progress_line
		printf '%s' "$progress_line" >&2
	else
		printf '%s\n' "$progress_line" >&2
	fi
}

start_progress() {
	bar_width=28
	start_epoch=$(date +%s)
	while :; do
		render_progress
		sleep "$progress_interval"
	done
}

trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

if [ "$fresh_index" -eq 1 ]; then
	printf '%s\n' 'index: FRESH_INDEX=1 requested; resetting the configured Chroma collection before rebuild.' >&2
fi

COMPOSE_FILE="$compose_file" docker compose --project-directory "$compose_project_dir" exec -T -e FRESH_INDEX="$fresh_index" -e RAG_INDEX_RUN_TOKEN="$index_run_token" rag-mcp /bin/sh -c '
	pid_file="/data/index-state/rag-index-${RAG_INDEX_RUN_TOKEN}.pid"
	mkdir -p /data/index-state
	cleanup() {
		if [ -n "${child_pid:-}" ]; then
			kill "$child_pid" 2>/dev/null || :
			wait "$child_pid" 2>/dev/null || :
		fi
		rm -f "$pid_file"
	}
	trap "cleanup; exit 130" INT TERM HUP
	/app/rag-index --output "$1" &
	child_pid=$!
	printf "%s\n" "$child_pid" >"$pid_file"
	wait "$child_pid"
	status=$?
	rm -f "$pid_file"
	exit "$status"
' sh "$output" >"$log_file" 2>&1 &
index_pid=$!

start_progress &
progress_pid=$!

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
if [ "$index_status" -eq 130 ] || [ "$index_status" -eq 143 ]; then
	terminate_container_index
fi
clear_progress_line

if [ "$index_status" -eq 0 ]; then
	if [ -s "$log_file" ]; then
		cat "$log_file"
	fi
	exit 0
fi

if [ -s "$log_file" ] && grep -F 'flag provided but not defined: -output' "$log_file" >/dev/null 2>&1; then
	printf '%s\n' 'index: running rag-mcp container does not support OUTPUT modes; it was built before this checkout.' >&2
	printf '%s\n' 'index: rebuild the stack with make up, then rerun make index.' >&2
	exit 1
fi

printf 'index: failed with exit code %s\n' "$index_status" >&2
if [ -s "$log_file" ]; then
	cat "$log_file" >&2
fi
exit "$index_status"
