#!/bin/sh
set -eu

violations_file=$(mktemp "${TMPDIR:-/tmp}/host-portability.XXXXXX")
syntax_log=$(mktemp "${TMPDIR:-/tmp}/host-portability-syntax.XXXXXX")

cleanup() {
	rm -f "$violations_file" "$syntax_log"
}

trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

record_violation() {
	printf '%s\n' "$*" >> "$violations_file"
}

list_portable_text_files() {
	git ls-files -- \
		.gitattributes \
		.env.example \
		Makefile \
		README.md \
		'docker/Dockerfile' \
		'.github/*.md' \
		'.github/workflows/*.yml' \
		'docs/**/*.md' \
		'shell/*.sh'
}

list_host_script_files() {
	git ls-files -- 'shell/*.sh'
}

list_host_automation_files() {
	git ls-files -- \
		Makefile \
		'.github/workflows/*.yml' \
		'shell/*.sh'
}

check_gitattributes_policy() {
	if [ ! -f .gitattributes ]; then
		record_violation '.gitattributes: missing LF line-ending policy'
		return
	fi

	for expected in \
		'*.sh text eol=lf' \
		'*.yml text eol=lf' \
		'*.yaml text eol=lf' \
		'Makefile text eol=lf' \
		'docker/Dockerfile text eol=lf'
	do
		if ! grep -Fxq "$expected" .gitattributes; then
			record_violation ".gitattributes: missing required policy: $expected"
		fi
	done
}

check_crlf_line_endings() {
	while IFS= read -r file; do
		[ -f "$file" ] || continue
		awk -v file="$file" '
/\r$/ {
	printf "%s:%d: CRLF line ending is not portable for project automation\n", file, FNR
}
' "$file" >> "$violations_file"
	done <<EOF
$(list_portable_text_files)
EOF
}

check_shell_syntax() {
	while IFS= read -r file; do
		[ -f "$file" ] || continue
		if ! sh -n "$file" > /dev/null 2> "$syntax_log"; then
			record_violation "$file: POSIX shell syntax check failed"
			while IFS= read -r line; do
				record_violation "  $line"
			done < "$syntax_log"
			: > "$syntax_log"
		fi
	done <<EOF
$(list_host_script_files)
EOF
}

check_shell_entrypoints() {
	while IFS= read -r file; do
		[ -f "$file" ] || continue
		first_line=$(sed -n '1p' "$file")
		if [ "$first_line" != '#!/bin/sh' ]; then
			record_violation "$file: shell scripts must use #!/bin/sh"
		fi
	done <<EOF
$(list_host_script_files)
EOF
}

check_known_host_portability_hazards() {
	while IFS= read -r file; do
		[ -f "$file" ] || continue
		awk -v file="$file" '
function report(message) {
	printf "%s:%d: %s\n", file, FNR, message
}
/^[[:space:]]*#/ {
	next
}
{
	line = $0
	if (line ~ /(^|[[:space:];|&])source[[:space:]]/) {
		report("source is not POSIX sh; use . file")
	}
	if (line ~ /(^|[[:space:];|&])readlink[[:space:]]+-f([[:space:]]|$)/) {
		report("readlink -f is not portable to macOS")
	}
	if (line ~ /(^|[[:space:];|&])realpath([[:space:]]|$)/) {
		report("realpath availability differs across supported hosts")
	}
	if (line ~ /(^|[[:space:];|&])sed[[:space:]][^#]*[[:space:]]-i([^[:alnum:]_-]|$)/) {
		report("sed -i differs between GNU sed and BSD sed")
	}
	if (line ~ /(^|[[:space:];|&])grep[[:space:]][^#]*-[A-Za-z]*P[A-Za-z]*([[:space:]]|$)/) {
		report("grep -P is not available in BSD grep")
	}
	if (line ~ /(^|[[:space:];|&])xargs[[:space:]][^#]*-[A-Za-z]*r[A-Za-z]*([[:space:]]|$)/) {
		report("xargs -r is GNU-specific")
	}
	if (line ~ /(^|[[:space:];|&])stat[[:space:]][^#]*-c([[:space:]]|$)/) {
		report("stat -c is GNU-specific")
	}
}
' "$file" >> "$violations_file"
	done <<EOF
$(list_host_automation_files)
EOF
}

check_gitattributes_policy
check_crlf_line_endings
check_shell_syntax
check_shell_entrypoints
check_known_host_portability_hazards

if [ -s "$violations_file" ]; then
	printf '%s\n' 'Host portability guard failed.' >&2
	printf '%s\n' 'Fix POSIX-sh, LF line-ending, or BSD/GNU portability issues before merging.' >&2
	printf '%s\n' '' >&2
	cat "$violations_file" >&2
	exit 1
fi

printf '%s\n' 'Host portability guard passed.'
