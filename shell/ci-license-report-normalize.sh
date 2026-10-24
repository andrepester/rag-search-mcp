#!/bin/sh
set -eu

report=$1
license_file=$2
module_version=$3

# go-licenses v2.0.1 does not recognize MIT-0. Only recover this module's
# Unknown rows after verifying its complete, reviewed upstream license text.
if awk -F, '$3 == "Unknown" && ($1 == "github.com/segmentio/asm" || index($1, "github.com/segmentio/asm/") == 1) { found = 1 } END { exit !found }' "$report"; then
	if command -v sha256sum >/dev/null 2>&1; then
		actual=$(sha256sum "$license_file" | awk '{ print $1 }')
	else
		actual=$(shasum -a 256 "$license_file" | awk '{ print $1 }')
	fi
	if [ "$actual" != cca993712df289a5958bdef69031a5dac0f951ac15afeb313f9eeea55ed59443 ]; then
		printf '%s\n' '[licenses] segmentio/asm license differs from the reviewed MIT-0 text' >&2
		exit 1
	fi
	case "$module_version" in
		v[0-9]*) ;;
		*) printf '%s\n' '[licenses] missing segmentio/asm module version' >&2; exit 1 ;;
	esac
	printf '%s\n' '[licenses] verified segmentio/asm MIT-0 license text; normalizing unknown rows' >&2
	awk -F, -v version="$module_version" '
		$3 == "Unknown" && ($1 == "github.com/segmentio/asm" || index($1, "github.com/segmentio/asm/") == 1) {
			print $1 ",https://github.com/segmentio/asm/blob/" version "/LICENSE,MIT-0"
			next
		}
		{ print }
	' "$report"
else
	cat "$report"
fi
