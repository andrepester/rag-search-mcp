#!/bin/sh
set -eu

. ./shell/lib.sh

repo_root=$(pwd -P)
runner_image=$(go_runner_image)
runner_bin=$(go_runner_bin)

to_abs_path_no_create() {
	input_path="$1"
	case "$input_path" in
		/*) target="$input_path" ;;
		*) target="$repo_root/$input_path" ;;
	esac
	while [ "$target" != "/" ]; do
		case "$target" in
			*/) target="${target%/}" ;;
			*) break ;;
		esac
	done
	printf '%s' "$target"
}

canonical_existing_path() {
	input_path="$1"
	if [ -d "$input_path" ]; then
		(cd "$input_path" && pwd -P)
		return 0
	fi
	dir_part=$(dirname "$input_path")
	base_part=$(basename "$input_path")
	if [ -d "$dir_part" ]; then
		dir_abs=$(cd "$dir_part" && pwd -P)
		printf '%s/%s' "$dir_abs" "$base_part"
		return 0
	fi
	return 1
}

build_go_runner_image

set -- docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/go-build -v "$repo_root:/workspace" -w /workspace

for key in \
	RAG_HTTP_HOST \
	RAG_HTTP_PORT \
	OLLAMA_PORT \
	HOST_DOCS_DIR \
	HOST_CODE_DIR \
	HOST_INDEX_DIR \
	HOST_MODELS_DIR \
	OLLAMA_HOST \
	EMBED_MODEL \
	RAG_ENABLE_CODE_INGEST \
	RAG_CHROMA_TENANT \
	RAG_CHROMA_DATABASE \
	RAG_COLLECTION_NAME \
	RAG_SCOPE_DEFAULT \
	RAG_CHUNK_SIZE \
	RAG_CHUNK_OVERLAP \
	RAG_MAX_TOP_K \
	RAG_MAX_SEARCH_DISTANCE
do
	eval "is_set=\${$key+x}"
	if [ -n "$is_set" ]; then
		set -- "$@" -e "$key"
	fi
done

for key in $(host_path_keys); do
	configured=$(resolve_host_path "$key")
	abs_path=$(to_abs_path_no_create "$configured")
	if [ -e "$abs_path" ]; then
		canonical=$(canonical_existing_path "$abs_path") || continue
		set -- "$@" -v "$canonical:$canonical:ro"
	fi
done

"$@" "$runner_image" "$runner_bin" run ./cmd/rag-config-doctor --repo-root /workspace --host-repo-root "$repo_root" --host-home "${HOME-}"
