#!/bin/sh

go_runner_image() {
	if is_non_empty_non_ws "${GO_IMAGE-}"; then
		printf '%s' "$GO_IMAGE"
		return 0
	fi
	printf '%s' "${GO_RUNNER_IMAGE:-rag-search-mcp-go-runner:local}"
}

go_runner_bin() {
	printf '%s' "${GO_BIN:-/usr/local/go/bin/go}"
}

go_runner_bindir() {
	runner_bin=$(go_runner_bin)
	case "$runner_bin" in
		*/*)
			printf '%s' "${runner_bin%/*}"
			;;
		*)
			printf ''
			;;
	esac
}

go_runner_gofmt_bin() {
	runner_bindir=$(go_runner_bindir)
	if [ -n "$runner_bindir" ]; then
		printf '%s/gofmt' "$runner_bindir"
		return 0
	fi
	printf '%s' 'gofmt'
}

build_go_runner_image() {
	if is_non_empty_non_ws "${GO_IMAGE-}"; then
		return 0
	fi
	dockerfile_path=${DOCKERFILE_PATH:-docker/Dockerfile}
	runner_target=${GO_RUNNER_TARGET:-go-runner}
	runner_image=$(go_runner_image)
	docker build -f "$dockerfile_path" --target "$runner_target" -t "$runner_image" .
}

run_go_runner() {
	runner_image=$(go_runner_image)
	docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOCACHE=/tmp/go-build -v "$(pwd):/workspace" -w /workspace "$runner_image" "$@"
}

run_go_command() {
	build_go_runner_image
	runner_bin=$(go_runner_bin)
	run_go_runner "$runner_bin" "$@"
}

is_non_empty_non_ws() {
	value="$1"
	non_ws=$(printf '%s' "$value" | tr -d '[:space:]')
	[ -n "$non_ws" ]
}

trim_env_token() {
	value="$1"
	value="${value#"${value%%[![:space:]]*}"}"
	value="${value%"${value##*[![:space:]]}"}"
	value="${value#\"}"
	value="${value%\"}"
	value="${value#\'}"
	value="${value%\'}"
	printf '%s' "$value"
}

resolve_host_override() {
	key="$1"
	default_value="$2"
	eval "value=\${$key-}"
	if is_non_empty_non_ws "$value"; then
		printf '%s' "$value"
		return 0
	fi

	if [ -f .env ]; then
		while IFS= read -r line || [ -n "$line" ]; do
			trimmed="${line#"${line%%[![:space:]]*}"}"
			case "$trimmed" in
				''|\#*) continue ;;
			esac
			case "$trimmed" in
				*=*) ;;
				*) continue ;;
			esac
			entry_key="${trimmed%%=*}"
			entry_key="${entry_key%"${entry_key##*[![:space:]]}"}"
			if [ "$entry_key" != "$key" ]; then
				continue
			fi
			entry_value="${trimmed#*=}"
			entry_value=$(trim_env_token "$entry_value")
			if is_non_empty_non_ws "$entry_value"; then
				printf '%s' "$entry_value"
				return 0
			fi
			done < .env
	fi

	printf '%s' "$default_value"
}

ensure_abs_dir() {
	repo_root="$1"
	input_path="$2"
	case "$input_path" in
		/*) target="$input_path" ;;
		*) target="$repo_root/$input_path" ;;
	esac
	mkdir -p "$target"
	(
		cd "$target"
		pwd -P
	)
}

upsert_env_value() {
	key="$1"
	value="$2"
	tmp_file=$(mktemp .env.upsert.XXXXXX)
	found=0

	if [ -f .env ]; then
		while IFS= read -r line || [ -n "$line" ]; do
			trimmed="${line#"${line%%[![:space:]]*}"}"
			case "$trimmed" in
				''|\#*)
					printf '%s\n' "$line" >> "$tmp_file"
					continue
					;;
			esac
			case "$trimmed" in
				*=*) ;;
				*)
					printf '%s\n' "$line" >> "$tmp_file"
					continue
					;;
			esac
			entry_key="${trimmed%%=*}"
			entry_key="${entry_key%"${entry_key##*[![:space:]]}"}"
			if [ "$entry_key" = "$key" ]; then
				printf '%s=%s\n' "$key" "$value" >> "$tmp_file"
				found=1
			else
				printf '%s\n' "$line" >> "$tmp_file"
			fi
		done < .env
	fi

	if [ "$found" -eq 0 ]; then
		printf '%s=%s\n' "$key" "$value" >> "$tmp_file"
	fi

	mv "$tmp_file" .env
	chmod 600 .env
}

to_abs_path() {
	repo_root="$1"
	value="$2"
	case "$value" in
		/*) target="$value" ;;
		*) target="$repo_root/$value" ;;
	esac
	target="${target%/}"
	if [ -z "$target" ]; then
		printf '/'
		return 0
	fi
	dir_part="${target%/*}"
	base_part="${target##*/}"
	if [ "$base_part" = "." ] || [ "$base_part" = ".." ]; then
		printf '%s\n' "cannot resolve terminal path segment '$base_part' in '$target'" >&2
		return 1
	fi
	if [ -z "$dir_part" ]; then
		dir_part="/"
	fi
	mkdir -p "$dir_part"
	dir_abs=$(cd "$dir_part" && pwd -P)
	printf '%s/%s' "$dir_abs" "$base_part"
}

parse_bool_01() {
	value="$1"
	default_value="$2"
	normalized=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')
	if [ -z "$normalized" ]; then
		printf '%s' "$default_value"
		return 0
	fi
	case "$normalized" in
		1|true|yes) printf '1' ;;
		0|false|no) printf '0' ;;
		*) return 1 ;;
	esac
}
