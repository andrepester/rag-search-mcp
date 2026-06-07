#!/bin/sh
set -eu

smoke_root=.host-path-resolver-smoke
backup_dir=$(mktemp -d .host-path-resolver-smoke-backup.XXXXXX)
had_env=0
restored=0

if [ -f .env ]; then
	cp .env "$backup_dir/.env"
	had_env=1
fi

restore() {
	if [ "$restored" -eq 1 ]; then
		return
	fi
	restored=1
	rm -rf "$smoke_root"
	if [ "$had_env" -eq 1 ] && [ -f "$backup_dir/.env" ]; then
		cp "$backup_dir/.env" .env
	else
		rm -f .env
	fi
	rm -rf "$backup_dir"
}

trap 'status=$?; restore; exit $status' 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

. ./shell/lib.sh

repo_root=$(pwd -P)

fail() {
	printf '%s\n' "host-path-resolver smoke: $*" >&2
	exit 1
}

assert_eq() {
	label="$1"
	expected="$2"
	actual="$3"
	if [ "$actual" != "$expected" ]; then
		printf '%s\n' "host-path-resolver smoke: $label" >&2
		printf '  expected: %s\n' "$expected" >&2
		printf '  actual:   %s\n' "$actual" >&2
		exit 1
	fi
}

assert_file_contains() {
	label="$1"
	file="$2"
	needle="$3"
	if ! grep -Fq "$needle" "$file"; then
		fail "$label: expected $file to contain: $needle"
	fi
}

assert_file_not_contains() {
	label="$1"
	file="$2"
	needle="$3"
	if grep -Fq "$needle" "$file"; then
		fail "$label: expected $file not to contain: $needle"
	fi
}

clear_host_env() {
	for key in $(host_path_keys); do
		unset "$key"
	done
}

set_host_var() {
	key="$1"
	value="$2"
	host_path_default "$key" > /dev/null || fail "cannot set unknown host path key $key"
	export "$key=$value"
}

set_host_vars_to_fixture() {
	prefix="$1"
	for key in $(host_path_keys); do
		set_host_var "$key" "$(expected_fixture_for_key "$key" "$prefix")"
	done
}

write_env_file() {
	rm -f .env
	for line do
		printf '%s\n' "$line" >> .env
	done
}

write_env_fixture_values() {
	prefix="$1"
	rm -f .env
	for key in $(host_path_keys); do
		printf '%s=%s\n' "$key" "$(expected_fixture_for_key "$key" "$prefix")" >> .env
	done
}

expected_default_for_key() {
	key="$1"
	case "$key" in
		HOST_DOCS_DIR) printf '%s' './data/docs' ;;
		HOST_CODE_DIR) printf '%s' './data/code' ;;
		HOST_INDEX_DIR) printf '%s' './data/index' ;;
		HOST_MODELS_DIR) printf '%s' './data/models' ;;
		*) fail "no expected default for $key" ;;
	esac
}

expected_fixture_for_key() {
	key="$1"
	prefix="$2"
	case "$key" in
		HOST_DOCS_DIR) printf './%s/%s-docs' "$smoke_root" "$prefix" ;;
		HOST_CODE_DIR) printf './%s/%s-code' "$smoke_root" "$prefix" ;;
		HOST_INDEX_DIR) printf './%s/%s-index' "$smoke_root" "$prefix" ;;
		HOST_MODELS_DIR) printf './%s/%s-models' "$smoke_root" "$prefix" ;;
		*) fail "no expected fixture for $key" ;;
	esac
}

compose_mount_for_key() {
	key="$1"
	case "$key" in
		HOST_DOCS_DIR) printf '%s' '${HOST_DOCS_DIR:-./data/docs}:/data/docs:ro' ;;
		HOST_CODE_DIR) printf '%s' '${HOST_CODE_DIR:-./data/code}:/data/code:ro' ;;
		HOST_INDEX_DIR) printf '%s' '${HOST_INDEX_DIR:-./data/index}:/data' ;;
		HOST_MODELS_DIR) printf '%s' '${HOST_MODELS_DIR:-./data/models}:/root/.ollama/models' ;;
		*) fail "no compose mount fixture for $key" ;;
	esac
}

bootstrap_default_const_for_key() {
	key="$1"
	case "$key" in
		HOST_DOCS_DIR) printf '%s' 'hostDocsDir       = "./data/docs"' ;;
		HOST_CODE_DIR) printf '%s' 'hostCodeDir       = "./data/code"' ;;
		HOST_INDEX_DIR) printf '%s' 'hostIndexDir      = "./data/index"' ;;
		HOST_MODELS_DIR) printf '%s' 'hostModelsDir     = "./data/models"' ;;
		*) fail "no bootstrap default fixture for $key" ;;
	esac
}

bootstrap_key_const_for_key() {
	key="$1"
	case "$key" in
		HOST_DOCS_DIR) printf '%s' 'hostDocsEnvKey    = "HOST_DOCS_DIR"' ;;
		HOST_CODE_DIR) printf '%s' 'hostCodeEnvKey    = "HOST_CODE_DIR"' ;;
		HOST_INDEX_DIR) printf '%s' 'hostIndexEnvKey   = "HOST_INDEX_DIR"' ;;
		HOST_MODELS_DIR) printf '%s' 'hostModelsEnvKey  = "HOST_MODELS_DIR"' ;;
		*) fail "no bootstrap key fixture for $key" ;;
	esac
}

expect_resolve_host_path() {
	label="$1"
	key="$2"
	expected="$3"
	actual=$(resolve_host_path "$key") || fail "$label: resolve_host_path failed for $key"
	assert_eq "$label ($key)" "$expected" "$actual"
}

expect_all_resolved_to_defaults() {
	label="$1"
	for key in $(host_path_keys); do
		expect_resolve_host_path "$label" "$key" "$(expected_default_for_key "$key")"
	done
}

expect_all_resolved_to_fixture() {
	label="$1"
	prefix="$2"
	for key in $(host_path_keys); do
		expect_resolve_host_path "$label" "$key" "$(expected_fixture_for_key "$key" "$prefix")"
	done
}

assert_host_key_project_surface() {
	key="$1"
	default_value=$(expected_default_for_key "$key")

	assert_file_contains ".env.example defines $key default" .env.example "$key=$default_value"
	assert_file_contains "docker-compose exposes $key mount default" docker/docker-compose.yml "$(compose_mount_for_key "$key")"
	assert_file_contains "bootstrap defines $key default" internal/bootstrap/bootstrap.go "$(bootstrap_default_const_for_key "$key")"
	assert_file_contains "bootstrap defines $key env key" internal/bootstrap/bootstrap.go "$(bootstrap_key_const_for_key "$key")"
	assert_file_contains "configdoctor hostPathKeys includes $key" internal/configdoctor/configdoctor.go "\"$key\","
}

expect_host_path_abs() {
	label="$1"
	key="$2"
	expected="$3"
	actual=$(host_path_abs "$repo_root" "$key") || fail "$label: host_path_abs failed for $key"
	assert_eq "$label ($key)" "$expected" "$actual"
}

expect_ensure_host_path_abs_dir() {
	label="$1"
	key="$2"
	expected="$3"
	actual=$(ensure_host_path_abs_dir "$repo_root" "$key") || fail "$label: ensure_host_path_abs_dir failed for $key"
	assert_eq "$label ($key)" "$expected" "$actual"
	if [ ! -d "$actual" ]; then
		fail "$label ($key): expected directory to exist: $actual"
	fi
}

reset_smoke_state() {
	clear_host_env
	rm -f .env
	rm -rf "$smoke_root"
	mkdir -p "$smoke_root"
}

test_resolve_defaults() {
	reset_smoke_state
	expect_all_resolved_to_defaults "default fallback without process env or .env"
}

test_resolve_env_file_values() {
	reset_smoke_state
	write_env_fixture_values env-file
	expect_all_resolved_to_fixture ".env value before default" env-file
}

test_resolve_process_env_values() {
	reset_smoke_state
	write_env_fixture_values env-file
	set_host_vars_to_fixture process
	expect_all_resolved_to_fixture "process env before .env" process
}

test_resolve_empty_values_fall_back() {
	reset_smoke_state
	write_env_fixture_values env-file
	HOST_DOCS_DIR=
	HOST_CODE_DIR='   '
	HOST_INDEX_DIR=
	HOST_MODELS_DIR='   '
	export HOST_DOCS_DIR HOST_CODE_DIR HOST_INDEX_DIR HOST_MODELS_DIR

	expect_all_resolved_to_fixture "empty or whitespace process env falls back to .env" env-file

	clear_host_env
	write_env_file \
		'HOST_DOCS_DIR=' \
		'HOST_CODE_DIR=   ' \
		"HOST_INDEX_DIR='   '" \
		'HOST_MODELS_DIR=""'
	expect_all_resolved_to_defaults "empty or whitespace .env falls back to default"
}

test_resolve_env_file_parsing() {
	reset_smoke_state
	write_env_file \
		'  # comment with leading spaces' \
		"UNKNOWN_HOST_DIR=./$smoke_root/unknown" \
		" HOST_DOCS_DIR = \"$(expected_fixture_for_key HOST_DOCS_DIR spaced)\" " \
		"HOST_CODE_DIR='$(expected_fixture_for_key HOST_CODE_DIR quoted)'" \
		"HOST_INDEX_DIR=$(expected_fixture_for_key HOST_INDEX_DIR plain)" \
		"HOST_MODELS_DIR=$(expected_fixture_for_key HOST_MODELS_DIR plain)"

	expect_resolve_host_path ".env parser trims key and double-quoted value" HOST_DOCS_DIR "$(expected_fixture_for_key HOST_DOCS_DIR spaced)"
	expect_resolve_host_path ".env parser trims single-quoted value" HOST_CODE_DIR "$(expected_fixture_for_key HOST_CODE_DIR quoted)"
	expect_resolve_host_path ".env parser accepts plain value" HOST_INDEX_DIR "$(expected_fixture_for_key HOST_INDEX_DIR plain)"
	expect_resolve_host_path ".env parser ignores comments and unknown keys" HOST_MODELS_DIR "$(expected_fixture_for_key HOST_MODELS_DIR plain)"
}

test_host_path_abs_relative_and_absolute() {
	reset_smoke_state
	for key in $(host_path_keys); do
		configured="./$smoke_root/relative/$key/target"
		expected="$repo_root/$smoke_root/relative/$key/target"
		set_host_var "$key" "$configured"
		expect_host_path_abs "relative path resolves from repo root" "$key" "$expected"
		if [ ! -d "$repo_root/$smoke_root/relative/$key" ]; then
			fail "relative path resolves from repo root ($key): expected parent directory to exist"
		fi
	done

	for key in $(host_path_keys); do
		configured="$repo_root/$smoke_root/absolute/$key/target"
		expected="$repo_root/$smoke_root/absolute/$key/target"
		set_host_var "$key" "$configured"
		expect_host_path_abs "absolute path is preserved" "$key" "$expected"
		if [ ! -d "$repo_root/$smoke_root/absolute/$key" ]; then
			fail "absolute path is preserved ($key): expected parent directory to exist"
		fi
	done
}

test_host_path_abs_rejects_terminal_dot_segments() {
	reset_smoke_state
	set_host_var HOST_INDEX_DIR "./$smoke_root/bad/."
	if host_path_abs "$repo_root" HOST_INDEX_DIR > "$smoke_root/unexpected.out" 2> "$smoke_root/error.log"; then
		fail "terminal . path should be rejected"
	fi
	assert_file_contains "terminal . error" "$smoke_root/error.log" "cannot resolve terminal path segment '.'"

	set_host_var HOST_MODELS_DIR "./$smoke_root/bad/.."
	if host_path_abs "$repo_root" HOST_MODELS_DIR > "$smoke_root/unexpected.out" 2> "$smoke_root/error.log"; then
		fail "terminal .. path should be rejected"
	fi
	assert_file_contains "terminal .. error" "$smoke_root/error.log" "cannot resolve terminal path segment '..'"
}

test_ensure_host_path_abs_dir() {
	reset_smoke_state
	for key in $(host_path_keys); do
		configured="./$smoke_root/ensure/$key"
		expected="$repo_root/$smoke_root/ensure/$key"
		set_host_var "$key" "$configured"
		expect_ensure_host_path_abs_dir "ensure creates missing target directory" "$key" "$expected"
	done

	for key in $(host_path_keys); do
		configured="./$smoke_root/existing/$key"
		expected="$repo_root/$smoke_root/existing/$key"
		mkdir -p "$expected"
		set_host_var "$key" "$configured"
		expect_ensure_host_path_abs_dir "ensure keeps existing target directory" "$key" "$expected"
	done
}

test_shared_resolver_callers_do_not_drift() {
	assert_file_contains "install-bootstrap reads docs path through shared resolver" shell/install-bootstrap.sh 'docs_value=$(resolve_host_path HOST_DOCS_DIR)'
	assert_file_contains "install-bootstrap reads code path through shared resolver" shell/install-bootstrap.sh 'code_value=$(resolve_host_path HOST_CODE_DIR)'
	assert_file_contains "install-bootstrap mounts through shared resolver" shell/install-bootstrap.sh 'resolved_abs=$(ensure_host_path_abs_dir "$host_repo" "$key")'
	assert_file_contains "clean-install resolves index path through shared resolver" shell/clean-install.sh 'index_abs=$(host_path_abs "$repo_root" HOST_INDEX_DIR)'
	assert_file_contains "clean-install resolves models path through shared resolver" shell/clean-install.sh 'models_abs=$(host_path_abs "$repo_root" HOST_MODELS_DIR)'
	assert_file_contains "config-doctor reads host paths through shared resolver" shell/config-doctor.sh 'configured=$(resolve_host_path "$key")'
	assert_file_contains "install delegates indexing through index helper" shell/install.sh 'sh ./shell/index.sh'
	assert_file_contains "doctor delegates indexing through index helper" shell/doctor.sh 'sh ./shell/index.sh'
	assert_file_contains "clean-install forces fresh index after FULL_RESET" shell/clean-install.sh 'install_fresh_index=1'
	assert_file_contains "index helper defaults to human output" shell/index.sh 'output=${OUTPUT:-human}'
	assert_file_contains "index helper passes fresh mode and output into container" shell/index.sh 'exec -T -e FRESH_INDEX="$fresh_index" rag-mcp /app/rag-index --output "$output"'
	assert_file_contains "index helper detects stale rag-index binary" shell/index.sh 'running rag-mcp container does not support OUTPUT modes'
	assert_file_not_contains "install-bootstrap must not use legacy host override directly" shell/install-bootstrap.sh 'resolve_host_override'
	assert_file_not_contains "clean-install must not use legacy host override directly" shell/clean-install.sh 'resolve_host_override'
	assert_file_not_contains "config-doctor must not use legacy host override directly" shell/config-doctor.sh 'resolve_host_override'
	if [ -f shell/reindex.sh ]; then
		fail "shell/reindex.sh should be renamed to shell/index.sh"
	fi
}

test_project_host_key_surfaces_do_not_drift() {
	for key in $(host_path_keys); do
		assert_host_key_project_surface "$key"
	done
	assert_file_contains "docker-compose mounts active generation state under HOST_INDEX_DIR" docker/docker-compose.yml '${HOST_INDEX_DIR:-./data/index}/rag-state:/data/index-state'
}

test_resolve_defaults
test_resolve_env_file_values
test_resolve_process_env_values
test_resolve_empty_values_fall_back
test_resolve_env_file_parsing
test_host_path_abs_relative_and_absolute
test_host_path_abs_rejects_terminal_dot_segments
test_ensure_host_path_abs_dir
test_shared_resolver_callers_do_not_drift
test_project_host_key_surfaces_do_not_drift

printf '%s\n' 'Host path resolver smoke passed.'
