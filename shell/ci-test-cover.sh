#!/bin/sh
set -eu

. ./shell/lib.sh

: "${COVERAGE_MIN:=60}"

build_go_runner_image
run_go_runner /usr/local/go/bin/go test -count=1 -covermode=atomic -coverprofile=coverage.out ./...
run_go_runner /usr/local/go/bin/go tool cover -func=coverage.out > coverage.txt
awk -v min="$COVERAGE_MIN" '/^total:/ { gsub(/%/, "", $3); if (($3 + 0) < (min + 0)) { printf("coverage %.1f%% is below minimum %.1f%%\n", $3, min); exit 1 }; found=1 } END { if (!found) { print "coverage total not found"; exit 1 } }' coverage.txt
