#!/bin/sh
set -eu

. ./shell/lib.sh

build_go_runner_image
run_go_runner sh -lc 'set -eu; PATH="/usr/local/go/bin:$PATH"; toolbin=/tmp/bin; mkdir -p "$toolbin"; GOBIN="$toolbin" /usr/local/go/bin/go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0; "$toolbin"/cyclonedx-gomod mod -json -licenses -output sbom-go.cdx.json'
