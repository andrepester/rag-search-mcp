#!/bin/sh
set -eu

. ./shell/lib.sh

run_go_tool github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod cyclonedx-gomod mod -json -licenses -output sbom-go.cdx.json
