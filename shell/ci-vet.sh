#!/bin/sh
set -eu

. ./shell/lib.sh

run_go_command vet ./...
