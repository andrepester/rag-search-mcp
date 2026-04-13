#!/bin/sh
set -eu

. ./shell/lib.sh

build_go_runner_image
runner_bin=$(go_runner_bin)
runner_bindir=$(go_runner_bindir)
runner_path_prefix=''
if [ -n "$runner_bindir" ]; then
	runner_path_prefix="$runner_bindir:"
fi
run_go_runner sh -lc "set -eu; PATH=\"$runner_path_prefix\$PATH\"; toolbin=/tmp/bin; mkdir -p \"\$toolbin\"; GOBIN=\"\$toolbin\" $runner_bin install github.com/google/go-licenses@v1.6.0; \"\$toolbin\"/go-licenses report ./... > licenses.csv"
