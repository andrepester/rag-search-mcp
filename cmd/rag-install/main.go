package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/andrepester/rag-search-mcp/internal/bootstrap"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(args []string, stdout io.Writer) error {
	var repoRoot string
	fs := flag.NewFlagSet("rag-install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&repoRoot, "repo-root", ".", "repository root directory")
	if err := fs.Parse(args); err != nil {
		return err
	}

	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repo root: %w", err)
	}

	created, err := bootstrap.EnsureEnvFile(absRoot)
	if err != nil {
		return err
	}
	if created {
		if _, err := fmt.Fprintln(stdout, "created .env from .env.example"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintln(stdout, "kept existing .env"); err != nil {
			return err
		}
	}

	port, err := bootstrap.ResolvePort(absRoot)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "MCP endpoint will be available at http://127.0.0.1:%d/mcp after the Docker stack starts\n", port); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "client configuration files are user-managed; none were created or modified"); err != nil {
		return err
	}

	if err := bootstrap.EnsureHostDataDirs(absRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "ensured host mount directories for docs, code, index state, and models"); err != nil {
		return err
	}

	return nil
}
