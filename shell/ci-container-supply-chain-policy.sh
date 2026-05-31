#!/bin/sh
set -eu

failed=0

fail() {
	printf '%s\n' "[container-policy] FAIL $*" >&2
	failed=1
}

check_dockerfile_base_images() {
	awk '
	/^FROM[[:space:]]+/ {
		image = $2
		if (image ~ /^--platform=/) {
			image = $3
		}
		if (!(image in stages) && image !~ /@sha256:[0-9a-f]{64}($|[[:space:]])/) {
			printf "%s\n", "[container-policy] FAIL Dockerfile FROM is not digest-pinned: " $0 > "/dev/stderr"
			failed = 1
		}
		for (i = 1; i < NF; i++) {
			if (toupper($i) == "AS") {
				stages[$(i + 1)] = 1
			}
		}
	}
	END { exit failed }
	' docker/Dockerfile || failed=1
}

check_no_grype_exceptions() {
	for path in .grype.yaml .grype.yml grype.yaml grype.yml .github/grype.yaml .github/grype.yml; do
		if [ -e "$path" ]; then
			fail "Grype exception/config file is not allowed: $path"
		fi
	done

	if grep -n -- '--only-fixed' .github/workflows/supply-chain.yml; then
		fail "Grype scans must include unfixed high/critical findings; remove --only-fixed"
	fi

	if grep -n -E 'grype .*--(ignore|config)' .github/workflows/supply-chain.yml; then
		fail "Grype ignore/config flags are not allowed"
	fi
}

check_grype_gates() {
	grype_runs=$(awk '/run: .*grype / { count++ } END { print count + 0 }' .github/workflows/supply-chain.yml)
	grype_fail_on_high_runs=$(awk '/run: .*grype / && /--fail-on high/ { count++ } END { print count + 0 }' .github/workflows/supply-chain.yml)

	if [ "$grype_runs" -lt 2 ]; then
		fail "Expected filesystem and image Grype scan steps in supply-chain workflow"
	fi

	if [ "$grype_runs" -ne "$grype_fail_on_high_runs" ]; then
		fail "Every Grype scan must use --fail-on high"
	fi
}

check_dockerfile_base_images
check_no_grype_exceptions
check_grype_gates

if [ "$failed" -ne 0 ]; then
	exit 1
fi

printf '%s\n' '[container-policy] container supply-chain policy gate passed'
