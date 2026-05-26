#!/bin/sh
set -eu

. ./shell/ci-tool-versions.env

: "${ANCHORE_TOOL_INSTALL_DIR:=/usr/local/bin}"
: "${ANCHORE_TOOL_USE_SUDO:=1}"

detect_anchore_tool_os() {
	case "$(uname -s)" in
		Linux) printf '%s' 'linux' ;;
		Darwin) printf '%s' 'darwin' ;;
		*)
			printf '%s\n' 'unsupported Anchore tool OS; set ANCHORE_TOOL_OS explicitly' >&2
			return 1
			;;
	esac
}

detect_anchore_tool_arch() {
	case "$(uname -m)" in
		x86_64|amd64) printf '%s' 'amd64' ;;
		arm64|aarch64) printf '%s' 'arm64' ;;
		*)
			printf '%s\n' 'unsupported Anchore tool architecture; set ANCHORE_TOOL_ARCH explicitly' >&2
			return 1
			;;
	esac
}

if [ -z "${ANCHORE_TOOL_OS-}" ]; then
	ANCHORE_TOOL_OS=$(detect_anchore_tool_os)
fi
if [ -z "${ANCHORE_TOOL_ARCH-}" ]; then
	ANCHORE_TOOL_ARCH=$(detect_anchore_tool_arch)
fi

tmpdir=$(mktemp -d)

cleanup() {
	rm -rf "$tmpdir"
}

trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

install_anchore_tool() {
	tool_name="$1"
	version="$2"
	tarball="${tool_name}_${version}_${ANCHORE_TOOL_OS}_${ANCHORE_TOOL_ARCH}.tar.gz"
	checksums="${tool_name}_${version}_checksums.txt"
	base_url="https://github.com/anchore/${tool_name}/releases/download/v${version}"
	tool_tmpdir="$tmpdir/$tool_name"

	mkdir -p "$tool_tmpdir"
	(
		cd "$tool_tmpdir"
		curl -sSfL --retry 5 --retry-delay 2 --retry-all-errors -o "$tarball" "$base_url/$tarball"
		curl -sSfL --retry 5 --retry-delay 2 --retry-all-errors -o "$checksums" "$base_url/$checksums"
		grep " ${tarball}$" "$checksums" > "${tool_name}_checksum.txt"
		sha256sum -c "${tool_name}_checksum.txt" >/dev/null
		tar -xzf "$tarball" "$tool_name"
		if [ "$ANCHORE_TOOL_USE_SUDO" = "1" ]; then
			sudo install -m 0755 "$tool_name" "$ANCHORE_TOOL_INSTALL_DIR/$tool_name"
		else
			install -m 0755 "$tool_name" "$ANCHORE_TOOL_INSTALL_DIR/$tool_name"
		fi
	)

	"$ANCHORE_TOOL_INSTALL_DIR/$tool_name" version
}

case "${1:-all}" in
	all)
		install_anchore_tool syft "$SYFT_VERSION"
		install_anchore_tool grype "$GRYPE_VERSION"
		;;
	syft)
		install_anchore_tool syft "$SYFT_VERSION"
		;;
	grype)
		install_anchore_tool grype "$GRYPE_VERSION"
		;;
	*)
		printf '%s\n' "usage: $0 [all|syft|grype]" >&2
		exit 2
		;;
esac
