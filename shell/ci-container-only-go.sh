#!/bin/sh
set -eu

violations_file=$(mktemp "${TMPDIR:-/tmp}/container-only-go.XXXXXX")

cleanup() {
	rm -f "$violations_file"
}

trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

list_scan_files() {
	if [ "$#" -gt 0 ]; then
		for file do
			printf '%s\n' "$file"
		done
		return 0
	fi

	git ls-files -- \
		Makefile \
		'shell/*.sh' \
		'.github/workflows/*.yml' \
		README.md \
		'docs/**/*.md'
}

scan_file() {
	file="$1"
	awk -v file="$file" '
BEGIN {
	subcommands = "(test|list|mod|run|install|build|vet|tool|env|version|get|work|generate|clean|fmt)"
	go_command = "^([./A-Za-z0-9_-]*/)?go[[:space:]]+" subcommands "([^[:alnum:]_-]|$)"
	gofmt_command = "^([./A-Za-z0-9_-]*/)?gofmt([[:space:]]|$)"
}
function trim(value) {
	gsub(/^[[:space:]]+/, "", value)
	gsub(/[[:space:]]+$/, "", value)
	return value
}
function command_segment_has_host_go(segment, candidate) {
	candidate = trim(segment)
	sub(/^-?[[:space:]]*run:[[:space:]]*/, "", candidate)
	sub(/^[@+-]+[[:space:]]*/, "", candidate)
	while (candidate ~ /^(if|then|do|command|exec|time|env)[[:space:]]+/) {
		sub(/^(if|then|do|command|exec|time|env)[[:space:]]+/, "", candidate)
	}
	while (candidate ~ /^[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]+[[:space:]]+/) {
		sub(/^[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]+[[:space:]]+/, "", candidate)
	}
	return candidate ~ go_command || candidate ~ gofmt_command
}
{
	line = $0
	if ((file == "Makefile" || file ~ /^shell\/.*\.sh$/) && line ~ /^[[:space:]]*#/) {
		next
	}
	scan = line
	gsub(/&&|\|\|/, ";", scan)
	gsub(/[;|()]/, "\n", scan)
	segment_count = split(scan, segments, "\n")
	for (i = 1; i <= segment_count; i++) {
		if (command_segment_has_host_go(segments[i])) {
			printf "%s:%d: direct host-side Go command: %s\n", file, FNR, line
			next
		}
	}
}
' "$file"
}

while IFS= read -r file; do
	[ -f "$file" ] || continue
	scan_file "$file" >> "$violations_file"
done <<EOF
$(list_scan_files "$@")
EOF

if [ -s "$violations_file" ]; then
	printf '%s\n' 'Host-side Go commands are not allowed in repository automation.' >&2
	printf '%s\n' 'Use shell/go-runner.sh, make targets backed by shell/go-runner.sh, or Dockerfile/container contexts instead.' >&2
	printf '%s\n' '' >&2
	cat "$violations_file" >&2
	exit 1
fi

printf '%s\n' 'Container-only Go guard passed.'
