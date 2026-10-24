#!/bin/sh
set -eu

. ./shell/lib.sh

raw_report=$(mktemp './.licenses-raw.XXXXXX')
normalized_report=$(mktemp './.licenses-normalized.XXXXXX')
cleanup() {
	rm -f "$raw_report" "$normalized_report"
}
trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15

run_go_tool github.com/google/go-licenses/v2 go-licenses report ./... > "$raw_report"
if awk -F, '$3 == "Unknown" && ($1 == "github.com/segmentio/asm" || index($1, "github.com/segmentio/asm/") == 1) { found = 1 } END { exit !found }' "$raw_report"; then
	run_go_runner sh -lc '
set -eu
runner_bin=$1
module_dir=$("$runner_bin" list -m -f "{{.Dir}}" github.com/segmentio/asm)
module_version=$("$runner_bin" list -m -f "{{.Version}}" github.com/segmentio/asm)
sh ./shell/ci-license-report-normalize.sh "$2" "$module_dir/LICENSE" "$module_version"
' sh "$(go_runner_bin)" "$raw_report" > "$normalized_report"
else
	cp "$raw_report" "$normalized_report"
fi
mv "$normalized_report" licenses.csv
