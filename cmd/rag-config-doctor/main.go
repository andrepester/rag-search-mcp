package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/andrepester/rag-search-mcp/internal/configdoctor"
)

func main() {
	code, err := run(os.Args[1:], os.Stdout)
	if err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

func run(args []string, stdout io.Writer) (int, error) {
	var repoRoot string
	var hostRepoRoot string
	var hostHome string
	var format string
	fs := flag.NewFlagSet("rag-config-doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&repoRoot, "repo-root", ".", "repository root directory")
	fs.StringVar(&hostRepoRoot, "host-repo-root", "", "host repository root to use in diagnostics")
	fs.StringVar(&hostHome, "host-home", "", "host HOME path to use in safety checks")
	fs.StringVar(&format, "format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return 2, err
	}

	report, err := configdoctor.CheckWithOptions(configdoctor.Options{
		RepoRoot:     repoRoot,
		HostRepoRoot: hostRepoRoot,
		HostHome:     hostHome,
	})
	if err != nil {
		return 2, err
	}

	switch format {
	case "text":
		if err := writeTextReport(stdout, report); err != nil {
			return 2, err
		}
	case "json":
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return 2, err
		}
		if _, err := fmt.Fprintln(stdout, string(out)); err != nil {
			return 2, err
		}
	default:
		return 2, fmt.Errorf("unsupported format %q", format)
	}

	if report.HasErrors() {
		return 1, nil
	}
	return 0, nil
}

func writeTextReport(stdout io.Writer, report configdoctor.Report) error {
	if len(report.Findings) == 0 {
		_, err := fmt.Fprintln(stdout, "config-doctor: ok (0 errors, 0 warnings)")
		return err
	}

	for _, finding := range report.Findings {
		if _, err := fmt.Fprintf(stdout, "config-doctor: %s [%s] %s\n", finding.Severity, finding.Code, finding.Message); err != nil {
			return err
		}
		if finding.Remediation != "" {
			if _, err := fmt.Fprintf(stdout, "  remediation: %s\n", finding.Remediation); err != nil {
				return err
			}
		}
	}

	summary := "passed"
	if report.HasErrors() {
		summary = "failed"
	}
	_, err := fmt.Fprintf(stdout, "config-doctor: %s (%d errors, %d warnings)\n", summary, report.ErrorCount(), report.WarningCount())
	return err
}
