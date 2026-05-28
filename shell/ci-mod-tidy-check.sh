#!/bin/sh
set -eu

module_files='go.mod go.sum tools/go.mod tools/go.sum'

printf '%s\n' '[mod-tidy] running root module tidy via shell/go-runner.sh'
sh ./shell/go-runner.sh mod tidy

printf '%s\n' '[mod-tidy] running tools module tidy via shell/go-runner.sh'
sh ./shell/go-runner.sh -C tools mod tidy

if ! git diff --quiet -- $module_files; then
	printf '%s\n' '[mod-tidy] module files drift after containerized tidy' >&2
	printf '%s\n' '[mod-tidy] reproduce and fix locally with:' >&2
	printf '%s\n' '  sh ./shell/ci-mod-tidy-check.sh' >&2
	printf '%s\n' '' >&2
	git diff -- $module_files >&2
	exit 1
fi

printf '%s\n' '[mod-tidy] no module file drift detected'
