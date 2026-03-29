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
	if err := run(os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(stdout io.Writer) error {
	var repoRoot string
	fs := flag.NewFlagSet("rag-install", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&repoRoot, "repo-root", ".", "repository root directory")
	if err := fs.Parse(os.Args[1:]); err != nil {
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
	if err := bootstrap.UpsertOpenCodeConfig(absRoot, port); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(stdout, "updated opencode.json for rag-search-mcp MCP alias at http://127.0.0.1:%d/mcp\n", port); err != nil {
		return err
	}

	if err := bootstrap.EnsureHostDataDirs(absRoot); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(stdout, "ensured host mount directories for docs, code, index, and models"); err != nil {
		return err
	}

	return nil
}
