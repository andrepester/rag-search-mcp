#!/bin/sh
set -eu

: "${GO_IMAGE:=golang:1.25.9-alpine@sha256:7a00384194cf2cb68924bbb918d675f1517357433c8541bac0ab2f929b9d5447}"
docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$(pwd):/workspace" -w /workspace "$GO_IMAGE" sh -lc 'set -eu; PATH="/usr/local/go/bin:$PATH"; toolbin=/tmp/bin; mkdir -p "$toolbin"; GOBIN="$toolbin" /usr/local/go/bin/go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0; "$toolbin"/cyclonedx-gomod mod -json -licenses -output sbom-go.cdx.json'
