#!/bin/sh
set -eu

repo_root=$(pwd -P)
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/container-policy-smoke.XXXXXX")

cleanup() {
	rm -rf "$tmpdir"
}

trap cleanup 0
trap 'exit 129' 1
trap 'exit 130' 2
trap 'exit 131' 3
trap 'exit 143' 15

reset_fixture() {
	rm -rf "$tmpdir/fixture"
	mkdir -p "$tmpdir/fixture/shell" "$tmpdir/fixture/docker" "$tmpdir/fixture/.github/workflows"
	cp "$repo_root/shell/ci-container-supply-chain-policy.sh" "$tmpdir/fixture/shell/"
	cp "$repo_root/docker/Dockerfile" "$tmpdir/fixture/docker/"
	cp "$repo_root/.github/workflows/supply-chain.yml" "$tmpdir/fixture/.github/workflows/"
}

run_guard() {
	(
		cd "$tmpdir/fixture"
		sh ./shell/ci-container-supply-chain-policy.sh
	)
}

expect_guard_failure() {
	name="$1"
	if run_guard > "$tmpdir/$name.out" 2>&1; then
		printf '%s\n' "[container-policy-smoke] FAIL expected guard failure for $name" >&2
		cat "$tmpdir/$name.out" >&2
		exit 1
	fi
	printf '%s\n' "[container-policy-smoke] OK guard rejected $name"
}

reset_fixture
run_guard > "$tmpdir/baseline.out" 2>&1
printf '%s\n' '[container-policy-smoke] OK baseline policy fixture passed'

reset_fixture
sed '1s/@sha256:[^ ]*//' "$tmpdir/fixture/docker/Dockerfile" > "$tmpdir/Dockerfile.unpinned"
mv "$tmpdir/Dockerfile.unpinned" "$tmpdir/fixture/docker/Dockerfile"
expect_guard_failure unpinned-base-image

reset_fixture
sed 's/--fail-on high/--only-fixed --fail-on high/' "$tmpdir/fixture/.github/workflows/supply-chain.yml" > "$tmpdir/supply-chain.only-fixed.yml"
mv "$tmpdir/supply-chain.only-fixed.yml" "$tmpdir/fixture/.github/workflows/supply-chain.yml"
expect_guard_failure only-fixed-scan

reset_fixture
: > "$tmpdir/fixture/.grype.yaml"
expect_guard_failure grype-config-exception

printf '%s\n' '[container-policy-smoke] container supply-chain policy smoke test passed'
