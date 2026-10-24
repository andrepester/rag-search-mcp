#!/bin/sh
set -eu

awk -F, '
	NF != 3 || $1 == "" || $2 == "" {
		print "[licenses] malformed license report row: " $0 > "/dev/stderr"
		failed = 1
		next
	}
	$3 !~ /^(MIT|MIT-0|BSD-2-Clause|BSD-3-Clause|Apache-2.0|ISC|MPL-2.0)$/ {
		print "[licenses] disallowed or unknown license: " $1 " (" $3 ")" > "/dev/stderr"
		failed = 1
	}
	END {
		if (NR == 0) {
			print "[licenses] empty license report" > "/dev/stderr"
			failed = 1
		}
		exit failed
	}
' "${1:-licenses.csv}"

printf '%s\n' '[licenses] license allowlist passed'
