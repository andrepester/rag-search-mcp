#!/bin/sh
set -eu

: "${GO_IMAGE:=golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447}"
: "${GO_BIN:=/usr/local/go/bin/go}"
: "${COVERAGE_MIN:=60}"

docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$(pwd):/workspace" -w /workspace "$GO_IMAGE" sh -lc "set -eu; $GO_BIN test -count=1 -covermode=atomic -coverprofile=coverage.out ./...; $GO_BIN tool cover -func=coverage.out | tee coverage.txt; awk -v min=\"$COVERAGE_MIN\" '/^total:/ { gsub(/%/, \"\", \$3); if ((\$3 + 0) < (min + 0)) { printf(\"coverage %.1f%% is below minimum %.1f%%\\n\", \$3, min); exit 1 }; found=1 } END { if (!found) { print \"coverage total not found\"; exit 1 } }' coverage.txt"
