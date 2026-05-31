#!/bin/sh
set -eu

. ./shell/lib.sh

run_go_command test -count=1 -run '^TestGoldenQueries$' -v ./internal/rag
