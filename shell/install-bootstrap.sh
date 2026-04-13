#!/bin/sh
set -eu

. ./shell/lib.sh

: "${GO_IMAGE:=golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447}"
: "${GO_BIN:=/usr/local/go/bin/go}"

host_repo=$(pwd -P)
host_parent=$(dirname "$host_repo")
repo_name=$(basename "$host_repo")

docs_value=$(resolve_host_override HOST_DOCS_DIR ./data/docs)
code_value=$(resolve_host_override HOST_CODE_DIR ./data/code)
persist_source_dirs=0
force_interactive_raw=${INSTALL_BOOTSTRAP_FORCE_INTERACTIVE-}
force_interactive=$(parse_bool_01 "$force_interactive_raw" 0) || {
	printf '%s\n' "invalid INSTALL_BOOTSTRAP_FORCE_INTERACTIVE '$force_interactive_raw' (expected 1/0/true/false/yes/no)" >&2
	exit 2
}

if [ ! -f .env ]; then
	cp .env.example .env
	chmod 600 .env
fi

if [ "$force_interactive" -eq 1 ] || { [ -t 0 ] && [ -t 1 ]; }; then
	persist_source_dirs=1
	printf '%s\n' "Current HOST_DOCS_DIR=$docs_value"
	printf '%s\n' "Current HOST_CODE_DIR=$code_value"
	printf '%s' 'Keep current [K], use standard (./data/docs + ./data/code) [s], or enter custom [c]? [K/s/c]: '
	IFS= read -r selection || true
	selection_normalized=$(printf '%s' "$selection" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
	case "$selection_normalized" in
		''|k|keep)
			;;
		y|yes|s|standard)
			docs_value=./data/docs
			code_value=./data/code
			;;
		n|no|c|custom)
			printf '%s' 'Enter HOST_DOCS_DIR: '
			IFS= read -r custom_docs
			if ! is_non_empty_non_ws "$custom_docs"; then
				printf '%s\n' 'HOST_DOCS_DIR must not be empty' >&2
				exit 2
			fi
			printf '%s' 'Enter HOST_CODE_DIR: '
			IFS= read -r custom_code
			if ! is_non_empty_non_ws "$custom_code"; then
				printf '%s\n' 'HOST_CODE_DIR must not be empty' >&2
				exit 2
			fi
			docs_value="$custom_docs"
			code_value="$custom_code"
			;;
		*)
			printf '%s\n' "invalid selection '$selection', expected keep/standard/custom" >&2
			exit 2
			;;
	esac
fi

if [ "$persist_source_dirs" -eq 1 ]; then
	upsert_env_value HOST_DOCS_DIR "$docs_value"
	upsert_env_value HOST_CODE_DIR "$code_value"
fi

set -- docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e RAG_HTTP_PORT -e HOST_DOCS_DIR -e HOST_CODE_DIR -e HOST_INDEX_DIR -e HOST_MODELS_DIR -v "$host_parent:/workspace-parent" -w "/workspace-parent/$repo_name"
for key in HOST_DOCS_DIR HOST_CODE_DIR HOST_INDEX_DIR HOST_MODELS_DIR; do
	resolved=$(resolve_host_override "$key" "")
	if [ -n "$resolved" ]; then
		resolved_abs=$(ensure_abs_dir "$host_repo" "$resolved")
		set -- "$@" -e "$key=$resolved_abs" -v "$resolved_abs:$resolved_abs"
	fi
done

"$@" "$GO_IMAGE" "$GO_BIN" run ./cmd/rag-install --repo-root "/workspace-parent/$repo_name"
