#!/bin/sh
set -eu

backup_dir=$(mktemp -d .bootstrap-smoke-backup.XXXXXX)
alongside_root=''
absolute_root=''
clean_install_tmp=''
had_env=0
had_config=0
had_config_invalid=0
had_smoke_override=0
restored=0

if [ -f .env ]; then
	cp .env "$backup_dir/.env"
	had_env=1
fi
if [ -f opencode.json ]; then
	cp opencode.json "$backup_dir/opencode.json"
	had_config=1
fi
if [ -f opencode.json.invalid ]; then
	cp opencode.json.invalid "$backup_dir/opencode.json.invalid"
	had_config_invalid=1
fi
if [ -e .smoke-override ]; then
	cp -R .smoke-override "$backup_dir/.smoke-override"
	had_smoke_override=1
fi

restore() {
	if [ "$restored" -eq 1 ]; then
		return
	fi
	restored=1
	rm -rf .smoke-override
	if [ "$had_smoke_override" -eq 1 ] && [ -e "$backup_dir/.smoke-override" ]; then
		cp -R "$backup_dir/.smoke-override" .smoke-override
	fi
	if [ -n "$alongside_root" ] && [ -d "$alongside_root" ]; then
		rm -rf "$alongside_root"
	fi
	if [ -n "$absolute_root" ] && [ -d "$absolute_root" ]; then
		rm -rf "$absolute_root"
	fi
	if [ -n "$clean_install_tmp" ] && [ -d "$clean_install_tmp" ]; then
		rm -rf "$clean_install_tmp"
	fi
	if [ "$had_env" -eq 1 ] && [ -f "$backup_dir/.env" ]; then
		cp "$backup_dir/.env" .env
	else
		rm -f .env
	fi
	if [ "$had_config" -eq 1 ] && [ -f "$backup_dir/opencode.json" ]; then
		cp "$backup_dir/opencode.json" opencode.json
	else
		rm -f opencode.json
	fi
	if [ "$had_config_invalid" -eq 1 ] && [ -f "$backup_dir/opencode.json.invalid" ]; then
		cp "$backup_dir/opencode.json.invalid" opencode.json.invalid
	else
		rm -f opencode.json.invalid
	fi
	rm -rf "$backup_dir"
}

trap 'status=$?; restore; exit $status' 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

rm -f .env opencode.json opencode.json.invalid
rm -rf .smoke-override

sh ./shell/host-path-resolver-smoke.sh

expect_env_line() {
	label="$1"
	key="$2"
	value="$3"
	if ! grep -Fxq "$key=$value" .env; then
		printf '%s\n' "bootstrap smoke: expected $label in .env" >&2
		printf '  expected: %s=%s\n' "$key" "$value" >&2
		exit 1
	fi
}

HOST_DOCS_DIR= HOST_CODE_DIR= HOST_INDEX_DIR= HOST_MODELS_DIR= sh ./shell/install-bootstrap.sh </dev/null
test -f .env
test -f opencode.json

interactive_docs=./.smoke-override/interactive-docs
interactive_code=./.smoke-override/interactive-code
printf 'c\n%s\n%s\n' "$interactive_docs" "$interactive_code" | INSTALL_BOOTSTRAP_FORCE_INTERACTIVE=1 HOST_DOCS_DIR= HOST_CODE_DIR= HOST_INDEX_DIR= HOST_MODELS_DIR= sh ./shell/install-bootstrap.sh
test -d "$interactive_docs"
test -d "$interactive_code"
expect_env_line 'interactive custom docs value' HOST_DOCS_DIR "$interactive_docs"
expect_env_line 'interactive custom code value' HOST_CODE_DIR "$interactive_code"

if printf 'maybe\n' | INSTALL_BOOTSTRAP_FORCE_INTERACTIVE=1 HOST_DOCS_DIR= HOST_CODE_DIR= HOST_INDEX_DIR= HOST_MODELS_DIR= sh ./shell/install-bootstrap.sh; then
	printf '%s\n' 'interactive smoke: expected invalid selection to fail' >&2
	exit 1
fi

keep_docs=./.smoke-override/keep-docs
keep_code=./.smoke-override/keep-code
printf 'c\n%s\n%s\n' "$keep_docs" "$keep_code" | INSTALL_BOOTSTRAP_FORCE_INTERACTIVE=1 HOST_DOCS_DIR= HOST_CODE_DIR= HOST_INDEX_DIR= HOST_MODELS_DIR= sh ./shell/install-bootstrap.sh
printf '\n' | INSTALL_BOOTSTRAP_FORCE_INTERACTIVE=1 HOST_DOCS_DIR= HOST_CODE_DIR= HOST_INDEX_DIR= HOST_MODELS_DIR= sh ./shell/install-bootstrap.sh
expect_env_line 'Enter to keep existing docs value' HOST_DOCS_DIR "$keep_docs"
expect_env_line 'Enter to keep existing code value' HOST_CODE_DIR "$keep_code"

runtime_docs=./.smoke-override/runtime-docs
runtime_code=./.smoke-override/runtime-code
printf '\n' | INSTALL_BOOTSTRAP_FORCE_INTERACTIVE=1 HOST_DOCS_DIR="$runtime_docs" HOST_CODE_DIR="$runtime_code" HOST_INDEX_DIR= HOST_MODELS_DIR= sh ./shell/install-bootstrap.sh
expect_env_line 'runtime docs override persisted on Enter' HOST_DOCS_DIR "$runtime_docs"
expect_env_line 'runtime code override persisted on Enter' HOST_CODE_DIR "$runtime_code"

HOST_DOCS_DIR=./.smoke-override/docs HOST_CODE_DIR=./.smoke-override/code HOST_INDEX_DIR=./.smoke-override/index HOST_MODELS_DIR=./.smoke-override/models sh ./shell/install-bootstrap.sh </dev/null
test -d ./.smoke-override/docs
test -d ./.smoke-override/code
test -d ./.smoke-override/index
test -d ./.smoke-override/models

repo_root_abs=$(pwd -P)
home_dir=${HOME-}
host_parent=$(dirname "$repo_root_abs")
absolute_root=$(mktemp -d "$host_parent/.bootstrap-smoke-absolute.XXXXXX")
HOST_DOCS_DIR="$absolute_root/docs" HOST_CODE_DIR="$absolute_root/code" HOST_INDEX_DIR="$absolute_root/index" HOST_MODELS_DIR="$absolute_root/models" sh ./shell/install-bootstrap.sh </dev/null
test -d "$absolute_root/docs"
test -d "$absolute_root/code"
test -d "$absolute_root/index"
test -d "$absolute_root/models"

alongside_root=$(mktemp -d "$host_parent/.bootstrap-smoke-alongside.XXXXXX")
alongside_name=$(basename "$alongside_root")
HOST_DOCS_DIR="../$alongside_name/docs" HOST_CODE_DIR="../$alongside_name/code" HOST_INDEX_DIR="../$alongside_name/index" HOST_MODELS_DIR="../$alongside_name/models" sh ./shell/install-bootstrap.sh </dev/null
test -d "$alongside_root/docs"
test -d "$alongside_root/code"
test -d "$alongside_root/index"
test -d "$alongside_root/models"

expect_clean_install_refused() {
	label="$1"
	shift
	output=$(env "$@" FULL_RESET=1 CLEAN_INSTALL_SKIP_DOWN=1 CLEAN_INSTALL_SKIP_INSTALL=1 sh ./shell/clean-install.sh 2>&1) && status=0 || status=$?
	if [ "$status" -eq 0 ]; then
		printf '%s\n' "clean-install smoke: expected $label to be refused" >&2
		exit 1
	fi
	if ! printf '%s\n' "$output" | grep -Fq 'FULL_RESET refused:'; then
		printf '%s\n%s\n' "clean-install smoke: expected $label to fail in FULL_RESET safety checks" "$output" >&2
		exit 1
	fi
}

clean_install_tmp=".clean-install-smoke"
rm -rf "$clean_install_tmp"
clean_install_rm_stub_dir="$clean_install_tmp/stub-bin"
mkdir -p "$clean_install_rm_stub_dir"
{
	printf '%s\n' '#!/bin/sh'
	printf '%s\n' "printf '%s\n' 'clean-install smoke: rm stub called unexpectedly' >&2"
	printf '%s\n' 'exit 64'
} > "$clean_install_rm_stub_dir/rm"
chmod +x "$clean_install_rm_stub_dir/rm"

HOST_INDEX_DIR="./$clean_install_tmp/deep/new/index" HOST_MODELS_DIR="./$clean_install_tmp/deep/new/models" FULL_RESET=1 CLEAN_INSTALL_SKIP_DOWN=1 CLEAN_INSTALL_SKIP_INSTALL=1 sh ./shell/clean-install.sh
test -d "$clean_install_tmp/deep/new"
test ! -e "$clean_install_tmp/deep/new/index"
test ! -e "$clean_install_tmp/deep/new/models"

clean_install_stub_path="PATH=$repo_root_abs/$clean_install_rm_stub_dir:$PATH"
clean_install_safe_models="./$clean_install_tmp/refusal/models"
expect_clean_install_refused 'root path' "$clean_install_stub_path" 'HOST_INDEX_DIR=/' "HOST_MODELS_DIR=$clean_install_safe_models"
expect_clean_install_refused 'repo root path' "$clean_install_stub_path" "HOST_INDEX_DIR=$repo_root_abs" "HOST_MODELS_DIR=$clean_install_safe_models"
expect_clean_install_refused 'repo parent path' "$clean_install_stub_path" "HOST_INDEX_DIR=$host_parent" "HOST_MODELS_DIR=$clean_install_safe_models"
if [ -n "$home_dir" ]; then
	expect_clean_install_refused 'HOME path' "$clean_install_stub_path" "HOST_INDEX_DIR=$home_dir" "HOST_MODELS_DIR=$clean_install_safe_models"
fi
expect_clean_install_refused 'broad absolute path' "$clean_install_stub_path" 'HOST_INDEX_DIR=/usr' "HOST_MODELS_DIR=$clean_install_safe_models"
