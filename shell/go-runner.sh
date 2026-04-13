#!/bin/sh
set -eu

. ./shell/lib.sh

if [ "$#" -eq 0 ]; then
	set -- test -count=1 ./...
fi

run_go_command "$@"
