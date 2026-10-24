#!/bin/sh
set -eu

tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/license-policy.XXXXXX")
cleanup() {
	rm -rf "$tmpdir"
}
trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 143' 15

license_file=shell/testdata/licenses/segmentio-asm-MIT-0.txt
report=$tmpdir/report.csv
normalized=$tmpdir/normalized.csv

expect_rejected() {
	if "$@" > "$tmpdir/output" 2>&1; then
		printf '%s\n' "[licenses-smoke] expected rejection: $*" >&2
		exit 1
	fi
}

for license in MIT MIT-0 BSD-2-Clause BSD-3-Clause Apache-2.0 ISC MPL-2.0; do
	printf 'example.org/module,https://example.org/LICENSE,%s\n' "$license"
done > "$report"
sh ./shell/ci-licenses-check.sh "$report"

printf '%s\n' \
	'github.com/segmentio/asm/ascii,Unknown,Unknown' \
	'github.com/segmentio/asm/base64,Unknown,Unknown' \
	'example.org/module,https://example.org/LICENSE,MIT' > "$report"
sh ./shell/ci-license-report-normalize.sh "$report" "$license_file" v1.2.1 > "$normalized"
grep -Fxq 'github.com/segmentio/asm/ascii,https://github.com/segmentio/asm/blob/v1.2.1/LICENSE,MIT-0' "$normalized"
grep -Fxq 'github.com/segmentio/asm/base64,https://github.com/segmentio/asm/blob/v1.2.1/LICENSE,MIT-0' "$normalized"
grep -Fxq 'example.org/module,https://example.org/LICENSE,MIT' "$normalized"
sh ./shell/ci-licenses-check.sh "$normalized"

cp "$license_file" "$tmpdir/changed-license"
printf '%s\n' 'Additional restriction' >> "$tmpdir/changed-license"
expect_rejected sh ./shell/ci-license-report-normalize.sh "$report" "$tmpdir/changed-license" v1.2.1
expect_rejected sh ./shell/ci-license-report-normalize.sh "$report" "$tmpdir/missing-license" v1.2.1

for package in github.com/segmentio/asm-other example.org/module; do
	printf '%s,Unknown,Unknown\n' "$package" > "$report"
	sh ./shell/ci-license-report-normalize.sh "$report" "$license_file" v1.2.1 > "$normalized"
	cmp "$report" "$normalized"
	expect_rejected sh ./shell/ci-licenses-check.sh "$normalized"
done

printf '%s\n' 'github.com/segmentio/asm/ascii,https://example.org/LICENSE,GPL-3.0' > "$report"
sh ./shell/ci-license-report-normalize.sh "$report" "$license_file" v1.2.1 > "$normalized"
cmp "$report" "$normalized"
expect_rejected sh ./shell/ci-licenses-check.sh "$normalized"

printf '%s\n' 'example.org/module,https://example.org/LICENSE,MIT,unexpected' > "$report"
expect_rejected sh ./shell/ci-licenses-check.sh "$report"
: > "$report"
expect_rejected sh ./shell/ci-licenses-check.sh "$report"

printf '%s\n' '[licenses-smoke] license policy red/green smoke passed'
