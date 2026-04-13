#!/bin/sh
set -eu

tmp_dir=$(mktemp -d .go-runner-overrides-smoke.XXXXXX)
smoke_image="rag-search-mcp-go-runner-override-smoke:local"
had_licenses=0
had_sbom=0

if [ -f licenses.csv ]; then
	cp licenses.csv "$tmp_dir/licenses.csv"
	had_licenses=1
fi
if [ -f sbom-go.cdx.json ]; then
	cp sbom-go.cdx.json "$tmp_dir/sbom-go.cdx.json"
	had_sbom=1
fi

cleanup() {
	if [ "$had_licenses" -eq 1 ]; then
		cp "$tmp_dir/licenses.csv" licenses.csv
	else
		rm -f licenses.csv
	fi
	if [ "$had_sbom" -eq 1 ]; then
		cp "$tmp_dir/sbom-go.cdx.json" sbom-go.cdx.json
	else
		rm -f sbom-go.cdx.json
	fi
	docker image rm "$smoke_image" >/dev/null 2>&1 || true
	rm -rf "$tmp_dir"
}

trap 'status=$?; cleanup; exit $status' 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

mkdir -p "$tmp_dir/fake-bin"

cat > "$tmp_dir/fake-bin/go" <<'EOF'
#!/bin/sh
set -eu

if [ "${1-}" = "install" ]; then
	pkg="${2-}"
	: "${GOBIN:?GOBIN must be set}"
	mkdir -p "$GOBIN"
	case "$pkg" in
		github.com/google/go-licenses@v1.6.0)
			cat > "$GOBIN/go-licenses" <<'EOX'
#!/bin/sh
set -eu
[ "${1-}" = "report" ] || {
	printf '%s\n' "unexpected go-licenses args: $*" >&2
	exit 2
}
printf '%s\n' 'example.com/module,https://example.com/license,MIT'
EOX
			chmod +x "$GOBIN/go-licenses"
			;;
		github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0)
			cat > "$GOBIN/cyclonedx-gomod" <<'EOX'
#!/bin/sh
set -eu

out=''
prev=''
for arg in "$@"; do
	if [ "$prev" = '-output' ]; then
		out="$arg"
		break
	fi
	prev="$arg"
done

[ -n "$out" ] || {
	printf '%s\n' 'missing -output argument' >&2
	exit 2
}

printf '%s\n' '{"bomFormat":"CycloneDX"}' > "$out"

EOX
			chmod +x "$GOBIN/cyclonedx-gomod"
			;;
		*)
			printf '%s\n' "unexpected go install target: $pkg" >&2
			exit 2
			;;
	esac
	exit 0
fi

printf '%s\n' "unexpected go invocation: $*" >&2
exit 2
EOF

cat > "$tmp_dir/fake-bin/gofmt" <<'EOF'
#!/bin/sh
set -eu

[ "${1-}" = "-l" ] || {
	printf '%s\n' "unexpected gofmt args: $*" >&2
	exit 2
}
exit 0
EOF

cat > "$tmp_dir/Dockerfile" <<'EOF'
FROM alpine:3.21
COPY fake-bin/go /opt/custom/go
COPY fake-bin/gofmt /opt/custom/gofmt
RUN chmod +x /opt/custom/go /opt/custom/gofmt
EOF

docker build -f "$tmp_dir/Dockerfile" -t "$smoke_image" "$tmp_dir"

GO_IMAGE="$smoke_image" GO_BIN=/opt/custom/go sh ./shell/ci-fmt-check.sh
GO_IMAGE="$smoke_image" GO_BIN=/opt/custom/go sh ./shell/ci-licenses-export.sh
GO_IMAGE="$smoke_image" GO_BIN=/opt/custom/go sh ./shell/ci-sbom-go.sh

test -s licenses.csv
test -s sbom-go.cdx.json
