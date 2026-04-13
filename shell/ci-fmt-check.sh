#!/bin/sh
set -eu

. ./shell/lib.sh

build_go_runner_image
runner_gofmt_bin=$(go_runner_gofmt_bin)
run_go_runner sh -lc "set -eu; out=\"\$($runner_gofmt_bin -l .)\"; if [ -n \"\$out\" ]; then printf '%s\n' 'Go files are not formatted:' >&2; printf '%s\n' \"\$out\" >&2; exit 1; fi"
