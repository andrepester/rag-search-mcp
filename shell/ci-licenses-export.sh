#!/bin/sh
set -eu

. ./shell/lib.sh

run_go_tool github.com/google/go-licenses go-licenses report ./... > licenses.csv
