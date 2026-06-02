package configdoctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckValidDefaultConfig(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings, got %#v", report.Findings)
	}
}

func TestCheckReportsFatalRuntimeAndPathErrors(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, strings.Join([]string{
		"RAG_HTTP_PORT=not-a-number",
		"RAG_CHUNK_SIZE=10",
		"RAG_CHUNK_OVERLAP=10",
		"RAG_ENABLE_CODE_INGEST=maybe",
		"HOST_INDEX_DIR=/",
		"",
	}, "\n"))

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !report.HasErrors() {
		t.Fatalf("expected errors, got %#v", report.Findings)
	}

	for _, code := range []string{
		"RAG_HTTP_PORT_INTEGER",
		"CHUNK_OVERLAP_RANGE",
		"RAG_ENABLE_CODE_INGEST_BOOLEAN",
		"HOST_INDEX_DIR_UNSAFE_ROOT",
	} {
		if !hasFinding(report, SeverityError, code) {
			t.Fatalf("missing error %s in %#v", code, report.Findings)
		}
	}
}

func TestCheckReportsInvalidDotEnvSyntax(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, "RAG_HTTP_PORT=8765\nnot syntax\n")

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !hasFinding(report, SeverityError, "DOTENV_SYNTAX") {
		t.Fatalf("missing DOTENV_SYNTAX in %#v", report.Findings)
	}
}

func TestCheckReportsInvalidOpenCodeJSON(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, "opencode.json"), 0o600, "{invalid")

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !hasFinding(report, SeverityError, "OPENCODE_JSON") {
		t.Fatalf("missing OPENCODE_JSON in %#v", report.Findings)
	}
}

func TestCheckOpenCodePortMismatchIsWarningOnly(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, "opencode.json"), 0o600, `{
	  "$schema": "https://opencode.ai/config.json",
	  "mcp": {
	    "rag-search-mcp": {
	      "type": "remote",
	      "url": "http://127.0.0.1:9999/mcp",
	      "enabled": true,
	      "timeout": 10000
	    }
	  }
	}`)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if report.HasErrors() {
		t.Fatalf("expected warning-only report, got %#v", report.Findings)
	}
	if !hasFinding(report, SeverityWarning, "OPENCODE_ALIAS_PORT_MISMATCH") {
		t.Fatalf("missing OPENCODE_ALIAS_PORT_MISMATCH in %#v", report.Findings)
	}
}

func TestCheckOpenCodeMissingPortIsMismatchWarning(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, "opencode.json"), 0o600, `{
	  "$schema": "https://opencode.ai/config.json",
	  "mcp": {
	    "rag-search-mcp": {
	      "type": "remote",
	      "url": "http://127.0.0.1/mcp",
	      "enabled": true,
	      "timeout": 10000
	    }
	  }
	}`)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if report.HasErrors() {
		t.Fatalf("expected warning-only report, got %#v", report.Findings)
	}
	if !hasFinding(report, SeverityWarning, "OPENCODE_ALIAS_PORT_MISMATCH") {
		t.Fatalf("missing OPENCODE_ALIAS_PORT_MISMATCH in %#v", report.Findings)
	}
}

func TestCheckEnvironmentOverridesDotEnv(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, "RAG_HTTP_PORT=bad\n")

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{"RAG_HTTP_PORT=8123"},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if hasFinding(report, SeverityError, "RAG_HTTP_PORT_INTEGER") {
		t.Fatalf("process env should override invalid .env port, got %#v", report.Findings)
	}
}

func TestCheckReportsPersistenceSourceOverlap(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, strings.Join([]string{
		"RAG_HTTP_PORT=8765",
		"HOST_DOCS_DIR=./data/docs",
		"HOST_CODE_DIR=./data/code",
		"HOST_INDEX_DIR=./data/docs/index",
		"HOST_MODELS_DIR=./data/models",
		"",
	}, "\n"))

	if err := os.MkdirAll(filepath.Join(repoRoot, "data", "docs", "index"), 0o755); err != nil {
		t.Fatalf("mkdir nested index: %v", err)
	}

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !hasFinding(report, SeverityError, "HOST_INDEX_DIR_INSIDE_SOURCE") {
		t.Fatalf("missing HOST_INDEX_DIR_INSIDE_SOURCE in %#v", report.Findings)
	}
}

func TestCheckAllowsLANHTTPHostWithWarning(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{"RAG_HTTP_HOST=192.168.178.27"},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if report.HasErrors() {
		t.Fatalf("expected warning-only report, got %#v", report.Findings)
	}
	if !hasFinding(report, SeverityWarning, "RAG_HTTP_HOST_LAN_OPT_IN") {
		t.Fatalf("missing RAG_HTTP_HOST_LAN_OPT_IN in %#v", report.Findings)
	}
}

func TestCheckRejectsPublicHTTPHost(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{"RAG_HTTP_HOST=8.8.8.8"},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !hasFinding(report, SeverityError, "RAG_HTTP_HOST_PUBLIC") {
		t.Fatalf("missing RAG_HTTP_HOST_PUBLIC in %#v", report.Findings)
	}
}

func TestCheckReportsHostlessMCPComposePublish(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, "docker", "docker-compose.yml"), 0o644, `services:
  ollama:
    ports:
      - "127.0.0.1:${OLLAMA_PORT:-11434}:11434"
  rag-mcp:
    ports:
      - "${RAG_HTTP_PORT:-8765}:8765"
`)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !hasFinding(report, SeverityError, "COMPOSE_MCP_HOSTLESS_PUBLISH") {
		t.Fatalf("missing COMPOSE_MCP_HOSTLESS_PUBLISH in %#v", report.Findings)
	}
}

func TestCheckReportsHostlessOllamaComposePublish(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, "docker", "docker-compose.yml"), 0o644, `services:
  ollama:
    ports:
      - "${OLLAMA_PORT:-11434}:11434"
  rag-mcp:
    ports:
      - "${RAG_HTTP_HOST:-127.0.0.1}:${RAG_HTTP_PORT:-8765}:8765"
`)

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if !hasFinding(report, SeverityError, "COMPOSE_OLLAMA_HOSTLESS_PUBLISH") {
		t.Fatalf("missing COMPOSE_OLLAMA_HOSTLESS_PUBLISH in %#v", report.Findings)
	}
}

func newConfigDoctorRepo(t *testing.T) string {
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
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, strings.Join([]string{
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
	writeFile(t, filepath.Join(repoRoot, "docker", "docker-compose.yml"), 0o644, `services:
  ollama:
    ports:
      - "127.0.0.1:${OLLAMA_PORT:-11434}:11434"
  rag-mcp:
    ports:
      - "${RAG_HTTP_HOST:-127.0.0.1}:${RAG_HTTP_PORT:-8765}:8765"
`)
	writeFile(t, filepath.Join(repoRoot, "opencode.json"), 0o600, `{
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

func writeFile(t *testing.T, path string, mode os.FileMode, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasFinding(report Report, severity Severity, code string) bool {
	for _, finding := range report.Findings {
		if finding.Severity == severity && finding.Code == code {
			return true
		}
	}
	return false
}
