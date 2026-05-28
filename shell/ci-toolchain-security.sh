#!/bin/sh
set -eu

. ./shell/lib.sh

TOOLS_DIR=${GO_TOOLS_DIR:-tools}
module_graph_file=$(mktemp "${TMPDIR:-/tmp}/toolchain-modules.XXXXXX")
failed=0

cleanup() {
	rm -f "$module_graph_file"
}

trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

version_core() {
	value="${1#v}"
	value="${value%%[-+]*}"
	printf '%s' "$value"
}

version_part() {
	value="$1"
	field="$2"
	awk -F. -v field="$field" '{ part = $field + 0; print part }' <<EOF
$value
EOF
}

version_ge() {
	left=$(version_core "$1")
	right=$(version_core "$2")

	left_major=$(version_part "$left" 1)
	left_minor=$(version_part "$left" 2)
	left_patch=$(version_part "$left" 3)
	right_major=$(version_part "$right" 1)
	right_minor=$(version_part "$right" 2)
	right_patch=$(version_part "$right" 3)

	[ "$left_major" -gt "$right_major" ] && return 0
	[ "$left_major" -lt "$right_major" ] && return 1
	[ "$left_minor" -gt "$right_minor" ] && return 0
	[ "$left_minor" -lt "$right_minor" ] && return 1
	[ "$left_patch" -ge "$right_patch" ]
}

module_version() {
	module="$1"
	awk -v module="$module" '$1 == module { print $2; found = 1; exit } END { if (!found) exit 1 }' "$module_graph_file"
}

check_forbidden_module() {
	module="$1"
	if module_version "$module" >/dev/null 2>&1; then
		printf '%s\n' "[toolchain] FAIL forbidden module is present: $module" >&2
		failed=1
		return 0
	fi
	printf '%s\n' "[toolchain] OK forbidden module absent: $module"
}

check_minimum_module() {
	module="$1"
	minimum="$2"
	if ! version=$(module_version "$module"); then
		printf '%s\n' "[toolchain] OK module not present: $module"
		return 0
	fi
	if version_ge "$version" "$minimum"; then
		printf '%s\n' "[toolchain] OK $module $version >= $minimum"
		return 0
	fi
	printf '%s\n' "[toolchain] FAIL $module $version < $minimum" >&2
	failed=1
}

printf '%s\n' "[toolchain] generating module graph for $TOOLS_DIR/go.mod via Docker go-runner"
build_go_runner_image
runner_bin=$(go_runner_bin)
run_go_runner sh -lc '
set -eu
runner_bin="$1"
tools_dir="$2"
cd "$tools_dir"
"$runner_bin" list -m all
' sh "$runner_bin" "$TOOLS_DIR" > "$module_graph_file"

printf '%s\n' '[toolchain] checking forbidden legacy modules'
check_forbidden_module gopkg.in/src-d/go-git.v4

printf '%s\n' '[toolchain] checking minimum dependency versions'
while read -r module minimum; do
	case "$module" in
		''|\#*) continue ;;
	esac
	check_minimum_module "$module" "$minimum"
done <<'EOF'
github.com/go-git/go-git/v5 v5.19.1
github.com/go-git/go-billy/v5 v5.9.0
github.com/cloudflare/circl v1.6.3
golang.org/x/crypto v0.52.0
golang.org/x/net v0.55.0
golang.org/x/sys v0.45.0
EOF

if [ "$failed" -ne 0 ]; then
	printf '%s\n' '[toolchain] toolchain dependency security gate failed' >&2
	exit 1
fi

printf '%s\n' '[toolchain] toolchain dependency security gate passed'
