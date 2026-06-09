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

func TestHostPathDefaultsStayBoundToKeys(t *testing.T) {
	expected := []struct {
		key   string
		value string
	}{
		{key: "HOST_DOCS_DIR", value: "./data/docs"},
		{key: "HOST_CODE_DIR", value: "./data/code"},
		{key: "HOST_INDEX_DIR", value: "./data/index"},
	}

	if len(hostPathKeys) != len(expected) {
		t.Fatalf("hostPathKeys length = %d, want %d (%v)", len(hostPathKeys), len(expected), hostPathKeys)
	}
	for i, item := range expected {
		if hostPathKeys[i] != item.key {
			t.Fatalf("hostPathKeys[%d] = %q, want %q (%v)", i, hostPathKeys[i], item.key, hostPathKeys)
		}
		got, ok := defaults[item.key]
		if !ok {
			t.Fatalf("defaults missing %s", item.key)
		}
		if got != item.value {
			t.Fatalf("defaults[%s] = %q, want %q", item.key, got, item.value)
		}
	}
}

func TestCheckReportsFatalRuntimeAndPathErrors(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, strings.Join([]string{
		"RAG_HTTP_PORT=not-a-number",
		"RAG_CHUNK_SIZE=10",
		"RAG_CHUNK_OVERLAP=10",
		"RAG_ENABLE_CODE_INGEST=maybe",
		"RAG_MAX_SEARCH_DISTANCE=not-a-number",
		"RAG_EMBED_CONCURRENCY=0",
		"RAG_EMBED_NUM_THREADS=-1",
		"RAG_REINDEX_TIMEOUT=not-a-duration",
		"RAG_LOG_LEVEL=noisy",
		"RAG_LOG_FORMAT=xml",
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
		"RAG_MAX_SEARCH_DISTANCE_NUMBER",
		"RAG_EMBED_CONCURRENCY_POSITIVE",
		"RAG_EMBED_NUM_THREADS_NON_NEGATIVE",
		"RAG_REINDEX_TIMEOUT_DURATION",
		"RAG_LOG_LEVEL_VALUE",
		"RAG_LOG_FORMAT_VALUE",
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

func TestCheckIgnoresUserManagedClientConfig(t *testing.T) {
	repoRoot := newConfigDoctorRepo(t)
	writeFile(t, filepath.Join(repoRoot, "opencode.json"), 0o644, "{user managed")

	report, err := CheckWithOptions(Options{
		RepoRoot: repoRoot,
		Environ:  []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	for _, finding := range report.Findings {
		if strings.HasPrefix(finding.Code, "OPENCODE_") {
			t.Fatalf("client config should be outside doctor scope, got %#v", report.Findings)
		}
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
		"OLLAMA_HOST=http://ollama.example.internal:11434",
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

func TestCheckResolvesRelativeHostPathsAgainstHostRepoRoot(t *testing.T) {
	parent := t.TempDir()
	hostRepoRoot := filepath.Join(parent, "rag-search-mcp")
	repoRoot := filepath.Join(parent, "container", "workspace")
	for _, dir := range []string{
		filepath.Join(parent, "docs"),
		filepath.Join(parent, "code"),
		filepath.Join(hostRepoRoot, "data", "index"),
		filepath.Join(repoRoot, "docker"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeConfigDoctorRepoFiles(t, repoRoot, strings.Join([]string{
		"RAG_HTTP_PORT=8765",
		"HOST_DOCS_DIR=../docs",
		"HOST_CODE_DIR=../code",
		"HOST_INDEX_DIR=./data/index",
		"OLLAMA_HOST=http://ollama.example.internal:11434",
		"EMBED_MODEL=nomic-embed-text",
		"RAG_ENABLE_CODE_INGEST=true",
		"RAG_CHROMA_TENANT=default_tenant",
		"RAG_CHROMA_DATABASE=default_database",
		"RAG_COLLECTION_NAME=rag",
		"RAG_SCOPE_DEFAULT=all",
		"RAG_CHUNK_SIZE=1200",
		"RAG_CHUNK_OVERLAP=200",
		"RAG_MAX_TOP_K=50",
		"RAG_MAX_SEARCH_DISTANCE=0.50",
		"RAG_LOG_LEVEL=info",
		"RAG_LOG_FORMAT=json",
		"",
	}, "\n"))

	report, err := CheckWithOptions(Options{
		RepoRoot:     repoRoot,
		HostRepoRoot: hostRepoRoot,
		Environ:      []string{},
	})
	if err != nil {
		t.Fatalf("CheckWithOptions() failed: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("expected no findings for sibling source dirs, got %#v", report.Findings)
	}
}

func TestCheckKeepsHostPathSafetyWhenUsingHostRepoRoot(t *testing.T) {
	parent := t.TempDir()
	hostRepoRoot := filepath.Join(parent, "rag-search-mcp")
	repoRoot := filepath.Join(parent, "container", "workspace")
	for _, dir := range []string{
		filepath.Join(parent, "docs", "index"),
		filepath.Join(parent, "code"),
		filepath.Join(repoRoot, "docker"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeConfigDoctorRepoFiles(t, repoRoot, strings.Join([]string{
		"RAG_HTTP_PORT=8765",
		"HOST_DOCS_DIR=../docs",
		"HOST_CODE_DIR=../code",
		"HOST_INDEX_DIR=../docs/index",
		"OLLAMA_HOST=http://ollama.example.internal:11434",
		"EMBED_MODEL=nomic-embed-text",
		"RAG_ENABLE_CODE_INGEST=true",
		"RAG_CHROMA_TENANT=default_tenant",
		"RAG_CHROMA_DATABASE=default_database",
		"RAG_COLLECTION_NAME=rag",
		"RAG_SCOPE_DEFAULT=all",
		"RAG_CHUNK_SIZE=1200",
		"RAG_CHUNK_OVERLAP=200",
		"RAG_MAX_TOP_K=50",
		"RAG_MAX_SEARCH_DISTANCE=0.50",
		"RAG_LOG_LEVEL=info",
		"RAG_LOG_FORMAT=json",
		"",
	}, "\n"))

	report, err := CheckWithOptions(Options{
		RepoRoot:     repoRoot,
		HostRepoRoot: hostRepoRoot,
		Environ:      []string{},
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

func newConfigDoctorRepo(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	for _, dir := range []string{
		filepath.Join(repoRoot, "data", "docs"),
		filepath.Join(repoRoot, "data", "code"),
		filepath.Join(repoRoot, "data", "index"),
		filepath.Join(repoRoot, "docker"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	writeConfigDoctorRepoFiles(t, repoRoot, strings.Join([]string{
		"RAG_HTTP_PORT=8765",
		"HOST_DOCS_DIR=./data/docs",
		"HOST_CODE_DIR=./data/code",
		"HOST_INDEX_DIR=./data/index",
		"OLLAMA_HOST=http://ollama.example.internal:11434",
		"EMBED_MODEL=nomic-embed-text",
		"RAG_ENABLE_CODE_INGEST=true",
		"RAG_CHROMA_TENANT=default_tenant",
		"RAG_CHROMA_DATABASE=default_database",
		"RAG_COLLECTION_NAME=rag",
		"RAG_SCOPE_DEFAULT=all",
		"RAG_CHUNK_SIZE=1200",
		"RAG_CHUNK_OVERLAP=200",
		"RAG_MAX_TOP_K=50",
		"RAG_MAX_SEARCH_DISTANCE=0.50",
		"RAG_LOG_LEVEL=info",
		"RAG_LOG_FORMAT=json",
		"",
	}, "\n"))
	return repoRoot
}

func writeConfigDoctorRepoFiles(t *testing.T, repoRoot string, dotenv string) {
	t.Helper()
	writeFile(t, filepath.Join(repoRoot, ".env"), 0o600, dotenv)
	writeFile(t, filepath.Join(repoRoot, "docker", "docker-compose.yml"), 0o644, `services:
  rag-mcp:
    ports:
      - "${RAG_HTTP_HOST:-127.0.0.1}:${RAG_HTTP_PORT:-8765}:8765"
`)
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
