#!/bin/sh
set -eu

. ./shell/lib.sh

: "${GO_IMAGE:=golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447}"
: "${GO_BIN:=/usr/local/go/bin/go}"
compose_project_dir=${COMPOSE_PROJECT_DIR:-.}
compose_file=${COMPOSE_FILE:-docker/docker-compose.yml}

full_reset_raw=${FULL_RESET-0}
is_full_reset=$(parse_bool_01 "$full_reset_raw" 0) || {
	printf '%s\n' 'FULL_RESET must be one of: 0,1,true,false,yes,no' >&2
	exit 2
}

skip_down_raw=${CLEAN_INSTALL_SKIP_DOWN-0}
skip_down=$(parse_bool_01 "$skip_down_raw" 0) || {
	printf '%s\n' 'CLEAN_INSTALL_SKIP_DOWN must be one of: 0,1,true,false,yes,no' >&2
	exit 2
}

skip_install_raw=${CLEAN_INSTALL_SKIP_INSTALL-0}
skip_install=$(parse_bool_01 "$skip_install_raw" 0) || {
	printf '%s\n' 'CLEAN_INSTALL_SKIP_INSTALL must be one of: 0,1,true,false,yes,no' >&2
	exit 2
}

if [ "$is_full_reset" -eq 1 ]; then
	repo_root=$(pwd -P)
	repo_parent=$(dirname "$repo_root")
	home_dir=${HOME-}

	index_dir=$(resolve_host_override HOST_INDEX_DIR ./data/index)
	models_dir=$(resolve_host_override HOST_MODELS_DIR ./data/models)
	index_abs=$(to_abs_path "$repo_root" "$index_dir")
	models_abs=$(to_abs_path "$repo_root" "$models_dir")

	assert_safe_reset_dir() {
		dir="$1"
		label="$2"
		if [ -z "$dir" ]; then
			printf '%s\n' "FULL_RESET refused: $label resolved to empty path" >&2
			exit 3
		fi
		case "$dir" in
			/|.)
				printf '%s\n' "FULL_RESET refused: unsafe $label path '$dir'" >&2
				exit 3
				;;
		esac
		if [ "$dir" = "$repo_root" ] || [ "$dir" = "$repo_parent" ]; then
			printf '%s\n' "FULL_RESET refused: $label points to repo/root-adjacent path '$dir'" >&2
			exit 3
		fi
		if [ -n "$home_dir" ] && [ "$dir" = "$home_dir" ]; then
			printf '%s\n' "FULL_RESET refused: $label points to HOME '$dir'" >&2
			exit 3
		fi
		case "$repo_root/" in
			"$dir"/*)
				printf '%s\n' "FULL_RESET refused: $label '$dir' is ancestor of repo '$repo_root'" >&2
				exit 3
				;;
		esac
		depth=$(printf '%s' "$dir" | tr -cd '/' | wc -c | tr -d '[:space:]')
		if [ "$depth" -lt 3 ]; then
			case "$dir" in
				/tmp/*|/mnt/*) ;;
				*)
					printf '%s\n' "FULL_RESET refused: $label '$dir' is too broad (depth $depth)" >&2
					exit 3
					;;
			esac
		fi
	}

	assert_safe_reset_dir "$index_abs" HOST_INDEX_DIR
	assert_safe_reset_dir "$models_abs" HOST_MODELS_DIR

	printf 'FULL_RESET=1: removing persistent runtime paths\n  - %s\n  - %s\n' "$index_abs" "$models_abs"
	if [ "$skip_down" -eq 0 ]; then
		docker compose --project-directory "$compose_project_dir" -f "$compose_file" down --remove-orphans
	fi
	rm -rf "$index_abs" "$models_abs"
else
	printf '%s\n' 'Safe clean-install: preserving HOST_INDEX_DIR and HOST_MODELS_DIR (set FULL_RESET=1 to wipe).'
	if [ "$skip_down" -eq 0 ]; then
		docker compose --project-directory "$compose_project_dir" -f "$compose_file" down --remove-orphans
	fi
fi

if [ "$skip_install" -eq 0 ]; then
	GO_IMAGE="$GO_IMAGE" GO_BIN="$GO_BIN" COMPOSE_PROJECT_DIR="$compose_project_dir" COMPOSE_FILE="$compose_file" sh ./shell/install.sh
fi
