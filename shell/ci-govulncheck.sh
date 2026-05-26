#!/bin/sh
set -eu

. ./shell/lib.sh

run_go_tool golang.org/x/vuln/cmd/govulncheck govulncheck ./...
