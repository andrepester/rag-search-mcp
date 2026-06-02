package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrepester/rag-search-mcp/internal/configdoctor"
)

func TestRunTextSuccess(t *testing.T) {
	repoRoot := newConfigDoctorCLIRepo(t)
	var stdout bytes.Buffer

	code, err := run([]string{"--repo-root", repoRoot}, &stdout)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output: %s", code, stdout.String())
	}
	if got := stdout.String(); !strings.Contains(got, "config-doctor: ok (0 errors, 0 warnings)") {
		t.Fatalf("unexpected output: %s", got)
	}
}

func TestRunTextFailureExit(t *testing.T) {
	repoRoot := newConfigDoctorCLIRepo(t)
	writeCLIFile(t, filepath.Join(repoRoot, ".env"), 0o600, "RAG_HTTP_PORT=not-a-number\n")
	var stdout bytes.Buffer

	code, err := run([]string{"--repo-root", repoRoot}, &stdout)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	if code != 1 {
		t.Fatalf("exit code = %d, want 1; output: %s", code, stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "config-doctor: error [RAG_HTTP_PORT_INTEGER]") {
		t.Fatalf("missing error finding in output: %s", output)
	}
	if !strings.Contains(output, "config-doctor: failed") {
		t.Fatalf("missing failed summary in output: %s", output)
	}
}

func TestRunJSONSuccess(t *testing.T) {
	repoRoot := newConfigDoctorCLIRepo(t)
	var stdout bytes.Buffer

	code, err := run([]string{"--repo-root", repoRoot, "--format", "json"}, &stdout)
	if err != nil {
		t.Fatalf("run() failed: %v", err)
	}
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; output: %s", code, stdout.String())
	}

	var report configdoctor.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal json output: %v\n%s", err, stdout.String())
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings, got %#v", report.Findings)
	}
}

func TestRunRejectsUnsupportedFormat(t *testing.T) {
	repoRoot := newConfigDoctorCLIRepo(t)
	var stdout bytes.Buffer

	code, err := run([]string{"--repo-root", repoRoot, "--format", "xml"}, &stdout)
	if err == nil {
		t.Fatal("expected unsupported format error")
	}
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newConfigDoctorCLIRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoRoot, "data", "docs"),
		filepath.Join(repoRoot, "data", "code"),
		filepath.Join(repoRoot, "data", "index"),
		filepath.Join(repoRoot, "data", "models"),
		filepath.Join(repoRoot, "docker"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	writeCLIFile(t, filepath.Join(repoRoot, ".env"), 0o600, strings.Join([]string{
		"RAG_HTTP_PORT=8765",
		"HOST_DOCS_DIR=./data/docs",
		"HOST_CODE_DIR=./data/code",
		"HOST_INDEX_DIR=./data/index",
		"HOST_MODELS_DIR=./data/models",
		"OLLAMA_HOST=http://ollama:11434",
		"EMBED_MODEL=nomic-embed-text",
		"OLLAMA_PORT=11434",
		"RAG_ENABLE_CODE_INGEST=true",
		"RAG_CHROMA_TENANT=default_tenant",
		"RAG_CHROMA_DATABASE=default_database",
		"RAG_COLLECTION_NAME=rag",
		"RAG_SCOPE_DEFAULT=all",
		"RAG_CHUNK_SIZE=1200",
		"RAG_CHUNK_OVERLAP=200",
		"RAG_MAX_TOP_K=50",
		"",
	}, "\n"))
	writeCLIFile(t, filepath.Join(repoRoot, "docker", "docker-compose.yml"), 0o644, `services:
  ollama:
    ports:
      - "127.0.0.1:${OLLAMA_PORT:-11434}:11434"
  rag-mcp:
    ports:
      - "127.0.0.1:${RAG_HTTP_PORT:-8765}:8765"
`)
	writeCLIFile(t, filepath.Join(repoRoot, "opencode.json"), 0o600, `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "rag-search-mcp": {
      "type": "remote",
      "url": "http://127.0.0.1:8765/mcp",
      "enabled": true,
      "timeout": 10000
    }
  }
}`)
	return repoRoot
}

func writeCLIFile(t *testing.T, path string, mode os.FileMode, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
