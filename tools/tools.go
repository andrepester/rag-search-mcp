//go:build tools

package tools

import (
	_ "github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod"
	_ "github.com/google/go-licenses"
	_ "golang.org/x/vuln/cmd/govulncheck"
)
