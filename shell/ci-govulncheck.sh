#!/bin/sh
set -eu

. ./shell/lib.sh

run_go_command run golang.org/x/vuln/cmd/govulncheck@v1.1.4 ./...
