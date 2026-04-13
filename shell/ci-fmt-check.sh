#!/bin/sh
set -eu

: "${GO_IMAGE:=golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447}"
: "${GOFMT_BIN:=/usr/local/go/bin/gofmt}"

docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$(pwd):/workspace" -w /workspace "$GO_IMAGE" sh -lc "set -eu; out=\"\$($GOFMT_BIN -l .)\"; if [ -n \"\$out\" ]; then printf '%s\n' 'Go files are not formatted:' >&2; printf '%s\n' \"\$out\" >&2; exit 1; fi"
