#!/bin/sh
set -eu

. ./shell/lib.sh

: "${COVERAGE_MIN:=60}"

build_go_runner_image
runner_bin=$(go_runner_bin)
run_go_runner "$runner_bin" test -count=1 -covermode=atomic -coverprofile=coverage.out ./...
run_go_runner "$runner_bin" tool cover -func=coverage.out > coverage.txt
awk -v min="$COVERAGE_MIN" '/^total:/ { gsub(/%/, "", $3); if (($3 + 0) < (min + 0)) { printf("coverage %.1f%% is below minimum %.1f%%\n", $3, min); exit 1 }; found=1 } END { if (!found) { print "coverage total not found"; exit 1 } }' coverage.txt
