#!/bin/sh
set -eu

. ./shell/lib.sh

build_go_runner_image
run_go_runner sh -lc "set -eu; out=\"\$(/usr/local/go/bin/gofmt -l .)\"; if [ -n \"\$out\" ]; then printf '%s\n' 'Go files are not formatted:' >&2; printf '%s\n' \"\$out\" >&2; exit 1; fi"
