#!/bin/sh
set -eu

. ./shell/lib.sh

build_go_runner_image
run_go_runner sh -lc 'set -eu; PATH="/usr/local/go/bin:$PATH"; toolbin=/tmp/bin; mkdir -p "$toolbin"; GOBIN="$toolbin" /usr/local/go/bin/go install github.com/google/go-licenses@v1.6.0; "$toolbin"/go-licenses report ./... > licenses.csv'
